package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"log"
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
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: godocs-importer <command> [options]

Commands:
  walk   <file.enex>                         List notes in an ENEX file
  check  <file.enex> [--db imports.db]       Check import status
  import <file.enex> --godocs-url <url>      Import into godocs
                     [--db imports.db]
                     [--dest-path <path>]
`)
}

func cmdWalk(args []string) {
	if len(args) < 1 {
		log.Fatal("usage: godocs-importer walk <file.enex>")
	}
	enexPath := args[0]

	export, err := enex.ParseFile(enexPath)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("ENEX file: %s\n", enexPath)
	fmt.Printf("Notes: %d\n\n", len(export.Notes))

	for i, note := range export.Notes {
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

	var pending, imported, errored int

	for i, note := range export.Notes {
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

	for i, note := range export.Notes {
		hash := enex.NoteHash(&note)

		// Always upload note body as HTML
		importNoteContent(client, trk, enexBase, &note, hash, *destPath, i)

		// Upload each resource (attachment)
		for j, res := range note.Resources {
			importResource(client, trk, enexBase, &note, hash, &res, j, *destPath, i)
		}

		// Apply tags and dimensions to uploaded documents
		applyTags(client, trk, &note, hash)
		applyDimensions(client, &note)
	}

	fmt.Println("\nImport complete.")
}

// importNoteContent uploads the note body as an HTML file.
// Uses resource_index -2 in the tracker to distinguish from the legacy no-resource case (-1).
func importNoteContent(client *godocs.Client, trk *tracker.Tracker, enexFile string, note *enex.Note, hash, destPath string, noteIdx int) {
	if trk.IsImported(hash, -2) {
		fmt.Printf("[%d] %s (content) — already imported\n", noteIdx+1, note.Title)
		return
	}

	safeName := safeFileName(note.Title) + ".html"
	uploadPath := filepath.Join(destPath, safeName)

	content := []byte(note.Content)
	uploadedPath, err := client.UploadBytes(content, safeName, destPath)
	if err != nil {
		fmt.Printf("[%d] %s (content) — upload error: %v\n", noteIdx+1, note.Title, err)
		trk.RecordImport(enexFile, note.Title, hash, -2, "", tracker.StatusError)
		trk.UpdateStatus(hash, -2, tracker.StatusError, err.Error())
		return
	}

	fmt.Printf("[%d] %s (content) — uploaded to %s\n", noteIdx+1, note.Title, uploadedPath)
	trk.RecordImport(enexFile, note.Title, hash, -2, uploadPath, tracker.StatusUploaded)
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
	uploadedPath, err := client.UploadBytes(data, fileName, destPath)
	if err != nil {
		fmt.Printf("[%d] %s / %s — upload error: %v\n", noteIdx+1, note.Title, fileName, err)
		trk.RecordImport(enexFile, note.Title, hash, resIdx, "", tracker.StatusError)
		trk.UpdateStatus(hash, resIdx, tracker.StatusError, err.Error())
		return
	}

	fmt.Printf("[%d] %s / %s — uploaded to %s\n", noteIdx+1, note.Title, fileName, uploadedPath)
	trk.RecordImport(enexFile, note.Title, hash, resIdx, uploadPath, tracker.StatusUploaded)
}

func applyTags(client *godocs.Client, trk *tracker.Tracker, note *enex.Note, hash string) {
	if len(note.Tags) == 0 {
		return
	}

	// Wait briefly for ingestion to process uploads
	if err := client.WaitForIngestion(30 * time.Second); err != nil {
		fmt.Printf("  warning: ingestion wait: %v\n", err)
	}

	// Find the uploaded document(s) by searching for the note title
	doc, err := client.SearchDocument(note.Title)
	if err != nil || doc == nil {
		fmt.Printf("  warning: could not find uploaded document for %q to apply tags\n", note.Title)
		return
	}

	for _, tagName := range note.Tags {
		tagID, err := client.EnsureTag(tagName)
		if err != nil {
			fmt.Printf("  warning: ensure tag %q: %v\n", tagName, err)
			continue
		}
		if err := client.AddTag(doc.ULID, tagID); err != nil {
			fmt.Printf("  warning: add tag %q to %s: %v\n", tagName, doc.ULID, err)
		}
	}

	// Update tracker status for content and all resources
	trk.UpdateStatus(hash, -2, tracker.StatusTagged, "")
	for j := range note.Resources {
		trk.UpdateStatus(hash, j, tracker.StatusTagged, "")
	}
}

func applyDimensions(client *godocs.Client, note *enex.Note) {
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

	doc, err := client.SearchDocument(note.Title)
	if err != nil || doc == nil {
		fmt.Printf("  warning: could not find document for %q to apply dimensions\n", note.Title)
		return
	}

	for name, value := range dims {
		if err := client.SetDimension(doc.ULID, name, value); err != nil {
			fmt.Printf("  warning: set dimension %q on %s: %v\n", name, doc.ULID, err)
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
