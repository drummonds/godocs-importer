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
}

// NewClient creates a godocs API client.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// Tag as returned by the godocs API.
type Tag struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// Document as returned by the godocs API.
type Document struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	ULID         string `json:"ulid"`
	DocumentType string `json:"document_type"`
}

// Job as returned by the godocs API.
type Job struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

// Upload sends a file to godocs via the upload API.
// Returns the uploaded path on success.
func (c *Client) Upload(filePath, destPath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	part, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", fmt.Errorf("creating form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", fmt.Errorf("copying file data: %w", err)
	}

	if destPath != "" {
		if err := w.WriteField("path", destPath); err != nil {
			return "", fmt.Errorf("writing path field: %w", err)
		}
	}
	w.Close()

	req, err := http.NewRequest("POST", c.BaseURL+"/api/document/upload", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload failed (status %d): %s", resp.StatusCode, body)
	}

	var result struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		// Some versions return plain text
		return string(body), nil
	}
	return result.Body, nil
}

// UploadBytes uploads in-memory content as a file to godocs.
func (c *Client) UploadBytes(content []byte, fileName, destPath string) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	part, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return "", fmt.Errorf("creating form file: %w", err)
	}
	if _, err := part.Write(content); err != nil {
		return "", fmt.Errorf("writing content: %w", err)
	}

	if destPath != "" {
		if err := w.WriteField("path", destPath); err != nil {
			return "", fmt.Errorf("writing path field: %w", err)
		}
	}
	w.Close()

	req, err := http.NewRequest("POST", c.BaseURL+"/api/document/upload", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload failed (status %d): %s", resp.StatusCode, body)
	}

	var result struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return string(body), nil
	}
	return result.Body, nil
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
func (c *Client) EnsureTag(name string) (int, error) {
	tags, err := c.GetTags()
	if err != nil {
		return 0, err
	}
	for _, t := range tags {
		if t.Name == name {
			return t.ID, nil
		}
	}
	tag, err := c.CreateTag(name)
	if err != nil {
		return 0, err
	}
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

// SetDimension sets a dimension value on a document.
func (c *Client) SetDimension(ulid, dimensionName, value string) error {
	body, _ := json.Marshal(map[string]string{
		"dimension_name": dimensionName,
		"value":          value,
	})
	resp, err := c.HTTPClient.Post(
		fmt.Sprintf("%s/api/documents/%s/dimensions", c.BaseURL, ulid),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set dimension failed (status %d): %s", resp.StatusCode, b)
	}
	return nil
}

// SearchDocument searches for a document by term and returns the first match.
func (c *Client) SearchDocument(term string) (*Document, error) {
	resp, err := c.HTTPClient.Get(fmt.Sprintf("%s/api/search?term=%s", c.BaseURL, term))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	var result struct {
		FileSystem []struct {
			ULID     string `json:"ulid"`
			Name     string `json:"name"`
			FullPath string `json:"fullPath"`
		} `json:"fileSystem"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.FileSystem) == 0 {
		return nil, nil
	}
	return &Document{
		ULID: result.FileSystem[0].ULID,
		Name: result.FileSystem[0].Name,
		Path: result.FileSystem[0].FullPath,
	}, nil
}

// GetActiveJobs returns currently active (pending/running) jobs.
func (c *Client) GetActiveJobs() ([]Job, error) {
	resp, err := c.HTTPClient.Get(c.BaseURL + "/api/jobs/active")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var jobs []Job
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

// WaitForIngestion polls active jobs until no ingestion jobs remain.
func (c *Client) WaitForIngestion(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		jobs, err := c.GetActiveJobs()
		if err != nil {
			return err
		}
		active := false
		for _, j := range jobs {
			if j.Type == "ingestion" {
				active = true
				break
			}
		}
		if !active {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("ingestion did not complete within %v", timeout)
}

// GetLatestDocuments returns the most recent documents.
func (c *Client) GetLatestDocuments(page int) ([]Document, error) {
	resp, err := c.HTTPClient.Get(fmt.Sprintf("%s/api/documents/latest?page=%d", c.BaseURL, page))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Documents []Document `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Documents, nil
}
