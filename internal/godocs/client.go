package godocs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Client talks to the godocs HTTP API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	tagCache   map[string]int // name → ID
}

// NewClient creates a godocs API client.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		tagCache:   make(map[string]int),
	}
}

// Tag as returned by the godocs API.
type Tag struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// UploadResult holds the response from the godocs upload API.
type UploadResult struct {
	Path string `json:"path"`
	ULID string `json:"ulid"`
	Name string `json:"name"`
}

// Upload sends a file to godocs via the upload API.
func (c *Client) Upload(filePath, destPath string) (*UploadResult, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	part, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("creating form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("copying file data: %w", err)
	}

	if destPath != "" {
		if err := w.WriteField("path", destPath); err != nil {
			return nil, fmt.Errorf("writing path field: %w", err)
		}
	}
	w.Close()

	return c.doUpload(&buf, w.FormDataContentType())
}

// UploadBytes uploads in-memory content as a file to godocs.
func (c *Client) UploadBytes(content []byte, fileName, destPath string) (*UploadResult, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	part, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return nil, fmt.Errorf("creating form file: %w", err)
	}
	if _, err := part.Write(content); err != nil {
		return nil, fmt.Errorf("writing content: %w", err)
	}

	if destPath != "" {
		if err := w.WriteField("path", destPath); err != nil {
			return nil, fmt.Errorf("writing path field: %w", err)
		}
	}
	w.Close()

	return c.doUpload(&buf, w.FormDataContentType())
}

func (c *Client) doUpload(body io.Reader, contentType string) (*UploadResult, error) {
	req, err := http.NewRequest("POST", c.BaseURL+"/api/document/upload", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upload failed (status %d): %s", resp.StatusCode, respBody)
	}

	var result UploadResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return &UploadResult{Path: string(respBody)}, nil
	}
	return &result, nil
}

// GetTags returns all tags from godocs.
func (c *Client) GetTags() ([]Tag, error) {
	resp, err := c.HTTPClient.Get(c.BaseURL + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tags []Tag
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}
	return tags, nil
}

// CreateTag creates a new tag and returns it.
func (c *Client) CreateTag(name string) (*Tag, error) {
	body, _ := json.Marshal(map[string]string{"name": name, "color": "#3498db"})
	resp, err := c.HTTPClient.Post(c.BaseURL+"/api/tags", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create tag failed (status %d): %s", resp.StatusCode, b)
	}

	var tag Tag
	if err := json.NewDecoder(resp.Body).Decode(&tag); err != nil {
		return nil, err
	}
	return &tag, nil
}

// EnsureTag finds or creates a tag by name, returning its ID.
// Results are cached for the lifetime of the client.
func (c *Client) EnsureTag(name string) (int, error) {
	if id, ok := c.tagCache[name]; ok {
		return id, nil
	}

	// Populate cache on first miss
	if len(c.tagCache) == 0 {
		tags, err := c.GetTags()
		if err != nil {
			return 0, err
		}
		for _, t := range tags {
			c.tagCache[t.Name] = t.ID
		}
		if id, ok := c.tagCache[name]; ok {
			return id, nil
		}
	}

	tag, err := c.CreateTag(name)
	if err != nil {
		return 0, err
	}
	c.tagCache[tag.Name] = tag.ID
	return tag.ID, nil
}

// AddTag adds a tag to a document by ULID.
func (c *Client) AddTag(ulid string, tagID int) error {
	body, _ := json.Marshal(map[string]int{"tag_id": tagID})
	resp, err := c.HTTPClient.Post(
		fmt.Sprintf("%s/api/documents/%s/tags", c.BaseURL, ulid),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("add tag failed (status %d): %s", resp.StatusCode, b)
	}
	return nil
}

// MetadataUpdate holds optional metadata fields for a document.
type MetadataUpdate struct {
	CreatedDate *time.Time `json:"created_date,omitempty"`
	UpdatedDate *time.Time `json:"updated_date,omitempty"`
	Author      *string    `json:"author,omitempty"`
	SourceURL   *string    `json:"source_url,omitempty"`
	Source      *string    `json:"source,omitempty"`
}

// UpdateMetadata sets metadata fields on a document via PUT /api/document/:id/metadata.
func (c *Client) UpdateMetadata(ulid string, meta MetadataUpdate) error {
	body, _ := json.Marshal(meta)
	req, err := http.NewRequest("PUT", fmt.Sprintf("%s/api/document/%s/metadata", c.BaseURL, ulid), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update metadata failed (status %d): %s", resp.StatusCode, b)
	}
	return nil
}

