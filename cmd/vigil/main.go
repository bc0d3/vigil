// Command vigil — content fingerprinting & change detection.
//
// Baja un recurso HTTP y emite su huella determinista (sha256) + metadata en
// una sola línea JSON. Una responsabilidad, estilo Unix.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bc0d3/vigil/internal/fingerprint"
	"github.com/bc0d3/vigil/internal/store"
)

// Info de versión. Se sobrescribe en build con -ldflags
// "-X main.version=... -X main.commit=... -X main.date=..." (lo hace GoReleaser).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func versionString() string {
	v, c, d := version, commit, date
	if v == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok {
			if info.Main.Version != "" && info.Main.Version != "(devel)" {
				v = info.Main.Version
			}
			for _, s := range info.Settings {
				switch s.Key {
				case "vcs.revision":
					if len(s.Value) >= 7 {
						c = s.Value[:7]
					} else if s.Value != "" {
						c = s.Value
					}
				case "vcs.time":
					if s.Value != "" {
						d = s.Value
					}
				}
			}
		}
	}
	return fmt.Sprintf("vigil %s (commit %s, built %s, %s)", v, c, d, runtime.Version())
}

// stringSlice acumula flags repetibles (-H "K: V" -H "K2: V2").
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

const scanHelp = `vigil scan — fingerprint HTTP resources

Usage:
  vigil scan <url> [flags]
  vigil scan -l urls.txt [flags]
  cat urls.txt | vigil scan -

Input:
  <url>             a single URL
  -                 read URLs from stdin (one per line; '#' and blanks ignored)
  -l, -list FILE    read URLs from a file

Output:
  -o, -output FILE  write JSONL to FILE (default: stdout)

Flags:
  -timeout DUR      request timeout (default 15s)
  -max-size N       max body bytes to read (default 5242880)
  -no-body          omit body_b64 from the output
  -insecure         skip TLS certificate verification
  -ua STRING        User-Agent header value
  -H "K: V"         extra header (repeatable)
  -concurrency N    URLs scanned in parallel (default 1)

Examples:
  vigil scan https://target.com/main.js
  vigil scan -l urls.txt -concurrency 25 -o out.jsonl
  katana -u https://target.com -silent | vigil scan -
`

const watchHelp = `vigil watch — track fingerprints and report what changed

Usage:
  vigil watch <url|-> [flags]
  vigil watch -l urls.txt [flags]

Stores each url -> sha256 and emits ONLY new or changed resources between runs.
Input and output options are the same as 'scan' (<url>, -, -l, -o).

Storage:
  -db FILE          SQLite database file (default ~/.vigil/vigil.db)
  -db-dsn DSN       use Postgres instead of SQLite (a postgres:// connection string)
  -snapshot-dir DIR also dump a full JSONL snapshot of each run into DIR

Behavior:
  -all              also emit unchanged URLs
  -interval DUR     loop every DUR until SIGINT/SIGTERM (daemon mode)
  -concurrency N    URLs scanned in parallel (default 1)
  (plus all scan flags: -timeout, -max-size, -no-body, -insecure, -ua, -H)

Examples:
  vigil watch -l urls.txt
  vigil watch -l urls.txt -interval 1h -db recon.db
  vigil watch -l urls.txt -concurrency 25 -o changes.jsonl
`

// parsePositional intercala flags y argumentos: el flag package corta en el
// primer no-flag, así que parseamos por tramos para aceptar flags antes y
// después de la url (vigil scan <url> -flag).
func parsePositional(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	rest := args
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		rest = fs.Args()
		if len(rest) > 0 {
			positional = append(positional, rest[0])
			rest = rest[1:]
		}
	}
	return positional, nil
}

// resolveInput decide la fuente de URLs: -l archivo, o el posicional (url | -).
func resolveInput(listFile string, positional []string, stdin io.Reader, stderr io.Writer) (target string, src io.Reader, closeFn func(), code int) {
	if listFile != "" {
		if len(positional) > 0 {
			fmt.Fprintln(stderr, "error: use either -l <file> or a url argument, not both")
			return "", nil, nil, 2
		}
		f, err := os.Open(listFile) //nolint:gosec // ruta provista por el usuario (CLI)
		if err != nil {
			fmt.Fprintf(stderr, "error: cannot open list file: %v\n", err)
			return "", nil, nil, 2
		}
		return "-", f, func() { _ = f.Close() }, 0
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "error: provide a url, '-' for stdin, or -l <file>")
		return "", nil, nil, 2
	}
	return positional[0], stdin, func() {}, 0
}

// resolveOutput abre el destino de salida: -o archivo, o stdout.
func resolveOutput(outFile string, stdout, stderr io.Writer) (w io.Writer, closeFn func(), code int) {
	if outFile == "" {
		return stdout, func() {}, 0
	}
	f, err := os.Create(outFile) //nolint:gosec // ruta provista por el usuario (CLI)
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot create output file: %v\n", err)
		return nil, nil, 1
	}
	return f, func() { _ = f.Close() }, 0
}

// runScan ejecuta el subcomando scan. Exit: 0 ok, 1 fallo de red, 2 uso.
func runScan(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, scanHelp) }

	timeout := fs.Duration("timeout", 15*time.Second, "")
	maxSize := fs.Int64("max-size", fingerprint.DefaultMaxSize, "")
	noBody := fs.Bool("no-body", false, "")
	insecure := fs.Bool("insecure", false, "")
	ua := fs.String("ua", "vigil/"+version, "")
	concurrency := fs.Int("concurrency", 1, "")
	var headers stringSlice
	fs.Var(&headers, "H", "")
	var listFile, outFile string
	fs.StringVar(&listFile, "l", "", "")
	fs.StringVar(&listFile, "list", "", "")
	fs.StringVar(&outFile, "o", "", "")
	fs.StringVar(&outFile, "output", "", "")

	positional, err := parsePositional(fs, args)
	if err != nil {
		return 2
	}

	target, src, closeIn, code := resolveInput(listFile, positional, stdin, stderr)
	if code != 0 {
		return code
	}
	defer closeIn()
	out, closeOut, code := resolveOutput(outFile, stdout, stderr)
	if code != 0 {
		return code
	}
	defer closeOut()

	opts := fingerprint.Options{
		Timeout:   *timeout,
		MaxSize:   *maxSize,
		NoBody:    *noBody,
		Insecure:  *insecure,
		UserAgent: *ua,
		Headers:   headers,
	}

	in := make(chan string)
	errc := make(chan error, 1)
	go func() { errc <- feedURLs(target, src, in) }()

	failed := false
	scanPool(in, opts, *concurrency, func(res fingerprint.Result) {
		if err := fingerprint.Emit(out, res); err != nil {
			fmt.Fprintf(stderr, "error writing output: %v\n", err)
			failed = true
			return
		}
		if res.Error != "" {
			failed = true
		}
	})

	if err := <-errc; err != nil {
		fmt.Fprintf(stderr, "error reading input: %v\n", err)
		return 1
	}
	if failed {
		return 1
	}
	return 0
}

// feedURLs envía a out las URLs objetivo (la única, o las de la fuente ignorando
// vacías y líneas '#') y cierra out. Devuelve el error de lectura.
func feedURLs(target string, src io.Reader, out chan<- string) error {
	defer close(out)
	if target != "-" {
		out <- target
		return nil
	}
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // hasta 1 MiB por línea
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out <- line
	}
	return sc.Err()
}

// scanPool corre Scan sobre cada url de `in` con `concurrency` workers y llama
// sink(res) desde un único goroutine (serializa salida y DB). Bloquea hasta drenar.
func scanPool(in <-chan string, opts fingerprint.Options, concurrency int, sink func(fingerprint.Result)) {
	if concurrency < 1 {
		concurrency = 1
	}
	results := make(chan fingerprint.Result, concurrency)
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			for u := range in {
				results <- fingerprint.Scan(u, opts)
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()
	for res := range results {
		sink(res)
	}
}

// collectURLs lee todas las URLs a memoria (necesario para re-escanear en cada
// pasada del modo --interval).
func collectURLs(target string, src io.Reader) ([]string, error) {
	if target != "-" {
		return []string{target}, nil
	}
	var urls []string
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	return urls, sc.Err()
}

// watchEvent es la salida de `watch`: la huella + el veredicto de cambio.
type watchEvent struct {
	fingerprint.Result
	Change         string `json:"change"`                    // new | changed | unchanged
	PreviousSHA256 string `json:"previous_sha256,omitempty"` // hash anterior si cambió
}

// runWatch ejecuta el subcomando watch. Exit: 0 ok, 1 fallo, 2 uso.
func runWatch(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, watchHelp) }

	timeout := fs.Duration("timeout", 15*time.Second, "")
	maxSize := fs.Int64("max-size", fingerprint.DefaultMaxSize, "")
	noBody := fs.Bool("no-body", false, "")
	insecure := fs.Bool("insecure", false, "")
	ua := fs.String("ua", "vigil/"+version, "")
	concurrency := fs.Int("concurrency", 1, "")
	var headers stringSlice
	fs.Var(&headers, "H", "")
	var listFile, outFile string
	fs.StringVar(&listFile, "l", "", "")
	fs.StringVar(&listFile, "list", "", "")
	fs.StringVar(&outFile, "o", "", "")
	fs.StringVar(&outFile, "output", "", "")
	dbPath := fs.String("db", defaultDBPath(), "")
	dbDSN := fs.String("db-dsn", "", "")
	snapshotDir := fs.String("snapshot-dir", "", "")
	all := fs.Bool("all", false, "")
	interval := fs.Duration("interval", 0, "")

	positional, err := parsePositional(fs, args)
	if err != nil {
		return 2
	}

	target, src, closeIn, code := resolveInput(listFile, positional, stdin, stderr)
	if code != 0 {
		return code
	}
	defer closeIn()
	out, closeOut, code := resolveOutput(outFile, stdout, stderr)
	if code != 0 {
		return code
	}
	defer closeOut()

	if *dbDSN == "" {
		//nolint:gosec // ruta provista por el usuario vía -db (CLI)
		if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
			fmt.Fprintf(stderr, "error creating db directory: %v\n", err)
			return 1
		}
	}
	st, err := store.Open(store.Config{SQLitePath: *dbPath, PostgresDSN: *dbDSN})
	if err != nil {
		fmt.Fprintf(stderr, "error opening store: %v\n", err)
		return 1
	}
	defer func() { _ = st.Close() }()

	opts := fingerprint.Options{
		Timeout:   *timeout,
		MaxSize:   *maxSize,
		NoBody:    *noBody,
		Insecure:  *insecure,
		UserAgent: *ua,
		Headers:   headers,
	}

	// En modo loop hay que re-escanear la misma lista en cada pasada, así que la
	// leemos completa a memoria una vez.
	urls, err := collectURLs(target, src)
	if err != nil {
		fmt.Fprintf(stderr, "error reading input: %v\n", err)
		return 1
	}

	// onePass escanea toda la lista una vez (con N workers): persiste, emite los
	// cambios y, si hay -snapshot-dir, vuelca un snapshot JSONL. Devuelve si falló.
	onePass := func() bool {
		var snap io.WriteCloser
		if *snapshotDir != "" {
			//nolint:gosec // carpeta provista por el usuario vía -snapshot-dir (CLI)
			if err := os.MkdirAll(*snapshotDir, 0o755); err != nil {
				fmt.Fprintf(stderr, "error creating snapshot dir: %v\n", err)
				return true
			}
			name := "snapshot-" + time.Now().UTC().Format("20060102T150405Z") + ".jsonl"
			f, err := os.Create(filepath.Join(*snapshotDir, name)) //nolint:gosec // ruta del usuario (CLI)
			if err != nil {
				fmt.Fprintf(stderr, "error creating snapshot: %v\n", err)
				return true
			}
			snap = f
		}

		in := make(chan string)
		go func() {
			for _, u := range urls {
				in <- u
			}
			close(in)
		}()

		failed := false
		scanPool(in, opts, *concurrency, func(res fingerprint.Result) {
			if snap != nil {
				_ = fingerprint.Emit(snap, res) // snapshot crudo de toda la corrida
			}
			if res.Error != "" {
				_ = fingerprint.Emit(out, res) // un fallo de red se reporta siempre
				failed = true
				return
			}
			ch, err := st.Observe(store.Observation{
				URL:         res.URL,
				SHA256:      res.SHA256,
				Status:      derefInt(res.Status),
				Size:        derefInt64(res.Size),
				ContentType: res.ContentType,
				At:          time.Now().UTC(),
			})
			if err != nil {
				fmt.Fprintf(stderr, "error storing %s: %v\n", res.URL, err)
				failed = true
				return
			}
			if ch.Status == store.StatusUnchanged && !*all {
				return // solo emitimos señal: lo que cambió
			}
			ev := watchEvent{Result: res, Change: string(ch.Status), PreviousSHA256: ch.PreviousSHA256}
			if err := emitJSON(out, ev); err != nil {
				fmt.Fprintf(stderr, "error writing output: %v\n", err)
				failed = true
			}
		})

		if snap != nil {
			_ = snap.Close()
		}
		return failed
	}

	if *interval <= 0 {
		if onePass() {
			return 1
		}
		return 0
	}

	// Modo loop (daemon): corre hasta SIGINT/SIGTERM. Los fallos por pasada se
	// loguean pero no detienen el monitoreo.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		onePass()
		select {
		case <-ctx.Done():
			fmt.Fprintln(stderr, "vigil: stopped")
			return 0
		case <-time.After(*interval):
		}
	}
}

// defaultDBPath devuelve ~/.vigil/vigil.db, o ./vigil.db si no hay HOME.
func defaultDBPath() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".vigil", "vigil.db")
	}
	return "vigil.db"
}

func emitJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

const usageText = `vigil — content fingerprinting & change detection

Usage:
  vigil scan  <url|-> [flags]   fingerprint a URL (or a stdin/-l list) -> JSON
  vigil watch <url|-> [flags]   store fingerprints and emit only what changed
  vigil version

Run 'vigil scan -h' or 'vigil watch -h' for the full flags of each command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "scan":
		os.Exit(runScan(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	case "watch":
		os.Exit(runWatch(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	case "version", "-v", "--version":
		fmt.Println(versionString())
	case "-h", "--help", "help":
		fmt.Fprint(os.Stdout, usageText)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %q\n\n", os.Args[1])
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
}
