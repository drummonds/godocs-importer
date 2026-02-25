# godocs-importer — Research Report

## Overview

CLI tool to import Evernote `.enex` exports into a godocs server via its HTTP API. ~1300 lines of Go across 7 files. No notification system, no task scheduling — it's a straightforward batch import tool.

## Architecture

```
cmd/godocs-importer/main.go   (427 lines) — CLI entry point, 4 subcommands
internal/enex/types.go         (77 lines)  — ENEX XML types
internal/enex/parser.go        (62 lines)  — streaming XML parser + MD5 hash
internal/enex/parser_test.go   (95 lines)  — parser tests
internal/tracker/tracker.go    (149 lines) — pglike-backed import state tracker
internal/tracker/tracker_test.go (141 lines) — tracker tests
internal/godocs/client.go      (351 lines) — HTTP client for godocs API
```

### Dependencies

- `github.com/drummonds/go-postgres` v0.3.0 — pglike (PostgreSQL-compat SQLite wrapper)
- Go 1.25.3

---

## Subcommands

### `walk <file.enex>`
Parses and displays notes: title, created/updated dates, tags, resource count, resource details (filename, MIME, size), content length. `-n` flag limits output.

### `check <file.enex>`
Compares notes against the tracker DB. For each note with resources, shows per-resource import status (pending/uploaded/tagged/error). Notes without resources are labelled "content-only (skipped)". Prints summary counts.

### `import <file.enex> --godocs-url <url>`
The main workhorse. For each note:
1. Skips content-only notes (no resources)
2. For each resource: checks if already imported → decodes base64 → uploads to godocs → records in tracker
3. Applies tags to the first uploaded resource's ULID (via `EnsureTag` + `AddTag`)
4. Applies metadata (created/updated dates, author, source URL, source) to the first ULID

### `ping --godocs-url <url>`
Connectivity check — hits base URL, `/api/tags`, `/api/jobs/active`, `/api/documents/latest`, and `/api/document/upload` (HEAD).

---

## ENEX Parser (`internal/enex/`)

**Streaming approach**: Uses `xml.Decoder.Token()` to find `<note>` start elements, then `DecodeElement` for each note. Avoids loading entire DOM.

**Types**: `Export` → `[]Note` → `[]Resource`. Resources have base64 `Data.Content`, MIME type, optional width/height, and `ResourceAttributes` (filename, source URL, timestamp).

**EnexTime**: Custom unmarshaler for Evernote's `20060102T150405Z` format, embedded in `time.Time`.

**NoteHash**: MD5 of `title + content` — used as dedup key in the tracker.

### Whitespace trimming
Base64 content in ENEX files contains whitespace. Parser trims it with `strings.TrimSpace` after decoding each note.

---

## Import Tracker (`internal/tracker/`)

SQLite database (via pglike) with one table:

```sql
CREATE TABLE IF NOT EXISTS imports (
    id SERIAL PRIMARY KEY,
    enex_file TEXT NOT NULL,
    note_title TEXT NOT NULL,
    note_hash TEXT NOT NULL,
    resource_index INT DEFAULT -1,
    godocs_path TEXT,
    godocs_ulid TEXT,
    status TEXT DEFAULT 'pending',
    error_text TEXT,
    imported_at TIMESTAMP DEFAULT NOW()
)
```

**Status flow**: pending → uploaded → tagged → complete (or error at any point).

**Key methods**:
- `IsImported(hash, idx)` — checks for uploaded/tagged/complete status
- `RecordImport(...)` — INSERT new record
- `UpdateStatus(hash, idx, status, err)` — UPDATE by hash+index
- `SetULID(hash, idx, ulid)` — sets godocs ULID after upload
- `GetRecord` / `AllRecords` — read operations

---

## godocs Client (`internal/godocs/`)

HTTP client for godocs API endpoints:

| Method | Endpoint | Purpose |
|--------|----------|---------|
| `Upload` | POST `/api/document/upload` | Upload file from disk (multipart) |
| `UploadBytes` | POST `/api/document/upload` | Upload in-memory bytes (multipart) |
| `LookupByHash` | GET `/api/document/lookup?hash=` | Find document by MD5 |
| `GetTags` | GET `/api/tags` | List all tags |
| `CreateTag` | POST `/api/tags` | Create new tag (hardcoded blue `#3498db`) |
| `EnsureTag` | — | Find-or-create tag by name |
| `AddTag` | POST `/api/documents/:ulid/tags` | Assign tag to document |
| `UpdateMetadata` | PUT `/api/document/:ulid/metadata` | Set created/updated dates, author, source |
| `SearchDocument` | GET `/api/search?term=` | Search documents |
| `GetActiveJobs` | GET `/api/jobs/active` | List active jobs |
| `WaitForIngestion` | — | Poll until no ingestion jobs remain |
| `GetLatestDocuments` | GET `/api/documents/latest?page=` | List recent documents |

Timeout: 60 seconds for the client, 10 seconds for ping.

---

## Bugs and Issues Found

### Bug 1: Tags only applied to first resource, status updated for all

In `applyTags()` (main.go:333-362), tags are applied to `firstULID` (the first successfully uploaded resource), but then the tracker status is updated to `tagged` for **all** resources in the note:

```go
for j := range note.Resources {
    trk.UpdateStatus(hash, j, tracker.StatusTagged, "")
}
```

If a note has 3 resources, only the first gets tags in godocs, but all 3 are marked as "tagged" in the tracker. Resources 2 and 3 are untagged in godocs but the tracker claims otherwise.

### Bug 2: Metadata only applied to first resource

`applyMetadata()` (main.go:364-394) sets created date, author, etc. on `firstULID` only. Other resources in the same note get no metadata. This may be intentional (tag the "primary" attachment), but it's inconsistent with the tracker marking all resources as tagged.

### Bug 3: No unique constraint on imports table

The `imports` table has no unique constraint on `(note_hash, resource_index)`. If the import fails mid-run and is restarted, `RecordImport` does an INSERT — a new row is created even if one already exists. `IsImported` uses `COUNT(*)` so it still works, but `GetRecord` uses `QueryRow` which returns an arbitrary row when duplicates exist. `UpdateStatus` and `SetULID` update ALL matching rows, which is fine for correctness but wasteful.

Over repeated retries, the table accumulates duplicate rows for the same resource.

### Bug 4: Error records don't prevent re-import (by design?) but create duplicates

When a resource fails, it's recorded with `StatusError`. On re-run, `IsImported` only checks for `uploaded`/`tagged`/`complete`, so errors are retried — good. But `RecordImport` INSERTs a new row without cleaning up the old error row, so you get:
```
row 1: hash=abc, idx=0, status=error
row 2: hash=abc, idx=0, status=uploaded  (retry succeeded)
```

### Bug 5: `uploadPath` constructed but not used correctly

In `importResource()` (main.go:314-325):
```go
uploadPath := filepath.Join(destPath, fileName)
result, err := client.UploadBytes(data, fileName, destPath)
...
trk.RecordImport(enexFile, note.Title, hash, resIdx, uploadPath, tracker.StatusUploaded)
```

The tracker records `uploadPath` (e.g. `evernote/invoice.pdf`), but the actual path comes from `result.Path` (the server's response). These could differ — the server might rename files, add ULID prefixes, etc. The tracker stores the *assumed* path, not the *actual* path.

### Bug 6: `EnsureTag` fetches all tags on every call

For each tag on each note, `EnsureTag` calls `GetTags()` which is a full HTTP GET of all tags. For a 1000-note import with 5 tags each, that's 5000 HTTP requests to `/api/tags`. No caching.

### Bug 7: MD5 hash collision potential

`NoteHash` uses MD5 of `title + content` with no separator. A note titled "ab" with content "c" has the same hash as a note titled "a" with content "bc". Should use a separator: `h.Write([]byte("\x00"))` between fields.

### Bug 8: `base64.StdEncoding` vs actual encoding

Resource data declares encoding via `encoding` attribute (`<data encoding="base64">`), but the code always uses `base64.StdEncoding.DecodeString`. This works for standard base64, but some Evernote exports might use different padding. The `res.Data.Encoding` field is parsed but never checked.

### Bug 9: Unused methods in godocs client

`LookupByHash`, `SearchDocument`, `WaitForIngestion`, and `GetLatestDocuments` are defined but never called from the CLI. They're dead code — `ping` does its own HTTP calls rather than using the client methods.

---

## Test Coverage

- **enex**: Parser tested with sample.enex (3 notes, tags, resources, attributes). Hash stability tested.
- **tracker**: CRUD operations, persistence across reopen, resource index isolation. Good coverage.
- **godocs client**: No tests (would need HTTP mocking or integration server).
- **main.go**: No tests for CLI commands.

---

## Design Observations

1. **Content-only notes are skipped entirely** — Evernote notes without attachments are never imported. The ENML content (which is rich HTML) is discarded. This is a conscious limitation noted in the roadmap.

2. **Idempotent re-runs** — The tracker prevents re-uploading already-imported resources. Error resources are retried. This is the core value of the tracker.

3. **No concurrency** — Everything is sequential. For large imports (thousands of notes), this could be slow.

4. **No rate limiting** — Back-to-back HTTP requests with no delay. Could overwhelm the godocs server.

5. **Tag color hardcoded** — All created tags get `#3498db` (blue). No way to customize.

6. **Release process** — Library-style tag-and-push (no goreleaser). Current version v0.2.1 based on git tags.
