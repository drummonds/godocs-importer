# godocs-importer

CLI tool to import Evernote `.enex` exports into godocs via the Upload API.

## Architecture

- `cmd/godocs-importer/` — CLI with `walk`, `check`, `import` subcommands
- `internal/enex/` — ENEX XML parser (types + stream parser)
- `internal/tracker/` — pglike DB for idempotent import tracking
- `internal/godocs/` — HTTP client for godocs Upload API, tags, dimensions

## Key Dependencies

- `github.com/drummonds/go-postgres` — pglike (PostgreSQL-compatible SQLite wrapper)
- godocs Upload API at `/api/document/upload` (multipart form)

## Build & Test

```
task check    # fmt + vet + test
task build    # produces bin/godocs-importer
```
