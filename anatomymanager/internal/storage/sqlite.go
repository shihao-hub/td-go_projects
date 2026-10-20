package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"anatomymanager/internal/model"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

// NewStore opens and initializes the SQLite database at dbPath.
func NewStore(dbPath string) (*Store, error) {
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dir := filepath.Join(home, ".anatomy")
		_ = os.MkdirAll(dir, 0755)
		dbPath = filepath.Join(dir, "anatomy.db")
	} else {
		dir := filepath.Dir(dbPath)
		_ = os.MkdirAll(dir, 0755)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db error: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite db error: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS artworks (
		id            TEXT PRIMARY KEY,
		title         TEXT NOT NULL,
		category      TEXT DEFAULT 'figure',
		description   TEXT,
		tags          TEXT,
		session_id    TEXT,
		primary_html  TEXT DEFAULT '',
		doc_markdown  TEXT,
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS artifact_files (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		artwork_id    TEXT NOT NULL,
		file_name     TEXT NOT NULL,
		file_type     TEXT NOT NULL,
		mime_type     TEXT NOT NULL,
		size_bytes    INTEGER NOT NULL,
		content_blob  BLOB,
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(artwork_id) REFERENCES artworks(id) ON DELETE CASCADE,
		UNIQUE(artwork_id, file_name)
	);
	CREATE INDEX IF NOT EXISTS idx_files_artwork ON artifact_files(artwork_id);
	`
	_, err := s.db.Exec(schema)
	return err
}

// SaveArtwork inserts or updates an artwork and its artifact files.
func (s *Store) SaveArtwork(art *model.Artwork, fileContents map[string][]byte) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	tagsBytes, _ := json.Marshal(art.Tags)

	// Upsert artwork
	query := `
	INSERT INTO artworks (id, title, category, description, tags, session_id, primary_html, doc_markdown, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		title=excluded.title,
		category=excluded.category,
		description=excluded.description,
		tags=excluded.tags,
		session_id=excluded.session_id,
		primary_html=excluded.primary_html,
		doc_markdown=excluded.doc_markdown,
		updated_at=excluded.updated_at;
	`
	_, err = tx.Exec(query,
		art.ID,
		art.Title,
		art.Category,
		art.Description,
		string(tagsBytes),
		art.SessionID,
		art.PrimaryHTML,
		art.DocMarkdown,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("upsert artwork failed: %w", err)
	}

	// Insert/update files
	fileInsert := `
	INSERT INTO artifact_files (artwork_id, file_name, file_type, mime_type, size_bytes, content_blob)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(artwork_id, file_name) DO UPDATE SET
		file_type=excluded.file_type,
		mime_type=excluded.mime_type,
		size_bytes=excluded.size_bytes,
		content_blob=excluded.content_blob;
	`
	for fname, data := range fileContents {
		ext := strings.TrimPrefix(filepath.Ext(fname), ".")
		mime := detectMime(ext)
		_, err = tx.Exec(fileInsert, art.ID, fname, ext, mime, len(data), data)
		if err != nil {
			return fmt.Errorf("upsert file %s failed: %w", fname, err)
		}
	}

	return tx.Commit()
}

// ListArtworks returns summary list of all artworks.
func (s *Store) ListArtworks() ([]model.Artwork, error) {
	rows, err := s.db.Query(`
		SELECT id, title, category, description, tags, session_id, primary_html, created_at, updated_at
		FROM artworks ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []model.Artwork
	for rows.Next() {
		var a model.Artwork
		var tagsStr sql.NullString
		var sessStr, descStr sql.NullString
		if err := rows.Scan(&a.ID, &a.Title, &a.Category, &descStr, &tagsStr, &sessStr, &a.PrimaryHTML, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.Description = descStr.String
		a.SessionID = sessStr.String
		if tagsStr.Valid && tagsStr.String != "" {
			_ = json.Unmarshal([]byte(tagsStr.String), &a.Tags)
		}
		// fetch file names
		a.Files = s.getFileNames(a.ID)
		list = append(list, a)
	}
	return list, nil
}

func (s *Store) getFileNames(artworkID string) []string {
	rows, err := s.db.Query(`SELECT file_name FROM artifact_files WHERE artwork_id=? ORDER BY file_name`, artworkID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil {
			names = append(names, n)
		}
	}
	return names
}

// GetArtwork returns an artwork with full details.
func (s *Store) GetArtwork(id string) (*model.Artwork, error) {
	row := s.db.QueryRow(`
		SELECT id, title, category, description, tags, session_id, primary_html, doc_markdown, created_at, updated_at
		FROM artworks WHERE id=?
	`, id)

	var a model.Artwork
	var tagsStr, descStr, sessStr, docStr sql.NullString
	if err := row.Scan(&a.ID, &a.Title, &a.Category, &descStr, &tagsStr, &sessStr, &a.PrimaryHTML, &docStr, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.Description = descStr.String
	a.SessionID = sessStr.String
	a.DocMarkdown = docStr.String
	if tagsStr.Valid && tagsStr.String != "" {
		_ = json.Unmarshal([]byte(tagsStr.String), &a.Tags)
	}
	a.Files = s.getFileNames(a.ID)
	return &a, nil
}

// GetFile retrieves a specific file from SQLite.
func (s *Store) GetFile(artworkID, fileName string) (*model.ArtifactFile, error) {
	row := s.db.QueryRow(`
		SELECT id, artwork_id, file_name, file_type, mime_type, size_bytes, content_blob, created_at
		FROM artifact_files WHERE artwork_id=? AND file_name=?
	`, artworkID, fileName)

	var f model.ArtifactFile
	if err := row.Scan(&f.ID, &f.ArtworkID, &f.FileName, &f.FileType, &f.MimeType, &f.SizeBytes, &f.ContentBlob, &f.CreatedAt); err != nil {
		return nil, err
	}
	return &f, nil
}

func detectMime(ext string) string {
	switch strings.ToLower(ext) {
	case "html", "htm":
		return "text/html; charset=utf-8"
	case "svg":
		return "image/svg+xml"
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "js", "mjs":
		return "application/javascript; charset=utf-8"
	case "css":
		return "text/css; charset=utf-8"
	case "json":
		return "application/json; charset=utf-8"
	case "md", "txt":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
