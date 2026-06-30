# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- `--normalize` (opt-in): hashes the **canonicalized** body instead of the raw bytes, so the
  `sha256` ignores per-request noise (CSRF tokens, CSP nonces, dated HTML comments, whitespace).
  Eliminates false positives when watching dynamic HTML. The default stays raw; `body_b64` and
  `size` always reflect the raw response.
- Multi-arch (`amd64` + `arm64`) container images, pushed to GHCR on every `main` push
  (tagged by commit `sha` + `latest`) and on every `vX.Y.Z` release.

### Changed
- Container image now runs as **non-root** (`USER 65534:65534`); works under `--read-only`,
  `--cap-drop ALL` and `--security-opt no-new-privileges`.

## [0.1.0] - 2026-06-29

### Added
- `vigil watch <url|->`: persists the fingerprint and emits **only** the URLs that are new
  or changed between runs (stateful change detection).
- `database/sql` storage backend: **SQLite** by default (`modernc.org/sqlite`, pure Go, no
  CGO) and optional **Postgres** via `--db-dsn`.
- `watch` flags: `--db`, `--db-dsn`, `--snapshot-dir` (full JSONL snapshot per run), `--all`.
- `watch` output adds `change` (new/changed/unchanged) and `previous_sha256` fields.
- `--concurrency N`: parallel scanning with N workers (in `scan` and `watch`). Defaults to
  1 (gentle on targets); output and DB writes are serialized.
- `--interval D` in `watch`: native loop every D until SIGINT/SIGTERM (daemon mode for
  screen/systemd/`docker run -d`, no cron or `while` needed).
- ProjectDiscovery-style ergonomics: `-l/--list` (read URLs from a file) and `-o/--output`
  (write output to a file), in both `scan` and `watch`.
- Rewritten `-h` help with per-command examples.

### Changed
- ProjectDiscovery-style layout: `cmd/vigil` (entry point) + `internal/fingerprint` (pure
  core) + `internal/store` (stateful layer).
- Requires Go 1.25 (imposed by the storage dependencies).
- `scan` stays pure and stateless; state lives only in `watch`.
- Public-facing docs (help + README) in English.

## [0.0.1] - 2026-06-28

### Added
- `vigil scan <url>`: fingerprint an HTTP resource into one JSON line (`sha256` of the raw
  body + metadata).
- `vigil scan -`: batch mode reading URLs from stdin, JSONL output.
- `vigil version`: version, commit and build date.
- `scan` flags: `--timeout`, `--max-size`, `--no-body`, `--insecure`, `--ua`, `-H`
  (repeatable).
- Stable snake_case output contract. An HTTP status (404/500) is not an error; only a
  network failure fills `error` and returns exit code 1.
- Multi-stage Dockerfile producing a `scratch` image (static binary, with `ca-certificates`).
- Test suite using `httptest` and CI (test + lint + image build).
- Release automation with GoReleaser (multi-platform binaries) and an image published to GHCR.

[Unreleased]: https://github.com/bc0d3/vigil/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/bc0d3/vigil/compare/v0.0.1...v0.1.0
[0.0.1]: https://github.com/bc0d3/vigil/releases/tag/v0.0.1
