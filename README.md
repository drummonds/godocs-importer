# godocs-importer

Import Evernote `.enex` exports into [godocs](https://github.com/drummonds/godocs).

## Usage

```bash
# List notes in an ENEX file
godocs-importer walk export.enex

# Check import status
godocs-importer check export.enex

# Import into godocs
godocs-importer import export.enex --godocs-url http://localhost:8080
```

## Install

```bash
go install github.com/drummonds/godocs-importer/cmd/godocs-importer@latest
```

## Links

| | |
|---|---|
| Documentation | https://h3-godocs-importer.statichost.page/ |
| Source (Codeberg) | https://codeberg.org/hum3/godocs-importer |
| Mirror (GitHub) | https://github.com/drummonds/godocs-importer |
| Docs repo | https://codeberg.org/hum3/godocs-importer-docs |
