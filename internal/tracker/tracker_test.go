package tracker

import (
	"os"
	"path/filepath"
	"testing"
)

func tempDB(t *testing.T) *Tracker {
	t.Helper()
	dir := t.TempDir()
	trk, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { trk.Close() })
	return trk
}

func TestIsImported(t *testing.T) {
	trk := tempDB(t)

	if trk.IsImported("hash1", -1) {
		t.Error("should not be imported yet")
	}

	err := trk.RecordImport("test.enex", "Note 1", "hash1", -1, "/path/to/doc", StatusUploaded)
	if err != nil {
		t.Fatalf("RecordImport: %v", err)
	}

	if !trk.IsImported("hash1", -1) {
		t.Error("should be imported after recording")
	}

	// Different resource index should not match
	if trk.IsImported("hash1", 0) {
		t.Error("different resource index should not match")
	}
}

func TestGetRecord(t *testing.T) {
	trk := tempDB(t)

	rec, err := trk.GetRecord("nonexistent", 0)
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if rec != nil {
		t.Error("expected nil for missing record")
	}

	trk.RecordImport("test.enex", "Note 1", "hash1", 0, "/path", StatusUploaded)

	rec, err = trk.GetRecord("hash1", 0)
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil record")
	}
	if rec.NoteTitle != "Note 1" {
		t.Errorf("title = %q", rec.NoteTitle)
	}
	if rec.Status != StatusUploaded {
		t.Errorf("status = %q", rec.Status)
	}
}

func TestUpdateStatus(t *testing.T) {
	trk := tempDB(t)

	trk.RecordImport("test.enex", "Note 1", "hash1", -1, "/path", StatusUploaded)
	trk.UpdateStatus("hash1", -1, StatusError, "something failed")

	rec, _ := trk.GetRecord("hash1", -1)
	if rec.Status != StatusError {
		t.Errorf("status = %q, want error", rec.Status)
	}
	if rec.ErrorText != "something failed" {
		t.Errorf("error_text = %q", rec.ErrorText)
	}
}

func TestSetULID(t *testing.T) {
	trk := tempDB(t)

	trk.RecordImport("test.enex", "Note 1", "hash1", -1, "/path", StatusUploaded)
	trk.SetULID("hash1", -1, "01ABCDEF")

	rec, _ := trk.GetRecord("hash1", -1)
	if rec.GodocsULID != "01ABCDEF" {
		t.Errorf("ulid = %q", rec.GodocsULID)
	}
}

func TestAllRecords(t *testing.T) {
	trk := tempDB(t)

	trk.RecordImport("a.enex", "Note A", "hashA", -1, "/a", StatusUploaded)
	trk.RecordImport("b.enex", "Note B", "hashB", -1, "/b", StatusUploaded)
	trk.RecordImport("a.enex", "Note C", "hashC", 0, "/c", StatusComplete)

	recs, err := trk.AllRecords("a.enex")
	if err != nil {
		t.Fatalf("AllRecords: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records for a.enex, got %d", len(recs))
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	trk1, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	trk1.RecordImport("test.enex", "Note", "hash1", -1, "/p", StatusUploaded)
	trk1.Close()

	trk2, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer trk2.Close()

	if !trk2.IsImported("hash1", -1) {
		t.Error("data should persist across reopen")
	}
}

// Ensure we don't leave temp files in working directory
func TestNoLocalDB(t *testing.T) {
	_, err := os.Stat("test.db")
	if err == nil {
		t.Log("warning: test.db exists in working directory")
	}
}
