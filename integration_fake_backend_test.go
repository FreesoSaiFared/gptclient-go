package sentinel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newFakeBackend creates a test server that simulates the ChatGPT web API.
func newFakeBackend(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var serverURL string

	// WebSocket upgrader
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	// WebSocket handler
	mux.HandleFunc("/ws/chat", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("ws upgrade error: %v", err)
			return
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	})

	// POST /backend-api/f/conversation/prepare
	mux.HandleFunc("/backend-api/f/conversation/prepare", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":        "ok",
			"conduit_token": "fake-conduit",
		})
	})

	// POST /backend-api/sentinel/chat-requirements/prepare
	mux.HandleFunc("/backend-api/sentinel/chat-requirements/prepare", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"persona": "test",
			"proofofwork": map[string]bool{
				"required": false,
			},
			"turnstile": map[string]bool{
				"required": false,
			},
			"prepare_token": "fake-prepare",
		})
	})

	// POST /backend-api/sentinel/chat-requirements/finalize
	mux.HandleFunc("/backend-api/sentinel/chat-requirements/finalize", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":        "fake-sentinel",
			"expire_after": 60,
		})
	})

	// GET /backend-api/celsius/ws/user
	mux.HandleFunc("/backend-api/celsius/ws/user", func(w http.ResponseWriter, r *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws/chat"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"websocket_url": wsURL,
		})
	})

	// POST /backend-api/f/conversation (SSE response)
	mux.HandleFunc("/backend-api/f/conversation", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		// Send a complete assistant message via SSE (non-delta encoding)
		evt := map[string]interface{}{
			"conversation_id": "fake-conv-123",
			"message": map[string]interface{}{
				"id": "fake-msg-456",
				"author": map[string]string{
					"role": "assistant",
				},
				"content": map[string]interface{}{
					"content_type": "text",
					"parts":        []string{"pong"},
				},
			},
		}
		evtBytes, _ := json.Marshal(evt)
		fmt.Fprintf(w, "data: %s\n\n", evtBytes)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})

	ts := httptest.NewServer(mux)
	serverURL = ts.URL
	return ts
}

func TestIntegration_FakeBackend_Chat(t *testing.T) {
	ts := newFakeBackend(t)
	defer ts.Close()

	client := NewClient(Config{
		BearerToken:        "test-bearer-token",
		BaseURL:            ts.URL,
		DisableImpersonate: true,
	})

	result, err := client.Chat("ping")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if result.Text != "pong" {
		t.Errorf("Text: got %q, want %q", result.Text, "pong")
	}
	if result.ConversationID != "fake-conv-123" {
		t.Errorf("ConversationID: got %q, want %q", result.ConversationID, "fake-conv-123")
	}
	if result.LastAssistantMsgID != "fake-msg-456" {
		t.Errorf("LastAssistantMsgID: got %q, want %q", result.LastAssistantMsgID, "fake-msg-456")
	}

	info := client.GetSessionInfo()
	if info.TurnCount != 1 {
		t.Errorf("TurnCount: got %d, want 1", info.TurnCount)
	}
	if info.ConversationID != "fake-conv-123" {
		t.Errorf("Session ConversationID: got %q, want %q", info.ConversationID, "fake-conv-123")
	}
}

func TestIntegration_FakeBackend_MultiTurn(t *testing.T) {
	ts := newFakeBackend(t)
	defer ts.Close()

	client := NewClient(Config{
		BearerToken:        "test-bearer-token",
		BaseURL:            ts.URL,
		DisableImpersonate: true,
	})

	// First turn
	result1, err := client.Chat("ping")
	if err != nil {
		t.Fatalf("Chat turn 1 failed: %v", err)
	}
	if result1.Text != "pong" {
		t.Errorf("Turn 1 Text: got %q, want %q", result1.Text, "pong")
	}
	info1 := client.GetSessionInfo()
	if info1.TurnCount != 1 {
		t.Errorf("Turn 1 TurnCount: got %d, want 1", info1.TurnCount)
	}

	// Second turn
	result2, err := client.Chat("ping again")
	if err != nil {
		t.Fatalf("Chat turn 2 failed: %v", err)
	}
	if result2.Text != "pong" {
		t.Errorf("Turn 2 Text: got %q, want %q", result2.Text, "pong")
	}
	info2 := client.GetSessionInfo()
	if info2.TurnCount != 2 {
		t.Errorf("Turn 2 TurnCount: got %d, want 2", info2.TurnCount)
	}
}

func TestIntegration_FakeBackend_ChatStream(t *testing.T) {
	ts := newFakeBackend(t)
	defer ts.Close()

	client := NewClient(Config{
		BearerToken:        "test-bearer-token",
		BaseURL:            ts.URL,
		DisableImpersonate: true,
	})

	var deltas []string
	result, err := client.ChatStream("ping", func(delta string) {
		deltas = append(deltas, delta)
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	if result.Text != "pong" {
		t.Errorf("Text: got %q, want %q", result.Text, "pong")
	}
	if len(deltas) == 0 {
		t.Error("Expected at least one delta callback")
	}
	// The full text should be in deltas
	combined := strings.Join(deltas, "")
	if combined != "pong" {
		t.Errorf("Combined deltas: got %q, want %q", combined, "pong")
	}
}

func TestIntegration_FakeBackend_ResetSession(t *testing.T) {
	ts := newFakeBackend(t)
	defer ts.Close()

	client := NewClient(Config{
		BearerToken:        "test-bearer-token",
		BaseURL:            ts.URL,
		DisableImpersonate: true,
	})

	// First turn
	_, err := client.Chat("ping")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if client.GetSessionInfo().TurnCount != 1 {
		t.Errorf("TurnCount after 1 chat: got %d", client.GetSessionInfo().TurnCount)
	}

	// Reset
	client.ResetSession()
	info := client.GetSessionInfo()
	if info.TurnCount != 0 {
		t.Errorf("TurnCount after reset: got %d, want 0", info.TurnCount)
	}
	if info.ConversationID != "" {
		t.Errorf("ConversationID after reset: got %q, want empty", info.ConversationID)
	}
	if info.ParentMessageID != "client-created-root" {
		t.Errorf("ParentMessageID after reset: got %q", info.ParentMessageID)
	}
}
