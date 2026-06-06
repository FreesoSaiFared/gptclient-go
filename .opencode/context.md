# Project Context

## Environment
- Language: Go 1.25.7
- Module: sentinel-go
- Build: `go build ./cmd/chat` and `go build ./cmd/server`
- Test: `go test ./...`
- Package Manager: go modules
- Root package: `sentinel`

## Project Type
- [x] Library/Package (sentinel package)
- [x] Application (CLI cmd/chat + HTTP server cmd/server)

## Structure
- Source: root directory (sentinel package: types.go, client.go, auth.go, chat.go, image.go, utils.go)
- Commands: cmd/chat/main.go, cmd/server/main.go
- Tests: none yet (to be created)
- Config: config.json (gitignored, contains bearer token)

## Key Architecture
- Client.Chat() → Client.ChatStream() → getConduitToken() → getSentinelToken() → dialChatWS() → streamConversation()
- Real backend: https://chatgpt.com
- SSE + WebSocket for streaming
- Sentinel auth flow: 3-step (prepare → PoW → finalize)
- Server creates new Client per request (no multi-turn state)

## Conventions
- Naming: camelCase (Go standard)
- Imports: sentinel "sentinel-go" alias
- Error handling: fmt.Errorf with %w wrapping
- Chinese comments in existing code
- Config: JSON file-based

## Critical Notes
- config.json contains a REAL JWT token - must not be committed
- go.mod requires go 1.25.0, go.sum exists but may need updating
- README says default model is gpt-5-4-thinking but code uses gpt-5-5-thinking (bug)
- cmd/server uses init() for config loading (needs refactoring)
- cmd/server Choice.Status should be Choice.FinishReason
- cmd/server only uses last message from OpenAI messages array
