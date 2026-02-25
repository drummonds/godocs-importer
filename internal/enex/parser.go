package enex

import (
	"crypto/md5"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseFile parses an ENEX file and returns the export data.
// Uses streaming XML decoding to handle large exports.
func ParseFile(path string) (*Export, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening enex file: %w", err)
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads ENEX XML from a reader.
func Parse(r io.Reader) (*Export, error) {
	decoder := xml.NewDecoder(r)
	var export Export

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading xml: %w", err)
		}

		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "note" {
			continue
		}

		var note Note
		if err := decoder.DecodeElement(&note, &se); err != nil {
			return nil, fmt.Errorf("decoding note: %w", err)
		}
		// Trim whitespace from base64 resource data
		for i := range note.Resources {
			note.Resources[i].Data.Content = strings.TrimSpace(note.Resources[i].Data.Content)
		}
		export.Notes = append(export.Notes, note)
	}

	return &export, nil
}

// NoteHash returns an MD5 hex digest of the note's title + content for dedup.
func NoteHash(n *Note) string {
	h := md5.New()
	h.Write([]byte(n.Title))
	h.Write([]byte{0})
	h.Write([]byte(n.Content))
	return fmt.Sprintf("%x", h.Sum(nil))
}
