# Changelog

Todos los cambios notables de este proyecto se documentan acá.

El formato sigue [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/)
y el versionado es [SemVer](https://semver.org/lang/es/).

## [Unreleased]

## [0.0.1] - 2026-06-28

### Added
- `vigil scan <url>`: fingerprint de un recurso HTTP en una línea JSON
  (`sha256` del contenido crudo + metadata).
- `vigil scan -`: modo batch leyendo URLs de stdin, salida JSONL.
- `vigil version`: versión, commit y fecha de build.
- Flags de `scan`: `--timeout`, `--max-size`, `--no-body`, `--insecure`,
  `--ua`, `-H` (repetible).
- Contrato de salida estable en snake_case. Un status HTTP (404/500) no es
  error; solo un fallo de red llena `error` y devuelve exit code 1.
- Dockerfile multi-stage hacia imagen `scratch` (binario estático, con
  `ca-certificates`).
- Suite de tests con `httptest` y CI (test + lint + build de imagen).
- Automatización de releases con GoReleaser (binarios multi-plataforma) e
  imagen publicada en GHCR.

[Unreleased]: https://github.com/bc0d3/vigil/compare/v0.0.1...HEAD
[0.0.1]: https://github.com/bc0d3/vigil/releases/tag/v0.0.1
