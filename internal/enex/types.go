package enex

import (
	"encoding/xml"
	"time"
)

// Export is the root element of an ENEX file.
type Export struct {
	Notes []Note `xml:"note"`
}

// Note represents a single Evernote note.
type Note struct {
	Title      string         `xml:"title"`
	Content    string         `xml:"content"`
	Created    EnexTime       `xml:"created"`
	Updated    EnexTime       `xml:"updated"`
	Tags       []string       `xml:"tag"`
	Attributes NoteAttributes `xml:"note-attributes"`
	Resources  []Resource     `xml:"resource"`
}

// NoteAttributes holds metadata about a note.
type NoteAttributes struct {
	SourceURL    string `xml:"source-url"`
	Author       string `xml:"author"`
	Source       string `xml:"source"`
	Latitude     string `xml:"latitude"`
	Longitude    string `xml:"longitude"`
	Altitude     string `xml:"altitude"`
	ContentClass string `xml:"content-class"`
}

// Resource is an attachment (PDF, image, etc.) embedded in a note.
type Resource struct {
	Data        ResourceData       `xml:"data"`
	Mime        string             `xml:"mime"`
	Width       int                `xml:"width"`
	Height      int                `xml:"height"`
	Attributes  ResourceAttributes `xml:"resource-attributes"`
	Recognition string             `xml:"recognition"`
}

// ResourceData holds base64-encoded binary data.
type ResourceData struct {
	Encoding string `xml:"encoding,attr"`
	Content  string `xml:",chardata"`
}

// ResourceAttributes holds metadata about a resource.
type ResourceAttributes struct {
	FileName  string `xml:"file-name"`
	SourceURL string `xml:"source-url"`
	Timestamp string `xml:"timestamp"`
}

// EnexTime handles Evernote's timestamp format (yyyyMMddTHHmmssZ).
type EnexTime struct {
	time.Time
}

func (t *EnexTime) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var s string
	if err := d.DecodeElement(&s, &start); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	parsed, err := time.Parse("20060102T150405Z", s)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}
