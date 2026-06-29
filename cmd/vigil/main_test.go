package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
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

// falta la url -> exit 2 (error de uso).
func TestRunScanUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runScan(nil, nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}
