package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runScan en modo batch: emite JSONL y devuelve exit 1 si alguna URL falla.
func TestRunScanBatchStdin(t *testing.T) {
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer okSrv.Close()
	deadSrv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	deadURL := deadSrv.URL
	deadSrv.Close()

	stdin := strings.NewReader(okSrv.URL + "\n# comentario\n\n" + deadURL + "\n")
	var stdout, stderr bytes.Buffer
	code := runScan([]string{"-"}, stdin, &stdout, &stderr)

	if code != 1 {
		t.Errorf("exit code = %d, want 1 (una URL falló)", code)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("se esperaban 2 líneas JSONL (comentario y vacía ignoradas), got %d: %q", len(lines), stdout.String())
	}
}

// una url single que falla por red -> exit 1.
func TestRunScanSingleNetworkFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close()

	var stdout, stderr bytes.Buffer
	code := runScan([]string{url, "--no-body", "--timeout", "2s"}, nil, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "\"error\"") {
		t.Errorf("se esperaba un campo error en la salida: %s", stdout.String())
	}
}

// watch: 1ra vez new, igual -> sin salida (unchanged), distinto -> changed con previo.
func TestRunWatchDetectsChange(t *testing.T) {
	body := "v1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	db := filepath.Join(t.TempDir(), "vigil.db")
	run := func() (string, int) {
		var out, errb bytes.Buffer
		code := runWatch([]string{srv.URL, "--db", db, "--no-body", "--timeout", "5s"}, nil, &out, &errb)
		return out.String(), code
	}

	// 1) primera vez -> new
	out, code := run()
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(out, `"change":"new"`) {
		t.Errorf("primera corrida: esperaba change:new, got %q", out)
	}

	// 2) mismo contenido -> unchanged -> sin salida
	out, _ = run()
	if strings.TrimSpace(out) != "" {
		t.Errorf("sin cambios no debería emitir nada, got %q", out)
	}

	// 3) cambia el contenido -> changed + previous_sha256
	body = "v2-distinto"
	out, _ = run()
	if !strings.Contains(out, `"change":"changed"`) {
		t.Errorf("tras cambiar: esperaba change:changed, got %q", out)
	}
	if !strings.Contains(out, `"previous_sha256"`) {
		t.Errorf("un cambio debería incluir previous_sha256, got %q", out)
	}
}

// scan con --concurrency procesa toda la lista (una línea por URL).
func TestRunScanConcurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path)) // cuerpo distinto por path
	}))
	defer srv.Close()

	var sb strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "%s/p%d\n", srv.URL, i)
	}
	var stdout, stderr bytes.Buffer
	code := runScan([]string{"-", "--concurrency", "8", "--no-body", "--timeout", "5s"},
		strings.NewReader(sb.String()), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 20 {
		t.Errorf("se esperaban 20 líneas, got %d", len(lines))
	}
}

// -l lee URLs de un archivo y -o escribe la salida a un archivo.
func TestRunScanListAndOutputFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path))
	}))
	defer srv.Close()

	dir := t.TempDir()
	listPath := filepath.Join(dir, "urls.txt")
	outPath := filepath.Join(dir, "out.jsonl")
	content := srv.URL + "/a\n" + srv.URL + "/b\n"
	if err := os.WriteFile(listPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runScan([]string{"-l", listPath, "-o", outPath, "--no-body", "--timeout", "5s"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("con -o stdout debería estar vacío, got %q", stdout.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("se esperaban 2 líneas en el archivo de salida, got %d", len(lines))
	}
}

// -l junto a un posicional es un error de uso.
func TestRunScanListConflictsWithPositional(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runScan([]string{"-l", "x.txt", "https://example.com"}, nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

// falta la url -> exit 2 (error de uso).
func TestRunScanUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runScan(nil, nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}
