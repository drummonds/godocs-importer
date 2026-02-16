package tracker

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/drummonds/go-postgres"
)

// Status values for import records.
const (
	StatusPending  = "pending"
	StatusUploaded = "uploaded"
	StatusTagged   = "tagged"
	StatusComplete = "complete"
	StatusError    = "error"
)

// Record represents a single import record in the tracker DB.
type Record struct {
	ID            int64
	EnexFile      string
	NoteTitle     string
	NoteHash      string
	ResourceIndex int // -1 = note itself, 0+ = resource index
	GodocsPath    string
	GodocsULID    string
	Status        string
	ErrorText     string
	ImportedAt    time.Time
}

// Tracker manages import state in a pglike database.
type Tracker struct {
	db *sql.DB
}

// New opens (or creates) the tracker database at the given path.
func New(dbPath string) (*Tracker, error) {
	db, err := sql.Open("pglike", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening tracker db: %w", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS imports (
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
	)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("creating imports table: %w", err)
	}

	return &Tracker{db: db}, nil
}

// Close closes the tracker database.
func (t *Tracker) Close() error {
	return t.db.Close()
}

// IsImported returns true if a note/resource has already been successfully imported.
func (t *Tracker) IsImported(noteHash string, resourceIndex int) bool {
	var count int
	err := t.db.QueryRow(
		`SELECT COUNT(*) FROM imports WHERE note_hash = $1 AND resource_index = $2 AND status IN ('uploaded', 'tagged', 'complete')`,
		noteHash, resourceIndex,
	).Scan(&count)
	return err == nil && count > 0
}

// GetRecord returns the import record for a note/resource, if it exists.
func (t *Tracker) GetRecord(noteHash string, resourceIndex int) (*Record, error) {
	var r Record
	err := t.db.QueryRow(
		`SELECT id, enex_file, note_title, note_hash, resource_index, COALESCE(godocs_path,''), COALESCE(godocs_ulid,''), status, COALESCE(error_text,''), imported_at
		 FROM imports WHERE note_hash = $1 AND resource_index = $2`,
		noteHash, resourceIndex,
	).Scan(&r.ID, &r.EnexFile, &r.NoteTitle, &r.NoteHash, &r.ResourceIndex,
		&r.GodocsPath, &r.GodocsULID, &r.Status, &r.ErrorText, &r.ImportedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// RecordImport inserts a new import record.
func (t *Tracker) RecordImport(enexFile, noteTitle, noteHash string, resourceIndex int, godocsPath, status string) error {
	_, err := t.db.Exec(
		`INSERT INTO imports (enex_file, note_title, note_hash, resource_index, godocs_path, status)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		enexFile, noteTitle, noteHash, resourceIndex, godocsPath, status,
	)
	return err
}

// UpdateStatus updates the status (and optionally error text) for a record.
func (t *Tracker) UpdateStatus(noteHash string, resourceIndex int, status, errorText string) error {
	_, err := t.db.Exec(
		`UPDATE imports SET status = $1, error_text = $2 WHERE note_hash = $3 AND resource_index = $4`,
		status, errorText, noteHash, resourceIndex,
	)
	return err
}

// SetULID sets the godocs ULID for an import record.
func (t *Tracker) SetULID(noteHash string, resourceIndex int, ulid string) error {
	_, err := t.db.Exec(
		`UPDATE imports SET godocs_ulid = $1 WHERE note_hash = $2 AND resource_index = $3`,
		ulid, noteHash, resourceIndex,
	)
	return err
}

// AllRecords returns all import records for a given ENEX file.
func (t *Tracker) AllRecords(enexFile string) ([]Record, error) {
	rows, err := t.db.Query(
		`SELECT id, enex_file, note_title, note_hash, resource_index, COALESCE(godocs_path,''), COALESCE(godocs_ulid,''), status, COALESCE(error_text,''), imported_at
		 FROM imports WHERE enex_file = $1 ORDER BY id`,
		enexFile,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.EnexFile, &r.NoteTitle, &r.NoteHash, &r.ResourceIndex,
			&r.GodocsPath, &r.GodocsULID, &r.Status, &r.ErrorText, &r.ImportedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}
