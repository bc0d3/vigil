// Package fingerprint baja un recurso HTTP y produce su huella determinista
// (sha256 del cuerpo crudo) + metadata. Es el núcleo de Vigil: no conoce flags,
// stdin/stdout ni estado global.
package fingerprint

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultMaxSize es el tope de lectura por defecto: 5 MiB.
const DefaultMaxSize int64 = 5 << 20

// Result es el contrato de salida (snake_case, una línea JSON por recurso).
//
// Status y Size son punteros a propósito: solo se setean cuando hubo respuesta,
// de modo que en un fallo de red se omiten junto con sha256/content_type/body.
type Result struct {
	URL         string `json:"url"`
	Status      *int   `json:"status,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Size        *int64 `json:"size,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	FetchedAt   string `json:"fetched_at"`
	Truncated   bool   `json:"truncated,omitempty"`
	BodyB64     string `json:"body_b64,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Options controla el comportamiento de un Scan. Sin estado global.
type Options struct {
	Timeout   time.Duration
	MaxSize   int64
	NoBody    bool
	Insecure  bool
	Normalize bool // hashea el cuerpo canonicalizado en vez del crudo (ver normalize.go)
	UserAgent string
	Headers   []string // cada una en formato "Clave: Valor"
}

// newClient arma un http.Client a partir de las opciones. La verificación TLS
// se desactiva solo con Insecure.
func newClient(opts Options) *http.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: opts.Insecure}, //nolint:gosec // opt-in vía --insecure
		Proxy:           http.ProxyFromEnvironment,
	}
	return &http.Client{Timeout: opts.Timeout, Transport: tr}
}

// Scan baja una URL y devuelve su huella. No toca os.Args, stdin/stdout ni
// estado global. Un status HTTP (404/500) NO es error; solo un fallo de red
// llena Result.Error.
func Scan(url string, opts Options) Result {
	res := Result{URL: url, FetchedAt: time.Now().UTC().Format(time.RFC3339)}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}
	for _, h := range opts.Headers {
		if k, v, ok := splitHeader(h); ok {
			req.Header.Set(k, v)
		}
	}

	resp, err := newClient(opts).Do(req)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer func() { _ = resp.Body.Close() }()

	status := resp.StatusCode
	res.Status = &status
	res.ContentType = resp.Header.Get("Content-Type")

	// Leemos hasta max+1 bytes para poder distinguir "justo en el límite" de
	// "se pasó". Si se pasó, cortamos en max y marcamos truncated.
	maxSize := opts.MaxSize
	if maxSize < 0 {
		maxSize = 0
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		res.Error = err.Error()
		// Hubo respuesta pero falló la lectura del cuerpo: descartamos los
		// campos derivados para no emitir una huella parcial/no determinista.
		res.Status = nil
		res.ContentType = ""
		return res
	}
	if int64(len(data)) > maxSize {
		data = data[:maxSize]
		res.Truncated = true
	}

	// El hash se computa sobre el cuerpo crudo, salvo --normalize, que lo
	// canonicaliza primero para ignorar ruido por-request. body_b64 y size
	// siguen reflejando SIEMPRE los bytes crudos recibidos.
	toHash := data
	if opts.Normalize {
		toHash = normalize(data)
	}
	sum := sha256.Sum256(toHash)
	res.SHA256 = hex.EncodeToString(sum[:])
	size := int64(len(data))
	res.Size = &size
	if !opts.NoBody && len(data) > 0 {
		res.BodyB64 = base64.StdEncoding.EncodeToString(data)
	}
	return res
}

// Emit serializa un Result a una sola línea JSON en w.
func Emit(w io.Writer, r Result) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

// splitHeader parsea "Clave: Valor". Devuelve ok=false si no hay ':' o la clave
// queda vacía.
func splitHeader(h string) (key, value string, ok bool) {
	i := strings.IndexByte(h, ':')
	if i < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(h[:i])
	value = strings.TrimSpace(h[i+1:])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}
