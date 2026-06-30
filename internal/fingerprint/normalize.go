package fingerprint

import (
	"regexp"
	"strings"
)

// El normalizador canonicaliza el cuerpo ANTES de hashearlo, para que el sha256
// ignore ruido que cambia en cada request (tokens CSRF, nonces CSP, comentarios
// con fecha, diferencias de whitespace). Es opt-in vía Options.Normalize: por
// default Vigil hashea los bytes crudos. body_b64 SIEMPRE expone el crudo; lo
// único que cambia con --normalize es QUÉ se hashea.
//
// El objetivo no es un parseo HTML completo (eso traería deps y estado): es
// borrar las fuentes de ruido más comunes en HTML dinámico para no generar
// falsos positivos de cambio. Los patrones son deliberadamente acotados.
var (
	// Comentarios HTML que contienen una fecha tipo 20xx (build stamps, "generated
	// at ..."). Solo se borran los que tienen año, no todos los comentarios.
	reHTMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	reYear        = regexp.MustCompile(`20\d\d`)

	// Meta tag de CSRF: <meta name="csrf-token" content="..."> en cualquier orden
	// de atributos. Se borra el tag entero (el valor cambia por sesión/request).
	reCSRFMeta = regexp.MustCompile(`(?is)<meta\b[^>]*\bcsrf[^>]*>`)

	// Input oculto con el token: name="_token" | "csrf_token" | "authenticity_token".
	reTokenInput = regexp.MustCompile(`(?is)<input\b[^>]*\bname=["'][^"']*(?:_token|csrf|authenticity_token)[^"']*["'][^>]*>`)

	// Atributo nonce de CSP: nonce="..." (en <script>/<style>). Se elimina junto a
	// su espacio previo. RE2 no soporta backreferences, así que se enumeran las
	// dos variantes de comillas.
	reNonce = regexp.MustCompile(`(?i)\s+nonce=(?:"[^"]*"|'[^']*')`)

	// Runs de whitespace -> un solo espacio (colapsa diferencias de indentación /
	// saltos de línea que no son cambio real de contenido).
	reWhitespace = regexp.MustCompile(`\s+`)
)

// normalize devuelve una versión canónica de b para hashear. Es determinista:
// el mismo contenido real siempre produce el mismo resultado, y dos respuestas
// que solo difieren en tokens/nonces/fechas-en-comentarios/whitespace colapsan
// al mismo output.
func normalize(b []byte) []byte {
	s := string(b)

	// 1. Borrar comentarios HTML con fecha (build stamps, timestamps).
	s = reHTMLComment.ReplaceAllStringFunc(s, func(m string) string {
		if reYear.MatchString(m) {
			return ""
		}
		return m
	})

	// 2. Borrar el ruido conocido de CSRF/CSP.
	s = reCSRFMeta.ReplaceAllString(s, "")
	s = reTokenInput.ReplaceAllString(s, "")
	s = reNonce.ReplaceAllString(s, "")

	// 3. Colapsar whitespace y recortar los bordes.
	s = reWhitespace.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)

	return []byte(s)
}
