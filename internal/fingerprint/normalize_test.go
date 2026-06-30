package fingerprint

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// helper: hashea body con --normalize y devuelve el sha256.
func normHash(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	opts := baseOpts()
	opts.Normalize = true
	r := Scan(srv.URL, opts)
	if r.Error != "" {
		t.Fatalf("error inesperado: %s", r.Error)
	}
	return r.SHA256
}

// determinismo: el mismo contenido normalizado produce el mismo sha256.
func TestNormalizeDeterministic(t *testing.T) {
	page := `<html><head><title>App</title></head><body>hello</body></html>`
	if a, b := normHash(t, page), normHash(t, page); a != b {
		t.Errorf("mismo contenido dio hashes distintos:\n a=%s\n b=%s", a, b)
	}
}

// el ruido por-request (CSRF token, CSP nonce, comentario con fecha) NO cambia el
// hash normalizado: dos respuestas que solo difieren en eso colapsan al mismo sha256.
func TestNormalizeIgnoresPerRequestNoise(t *testing.T) {
	tmpl := `<html><head>
  <meta name="csrf-token" content="%s">
  <!-- generated 2026-06-29T%s by build -->
</head>
<body>
  <form>
    <input type="hidden" name="_token" value="%s">
    <button>go</button>
  </form>
  <script nonce="%s">init();</script>
  <p>real content that never changes</p>
</body></html>`

	v1 := normHash(t, fmt.Sprintf(tmpl, "TOKEN_AAA", "10:00:00", "FORM_AAA", "NONCE_AAA"))
	v2 := normHash(t, fmt.Sprintf(tmpl, "TOKEN_BBB", "23:59:59", "FORM_BBB", "NONCE_BBB"))

	if v1 != v2 {
		t.Errorf("el hash normalizado cambió pese a que solo varió el ruido por-request:\n v1=%s\n v2=%s", v1, v2)
	}
}

// el mismo cuerpo con distinto whitespace (indentación / saltos) colapsa al mismo hash.
func TestNormalizeCollapsesWhitespace(t *testing.T) {
	a := normHash(t, "<div>   a    b\n\n\tc </div>")
	b := normHash(t, "<div> a b c </div>")
	if a != b {
		t.Errorf("diferencias de whitespace no deberían cambiar el hash:\n a=%s\n b=%s", a, b)
	}
}

// un cambio REAL de contenido (un endpoint nuevo) SÍ cambia el hash normalizado:
// el normalizador no debe ocultar señales reales.
func TestNormalizeStillDetectsRealChange(t *testing.T) {
	base := `<script nonce="X">var api=["/a"]</script>`
	changed := `<script nonce="X">var api=["/a","/b"]</script>`
	if normHash(t, base) == normHash(t, changed) {
		t.Error("un cambio real de contenido debería cambiar el hash normalizado")
	}
}

// --normalize y el modo crudo (default) producen hashes distintos cuando hay
// ruido: confirma que el crudo SÍ se ve afectado por el token (y por eso da
// falsos positivos) y que normalize es lo que lo arregla.
func TestRawHashChangesWithTokenButNormalizeDoesNot(t *testing.T) {
	page := func(tok string) string {
		return `<html><body><input name="_token" value="` + tok + `"><p>x</p></body></html>`
	}
	srv1 := serveOnce(t, page("AAA"))
	srv2 := serveOnce(t, page("BBB"))

	// Crudo: el token cambia el hash (falso positivo).
	raw1 := Scan(srv1, baseOpts())
	raw2 := Scan(srv2, baseOpts())
	if raw1.SHA256 == raw2.SHA256 {
		t.Fatal("setup inválido: el hash crudo debería diferir al cambiar el token")
	}

	// Normalizado: mismo hash pese al token distinto.
	optsN := baseOpts()
	optsN.Normalize = true
	n1 := Scan(srv1, optsN)
	n2 := Scan(srv2, optsN)
	if n1.SHA256 != n2.SHA256 {
		t.Errorf("--normalize debería neutralizar el token:\n n1=%s\n n2=%s", n1.SHA256, n2.SHA256)
	}
}

// con --normalize, body_b64 SIGUE siendo el cuerpo CRUDO (no el canonicalizado):
// solo cambia qué se hashea.
func TestNormalizeKeepsRawBody(t *testing.T) {
	raw := "<div>   raw    body </div>"
	srv := serveOnce(t, raw)
	opts := baseOpts()
	opts.Normalize = true
	r := Scan(srv, opts)
	got := decodeB64(t, r.BodyB64)
	if got != raw {
		t.Errorf("body_b64 debería ser el crudo %q, got %q", raw, got)
	}
	if r.Size == nil || *r.Size != int64(len(raw)) {
		t.Errorf("size debería reflejar el crudo (%d), got %v", len(raw), r.Size)
	}
}

// normalize() es estable a nivel de bytes para los casos centrales.
func TestNormalizeUnit(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantSame string // si !="", debe normalizar igual que este otro input
	}{
		{"nonce", `<script nonce="abc">x</script>`, `<script nonce="zzz">x</script>`},
		{"csrf meta", `<meta name="csrf-token" content="a"><p>x</p>`, `<meta name="csrf-token" content="b"><p>x</p>`},
		{"token input", `<input name="_token" value="a"><p>x</p>`, `<input name="_token" value="b"><p>x</p>`},
		{"dated comment", `<!-- built 2026-01-01 --><p>x</p>`, `<!-- built 2026-12-31 --><p>x</p>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if string(normalize([]byte(c.in))) != string(normalize([]byte(c.wantSame))) {
				t.Errorf("normalize(%q) != normalize(%q)", c.in, c.wantSame)
			}
		})
	}
}

// un comentario SIN fecha se preserva (solo se borran los que tienen año 20xx).
func TestNormalizeKeepsUndatedComments(t *testing.T) {
	withComment := string(normalize([]byte(`<!-- nav --><p>x</p>`)))
	without := string(normalize([]byte(`<p>x</p>`)))
	if withComment == without {
		t.Error("un comentario sin fecha no debería borrarse")
	}
}

// serveOnce arranca un server que sirve body y vive hasta el fin del test.
// Devuelve su URL.
func serveOnce(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func decodeB64(t *testing.T, s string) string {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("body_b64 no decodifica: %v", err)
	}
	return string(b)
}
