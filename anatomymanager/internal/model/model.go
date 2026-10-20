package model

import "time"

// Artwork represents an anatomy design project/diagram.
type Artwork struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Category    string    `json:"category"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	SessionID   string    `json:"sessionId"`
	PrimaryHTML string    `json:"primaryHtml"`
	DocMarkdown string    `json:"docMarkdown,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Files       []string  `json:"files,omitempty"`
}

// ArtifactFile represents a physical or embedded file for an Artwork.
type ArtifactFile struct {
	ID          int64     `json:"id"`
	ArtworkID   string    `json:"artworkId"`
	FileName    string    `json:"fileName"`
	FileType    string    `json:"fileType"`
	MimeType    string    `json:"mimeType"`
	SizeBytes   int64     `json:"sizeBytes"`
	ContentBlob []byte    `json:"-"` // Not serialized in json list
	CreatedAt   time.Time `json:"createdAt"`
}

// IngestRequest defines the input payload for ingesting an artwork.
type IngestRequest struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Category    string   `json:"category,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Dir         string   `json:"dir,omitempty"`
	Files       []string `json:"files,omitempty"`
	DocPath     string   `json:"docPath,omitempty"`
	SessionID   string   `json:"sessionId,omitempty"`
}
