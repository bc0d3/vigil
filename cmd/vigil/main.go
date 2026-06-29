// Command vigil — content fingerprinting & change detection.
//
// Baja un recurso HTTP y emite su huella determinista (sha256) + metadata en
// una sola línea JSON. Una responsabilidad, estilo Unix. Solo stdlib.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/bc0d3/vigil/internal/fingerprint"
)

// Info de versión. Se sobrescribe en build con -ldflags
// "-X main.version=... -X main.commit=... -X main.date=..." (lo hace GoReleaser).
// Si no se inyecta (p.ej. `go install`), se intenta leer de la build info de Go.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// versionString arma la línea de `vigil version`.
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

// runScan parsea los flags de `scan`, ejecuta uno o varios scans y escribe la
// salida. Devuelve el exit code: 0 ok, 1 si alguna URL falló por red, 2 uso.
func runScan(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Uso: vigil scan <url|-> [flags]\n\n")
		fmt.Fprintf(stderr, "  '-' como url lee URLs de stdin (una por línea) y emite JSONL.\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	timeout := fs.Duration("timeout", 15*time.Second, "timeout total de la request")
	maxSize := fs.Int64("max-size", fingerprint.DefaultMaxSize, "máximo de bytes a leer del cuerpo")
	noBody := fs.Bool("no-body", false, "omitir body_b64 en la salida")
	insecure := fs.Bool("insecure", false, "no verificar el certificado TLS")
	ua := fs.String("ua", "vigil/"+version, "valor del header User-Agent")
	var headers stringSlice
	fs.Var(&headers, "H", "header extra \"Clave: Valor\" (repetible)")

	// flag.Parse() corta en el primer argumento no-flag, así que intercalamos a
	// mano: permite flags antes y después de la url (vigil scan <url> --flag).
	var positional []string
	rest := args
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		rest = fs.Args()
		if len(rest) > 0 {
			positional = append(positional, rest[0])
			rest = rest[1:]
		}
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "error: se espera exactamente una url (o '-' para stdin)")
		fs.Usage()
		return 2
	}

	opts := fingerprint.Options{
		Timeout:   *timeout,
		MaxSize:   *maxSize,
		NoBody:    *noBody,
		Insecure:  *insecure,
		UserAgent: *ua,
		Headers:   headers,
	}

	target := positional[0]
	if target == "-" {
		return runBatch(stdin, stdout, stderr, opts)
	}

	r := fingerprint.Scan(target, opts)
	if err := fingerprint.Emit(stdout, r); err != nil {
		fmt.Fprintf(stderr, "error escribiendo salida: %v\n", err)
		return 1
	}
	if r.Error != "" {
		return 1
	}
	return 0
}

// runBatch lee URLs de r (una por línea, ignora vacías y líneas '#') y emite
// una línea JSON por cada una. Exit 1 si alguna falló por red.
func runBatch(r io.Reader, stdout, stderr io.Writer, opts fingerprint.Options) int {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // hasta 1 MiB por línea (URLs largas)
	failed := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		res := fingerprint.Scan(line, opts)
		if err := fingerprint.Emit(stdout, res); err != nil {
			fmt.Fprintf(stderr, "error escribiendo salida: %v\n", err)
			return 1
		}
		if res.Error != "" {
			failed = true
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(stderr, "error leyendo stdin: %v\n", err)
		return 1
	}
	if failed {
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintf(w, "vigil — content fingerprinting & change detection\n\n")
	fmt.Fprintf(w, "Uso:\n")
	fmt.Fprintf(w, "  vigil scan <url> [flags]   fingerprint de una URL -> JSON\n")
	fmt.Fprintf(w, "  vigil scan - [flags]       lee URLs de stdin -> JSONL\n")
	fmt.Fprintf(w, "  vigil version\n")
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "scan":
		os.Exit(runScan(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	case "version", "-v", "--version":
		fmt.Println(versionString())
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "subcomando desconocido: %q\n\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}
