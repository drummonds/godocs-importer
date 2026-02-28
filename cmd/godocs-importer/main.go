package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	godocsclient "github.com/drummonds/godocs-client"
	"github.com/drummonds/godocs-importer/internal/enex"
	"github.com/drummonds/godocs-importer/internal/tracker"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "walk":
		cmdWalk(os.Args[2:])
	case "check":
		cmdCheck(os.Args[2:])
	case "import":
		cmdImport(os.Args[2:])
	case "ping":
		cmdPing(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: godocs-importer <command> [flags] <file.enex>

Note: flags must come before the filename.

Commands:
  walk   [-n count] <file.enex>
  check  [-n count] [--db imports.db] <file.enex>
  import --godocs-url <url> [-n count] [--db imports.db] [--dest-path <path>] <file.enex>
  ping   --godocs-url <url>
`)
}

func cmdPing(args []string) {
	fs := flag.NewFlagSet("ping", flag.ExitOnError)
	godocsURL := fs.String("godocs-url", os.Getenv("GODOCS_URL"), "godocs server URL (or set GODOCS_URL env)")
	fs.Parse(args)

	if *godocsURL == "" {
		log.Fatal("usage: godocs-importer ping --godocs-url <url>\n       (or set GODOCS_URL environment variable)")
	}

	fmt.Printf("Pinging %s ...\n", *godocsURL)

	// Test basic connectivity
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(*godocsURL)
	if err != nil {
		fmt.Printf("  connection: FAIL — %v\n", err)
		os.Exit(1)
	}
	resp.Body.Close()
	fmt.Printf("  connection: OK (status %d)\n", resp.StatusCode)

	// Test API endpoints
	for _, endpoint := range []string{"/api/tags", "/api/jobs/active", "/api/documents/latest?page=0"} {
		resp, err := client.Get(*godocsURL + endpoint)
		if err != nil {
			fmt.Printf("  %s: FAIL — %v\n", endpoint, err)
			continue
		}
		resp.Body.Close()
		fmt.Printf("  %s: %d\n", endpoint, resp.StatusCode)
	}

	// Test upload endpoint (OPTIONS/HEAD to check it exists without uploading)
	resp, err = client.Head(*godocsURL + "/api/document/upload")
	if err != nil {
		fmt.Printf("  /api/document/upload: FAIL — %v\n", err)
	} else {
		resp.Body.Close()
		fmt.Printf("  /api/document/upload: %d\n", resp.StatusCode)
	}
}

func cmdWalk(args []string) {
	fs := flag.NewFlagSet("walk", flag.ExitOnError)
	maxNotes := fs.Int("n", 0, "max notes to show (0 = all)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		log.Fatal("usage: godocs-importer walk <file.enex> [-n count]")
	}
	enexPath := fs.Arg(0)

	export, err := enex.ParseFile(enexPath)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	notes := export.Notes
	if *maxNotes > 0 && *maxNotes < len(notes) {
		notes = notes[:*maxNotes]
	}

	fmt.Printf("ENEX file: %s\n", enexPath)
	fmt.Printf("Notes: %d (showing %d)\n\n", len(export.Notes), len(notes))

	for i, note := range notes {
		fmt.Printf("[%d] %s\n", i+1, note.Title)
		fmt.Printf("    Created:    %s\n", note.Created.Format(time.RFC3339))
		if !note.Updated.IsZero() {
			fmt.Printf("    Updated:    %s\n", note.Updated.Format(time.RFC3339))
		}
		if len(note.Tags) > 0 {
			fmt.Printf("    Tags:       %s\n", strings.Join(note.Tags, ", "))
		}
		fmt.Printf("    Resources:  %d\n", len(note.Resources))
		for j, res := range note.Resources {
			name := res.Attributes.FileName
			if name == "" {
				name = fmt.Sprintf("resource_%d", j)
			}
			fmt.Printf("      [%d] %s (%s, %d bytes)\n", j, name, res.Mime, len(res.Data.Content))
		}
		fmt.Printf("    Content:    %d chars\n", len(note.Content))
		fmt.Println()
	}
}

func cmdCheck(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	dbPath := fs.String("db", "imports.db", "tracker database path")
	maxNotes := fs.Int("n", 0, "max notes to check (0 = all)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		log.Fatal("usage: godocs-importer check <file.enex> [--db imports.db]")
	}
	enexPath := fs.Arg(0)

	export, err := enex.ParseFile(enexPath)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	trk, err := tracker.New(*dbPath)
	if err != nil {
		log.Fatalf("tracker error: %v", err)
	}
	defer trk.Close()

	notes := export.Notes
	if *maxNotes > 0 && *maxNotes < len(notes) {
		notes = notes[:*maxNotes]
	}

	var pending, imported, errored int

	var contentOnly int

	for i, note := range notes {
		hash := enex.NoteHash(&note)

		if len(note.Resources) == 0 {
			fmt.Printf("[%d] %s — content-only (skipped)\n", i+1, note.Title)
			contentOnly++
			continue
		}

		for j, res := range note.Resources {
			name := res.Attributes.FileName
			if name == "" {
				name = fmt.Sprintf("resource_%d", j)
			}
			rec, _ := trk.GetRecord(hash, j)
			status := statusLabel(rec)
			fmt.Printf("[%d] %s / %s — %s\n", i+1, note.Title, name, status)
			countStatus(status, &pending, &imported, &errored)
		}
	}

	if contentOnly > 0 {
		fmt.Printf("\n%d content-only notes (no attachments) — not imported\n", contentOnly)
	}

	fmt.Printf("\nSummary: %d pending, %d imported, %d errors\n", pending, imported, errored)
}

func statusLabel(rec *tracker.Record) string {
	if rec == nil {
		return "pending"
	}
	if rec.Status == tracker.StatusError {
		return fmt.Sprintf("error: %s", rec.ErrorText)
	}
	return rec.Status
}

func countStatus(status string, pending, imported, errored *int) {
	switch {
	case status == "pending":
		*pending++
	case strings.HasPrefix(status, "error"):
		*errored++
	default:
		*imported++
	}
}

func cmdImport(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	dbPath := fs.String("db", "imports.db", "tracker database path")
	godocsURL := fs.String("godocs-url", os.Getenv("GODOCS_URL"), "godocs server URL (or set GODOCS_URL env)")
	destPath := fs.String("dest-path", "evernote", "destination path in godocs")
	maxNotes := fs.Int("n", 0, "max notes to import (0 = all)")
	fs.Parse(args)

	if fs.NArg() < 1 || *godocsURL == "" {
		log.Fatal("usage: godocs-importer import <file.enex> --godocs-url <url> [--db imports.db] [--dest-path <path>]\n       (or set GODOCS_URL environment variable)")
	}
	enexPath := fs.Arg(0)

	export, err := enex.ParseFile(enexPath)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	trk, err := tracker.New(*dbPath)
	if err != nil {
		log.Fatalf("tracker error: %v", err)
	}
	defer trk.Close()

	client := godocsclient.NewClient(*godocsURL)
	enexBase := filepath.Base(enexPath)

	notes := export.Notes
	if *maxNotes > 0 && *maxNotes < len(notes) {
		notes = notes[:*maxNotes]
	}

	var imported, skippedHTML, skippedAlready, errored, warnings int

	for i, note := range notes {
		hash := enex.NoteHash(&note)

		if len(note.Resources) == 0 {
			// Note with no attachments — content-only, skip (HTML not supported)
			fmt.Printf("[%d] %s — skipped (content-only note)\n", i+1, note.Title)
			skippedHTML++
			continue
		}

		// Upload each resource, capture first ULID for tags/dimensions
		var firstULID string
		firstResIdx := -1
		for j, res := range note.Resources {
			ulid := importResource(client, trk, enexBase, &note, hash, &res, j, *destPath, i, &imported, &skippedAlready, &errored)
			if firstULID == "" && ulid != "" {
				firstULID = ulid
				firstResIdx = j
			}
		}

		applyTags(client, trk, &note, hash, firstULID, firstResIdx, &warnings)
		applyMetadata(client, &note, firstULID, &warnings)
	}

	fmt.Printf("\nImport complete: %d uploaded, %d already imported, %d content-only skipped, %d errors, %d warnings\n",
		imported, skippedAlready, skippedHTML, errored, warnings)
}

func importResource(client *godocsclient.Client, trk *tracker.Tracker, enexFile string, note *enex.Note, hash string, res *enex.Resource, resIdx int, destPath string, noteIdx int, imported, skippedAlready, errored *int) string {
	name := res.Attributes.FileName
	if name == "" {
		name = fmt.Sprintf("resource_%d", resIdx)
	}

	if trk.IsImported(hash, resIdx) {
		fmt.Printf("[%d] %s / %s — already imported\n", noteIdx+1, note.Title, name)
		*skippedAlready++
		rec, _ := trk.GetRecord(hash, resIdx)
		if rec != nil {
			return rec.GodocsULID
		}
		return ""
	}

	// Validate encoding attribute
	if res.Data.Encoding != "" && res.Data.Encoding != "base64" {
		fmt.Printf("[%d] %s / resource %d — unsupported encoding: %s\n", noteIdx+1, note.Title, resIdx, res.Data.Encoding)
		trk.RecordImport(enexFile, note.Title, hash, resIdx, "", tracker.StatusError)
		trk.UpdateStatus(hash, resIdx, tracker.StatusError, fmt.Sprintf("unsupported encoding: %s", res.Data.Encoding))
		*errored++
		return ""
	}

	// Decode base64 resource data
	data, err := base64.StdEncoding.DecodeString(res.Data.Content)
	if err != nil {
		fmt.Printf("[%d] %s / resource %d — base64 decode error: %v\n", noteIdx+1, note.Title, resIdx, err)
		trk.RecordImport(enexFile, note.Title, hash, resIdx, "", tracker.StatusError)
		trk.UpdateStatus(hash, resIdx, tracker.StatusError, err.Error())
		*errored++
		return ""
	}

	// Determine filename
	fileName := res.Attributes.FileName
	if fileName == "" {
		ext := extensionForMime(res.Mime)
		fileName = fmt.Sprintf("%s_%d%s", safeFileName(note.Title), resIdx, ext)
	}

	result, err := client.UploadBytes(data, fileName, destPath)
	if err != nil {
		fmt.Printf("[%d] %s / %s — upload error: %v\n", noteIdx+1, note.Title, fileName, err)
		trk.RecordImport(enexFile, note.Title, hash, resIdx, "", tracker.StatusError)
		trk.UpdateStatus(hash, resIdx, tracker.StatusError, err.Error())
		*errored++
		return ""
	}

	if result.Duplicate {
		fmt.Printf("[%d] %s / %s — already exists on server (ulid %s)\n", noteIdx+1, note.Title, fileName, result.ULID)
		trk.RecordImport(enexFile, note.Title, hash, resIdx, result.Name, tracker.StatusUploaded)
		*skippedAlready++
	} else {
		fmt.Printf("[%d] %s / %s — uploaded (ulid %s)\n", noteIdx+1, note.Title, fileName, result.ULID)
		trk.RecordImport(enexFile, note.Title, hash, resIdx, result.Name, tracker.StatusUploaded)
		*imported++
	}
	if result.ULID != "" {
		trk.SetULID(hash, resIdx, result.ULID)
	}
	return result.ULID
}

func applyTags(client *godocsclient.Client, trk *tracker.Tracker, note *enex.Note, hash string, ulid string, resIdx int, warnings *int) {
	if len(note.Tags) == 0 {
		return
	}
	if ulid == "" {
		fmt.Printf("  warning: skipping tags for %q — no ULID\n", note.Title)
		*warnings++
		return
	}

	for _, tagName := range note.Tags {
		tagID, err := client.EnsureTag(tagName)
		if err != nil {
			fmt.Printf("  warning: ensure tag %q: %v\n", tagName, err)
			*warnings++
			continue
		}
		if err := client.AddTag(ulid, tagID); err != nil {
			fmt.Printf("  warning: add tag %q to %s: %v\n", tagName, ulid, err)
			*warnings++
		} else {
			fmt.Printf("  tag %q → %s\n", tagName, ulid)
		}
	}

	trk.UpdateStatus(hash, resIdx, tracker.StatusTagged, "")
}

func applyMetadata(client *godocsclient.Client, note *enex.Note, ulid string, warnings *int) {
	if ulid == "" {
		fmt.Printf("  warning: skipping metadata for %q — no ULID\n", note.Title)
		*warnings++
		return
	}

	var meta godocsclient.MetadataUpdate
	if !note.Created.IsZero() {
		t := note.Created.Time
		meta.CreatedDate = &t
	}
	if !note.Updated.IsZero() {
		t := note.Updated.Time
		meta.UpdatedDate = &t
	}
	if note.Attributes.Author != "" {
		meta.Author = &note.Attributes.Author
	}
	if note.Attributes.SourceURL != "" {
		meta.SourceURL = &note.Attributes.SourceURL
	}
	if note.Attributes.Source != "" {
		meta.Source = &note.Attributes.Source
	}

	if err := client.UpdateMetadata(ulid, meta); err != nil {
		fmt.Printf("  warning: update metadata for %q: %v\n", note.Title, err)
		*warnings++
	}
}

func safeFileName(name string) string {
	// Replace characters unsafe for filenames
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_",
		"|", "_",
	)
	s := replacer.Replace(name)
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

func extensionForMime(mime string) string {
	switch mime {
	case "application/pdf":
		return ".pdf"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "text/plain":
		return ".txt"
	case "text/html":
		return ".html"
	default:
		return ".bin"
	}
}
