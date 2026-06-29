# Política de seguridad

## Reportar una vulnerabilidad

Si encontrás un problema de seguridad en Vigil, **no abras un issue público**.
Usá los [Security Advisories](https://github.com/bc0d3/vigil/security/advisories/new)
privados de GitHub para reportarlo.

Hacemos lo posible por responder dentro de los 7 días.

## Alcance y uso responsable

Vigil es una herramienta de recon: hace peticiones HTTP a las URLs que se le
indiquen. Usala solo contra objetivos para los que tengas autorización
(programas de bug bounty en alcance, tus propios activos, laboratorios).

Notas de diseño relevantes:

- `--insecure` desactiva la verificación de TLS. Es opt-in y solo para entornos
  donde sabés lo que hacés.
- Vigil respeta `HTTP_PROXY`/`HTTPS_PROXY` del entorno y sale por la red del
  proceso/contenedor: corriéndolo en Docker dentro de una VPN, el tráfico queda
  encapsulado ahí.
- No ejecuta ni interpreta el contenido que descarga: solo lo hashea y lo
  codifica en base64.
