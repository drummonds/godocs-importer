package enex

import (
	"path/filepath"
	"runtime"
	"testing"
)

func testdataPath(name string) string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "testdata", name)
}

func TestParseFile(t *testing.T) {
	export, err := ParseFile(testdataPath("sample.enex"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if got := len(export.Notes); got != 3 {
		t.Fatalf("expected 3 notes, got %d", got)
	}

	// Note 1: text only with tags
	n := export.Notes[0]
	if n.Title != "Test Note One" {
		t.Errorf("note 0 title = %q", n.Title)
	}
	if len(n.Tags) != 2 {
		t.Errorf("note 0 tags = %d, want 2", len(n.Tags))
	}
	if n.Tags[0] != "receipts" || n.Tags[1] != "2024" {
		t.Errorf("note 0 tags = %v", n.Tags)
	}
	if n.Created.Year() != 2024 || n.Created.Month() != 1 {
		t.Errorf("note 0 created = %v", n.Created)
	}
	if len(n.Resources) != 0 {
		t.Errorf("note 0 resources = %d, want 0", len(n.Resources))
	}
	if n.Attributes.Author != "testuser" {
		t.Errorf("note 0 author = %q", n.Attributes.Author)
	}

	// Note 2: with attachments
	n = export.Notes[1]
	if n.Title != "Note With Attachment" {
		t.Errorf("note 1 title = %q", n.Title)
	}
	if len(n.Resources) != 2 {
		t.Errorf("note 1 resources = %d, want 2", len(n.Resources))
	}
	if n.Resources[0].Mime != "application/pdf" {
		t.Errorf("resource 0 mime = %q", n.Resources[0].Mime)
	}
	if n.Resources[0].Attributes.FileName != "invoice.pdf" {
		t.Errorf("resource 0 filename = %q", n.Resources[0].Attributes.FileName)
	}
	if n.Resources[1].Attributes.FileName != "screenshot.png" {
		t.Errorf("resource 1 filename = %q", n.Resources[1].Attributes.FileName)
	}
	if len(n.Tags) != 1 || n.Tags[0] != "invoices" {
		t.Errorf("note 1 tags = %v", n.Tags)
	}
	if n.Attributes.SourceURL != "https://example.com/invoice" {
		t.Errorf("note 1 source-url = %q", n.Attributes.SourceURL)
	}

	// Note 3: plain, no tags or resources
	n = export.Notes[2]
	if n.Title != "Plain Note" {
		t.Errorf("note 2 title = %q", n.Title)
	}
	if len(n.Tags) != 0 {
		t.Errorf("note 2 tags = %d", len(n.Tags))
	}
	if len(n.Resources) != 0 {
		t.Errorf("note 2 resources = %d", len(n.Resources))
	}
}

func TestNoteHash(t *testing.T) {
	n := &Note{Title: "Test", Content: "Hello"}
	h1 := NoteHash(n)
	h2 := NoteHash(n)
	if h1 != h2 {
		t.Error("same note should produce same hash")
	}

	n2 := &Note{Title: "Test", Content: "Different"}
	h3 := NoteHash(n2)
	if h1 == h3 {
		t.Error("different content should produce different hash")
	}

	// Boundary ambiguity: "ab"+"c" vs "a"+"bc"
	na := &Note{Title: "ab", Content: "c"}
	nb := &Note{Title: "a", Content: "bc"}
	if NoteHash(na) == NoteHash(nb) {
		t.Error("boundary ambiguity should produce different hashes")
	}
}
