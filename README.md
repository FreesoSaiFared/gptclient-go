# sentinel-go

> A Go reverse-engineered ChatGPT web client library. Uses browser Bearer Token to interact with ChatGPT directly — no OpenAI API key required.

---

## Features

- Full ChatGPT web Sentinel authentication flow (conduit token + PoW + sentinel token)
- SSE streaming output (real-time incremental text callback)
- Multi-turn conversation support (auto-maintains conversation_id / parent_message_id)
- DALL-E image generation with auto-download
- Temporary mode (don't save conversation history / don't update memory)
- Browser fingerprint impersonation (TLS fingerprint + Chrome UA + full sec-ch-ua headers)
- Ready-to-use interactive CLI (REPL)
- OpenAI-compatible HTTP server (`POST /v1/chat/completions`)
- **Systemd service integration with auto-start on boot**

---

## Quick Start

### 1. Clone the project

```bash
git clone https://github.com/yourname/sentinel-go.git
cd sentinel-go
```

### 2. Install dependencies

```bash
go mod tidy
```

### 3. Get a Bearer Token

1. Open a browser and log in to [https://chatgpt.com](https://chatgpt.com)
2. Press `F12` to open Developer Tools → Network tab
3. Send any message, filter requests to find `/backend-api/conversation`
4. In the request headers, find `Authorization: Bearer eyJ...` and copy the full JWT after `Bearer `

> ⚠️ Token validity is approximately **10 days**. You'll need to re-obtain it after expiry.

### 4. Configure credentials

Copy the example config and edit it:

```bash
cp config.example.json config.json
```

Edit `config.json`:

```json
{
  "bearerToken": "eyJhbGciOi...your JWT Token here...",
  "cookieString": ""
}
```

Or with automatic cookie extraction:

```json
{
  "bearerToken": "REPLACE_WITH_JWT",
  "cookieString": "",
  "cookies": {
    "enabled": true,
    "browser": "chrome",
    "profile": "Default",
    "domain": "chatgpt.com",
    "keyring": "auto"
  }
}
```

> ⚠️ **NEVER commit `config.json` to a public repository.** It contains your personal Bearer Token and cookies. The file is already listed in `.gitignore`.

### 5. Run

```bash
# One-command startup — auto-detects browser cookies, refreshes token, starts server
go run ./cmd/start

# Interactive multi-turn conversation (REPL mode)
go run ./cmd/chat -config config.json

# One-shot question
go run ./cmd/chat -config config.json "Hello, introduce yourself"

# Specify a model
go run ./cmd/chat -config config.json -model gpt-5-5-thinking "Write some Go code"

# Temporary mode (don't save history)
go run ./cmd/chat -config config.json -temp

# OpenAI-compatible server
go run ./cmd/server -config config.json -addr :7777

# Server with custom base URL (for testing)
go run ./cmd/server -config config.json -addr :7777 -base-url http://localhost:9999
```

---

## CLI Flags

### Chat CLI (`cmd/chat`)

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `config.json` | Config file path |
| `-model` | `gpt-5-5-thinking` | Model name |
| `-temp` | `false` | Enable temporary mode (don't save history) |
| `-base-url` | `""` | Backend base URL (empty defaults to `https://chatgpt.com`) |
| `-cookie-file` | `""` | Netscape cookies.txt file path (overrides config) |
| `-browser` | `""` | Browser to extract cookies from (overrides config) |
| `-profile` | `""` | Browser profile name or path (overrides config) |
| `-cookie-domain` | `""` | Domain to filter cookies for (default chatgpt.com) |
| `-print-cookie-status` | `false` | Print cookie resolution status |

### Server (`cmd/server`)

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `config.json` | Config file path |
| `-addr` | `:7777` | Listen address |
| `-model` | `""` | Default model (empty uses request model or `gpt-5-5-thinking` fallback) |
| `-base-url` | `""` | Backend base URL (empty defaults to `https://chatgpt.com`) |

### Cookie Utility (`cmd/cookies`)

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `config.json` | Config file path (for `-write`) |
| `-browser` | `""` | Browser to extract cookies from |
| `-profile` | `""` | Browser profile name or path |
| `-cookie-file` | `""` | Netscape cookies.txt file path |
| `-domain` | `chatgpt.com` | Domain to filter cookies for |
| `-print` | `false` | Print redacted cookie status summary |
| `-write` | `false` | Write resolved cookie string to config file |

### Start (`cmd/start`)

One-command launcher with auto-detection and interactive browser selection.

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `config.json` | Config file path |
| `-addr` | `:7777` | Listen address |
| `-model` | `gpt-5-5-thinking` | Default model |

The start command will:
1. Check if the current bearer token is valid
2. If not, probe all installed browsers for chatgpt.com cookies
3. Display a table with cookie counts, keyring status, and account info
4. Let you select which browser to use
5. Auto-refresh the access token and save config
6. Start the OpenAI-compatible server

---

## REPL Commands

When in interactive mode, these built-in commands are available:

| Command | Description |
|---------|-------------|
| `/new` | Start a new conversation, clear context |
| `/model <name>` | Switch model (no argument shows current model and options) |
| `/temp` | Toggle temporary mode on/off |
| `/info` | View current session details (conversation_id, model, turn count, etc.) |
| `/exit` or `/quit` | Exit the program |

**Available models:**

```
gpt-5-5-thinking
gpt-5-5
o4-mini-high
```

---

## OpenAI-Compatible Server

The server exposes a single endpoint compatible with the OpenAI chat completions API:

```
POST /v1/chat/completions
```

### Curl example

```bash
curl -X POST http://localhost:7777/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-dummy" \
  -d '{
    "model": "gpt-5-5-thinking",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": false
  }'
```

### Streaming example

```bash
curl -X POST http://localhost:7777/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-dummy" \
  -d '{
    "model": "gpt-5-5-thinking",
    "messages": [{"role": "user", "content": "Tell me a story"}],
    "stream": true
  }'
```

### Auth behavior

- If the request includes `Authorization: Bearer <token>`, that token is used.
- If the token is empty, `sk-dummy`, or `sk-sentinel-local`, the server falls back to the `bearerToken` in `config.json`.
- If no valid token is found, the server returns `401 Unauthorized`.

### Important notes

- The server operates in **last-message proxy mode**: all messages are assembled into a prompt string, but only the combined text is sent to ChatGPT. Full OpenAI chat semantics (system messages, multi-message context) are not preserved end-to-end.
- A new `Client` is created per request, so **conversation state is not preserved** across requests.
- Auto-image download is disabled on the server (`DisableAutoImage = true`).

---

## Testing

All tests use a **fake local backend** — no real ChatGPT credentials or network access required.

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run only unit tests
go test -v -run "Test(RuneSlice|OrDefault|TruncateStr|NewClient|ProcessDelta)" ./...

# Run only integration tests
go test -v -run "TestIntegration" ./...
```

### Test categories

| File | Description |
|------|-------------|
| `utils_test.go` | Unit tests for utility functions |
| `client_test.go` | Unit tests for Client creation and configuration |
| `chat_sse_test.go` | Unit tests for SSE/WS message processing |
| `cookies_test.go` | Unit tests for cookie loading, extraction, and decryption |
| `integration_fake_backend_test.go` | Integration test with fake HTTP/WS backend |
| `cmd/server/main_test.go` | Server endpoint tests against `newHandler` |

---

## Cookie Configuration

The `bearerToken` is still required for authentication. Cookies are additional context that may improve compatibility or bypass certain restrictions.

### Resolution order

1. If `cookieString` is non-empty in config, use it exactly as provided.
2. Else if `cookies.enabled` is `true` and `cookies.file` is set, load a Netscape cookies.txt file.
3. Else if `cookies.enabled` is `true` and `cookies.browser` is set, extract browser cookies.
4. Else use an empty cookie string.

### Manual cookie string

Set `cookieString` in `config.json` to a semicolon-separated `name=value` string:

```json
{
  "bearerToken": "eyJ...",
  "cookieString": "cookie1=val1; cookie2=val2"
}
```

### Netscape cookies.txt

Export cookies from your browser using a browser extension (e.g., "Get cookies.txt LOCALLY") and point the config at the file:

```json
{
  "bearerToken": "eyJ...",
  "cookies": {
    "enabled": true,
    "file": "/path/to/cookies.txt",
    "domain": "chatgpt.com"
  }
}
```

### Browser extraction

The `cookies.browser` field extracts cookies directly from your browser's local profile. Supported browsers:

- `chrome`, `chromium`, `brave`, `edge`, `opera`, `vivaldi` (Chromium-based)
- `firefox`

Example:

```json
{
  "bearerToken": "eyJ...",
  "cookies": {
    "enabled": true,
    "browser": "chrome",
    "profile": "Default",
    "domain": "chatgpt.com"
  }
}
```

### Browser extraction caveats

- **Linux-first**: Browser extraction is implemented for Linux. Cross-platform path mapping exists where simple, but Windows DPAPI and macOS Keychain are not implemented.
- **Chromium v10 cookies**: AES-CBC encrypted cookies using the `peanuts` password and empty-password fallback are supported.
- **Chromium v11 cookies**: These require OS keyring support and are **not supported**. The error message will suggest using `cookies.file` or `cookieString` instead.
- **Firefox**: Reads `cookies.sqlite` from local profiles. Schema version 16+ (millisecond expiry) is handled.
- If extraction is explicitly enabled (`cookies.enabled: true`) and fails, the application exits with a clear error. If cookies are not enabled, extraction failures are silently ignored.
- **No real browser profiles are accessed in tests.**

### Cookie utility command

Use `cmd/cookies` to test or write cookie configuration:

```bash
# Print a redacted summary (no cookie values shown)
go run ./cmd/cookies -browser chrome -profile Default -domain chatgpt.com -print

# Extract from browser and write to config
go run ./cmd/cookies -browser chrome -profile Default -domain chatgpt.com -config config.json -write

# Load from a cookies.txt file and write to config
go run ./cmd/cookies -cookie-file ./cookies.txt -domain chatgpt.com -config config.json -write
```

> ⚠️ No cookie values are ever printed. The `-print` flag only shows counts and byte sizes.

---

## Library Usage

```go
import sentinel "sentinel-go"

client := sentinel.NewClient(sentinel.Config{
    BearerToken: "eyJ...",
    Model:       "gpt-5-5-thinking",
    TempMode:    false,
})

// Non-streaming (wait for complete reply)
result, err := client.Chat("Hello!")
fmt.Println(result.Text)

// Streaming (print increments in real time)
result, err := client.ChatStream("Tell me a story", func(delta string) {
    fmt.Print(delta)
})

// Multi-turn conversation (IDs are maintained automatically)
client.Chat("My name is Alice")
result, _ := client.Chat("What is my name?") // → Alice

// Reset session (start new conversation)
client.ResetSession()

// Switch model
client.SetModel("gpt-5-5-thinking")
```

### Config Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `BearerToken` | string | ✅ | ChatGPT JWT Token |
| `CookieString` | string | ❌ | Browser cookie (optional, improves compatibility) |
| `Model` | string | ❌ | Model name, default `gpt-5-5-thinking` |
| `DeviceID` | string | ❌ | Device ID; auto-generates UUID if empty |
| `BuildHash` | string | ❌ | Client build hash |
| `BuildNumber` | string | ❌ | Client build number |
| `UserAgent` | string | ❌ | User-Agent, defaults to Edge 146 |
| `Language` | string | ❌ | Language, default `zh-CN` |
| `ImageDir` | string | ❌ | Image download directory, default `images/` |
| `TempMode` | bool | ❌ | Temporary mode, default `false` |
| `BaseURL` | string | ❌ | Backend base URL, default `https://chatgpt.com` |
| `DisableImpersonate` | bool | ❌ | Disable Chrome TLS fingerprint (for testing), default `false` |

### RuntimeConfig Fields (config.json)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `bearerToken` | string | ✅ | ChatGPT JWT Token |
| `cookieString` | string | ❌ | Manual cookie string (highest priority) |
| `cookies.enabled` | bool | ❌ | Enable automatic cookie extraction, default `false` |
| `cookies.file` | string | ❌ | Path to Netscape cookies.txt file |
| `cookies.browser` | string | ❌ | Browser name for extraction (chrome, firefox, etc.) |
| `cookies.profile` | string | ❌ | Browser profile name or path |
| `cookies.domain` | string | ❌ | Domain to filter cookies for, default `chatgpt.com` |
| `cookies.keyring` | string | ❌ | Keyring backend, default `auto` |

### ChatResult Fields

| Field | Description |
|-------|-------------|
| `Text` | Full assistant reply text |
| `ConversationID` | Conversation ID |
| `LastAssistantMsgID` | Last assistant message ID (for multi-turn chaining) |
| `ImageTaskID` | DALL-E image task ID (if any) |
| `ImagePath` | Local path of downloaded image (if any) |

---

## Systemd Service Installation

For production use, install the server as a systemd service to enable auto-start on boot and automatic restart on failure.

### Installation

```bash
# Install the service (requires sudo)
sudo ./scripts/install-service.sh

# Start the service
sudo ./scripts/start-sentinel.sh

# Check status
./scripts/status-sentinel.sh

# Stop the service
sudo ./scripts/stop-sentinel.sh

# View logs
sudo journalctl -u sentinel-go -f
```

### Service Features

- Auto-start on system boot
- Automatic restart on failure (after 5 seconds)
- Logs to systemd journal
- Runs as your user account
- Monitors port 7777

### Manual Management

You can also manage the service directly with systemctl:

```bash
sudo systemctl start sentinel-go
sudo systemctl stop sentinel-go
sudo systemctl restart sentinel-go
sudo systemctl enable sentinel-go
sudo systemctl disable sentinel-go
sudo systemctl status sentinel-go
```

### opencode Integration

The project includes opencode provider configuration for "ChatGPT Sentinel":

```bash
# Start the service first
sudo ./scripts/start-sentinel.sh

# Then use in opencode
opencode --model chatgpt-sentinel/gpt-5-5-thinking
```

See `.opencode/README.md` for details.

---

## Project Structure

```
sentinel-go/
├── types.go          # Public type definitions
├── client.go         # Client core struct & HTTP initialization
├── auth.go           # Sentinel three-step authentication flow
├── chat.go           # Conversation flow & SSE event parsing
├── cookies.go        # Cookie loading, browser extraction, decryption, and probing
├── image.go          # DALL-E image polling & download
├── utils.go          # UUID, FNV hash, browser fingerprint construction
├── config.example.json  # Example configuration (safe to commit)
├── scripts/          # Service installation and management scripts
├── go.mod
└── cmd/
    ├── chat/
    │   └── main.go   # CLI entry point
    ├── cookies/
    │   └── main.go   # Cookie configuration utility
    ├── server/
    │   └── main.go   # OpenAI-compatible HTTP server
    └── start/
        └── main.go   # One-command launcher with auto-detection
```

---

## Authentication Flow

Before each message, the following steps are completed:

```
1. POST /conversation/prepare      → Get conduit_token
2. POST /sentinel/chat-requirements/prepare → Get PoW challenge
3. (If PoW required) Brute-force FNV-1a hash until difficulty prefix is met
4. POST /sentinel/chat-requirements/finalize → Get sentinel_token
5. POST /backend-api/f/conversation (SSE)  → Stream reply
```

---

## Dependencies

| Dependency | Description |
|------------|-------------|
| [imroc/req/v3](https://github.com/imroc/req) | HTTP client with Chrome TLS fingerprint impersonation |
| [refraction-networking/utls](https://github.com/refraction-networking/utls) | TLS fingerprint library (indirect dependency) |
| [quic-go/quic-go](https://github.com/quic-go/quic-go) | HTTP/3 support (indirect dependency) |
| [gorilla/websocket](https://github.com/gorilla/websocket) | WebSocket client for streaming handoff |
| [modernc.org/sqlite](https://modernc.org/sqlite/) | Pure-Go SQLite driver for browser cookie extraction |

---

## Caveats

- This project is for **learning and research only**. Do not use it in ways that violate OpenAI's Terms of Service.
- Bearer Tokens are personal credentials. **Never share them** and **never commit `config.json` to a public repository**.
- The server creates a new Client per HTTP request — multi-turn conversation state is **not preserved** across requests.
- The server operates in **last-message proxy mode**: messages are combined into a single prompt string rather than being sent as a structured chat history.
- `config.json` is already in `.gitignore` — make sure it stays that way.

---

## License

MIT
