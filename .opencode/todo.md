# Mission: Configure, test, and deliver sentinel-go as a working Go application

## M1: Core Library Modifications | status: completed
### T1.1: Update types.go | agent:Commander
- [x] S1.1.1: Add BaseURL and DisableImpersonate fields to Config struct | size:S

### T1.2: Update client.go | agent:Commander | depends:T1.1
- [x] S1.2.1: Use cfg.BaseURL with fallback to https://chatgpt.com in NewClient | size:S
- [x] S1.2.2: Make ImpersonateChrome() conditional on cfg.DisableImpersonate | size:S

### T1.3: Refactor cmd/server/main.go | agent:Commander
- [x] S1.3.1: Remove init() and global localConfig; add loadConfig() function | size:M
- [x] S1.3.2: Add ServerConfig struct and newHandler() function | size:M
- [x] S1.3.3: Add CLI flags: -config, -addr, -model, -base-url | size:S
- [x] S1.3.4: Rename Choice.Status to Choice.FinishReason | size:S
- [x] S1.3.5: Add messagesToPrompt() helper | size:S
- [x] S1.3.6: Add newSentinelClient() helper that passes BaseURL | size:S
- [x] S1.3.7: Fix streaming error behavior (pre-chunk vs mid-stream errors) | size:M
- [x] S1.3.8: Wire main() with flags → loadConfig → newHandler → ListenAndServe | size:S

### T1.4: Update cmd/chat/main.go | agent:Commander
- [x] S1.4.1: Add -base-url flag and pass BaseURL to NewClient | size:S

## M2: Configuration and Documentation | status: completed
### T2.1: Create config.example.json | agent:Commander
- [x] S2.1.1: Create config.example.json with placeholder values | size:S

### T2.2: Update README.md | agent:Commander
- [x] S2.2.1: Fix default model mismatch (gpt-5-4-thinking → gpt-5-5-thinking) | size:S
- [x] S2.2.2: Add setup, test, CLI, server, curl instructions | size:M
- [x] S2.2.3: Add warning not to commit config.json | size:S
- [x] S2.2.4: Add note about fake local backends in tests | size:S

## M3: Unit Tests | status: completed
### T3.1: Create client_test.go | agent:Commander | depends:T1.2
- [x] S3.1.1: Test NewClient with defaults | size:S
- [x] S3.1.2: Test ResetSession, SetModel, SetTempMode, SetDisableAutoImage | size:S
- [x] S3.1.3: Test GetSessionInfo | size:S
- [x] S3.1.4: Test Config.BaseURL is honored | size:S
- [x] S3.1.5: Test Config.DisableImpersonate allows httptest | size:S

### T3.2: Create utils_test.go | agent:Commander
- [x] S3.2.1: Test runeSlice, orDefault, truncateStr | size:S
- [x] S3.2.2: Test extractFileID, getNestedString, getFirstStringPart | size:S
- [x] S3.2.3: Test parseStreamHandoff, parseWSFrames | size:S

### T3.3: Create chat_sse_test.go | agent:Commander
- [x] S3.3.1: Test processDeltaSSE - top-level append patch (format A) | size:S
- [x] S3.3.2: Test processDeltaSSE - simple v text (format B) | size:S
- [x] S3.3.3: Test processDeltaSSE - message object with assistant final (format C) | size:S
- [x] S3.3.4: Test processDeltaSSE - patch array (format D) | size:S
- [x] S3.3.5: Test processFullSSE | size:S
- [x] S3.3.6: Test processWSMessage | size:S
- [x] S3.3.7: Test LastAssistantMsgID updates on assistant message | size:S

## M4: Integration Tests | status: completed
### T4.1: Create integration_fake_backend_test.go | agent:Commander | depends:T1.2
- [x] S4.1.1: Build fake httptest.Server with all 5 endpoints | size:L
- [x] S4.1.2: Test client.Chat("ping") returns expected text and session state | size:M

### T4.2: Create cmd/server/main_test.go | agent:Commander | depends:T1.3,T4.1
- [x] S4.2.1: Test GET returns 405 | size:S
- [x] S4.2.2: Test malformed JSON returns 400 | size:S
- [x] S4.2.3: Test empty messages returns 400 | size:S
- [x] S4.2.4: Test missing auth returns 401 | size:S
- [x] S4.2.5: Test dummy token falls back to config | size:S
- [x] S4.2.6: Test non-streaming request returns OpenAI-shaped JSON | size:S
- [x] S4.2.7: Test streaming request returns SSE chunks and [DONE] | size:S

## M5: Build, Format, and Verify | status: completed
### T5.1: Format and tidy | agent:Commander | depends:M1,M2,M3,M4
- [x] S5.1.1: Run gofmt -w on all modified/created files | size:S
- [x] S5.1.2: Run go mod tidy | size:S

### T5.2: Test and build | agent:Commander | depends:T5.1
- [x] S5.2.1: Run go test ./... - all pass (50 tests) | size:M
- [x] S5.2.2: Run go build ./cmd/chat - success | size:S
- [x] S5.2.3: Run go build ./cmd/server - success | size:S
- [x] S5.2.4: Smoke test CLI with missing config | size:S
- [x] S5.2.5: Smoke test server with missing config | size:S

### T5.3: Comments translated to English | status: completed
- [x] S5.3.1: All Chinese comments in types.go, client.go, auth.go, chat.go, utils.go, image.go translated | size:M
