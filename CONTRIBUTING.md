# Contributing to Vigil

Thanks for your interest! Vigil is deliberately small: one job, done well. Let's keep it
that way.

## Before opening a PR

```bash
make check   # gofmt + go vet + go test -race
make lint    # golangci-lint (optional but recommended)
```

Everything must be green.

## Design principles (non-negotiable)

1. **The core (`scan` / `internal/fingerprint`) is stdlib-only and stateless.** No
   dependencies and no disk I/O there. Persistence lives only in `internal/store` (used by
   `watch`), where a small set of vetted, CGO-free dependencies is allowed.
2. **Output = one JSON line per resource**, deterministic, snake_case.
3. **The raw body is hashed**, never normalized.
4. **An HTTP status is not an error.** Only a network failure fills `error`.

If your change touches the output contract, open an issue first to discuss it: there are
automated consumers that depend on its stability.

## Style

- `gofmt` / `goimports` required.
- [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`,
  `test:`, `chore:`…). The release changelog is built from them.

## Tests

Any behavior change needs a test using `httptest`. See `internal/fingerprint/fingerprint_test.go`
and `cmd/vigil/main_test.go` for reference.
