package fingerprint

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func baseOpts() Options {
	return Options{Timeout: 5 * time.Second, MaxSize: DefaultMaxSize}
}

// hashea bien: el sha256 reportado es el del cuerpo crudo, y body_b64 decodifica
// exactamente al contenido servido.
func TestScanHashesRawBody(t *testing.T) {
	body := []byte("console.log('v1');\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write(body)
	}))
	defer srv.Close()

	r := Scan(srv.URL, baseOpts())

	if r.Error != "" {
		t.Fatalf("error inesperado: %s", r.Error)
	}
	if r.Status == nil || *r.Status != 200 {
		t.Fatalf("status esperado 200, got %v", r.Status)
	}
	want := hex.EncodeToString(sha256Sum(body))
	if r.SHA256 != want {
		t.Errorf("sha256 = %s, want %s", r.SHA256, want)
	}
	if r.Size == nil || *r.Size != int64(len(body)) {
		t.Errorf("size = %v, want %d", r.Size, len(body))
	}
	if r.ContentType != "application/javascript" {
		t.Errorf("content_type = %q", r.ContentType)
	}
	got, err := base64.StdEncoding.DecodeString(r.BodyB64)
	if err != nil || !bytes.Equal(got, body) {
		t.Errorf("body_b64 no decodifica al cuerpo original (err=%v)", err)
	}
	if _, err := time.Parse(time.RFC3339, r.FetchedAt); err != nil {
		t.Errorf("fetched_at no es RFC3339: %q", r.FetchedAt)
	}
}

// detecta cambio: v1 y v2 producen hashes distintos.
func TestScanDetectsChange(t *testing.T) {
	serve := func(content string) Result {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(content))
		}))
		defer srv.Close()
		return Scan(srv.URL, baseOpts())
	}
	v1 := serve("endpoints = ['/a']")
	v2 := serve("endpoints = ['/a','/b']")

	if v1.SHA256 == "" || v2.SHA256 == "" {
		t.Fatal("sha256 vacío")
	}
	if v1.SHA256 == v2.SHA256 {
		t.Errorf("v1 y v2 tienen el mismo hash: %s", v1.SHA256)
	}
}

// un 404 NO es error: va en status, error queda vacío.
func TestScan404IsNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	r := Scan(srv.URL, baseOpts())

	if r.Error != "" {
		t.Errorf("404 no debería llenar error, got %q", r.Error)
	}
	if r.Status == nil || *r.Status != 404 {
		t.Errorf("status esperado 404, got %v", r.Status)
	}
	if r.SHA256 == "" {
		t.Error("aún un 404 debería hashear el cuerpo de la respuesta")
	}
}

// fallo de red llena error y deja status/sha256 fuera.
func TestScanNetworkFailureFillsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close() // ahora el puerto no acepta conexiones

	r := Scan(url, baseOpts())

	if r.Error == "" {
		t.Fatal("se esperaba error de red")
	}
	if r.Status != nil {
		t.Errorf("en fallo de red status debe omitirse, got %v", *r.Status)
	}
	if r.SHA256 != "" {
		t.Errorf("en fallo de red sha256 debe omitirse, got %q", r.SHA256)
	}
}

// truncado: marca truncated, corta en max-size y hashea solo lo leído.
func TestScanTruncatesAtMaxSize(t *testing.T) {
	full := bytes.Repeat([]byte("A"), 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(full)
	}))
	defer srv.Close()

	opts := baseOpts()
	opts.MaxSize = 100
	r := Scan(srv.URL, opts)

	if !r.Truncated {
		t.Error("se esperaba truncated = true")
	}
	if r.Size == nil || *r.Size != 100 {
		t.Fatalf("size esperado 100, got %v", r.Size)
	}
	want := hex.EncodeToString(sha256Sum(full[:100]))
	if r.SHA256 != want {
		t.Errorf("sha256 = %s, want hash de los primeros 100 bytes %s", r.SHA256, want)
	}
	got, _ := base64.StdEncoding.DecodeString(r.BodyB64)
	if len(got) != 100 {
		t.Errorf("body_b64 decodifica a %d bytes, want 100", len(got))
	}
}

// justo en el límite (size == max) no se marca truncated.
func TestScanExactlyAtLimitNotTruncated(t *testing.T) {
	full := bytes.Repeat([]byte("B"), 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(full)
	}))
	defer srv.Close()

	opts := baseOpts()
	opts.MaxSize = 100
	r := Scan(srv.URL, opts)

	if r.Truncated {
		t.Error("100 bytes con max-size=100 no debería marcar truncated")
	}
	if r.Size == nil || *r.Size != 100 {
		t.Errorf("size esperado 100, got %v", r.Size)
	}
}

// --no-body omite body_b64 pero mantiene el hash.
func TestScanNoBodyOmitsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	opts := baseOpts()
	opts.NoBody = true
	r := Scan(srv.URL, opts)

	if r.BodyB64 != "" {
		t.Errorf("con --no-body body_b64 debe estar vacío, got %q", r.BodyB64)
	}
	if r.SHA256 == "" {
		t.Error("el hash debe seguir presente con --no-body")
	}
}

// -H setea headers; el handler los ve.
func TestScanSendsCustomHeaders(t *testing.T) {
	var gotAuth, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
	}))
	defer srv.Close()

	opts := baseOpts()
	opts.UserAgent = "vigil/test"
	opts.Headers = []string{"Authorization: Bearer xyz"}
	Scan(srv.URL, opts)

	if gotAuth != "Bearer xyz" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotUA != "vigil/test" {
		t.Errorf("User-Agent = %q", gotUA)
	}
}

// --insecure permite un cert self-signed; sin él, falla con error de red.
func TestScanInsecureTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("secure"))
	}))
	defer srv.Close()

	secure := Scan(srv.URL, baseOpts())
	if secure.Error == "" {
		t.Error("sin --insecure un cert self-signed debería fallar la verificación TLS")
	}

	opts := baseOpts()
	opts.Insecure = true
	insecure := Scan(srv.URL, opts)
	if insecure.Error != "" {
		t.Errorf("con --insecure no debería fallar, got %q", insecure.Error)
	}
	if insecure.Status == nil || *insecure.Status != 200 {
		t.Errorf("status esperado 200, got %v", insecure.Status)
	}
}

// Emit omite status/sha256/size en caso de error y termina en newline.
func TestEmitOmitsEmptyFields(t *testing.T) {
	var buf bytes.Buffer
	if err := Emit(&buf, Result{URL: "http://x", FetchedAt: "2026-06-28T00:00:00Z", Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, field := range []string{"status", "sha256", "size", "truncated", "body_b64"} {
		if strings.Contains(out, "\""+field+"\"") {
			t.Errorf("salida no debería contener %q en caso de error: %s", field, out)
		}
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("cada Result debe terminar en newline (JSONL)")
	}
}

func TestSplitHeader(t *testing.T) {
	cases := []struct {
		in   string
		k, v string
		ok   bool
	}{
		{"Authorization: Bearer x", "Authorization", "Bearer x", true},
		{"X-Foo:bar", "X-Foo", "bar", true},
		{"no-colon", "", "", false},
		{": novalue", "", "", false},
	}
	for _, c := range cases {
		k, v, ok := splitHeader(c.in)
		if k != c.k || v != c.v || ok != c.ok {
			t.Errorf("splitHeader(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, k, v, ok, c.k, c.v, c.ok)
		}
	}
}

func sha256Sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}
