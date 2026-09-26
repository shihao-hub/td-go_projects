package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"typeai/internal/config"
	"typeai/internal/session"
)

func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	raw := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 't', 'y', 'p', 'e'}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestPrepareImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test image.png")
	writeTestPNG(t, path)

	image, err := PrepareImage(path)
	if err != nil {
		t.Fatalf("PrepareImage() error = %v", err)
	}
	if image.MediaType != "image/png" || image.FileName != "test image.png" || image.Size <= 0 || image.Base64 == "" {
		t.Fatalf("image = %#v", image)
	}
	if image.Path != path && image.Path != filepath.Clean(path) {
		t.Errorf("image.Path = %q, want %q", image.Path, path)
	}
}

func TestSendWithImagesUsesContentPartsAndStoresMetadata(t *testing.T) {
	var mu sync.Mutex
	var contents [][]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		contents = append(contents, request.Messages)
		count := len(contents)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer-%d\"}}]}\n\n", count)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "test.png")
	writeTestPNG(t, path)
	image, err := PrepareImage(path)
	if err != nil {
		t.Fatalf("PrepareImage() error = %v", err)
	}

	chat, err := NewChat(config.Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "test-model",
	}, t.TempDir(), time.Now())
	if err != nil {
		t.Fatalf("NewChat() error = %v", err)
	}
	if err := chat.SendWithImages(t.Context(), "describe", []Image{image}, nil); err != nil {
		t.Fatalf("SendWithImages() error = %v", err)
	}
	if err := chat.Send(t.Context(), "again", nil); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(contents) != 2 {
		t.Fatalf("request count = %d, want 2", len(contents))
	}

	var firstMessage struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(contents[0][0], &firstMessage); err != nil {
		t.Fatalf("unmarshal first message: %v", err)
	}

	var firstParts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(firstMessage.Content, &firstParts); err != nil {
		t.Fatalf("unmarshal first content: %v", err)
	}
	if len(firstParts) != 2 || firstParts[0].Text != "describe" || firstParts[1].ImageURL == nil {
		t.Fatalf("first parts = %#v", firstParts)
	}
	if !strings.HasPrefix(firstParts[1].ImageURL.URL, "data:image/png;base64,") {
		t.Errorf("image URL = %q", firstParts[1].ImageURL.URL)
	}
	if !strings.Contains(firstParts[1].ImageURL.URL, image.Base64) {
		t.Error("request does not contain prepared image payload")
	}

	var historyParts []struct {
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	var historyMessage struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(contents[1][0], &historyMessage); err != nil {
		t.Fatalf("unmarshal history message: %v", err)
	}
	if err := json.Unmarshal(historyMessage.Content, &historyParts); err != nil {
		t.Fatalf("unmarshal history content: %v", err)
	}
	if len(historyParts) != 2 || historyParts[1].ImageURL == nil {
		t.Fatalf("history parts = %#v", historyParts)
	}
	if !strings.Contains(historyParts[1].ImageURL.URL, image.Base64) {
		t.Error("history image payload was not rebuilt")
	}
}

func TestMarkerMessageKeepsRawSessionTextAndCachesTempImage(t *testing.T) {
	var mu sync.Mutex
	var contents [][]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		contents = append(contents, request.Messages)
		count := len(contents)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer-%d\"}}]}\n\n", count)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "clipboard.png")
	writeTestPNG(t, path)
	prepared, err := PrepareImage(path)
	if err != nil {
		t.Fatalf("PrepareImage() error = %v", err)
	}
	rawInput := "look [[image:" + path + "]] now"
	chat, err := NewChat(config.Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "test-model",
	}, t.TempDir(), time.Now())
	if err != nil {
		t.Fatalf("NewChat() error = %v", err)
	}
	if err := chat.Send(t.Context(), rawInput, nil); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := chat.Send(t.Context(), "again", nil); err != nil {
		t.Fatalf("second Send() error = %v", err)
	}

	var saved session.Session
	if err := json.Unmarshal(readFile(t, chat.Path()), &saved); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}
	if saved.Messages[0].Content != rawInput {
		t.Errorf("session content = %q, want raw input %q", saved.Messages[0].Content, rawInput)
	}
	if len(saved.Messages[0].Images) != 1 || saved.Messages[0].Images[0].Path != filepath.Clean(path) {
		t.Fatalf("session images = %#v", saved.Messages[0].Images)
	}
	if strings.Contains(string(readFile(t, chat.Path())), prepared.Base64) {
		t.Error("session contains image payload")
	}

	var firstMessage struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(contents[0][0], &firstMessage); err != nil {
		t.Fatalf("unmarshal first message: %v", err)
	}
	var parts []struct {
		Text     string `json:"text"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(firstMessage.Content, &parts); err != nil {
		t.Fatalf("unmarshal first content: %v", err)
	}
	if len(parts) != 2 || parts[0].Text != "look now" || parts[1].ImageURL == nil {
		t.Fatalf("request parts = %#v", parts)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return raw
}
