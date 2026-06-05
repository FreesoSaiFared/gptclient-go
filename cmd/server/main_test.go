package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newFakeBackendForServer creates a fake backend for server tests.
// This mirrors the fake backend in the sentinel package tests but is
// defined here for the server package.
func newFakeBackendForServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var serverURL string

	mux.HandleFunc("/ws/chat", func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
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

	mux.HandleFunc("/backend-api/f/conversation/prepare", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":        "ok",
			"conduit_token": "fake-conduit",
		})
	})

	mux.HandleFunc("/backend-api/sentinel/chat-requirements/prepare", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/backend-api/sentinel/chat-requirements/finalize", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":        "fake-sentinel",
			"expire_after": 60,
		})
	})

	mux.HandleFunc("/backend-api/celsius/ws/user", func(w http.ResponseWriter, r *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws/chat"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"websocket_url": wsURL,
		})
	})

	mux.HandleFunc("/backend-api/f/conversation", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		evt := map[string]interface{}{
			"conversation_id": "fake-conv-123",
			"message": map[string]interface{}{
				"id": "fake-msg-456",
				"author": map[string]string{
					"role": "assistant",
				},
				"content": map[string]interface{}{
					"content_type": "text",
					"parts":        []string{"Hello from fake backend"},
				},
			},
		}
		evtBytes, _ := json.Marshal(evt)
		w.Write([]byte("data: " + string(evtBytes) + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})

	ts := httptest.NewServer(mux)
	serverURL = ts.URL
	return ts
}

func TestServer_GETReturns405(t *testing.T) {
	cfg := ServerConfig{
		BearerToken: "test-token",
	}
	handler := newHandler(&cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestServer_MalformedJSONReturns400(t *testing.T) {
	cfg := ServerConfig{
		BearerToken: "test-token",
	}
	handler := newHandler(&cfg)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Malformed JSON status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestServer_EmptyMessagesReturns400(t *testing.T) {
	cfg := ServerConfig{
		BearerToken: "test-token",
	}
	handler := newHandler(&cfg)

	body := `{"model":"gpt-4o","messages":[],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Empty messages status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestServer_MissingAuthReturns401(t *testing.T) {
	cfg := ServerConfig{
		BearerToken: "", // No config token either
	}
	handler := newHandler(&cfg)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Missing auth status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestServer_ReplaceWithJWTReturns401(t *testing.T) {
	cfg := ServerConfig{
		BearerToken: "REPLACE_WITH_JWT",
	}
	handler := newHandler(&cfg)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("REPLACE_WITH_JWT status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestServer_DummyTokenFallsBackToConfig(t *testing.T) {
	ts := newFakeBackendForServer(t)
	defer ts.Close()

	cfg := ServerConfig{
		BearerToken:  "test-bearer-token",
		CookieString: "",
		DefaultModel: "",
		BaseURL:      ts.URL,
	}
	handler := newHandler(&cfg)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-dummy")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("sk-dummy fallback status: got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestServer_NonStreamingRequest(t *testing.T) {
	ts := newFakeBackendForServer(t)
	defer ts.Close()

	cfg := ServerConfig{
		BearerToken:  "test-bearer-token",
		CookieString: "",
		DefaultModel: "",
		BaseURL:      ts.URL,
	}
	handler := newHandler(&cfg)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-bearer-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Non-streaming status: got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp OpenAINonStreamResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("Object: got %q, want %q", resp.Object, "chat.completion")
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("Choices length: got %d, want 1", len(resp.Choices))
	}
	choice := resp.Choices[0]
	if choice.Message.Role != "assistant" {
		t.Errorf("Choice role: got %q, want %q", choice.Message.Role, "assistant")
	}
	if choice.FinishReason != "stop" {
		t.Errorf("FinishReason: got %q, want %q", choice.FinishReason, "stop")
	}
	if choice.Message.Content != "Hello from fake backend" {
		t.Errorf("Content: got %q, want %q", choice.Message.Content, "Hello from fake backend")
	}
}

func TestServer_StreamingRequest(t *testing.T) {
	ts := newFakeBackendForServer(t)
	defer ts.Close()

	cfg := ServerConfig{
		BearerToken:  "test-bearer-token",
		CookieString: "",
		DefaultModel: "",
		BaseURL:      ts.URL,
	}
	handler := newHandler(&cfg)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-bearer-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Streaming status: got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Errorf("Content-Type: got %q, want text/event-stream", contentType)
	}

	// Parse SSE chunks
	respBody := w.Body.String()
	scanner := bufio.NewScanner(strings.NewReader(respBody))
	var chunks []OpenAIStreamChunk
	var gotDone bool

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			gotDone = true
			continue
		}
		var chunk OpenAIStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Logf("Failed to parse chunk: %v, payload: %q", err, payload)
			continue
		}
		chunks = append(chunks, chunk)
	}

	if !gotDone {
		t.Error("Expected data: [DONE] in SSE stream")
	}
	if len(chunks) < 2 {
		t.Errorf("Expected at least 2 chunks (content + finish), got %d", len(chunks))
	}

	// Check first chunk has content
	if chunks[0].Object != "chat.completion.chunk" {
		t.Errorf("First chunk object: got %q", chunks[0].Object)
	}
	if len(chunks[0].Choices) != 1 {
		t.Errorf("First chunk choices: got %d", len(chunks[0].Choices))
	}

	// Check last chunk has finish_reason: stop
	lastChunk := chunks[len(chunks)-1]
	if len(lastChunk.Choices) != 1 {
		t.Errorf("Last chunk choices: got %d", len(lastChunk.Choices))
	}
	if lastChunk.Choices[0].FinishReason == nil || *lastChunk.Choices[0].FinishReason != "stop" {
		t.Errorf("Last chunk finish_reason: got %v, want 'stop'", lastChunk.Choices[0].FinishReason)
	}
}

func TestServer_MessagesToPrompt(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		want     string
	}{
		{
			name: "single message",
			messages: []Message{
				{Role: "user", Content: "hello"},
			},
			want: "hello",
		},
		{
			name: "system + user",
			messages: []Message{
				{Role: "system", Content: "You are helpful"},
				{Role: "user", Content: "hi"},
			},
			want: "System: You are helpful\nUser: hi\n",
		},
		{
			name: "full conversation",
			messages: []Message{
				{Role: "system", Content: "Be concise"},
				{Role: "user", Content: "What is Go?"},
				{Role: "assistant", Content: "A programming language"},
				{Role: "user", Content: "Thanks"},
			},
			want: "System: Be concise\nUser: What is Go?\nAssistant: A programming language\nUser: Thanks\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := messagesToPrompt(tt.messages)
			if got != tt.want {
				t.Errorf("messagesToPrompt: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServer_LoadConfig(t *testing.T) {
	// Non-existent file
	_, err := loadConfig("nonexistent.json")
	if err == nil {
		t.Error("Expected error for nonexistent config file")
	}

	// Valid config (use config.example.json from project root)
	cf, err := loadConfig("../../config.example.json")
	if err != nil {
		t.Fatalf("Failed to load config.example.json: %v", err)
	}
	if cf.BearerToken != "REPLACE_WITH_JWT" {
		t.Errorf("BearerToken: got %q, want %q", cf.BearerToken, "REPLACE_WITH_JWT")
	}
}

func TestServer_RequestBearerTokenOverConfig(t *testing.T) {
	ts := newFakeBackendForServer(t)
	defer ts.Close()

	cfg := ServerConfig{
		BearerToken:  "config-bearer-token",
		CookieString: "",
		DefaultModel: "",
		BaseURL:      ts.URL,
	}
	handler := newHandler(&cfg)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Provide a valid bearer token via header - should be used instead of config
	req.Header.Set("Authorization", "Bearer test-bearer-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Request bearer token status: got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}
