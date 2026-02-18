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

	"github.com/drummonds/godocs-importer/internal/enex"
	"github.com/drummonds/godocs-importer/internal/godocs"
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
	godocsURL := fs.String("godocs-url", "", "godocs server URL (required)")
	fs.Parse(args)

	if *godocsURL == "" {
		log.Fatal("usage: godocs-importer ping --godocs-url <url>")
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

	for i, note := range notes {
		hash := enex.NoteHash(&note)

		// Check note content (resource_index -2)
		rec, _ := trk.GetRecord(hash, -2)
		status := statusLabel(rec)
		fmt.Printf("[%d] %s (content) — %s\n", i+1, note.Title, status)
		countStatus(status, &pending, &imported, &errored)

		// Check each resource
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
	godocsURL := fs.String("godocs-url", "", "godocs server URL (required)")
	destPath := fs.String("dest-path", "evernote", "destination path in godocs")
	maxNotes := fs.Int("n", 0, "max notes to import (0 = all)")
	fs.Parse(args)

	if fs.NArg() < 1 || *godocsURL == "" {
		log.Fatal("usage: godocs-importer import <file.enex> --godocs-url <url> [--db imports.db] [--dest-path <path>]")
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

	client := godocs.NewClient(*godocsURL)
	enexBase := filepath.Base(enexPath)

	notes := export.Notes
	if *maxNotes > 0 && *maxNotes < len(notes) {
		notes = notes[:*maxNotes]
	}

	for i, note := range notes {
		hash := enex.NoteHash(&note)

		// Always upload note body as HTML — capture ULID for tags/dimensions
		contentULID := importNoteContent(client, trk, enexBase, &note, hash, *destPath, i)

		// Upload each resource (attachment)
		for j, res := range note.Resources {
			importResource(client, trk, enexBase, &note, hash, &res, j, *destPath, i)
		}

		// Apply tags and dimensions using the content document ULID
		applyTags(client, trk, &note, hash, contentULID)
		applyDimensions(client, &note, contentULID)
	}

	fmt.Println("\nImport complete.")
}

// importNoteContent uploads the note body as an HTML file.
// Returns the godocs ULID for the uploaded document (empty if failed or already imported).
func importNoteContent(client *godocs.Client, trk *tracker.Tracker, enexFile string, note *enex.Note, hash, destPath string, noteIdx int) string {
	if trk.IsImported(hash, -2) {
		fmt.Printf("[%d] %s (content) — already imported\n", noteIdx+1, note.Title)
		// Retrieve stored ULID
		rec, _ := trk.GetRecord(hash, -2)
		if rec != nil {
			return rec.GodocsULID
		}
		return ""
	}

	safeName := safeFileName(note.Title) + ".html"
	uploadPath := filepath.Join(destPath, safeName)

	content := []byte(note.Content)
	result, err := client.UploadBytes(content, safeName, destPath)
	if err != nil {
		fmt.Printf("[%d] %s (content) — upload error: %v\n", noteIdx+1, note.Title, err)
		trk.RecordImport(enexFile, note.Title, hash, -2, "", tracker.StatusError)
		trk.UpdateStatus(hash, -2, tracker.StatusError, err.Error())
		return ""
	}

	fmt.Printf("[%d] %s (content) — uploaded to %s (ULID: %s)\n", noteIdx+1, note.Title, result.Path, result.ULID)
	trk.RecordImport(enexFile, note.Title, hash, -2, uploadPath, tracker.StatusUploaded)
	if result.ULID != "" {
		trk.SetULID(hash, -2, result.ULID)
	}
	return result.ULID
}

func importResource(client *godocs.Client, trk *tracker.Tracker, enexFile string, note *enex.Note, hash string, res *enex.Resource, resIdx int, destPath string, noteIdx int) {
	if trk.IsImported(hash, resIdx) {
		name := res.Attributes.FileName
		if name == "" {
			name = fmt.Sprintf("resource_%d", resIdx)
		}
		fmt.Printf("[%d] %s / %s — already imported\n", noteIdx+1, note.Title, name)
		return
	}

	// Decode base64 resource data
	data, err := base64.StdEncoding.DecodeString(res.Data.Content)
	if err != nil {
		fmt.Printf("[%d] %s / resource %d — base64 decode error: %v\n", noteIdx+1, note.Title, resIdx, err)
		trk.RecordImport(enexFile, note.Title, hash, resIdx, "", tracker.StatusError)
		trk.UpdateStatus(hash, resIdx, tracker.StatusError, err.Error())
		return
	}

	// Determine filename
	fileName := res.Attributes.FileName
	if fileName == "" {
		ext := extensionForMime(res.Mime)
		fileName = fmt.Sprintf("%s_%d%s", safeFileName(note.Title), resIdx, ext)
	}

	uploadPath := filepath.Join(destPath, fileName)
	result, err := client.UploadBytes(data, fileName, destPath)
	if err != nil {
		fmt.Printf("[%d] %s / %s — upload error: %v\n", noteIdx+1, note.Title, fileName, err)
		trk.RecordImport(enexFile, note.Title, hash, resIdx, "", tracker.StatusError)
		trk.UpdateStatus(hash, resIdx, tracker.StatusError, err.Error())
		return
	}

	fmt.Printf("[%d] %s / %s — uploaded to %s\n", noteIdx+1, note.Title, fileName, result.Path)
	trk.RecordImport(enexFile, note.Title, hash, resIdx, uploadPath, tracker.StatusUploaded)
	if result.ULID != "" {
		trk.SetULID(hash, resIdx, result.ULID)
	}
}

func applyTags(client *godocs.Client, trk *tracker.Tracker, note *enex.Note, hash string, ulid string) {
	if len(note.Tags) == 0 {
		return
	}
	if ulid == "" {
		fmt.Printf("  skipping tags for %q — no ULID\n", note.Title)
		return
	}

	for _, tagName := range note.Tags {
		tagID, err := client.EnsureTag(tagName)
		if err != nil {
			fmt.Printf("  warning: ensure tag %q: %v\n", tagName, err)
			continue
		}
		if err := client.AddTag(ulid, tagID); err != nil {
			fmt.Printf("  warning: add tag %q to %s: %v\n", tagName, ulid, err)
		} else {
			fmt.Printf("  tag %q → %s\n", tagName, ulid)
		}
	}

	// Update tracker status for content and all resources
	trk.UpdateStatus(hash, -2, tracker.StatusTagged, "")
	for j := range note.Resources {
		trk.UpdateStatus(hash, j, tracker.StatusTagged, "")
	}
}

func applyDimensions(client *godocs.Client, note *enex.Note, ulid string) {
	dims := map[string]string{}
	if !note.Created.IsZero() {
		dims["created_date"] = note.Created.Format(time.RFC3339)
	}
	if !note.Updated.IsZero() {
		dims["updated_date"] = note.Updated.Format(time.RFC3339)
	}
	if note.Attributes.Author != "" {
		dims["author"] = note.Attributes.Author
	}
	if note.Attributes.SourceURL != "" {
		dims["source_url"] = note.Attributes.SourceURL
	}
	if note.Attributes.Source != "" {
		dims["source"] = note.Attributes.Source
	}
	if len(dims) == 0 {
		return
	}
	if ulid == "" {
		fmt.Printf("  skipping dimensions for %q — no ULID\n", note.Title)
		return
	}

	for name, value := range dims {
		if err := client.SetDimension(ulid, name, value); err != nil {
			fmt.Printf("  warning: set dimension %q on %s: %v\n", name, ulid, err)
		}
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
