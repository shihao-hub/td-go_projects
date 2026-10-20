package api

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os/exec"
	"runtime"
	"strings"

	"anatomymanager/internal/model"
	"anatomymanager/internal/storage"
	"anatomymanager/internal/web"
)

type Server struct {
	store *storage.Store
	port  int
}

func NewServer(store *storage.Store, port int) *Server {
	return &Server{store: store, port: port}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/artworks", s.handleArtworks)
	mux.HandleFunc("/api/artworks/", s.handleArtworkResource)
	subFS, err := fs.Sub(web.Assets, "assets")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(subFS)))
	}
	return mux
}

func (s *Server) Start(openBrowser bool) error {
	mux := s.Handler()
	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	fmt.Printf("Anatomy Manager WebUI is running at http://%s\n", addr)

	if openBrowser {
		go openURL(fmt.Sprintf("http://%s", addr))
	}

	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleArtworks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		list, err := s.store.ListArtworks()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []model.Artwork{}
		}
		writeJSON(w, list)
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleArtworkResource(w http.ResponseWriter, r *http.Request) {
	// Path format: /api/artworks/{id} or /api/artworks/{id}/files/{filename}
	path := strings.TrimPrefix(r.URL.Path, "/api/artworks/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	artworkID := parts[0]

	// 1. /api/artworks/{id}
	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			art, err := s.store.GetArtwork(artworkID)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			writeJSON(w, art)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 2. /api/artworks/{id}/files/{filename}
	if len(parts) >= 3 && parts[1] == "files" {
		fileName := strings.Join(parts[2:], "/")
		f, err := s.store.GetFile(artworkID, fileName)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", f.MimeType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", f.SizeBytes))
		// Cache control for performance
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(f.ContentBlob)
		return
	}

	http.NotFound(w, r)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func openURL(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}

// ReadRequestBody helper
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}
