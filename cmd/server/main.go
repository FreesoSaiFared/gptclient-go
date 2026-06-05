package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	sentinel "sentinel-go"
)

// OpenAIRequest represents an incoming OpenAI /v1/chat/completions payload.
type OpenAIRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// Message represents a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAINonStreamResponse represents a standard OpenAI JSON response.
type OpenAINonStreamResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

// Choice represents a single choice in a non-streaming response.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// OpenAIStreamChunk represents a single SSE chunk in the OpenAI format.
type OpenAIStreamChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}

// ChunkChoice represents a choice in a streaming chunk.
type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

// ChunkDelta represents the delta content in a streaming chunk.
type ChunkDelta struct {
	Content string `json:"content,omitempty"`
}

// ConfigFile matches the structure of config.json.
type ConfigFile = sentinel.RuntimeConfig

// ServerConfig holds the runtime configuration for the server handler.
type ServerConfig struct {
	BearerToken  string
	CookieString string
	DefaultModel string
	BaseURL      string

	mu sync.Mutex // protects bearerToken for auto-refresh
}

// loadConfig reads and parses a JSON config file.
func loadConfig(path string) (sentinel.RuntimeConfig, error) {
	return sentinel.LoadRuntimeConfig(path)
}

// newHandler creates an http.Handler wired with the given ServerConfig.
func newHandler(cfg *ServerConfig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		handleChatCompletionsWithConfig(cfg, w, r)
	})
	return mux
}

// messagesToPrompt assembles all messages into a single deterministic prompt string.
// If there is exactly one message, only its content is returned.
func messagesToPrompt(messages []Message) string {
	if len(messages) == 1 {
		return messages[0].Content
	}
	var b strings.Builder
	for _, m := range messages {
		switch m.Role {
		case "system":
			b.WriteString("System: ")
		case "user":
			b.WriteString("User: ")
		case "assistant":
			b.WriteString("Assistant: ")
		default:
			b.WriteString(m.Role + ": ")
		}
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

// newSentinelClient creates a configured sentinel.Client for the given request.
func newSentinelClient(cfg *ServerConfig, bearerToken string, model string) *sentinel.Client {
	client := sentinel.NewClient(sentinel.Config{
		BearerToken:        bearerToken,
		CookieString:       cfg.CookieString,
		Model:              model,
		TempMode:           true,
		BaseURL:            cfg.BaseURL,
		DisableImpersonate: cfg.BaseURL != "",
	})
	client.SetDisableAutoImage(true)
	return client
}

func main() {
	configPath := flag.String("config", "config.json", "Config file path")
	addr := flag.String("addr", ":7777", "Listen address")
	defaultModel := flag.String("model", "", "Default model (empty uses request model or gpt-5-5-thinking fallback)")
	baseURL := flag.String("base-url", "", "Backend base URL (empty defaults to https://chatgpt.com)")
	flag.Parse()

	cf, err := loadConfig(*configPath)
	if err != nil {
		log.Printf("Warning: Failed to load config from %s: %v", *configPath, err)
	}

	// Resolve cookies once at startup
	cookieString, err := sentinel.ResolveCookieString(context.Background(), cf.CookieString, cf.Cookies)
	if err != nil {
		if cf.Cookies.Enabled {
			log.Fatalf("Failed to resolve cookies (cookies.enabled=true): %v", err)
		}
		log.Printf("Warning: failed to resolve cookies: %v", err)
		cookieString = cf.CookieString
	}

	serverCfg := ServerConfig{
		BearerToken:  cf.BearerToken,
		CookieString: cookieString,
		DefaultModel: *defaultModel,
		BaseURL:      *baseURL,
	}

	// Try to auto-refresh an expired token at startup if cookies are available
	if cookieString != "" && (cf.BearerToken == "REPLACE_WITH_JWT" || cf.BearerToken == "") {
		log.Println("No bearer token configured, attempting auto-refresh from cookies...")
		fresh, err := sentinel.RefreshAccessToken(cookieString)
		if err != nil {
			log.Printf("Warning: auto-refresh failed: %v", err)
		} else {
			serverCfg.BearerToken = fresh
			log.Printf("Auto-refresh succeeded: got token (%d chars)", len(fresh))
		}
	}

	handler := newHandler(&serverCfg)
	log.Printf("Starting OpenAI-compatible server on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, handler))
}

// handleChatCompletionsWithConfig handles the /v1/chat/completions endpoint.
func handleChatCompletionsWithConfig(cfg *ServerConfig, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Authenticate: check header first, fall back to config
	bearerToken := ""
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
		bearerToken = strings.TrimPrefix(authHeader, "Bearer ")
	}

	// Fall back to config token for empty/dummy header tokens
	cfg.mu.Lock()
	configToken := cfg.BearerToken
	cfg.mu.Unlock()
	if bearerToken == "" || bearerToken == "sk-dummy" || bearerToken == "sk-sentinel-local" {
		bearerToken = configToken
	}

	if bearerToken == "" || bearerToken == "REPLACE_WITH_JWT" {
		http.Error(w, "Unauthorized: No Bearer token found in request or config.json", http.StatusUnauthorized)
		return
	}

	// 2. Parse request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req OpenAIRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		http.Error(w, "Messages array cannot be empty", http.StatusBadRequest)
		return
	}

	// Assemble prompt from all messages
	prompt := messagesToPrompt(req.Messages)

	// Determine model with fallback chain: request → config → default
	model := req.Model
	if model == "" {
		model = cfg.DefaultModel
	}
	if model == "" {
		model = "gpt-5-5-thinking"
	}

	// 3. Create Sentinel client
	client := newSentinelClient(cfg, bearerToken, model)

	responseID := "chatcmpl-" + sentinel.GenerateUUID()
	createdTS := time.Now().Unix()

	// 4. Handle streaming vs. non-streaming
	if req.Stream {
		handleStreamingResponse(w, client, prompt, model, responseID, createdTS)
	} else {
		handleNonStreamingResponseWithRefresh(w, client, cfg, prompt, model, responseID, createdTS)
	}
}

// tryRefreshToken attempts to refresh the bearer token if cookies are available.
// Returns the new token or empty string if refresh was not possible.
func tryRefreshToken(cfg *ServerConfig) string {
	if cfg.CookieString == "" {
		return ""
	}
	fresh, err := sentinel.RefreshAccessToken(cfg.CookieString)
	if err != nil {
		log.Printf("Auto-refresh failed: %v", err)
		return ""
	}
	cfg.mu.Lock()
	cfg.BearerToken = fresh
	cfg.mu.Unlock()
	log.Printf("Auto-refresh succeeded: new token (%d chars)", len(fresh))
	return fresh
}

// handleStreamingResponse handles streaming OpenAI-compatible responses.
func handleStreamingResponse(w http.ResponseWriter, client *sentinel.Client, msg, model, id string, created int64) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	var streamStarted bool
	streamHandler := func(delta string) {
		if delta == "" {
			return
		}
		streamStarted = true
		chunk := OpenAIStreamChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []ChunkChoice{
				{
					Index: 0,
					Delta: ChunkDelta{Content: delta},
				},
			},
		}
		chunkBytes, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
		flusher.Flush()
	}

	_, err := client.ChatStream(msg, streamHandler)
	if err != nil {
		log.Printf("Streaming error: %v", err)
		if !streamStarted {
			// Error before any chunk was written: return HTTP error
			http.Error(w, fmt.Sprintf(`{"error": {"message": "%v"}}`, err), http.StatusInternalServerError)
			return
		}
		// Mid-stream error: emit error finish reason and stop cleanly
		errReason := "error"
		errChunk := OpenAIStreamChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []ChunkChoice{
				{
					Index:        0,
					Delta:        ChunkDelta{},
					FinishReason: &errReason,
				},
			},
		}
		errBytes, _ := json.Marshal(errChunk)
		fmt.Fprintf(w, "data: %s\n\n", errBytes)
		flusher.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	// Success: emit stop finish reason
	finishReason := "stop"
	finalChunk := OpenAIStreamChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []ChunkChoice{
			{
				Index:        0,
				Delta:        ChunkDelta{},
				FinishReason: &finishReason,
			},
		},
	}
	finalBytes, _ := json.Marshal(finalChunk)
	fmt.Fprintf(w, "data: %s\n\n", finalBytes)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// handleNonStreamingResponseWithRefresh handles non-streaming responses with auto-refresh on token_expired.
func handleNonStreamingResponseWithRefresh(w http.ResponseWriter, client *sentinel.Client, cfg *ServerConfig, msg, model, id string, created int64) {
	w.Header().Set("Content-Type", "application/json")

	result, err := client.Chat(msg)
	if err != nil && (strings.Contains(err.Error(), "token_expired") || strings.Contains(err.Error(), "unauthorized_unknown")) {
		// Try auto-refresh
		fresh := tryRefreshToken(cfg)
		if fresh != "" {
			client.SetBearerToken(fresh)
			result, err = client.Chat(msg)
		}
	}
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": {"message": "%v"}}`, err), http.StatusInternalServerError)
		return
	}

	resp := OpenAINonStreamResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: result.Text,
				},
				FinishReason: "stop",
			},
		},
	}

	json.NewEncoder(w).Encode(resp)
}
