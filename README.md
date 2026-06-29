<h1 align="center">Vigil</h1>

<p align="center">
  <b>Content fingerprinting &amp; change detection for recon.</b><br>
  Hash any HTTP resource into one deterministic JSON line — diff it over time to catch what changed.
</p>

<p align="center">
  <a href="https://github.com/bc0d3/vigil/actions/workflows/ci.yml"><img src="https://github.com/bc0d3/vigil/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/bc0d3/vigil/releases"><img src="https://img.shields.io/github/v/release/bc0d3/vigil?sort=semver" alt="Release"></a>
  <a href="https://goreportcard.com/report/github.com/bc0d3/vigil"><img src="https://goreportcard.com/badge/github.com/bc0d3/vigil" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT"></a>
</p>

<p align="center">
  <a href="#features">Features</a> •
  <a href="#installation">Installation</a> •
  <a href="#usage">Usage</a> •
  <a href="#examples">Examples</a> •
  <a href="#output">Output</a>
</p>

---

**Vigil** downloads an HTTP resource (a `main.js`, a page, `robots.txt`, a JSON API)
and emits its deterministic fingerprint (`sha256`) plus metadata as **a single JSON line**.

When a JavaScript bundle mutates, someone moved or added endpoints. Vigil turns that signal
into a stable hash you can store and diff across runs. One job, done well, Unix-style.

```console
$ vigil scan https://example.com/main.js
{"url":"https://example.com/main.js","status":200,"sha256":"9f86d0...","size":18234,"content_type":"application/javascript","fetched_at":"2026-06-28T23:41:25Z","body_b64":"Y29uc29sZS5sb2c..."}
```

## Features

- **Deterministic fingerprint** — `sha256` of the **raw** response body. No beautifying, no normalization; the bytes are the signal.
- **One JSON line per resource** — JSONL in batch mode. Pipe-friendly and trivial to diff or store.
- **Built-in change detection** — `vigil watch` stores fingerprints (SQLite by default, Postgres optional) and emits **only what changed** between runs. No external state needed.
- **Stateless core** — `vigil scan` never touches disk; it just hashes and prints. Persistence is opt-in via `watch`.
- **Batch from stdin** — feed it thousands of URLs from a file or another tool. Tunable `--concurrency`; gentle (serial) by default.
- **Runs as a daemon or a cron job** — `watch --interval 1h` loops natively (screen/systemd/`docker run -d`) and stops cleanly on signals, or fire it from cron.
- **Static, CGO-free binary** — `scratch` Docker image (~19 MB), trivial cross-compile. SQLite is embedded in pure Go, so `CGO_ENABLED=0` still holds.
- **Tunable** — timeout, max body size (with truncation marker), custom headers, User-Agent, TLS skip.

## Installation

**Precompiled binary** (Linux / macOS / Windows · amd64 / arm64) from [Releases](https://github.com/bc0d3/vigil/releases):

```bash
# example: Linux amd64
curl -sSfL https://github.com/bc0d3/vigil/releases/latest/download/vigil_0.0.1_linux_amd64.tar.gz | tar -xz vigil
sudo mv vigil /usr/local/bin/
```

**With Go:**

```bash
go install github.com/bc0d3/vigil/cmd/vigil@latest
```

**Docker** (`scratch` image published to GHCR):

```bash
docker pull ghcr.io/bc0d3/vigil:latest
docker run --rm ghcr.io/bc0d3/vigil:latest scan https://example.com/main.js
```

**From source:**

```bash
git clone https://github.com/bc0d3/vigil && cd vigil
make build
```

## Usage

```console
$ vigil -h
vigil — content fingerprinting & change detection

Usage:
  vigil scan <url> [flags]     fingerprint a URL -> JSON
  vigil scan - [flags]         read URLs from stdin -> JSONL
  vigil watch <url|-> [flags]  store the fingerprint and emit only what changed
  vigil version
```

`scan` flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-l`, `--list` | — | read URLs from a file (instead of stdin) |
| `-o`, `--output` | stdout | write JSONL to a file |
| `--timeout` | `15s` | total request timeout |
| `--max-size` | `5242880` (5 MiB) | max bytes to read; truncates and sets `truncated` if exceeded |
| `--no-body` | `false` | omit `body_b64` from the output |
| `--insecure` | `false` | skip TLS certificate verification |
| `--ua` | `vigil/<version>` | `User-Agent` header value |
| `-H "K: V"` | — | extra header, repeatable |
| `--concurrency` | `1` | URLs scanned in parallel (raise for big lists) |

Exit codes: `0` ok · `1` if any URL failed at the network level (batch: if any failed) · `2` usage error.

> By default Vigil scans **one URL at a time** (gentle on targets / WAFs). Bump `--concurrency` for throughput on large lists.

## Examples

```bash
# fingerprint a single resource
vigil scan https://target.com/static/main.js

# fingerprint only, no body (lightweight watchers)
vigil scan https://target.com/static/main.js --no-body

# authenticated request with a custom User-Agent
vigil scan https://target.com/api/config.json \
  -H "Authorization: Bearer $TOKEN" --ua "recon-bot/1.0"

# batch: one URL per line from a file, written to a file
vigil scan -l urls.txt -o fingerprints.jsonl

# (equivalent with shell redirection)
vigil scan - < urls.txt > fingerprints.jsonl

# pipe from another tool (e.g. katana) -> JSONL of fingerprints
katana -u https://target.com -silent | vigil scan -

# diff against a previous snapshot to detect changes
vigil scan - < urls.txt | jq -r '[.url, .sha256] | @tsv' | sort > today.tsv
diff yesterday.tsv today.tsv
```

## Change detection (`watch`)

`scan` gives you a fingerprint; `watch` **remembers** it and tells you what changed.
It stores each `url → sha256` (plus metadata and a change log) and on every run emits
**only the URLs that are new or changed** — nothing for unchanged ones.

```bash
# first run: everything is "new"
vigil watch - < urls.txt

# next run (cron, CI, whenever): emits ONLY what changed since last time
vigil watch - < urls.txt
# {"url":"https://t/main.js","status":200,"sha256":"...","change":"changed","previous_sha256":"..."}
```

Storage options:

```bash
vigil watch - < urls.txt                          # SQLite (default: ~/.vigil/vigil.db)
vigil watch - --db ./recon/target.db < urls.txt   # custom SQLite file
vigil watch - --db-dsn "postgres://user:pass@host/db" < urls.txt   # Postgres instead
vigil watch -l urls.txt --snapshot-dir ./snapshots  # also dump a full JSONL snapshot per run
vigil watch -l urls.txt --all                       # emit unchanged URLs too
vigil watch -l urls.txt --concurrency 25            # scan the list with 25 workers
```

**Run it continuously** with `--interval` (native loop, no cron/while needed) — ideal for `screen`/`tmux`, `systemd`, or `docker run -d`:

```bash
# re-scan every hour, forever; stops cleanly on Ctrl-C / SIGTERM
vigil watch - --interval 1h --db recon.db < urls.txt

# detached in a screen session
screen -dmS vigil vigil watch - --interval 30m --db recon.db < urls.txt
```

Or drive it from **cron** (one-shot per tick):

```bash
0 * * * * vigil watch - --db /var/lib/vigil/recon.db < /etc/vigil/urls.txt >> /var/log/vigil.jsonl 2>&1
```

`watch` flags add to all `scan` flags (incl. `-l`, `-o`, `--concurrency`): `--db`, `--db-dsn`, `--snapshot-dir`, `--all`, `--interval`.

> The SQLite driver is pure Go (`modernc.org/sqlite`), so the binary stays static and CGO-free.

## Output

One JSON line per resource (JSONL in batch), snake_case:

```json
{
  "url": "https://x/main.js",
  "status": 200,
  "sha256": "hex...",
  "size": 18234,
  "content_type": "application/javascript",
  "fetched_at": "2026-06-28T23:41:25Z",
  "truncated": false,
  "body_b64": "base64 of the raw body",
  "error": "..."
}
```

- `truncated` — omitted when `false`.
- `body_b64` — omitted with `--no-body` or when the body is empty.
- `error` — present **only** on a network failure (no connection / timeout / DNS). When `error` is set, `status` / `sha256` / `size` are omitted.

**Key rule:** an HTTP status (404, 500) is **not** an error — it goes in `status`. Only a network
failure fills `error` and returns exit code 1. This makes the output safe to consume programmatically:
a hash is always a hash, never a disguised failure.

## Development

```bash
make check    # gofmt + go vet + go test -race
make lint     # golangci-lint
make build    # binary with version/commit/date injected
make help     # list targets
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the design principles.

## Releases

Releases are automated with [GoReleaser](https://goreleaser.com) on every `vX.Y.Z` tag:
multi-platform binaries, checksums, a GitHub Release, and an image pushed to
`ghcr.io/bc0d3/vigil`. Versioning follows [SemVer](https://semver.org); see [CHANGELOG.md](CHANGELOG.md).

## License

[MIT](LICENSE).
