package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatStreamHappyPath(t *testing.T) {
	var request chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "test-key", Model: "test-model", HTTP: server.Client()}
	var deltas []Delta
	full, err := client.ChatStream(t.Context(), []Message{{Role: "user", Content: "hi"}}, func(delta Delta) {
		deltas = append(deltas, delta)
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if full != "你好" {
		t.Errorf("full = %q, want 你好", full)
	}
	if len(deltas) != 2 || deltas[0].Kind != DeltaAnswer || deltas[0].Text != "你" || deltas[1].Kind != DeltaAnswer || deltas[1].Text != "好" {
		t.Errorf("deltas = %#v", deltas)
	}
	if !request.Stream {
		t.Error("request.Stream = false, want true")
	}
	if request.Model != "test-model" {
		t.Errorf("request.Model = %q", request.Model)
	}
	if len(request.Messages) != 1 || request.Messages[0].Content != "hi" {
		t.Errorf("request.Messages = %#v", request.Messages)
	}
}

func TestChatStreamReasoningDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"想\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"答\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "k", Model: "m", HTTP: server.Client()}
	var deltas []Delta
	full, err := client.ChatStream(t.Context(), []Message{{Role: "user", Content: "q"}}, func(delta Delta) {
		deltas = append(deltas, delta)
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if full != "答" {
		t.Errorf("full = %q, want 答", full)
	}
	if len(deltas) != 2 || deltas[0].Kind != DeltaReasoning || deltas[0].Text != "想" || deltas[1].Kind != DeltaAnswer || deltas[1].Text != "答" {
		t.Errorf("deltas = %#v", deltas)
	}
}

func TestChatStreamHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid key", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "bad", Model: "m", HTTP: server.Client()}
	_, err := client.ChatStream(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("ChatStream() error = %#v, want HTTPError", err)
	}
	if httpErr.Status != http.StatusUnauthorized {
		t.Errorf("HTTPError.Status = %d", httpErr.Status)
	}
}
