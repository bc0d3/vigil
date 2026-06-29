# Security Policy

## Reporting a vulnerability

If you find a security issue in Vigil, **please do not open a public issue**. Use GitHub's
private [Security Advisories](https://github.com/bc0d3/vigil/security/advisories/new) to
report it. We aim to respond within 7 days.

## Scope and responsible use

Vigil is a recon tool: it makes HTTP requests to whatever URLs you give it. Only use it
against targets you are authorized to test (in-scope bug bounty programs, your own assets,
labs).

Relevant design notes:

- `--insecure` disables TLS verification. It is opt-in and only for environments where you
  know what you are doing.
- Vigil honors `HTTP_PROXY` / `HTTPS_PROXY` from the environment and exits through the
  process/container's network: run it in Docker inside a VPN and the traffic stays there.
- It never executes or interprets the content it downloads — it only hashes and base64-encodes it.
- `vigil scan` writes nothing to disk. `vigil watch` does: it persists the URL, `sha256`
  and metadata (status, size, content type, timestamps) to a local SQLite database or to the
  DB you point it at with `--db-dsn`. **It does not store response bodies.** The default
  SQLite file lives at `~/.vigil/vigil.db`.
