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
