# ChatGPT Sentinel - Request Architecture & Auto-Update Guide

This document describes how ChatGPT requests are precisely formed, which files are involved, and how to verify/monitor changes using Chrome DevTools and automated tools like anything-analyzer and mitmproxy.

## Table of Contents

- [Complete Request Flow](#complete-request-flow)
- [File Responsibility Matrix](#file-responsibility-matrix)
- [Verification Using Chrome DevTools](#verification-using-chrome-devtools)
- [What Changes & How to Detect](#what-changes--how-to-detect)
- [Anything-Analyzer Integration](#anything-analyzer-integration)
- [Mitmproxy Setup](#mitmproxy-setup)
- [Auto-Update Implementation Strategy](#auto-update-implementation-strategy)
- [Monitoring Checklist](#monitoring-checklist)

---

## Complete Request Flow

### High-Level Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                    ChatGPT Request Generation Flow                 │
└─────────────────────────────────────────────────────────────────────┘

1. INITIAL SETUP (client.go)
   ├─ Browser fingerprint generation
   ├─ HTTP client with Chrome TLS impersonation
   └─ Common headers preparation

2. CONDUIT TOKEN (auth.go → getConduitToken)
   ├─ POST /backend-api/f/conversation/prepare
   ├─ Request body: model, partial_query, conversation_mode
   └─ Response: conduit_token

3. SENTINEL TOKEN + PoW (auth.go → getSentinelToken)
   ├─ POST /backend-api/sentinel/chat-requirements/prepare
   ├─ Brute-force FNV-1a hash (if PoW required)
   ├─ POST /backend-api/sentinel/chat-requirements/finalize
   └─ Response: sentinel_token + proof_token

4. WEBSOCKET CONNECTION (chat.go → dialChatWS)
   ├─ GET /backend-api/celsius/ws/user → websocket_url
   ├─ WebSocket handshake with browser headers
   └─ Subscribe to 4 topics: connect, calpico-chatgpt, conversations, app_notifications

5. MAIN CONVERSATION REQUEST (chat.go → streamConversation)
   ├─ POST /backend-api/f/conversation
   ├─ Headers: openai-sentinel-chat-requirements-token, x-conduit-token, proof-token
   ├─ Body: messages, model, conversation_mode, thinking_effort
   └─ Response: SSE streaming with delta encoding

6. STREAMING HANDOFF (chat.go → subscribeWSStream)
   ├─ Parse SSE events: delta_encoding, stream_handoff
   ├─ Subscribe to WebSocket topic from handoff
   └─ Continue streaming via WebSocket
```

### Detailed Request Flow

#### Step 1: Conduit Token Acquisition
**File**: `auth.go` → `getConduitToken()` (lines 11-67)

**Endpoint**: `POST https://chatgpt.com/backend-api/f/conversation/prepare`

**Request Headers**:
```http
Accept: */*
Content-Type: application/json
x-conduit-token: no-token
x-oai-turn-trace-id: <uuid>
x-openai-target-path: /backend-api/f/conversation/prepare
x-openai-target-route: /backend-api/f/conversation/prepare
```

**Request Body**:
```json
{
  "action": "next",
  "fork_from_shared_post": false,
  "parent_message_id": "client-created-root",
  "model": "gpt-5-5-thinking",
  "timezone_offset_min": -480,
  "timezone": "Asia/Shanghai",
  "conversation_mode": {"kind": "primary_assistant"},
  "system_hints": [],
  "partial_query": {
    "id": "<uuid>",
    "author": {"role": "user"},
    "content": {
      "content_type": "text",
      "parts": ["h"]  // First 5 runes of user message
    }
  },
  "supports_buffering": true,
  "supported_encodings": ["v1"],
  "client_contextual_info": {"app_name": "chatgpt.com"},
  "thinking_effort": "standard"
}
```

**Response**:
```json
{
  "status": "success",
  "conduit_token": "<conduit_token>"
}
```

---

#### Step 2: Sentinel Token + Proof of Work
**File**: `auth.go` → `getSentinelToken()` (lines 69-174)

**2a. Prepare Request**
**Endpoint**: `POST https://chatgpt.com/backend-api/sentinel/chat-requirements/prepare`

**Request Headers**:
```http
Accept: */*
Content-Type: application/json
x-openai-target-path: /backend-api/sentinel/chat-requirements/prepare
x-openai-target-route: /backend-api/sentinel/chat-requirements/prepare
```

**Request Body**:
```json
{
  "p": "gAAAAAC<base64_encoded_fingerprint_config>"
}
```

**Fingerprint Config Array** (`utils.go` → `buildCfg()`, lines 44-66):
```javascript
[
  3000,                                    // [0] Constant
  "Fri Jun 06 2025 18:00:00 GMT+0800 (CST)", // [1] JS Date.toString()
  4294967296,                              // [2] Constant
  null,                                    // [3] PoW counter (for brute force)
  "Mozilla/5.0...",                       // [4] User-Agent
  "",                                      // [5] Empty string
  "prod-81e0c5cdf6140e8c5db714d613337f4aeab94029", // [6] Build hash
  "zh-CN",                                 // [7] Language
  "zh-CN,en,en-GB,en-US",                 // [8] Language list
  null,                                    // [9] Performance timing
  "credentials\u2252[object Navigator]",   // [10] Credentials check
  "location",                              // [11] Location check
  "fetch",                                 // [12] Fetch check
  123.456,                                 // [13] Performance.now()
  "<uuid>",                                // [14] Session ID
  "",                                      // [15] Empty string
  28,                                      // [16] Constant
  1717660800000,                           // [17] Timestamp
  0, 0, 0, 0, 0, 0, 0                      // [18-24] Zeros
]
```

**Response**:
```json
{
  "persona": "default",
  "proofofwork": {
    "required": true,
    "seed": "abc123...",
    "difficulty": "0000"
  },
  "turnstile": {"required": false},
  "prepare_token": "<prepare_token>"
}
```

**2b. Proof of Work (if required)**
**File**: `utils.go` → `fnvHash()` (lines 29-41)

**Algorithm**:
```go
// FNV-1a variant hash (JavaScript rEe function)
hash = 2166136261  // FNV offset basis
for each byte in (seed + base64_config):
    hash ^= byte
    hash *= 16777619  // FNV prime
hash ^= hash >> 16
hash *= 2246822507
hash ^= hash >> 13
hash *= 3266489909
hash ^= hash >> 16
return hex(hash)
```

**Brute Force** (auth.go:118-136):
```go
for r := 0; r < 500000; r++ {
    cfg[3] = r                    // Set PoW counter
    cfg[9] = elapsed_ms          // Performance timing
    encoded = base64(json(cfg))
    hash = fnvHash(seed + encoded)
    if hash[:difficulty_length] <= difficulty:
        proof_token = "gAAAAAB" + encoded + "~S"
        break
}
```

**2c. Finalize Request**
**Endpoint**: `POST https://chatgpt.com/backend-api/sentinel/chat-requirements/finalize`

**Request Headers**:
```http
Accept: */*
Content-Type: application/json
x-openai-target-path: /backend-api/sentinel/chat-requirements/finalize
x-openai-target-route: /backend-api/sentinel/chat-requirements/finalize
```

**Request Body**:
```json
{
  "prepare_token": "<prepare_token>",
  "proofofwork": "gAAAAAB<base64_encoded_config_with_pow>~S"
}
```

**Response**:
```json
{
  "token": "<sentinel_token>",
  "expire_after": 300
}
```

---

#### Step 3: WebSocket Connection
**File**: `chat.go` → `getWsURL()` (lines 136-160) → `dialChatWS()` (lines 163-198)

**3a. Get WebSocket URL**
**Endpoint**: `GET https://chatgpt.com/backend-api/celsius/ws/user`

**Request Headers**:
```http
Accept: */*
x-openai-target-path: /backend-api/celsius/ws/user
x-openai-target-route: /backend-api/celsius/ws/user
```

**Response**:
```json
{
  "websocket_url": "wss://r.chatgpt.com/backend-api/ws/..."
}
```

**3b. WebSocket Handshake**
**Headers**:
```http
User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36...
Origin: https://chatgpt.com
```

**Initialization Messages** (chat.go:182-190):
```json
[
  {"id": 1, "command": {"type": "connect", "presence": {"type": "presence", "state": "background"}}},
  {"id": 2, "command": {"type": "subscribe", "topic_id": "calpico-chatgpt"}},
  {"id": 3, "command": {"type": "subscribe", "topic_id": "conversations"}},
  {"id": 4, "command": {"type": "subscribe", "topic_id": "app_notifications"}}
]
```

---

#### Step 4: Main Conversation Request
**File**: `chat.go` → `streamConversation()` (lines 208-334)

**Endpoint**: `POST https://chatgpt.com/backend-api/f/conversation`

**Request Headers**:
```http
Accept: text/event-stream
Content-Type: application/json
openai-sentinel-chat-requirements-token: <sentinel_token>
x-conduit-token: <conduit_token>
x-oai-turn-trace-id: <uuid>
x-openai-target-path: /backend-api/f/conversation
x-openai-target-route: /backend-api/f/conversation
openai-sentinel-proof-token: <proof_token>  // if PoW required
```

**Request Body**:
```json
{
  "action": "next",
  "messages": [{
    "id": "<uuid>",
    "author": {"role": "user"},
    "create_time": 1717660800.123,
    "content": {
      "content_type": "text",
      "parts": ["Your message here"]
    },
    "metadata": {
      "developer_mode_connector_ids": [],
      "selected_sources": [],
      "selected_github_repos": [],
      "selected_all_github_repos": false,
      "serialization_metadata": {"custom_symbol_offsets": []}
    }
  }],
  "parent_message_id": "<conversation_parent_id>",
  "model": "gpt-5-5-thinking",
  "timezone_offset_min": -480,
  "timezone": "Asia/Shanghai",
  "conversation_mode": {"kind": "primary_assistant"},
  "enable_message_followups": true,
  "system_hints": [],
  "supports_buffering": true,
  "supported_encodings": ["v1"],
  "client_contextual_info": {
    "is_dark_mode": false,
    "time_since_loaded": 12345,
    "page_height": 1014,
    "page_width": 1055,
    "pixel_ratio": 1,
    "screen_height": 1080,
    "screen_width": 1920,
    "app_name": "chatgpt.com"
  },
  "history_and_training_disabled": false,
  "paragen_cot_summary_display_override": "allow",
  "force_parallel_switch": "auto",
  "thinking_effort": "standard"
}
```

**Response (SSE Stream)**:
```
event: delta_encoding
data: {"type":"delta_encoding","content_type":"text"}

event: delta
data: {"p":"/message/content/parts/0","o":"append","v":"Hello"}

event: delta
data: {"p":"/message/content/parts/0","o":"append","v":" world"}

event: stream_handoff
data: {"type":"stream_handoff","options":[{"type":"subscribe_ws_topic","topic_id":"conversation-turn-xxx"}]}

event: done
data: [DONE]
```

---

#### Step 5: WebSocket Handoff
**File**: `chat.go` → `subscribeWSStream()` (lines 487-559)

**Subscribe to Topic**:
```json
{
  "id": 5,
  "command": {
    "type": "subscribe",
    "topic_id": "conversation-turn-xxx",
    "offset": "0"
  }
}
```

**WebSocket Response** (lines 520-555):
```json
[
  {
    "type": "reply",
    "reply": {
      "topic_id": "conversation-turn-xxx",
      "catchups": [
        {
          "payload": {
            "payload": {
              "encoded_item": "event: delta\ndata: {...}\n\n"
            }
          }
        }
      ]
    }
  }
]
```

---

## File Responsibility Matrix

| File | Primary Function | Key Functions | Lines to Monitor |
|------|------------------|---------------|------------------|
| **auth.go** | Authentication flow | `getConduitToken()`<br>`getSentinelToken()` | 11-67, 69-174 |
| **chat.go** | Main conversation logic | `ChatStream()`<br>`streamConversation()`<br>`dialChatWS()`<br>`subscribeWSStream()` | 23-133, 208-334, 163-198, 487-559 |
| **client.go** | HTTP client setup | `commonHeaders()`<br>`NewClient()` | 49-79, 132-161 |
| **utils.go** | Utilities & helpers | `buildCfg()`<br>`fnvHash()`<br>`GenerateUUID()` | 44-66, 29-41, 13-20 |
| **image.go** | Image handling | `DownloadImageByFileID()`<br>`PollAndDownloadImage()` | 98-149, 12-95 |
| **cookies.go** | Cookie management | `ResolveCookieString()`<br>`ExtractBrowserCookies()` | 70-99, (continued) |

### File Dependency Graph

```
client.go (HTTP setup)
    ├── commonHeaders() → provides authentication headers
    ├── NewClient() → initializes http.Client with impersonation
    └── ↓
        ↓
auth.go (authentication)
    ├── getConduitToken() → conduit token for conversation request
    ├── getSentinelToken() → sentinel + proof tokens
    │   ├── buildCfg() [from utils.go] → fingerprint config
    │   └── fnvHash() [from utils.go] → PoW computation
    └── ↓
        ↓
chat.go (conversation flow)
    ├── dialChatWS() → WebSocket connection
    │   └── getWsURL() → fetches WebSocket endpoint
    ├── streamConversation() → main POST request
    └── subscribeWSStream() → WebSocket continuation
        └── parseWSFrames() → WebSocket message parsing
utils.go (utilities)
    ├── buildCfg() → browser fingerprint config
    ├── fnvHash() → FNV-1a hash for PoW
    ├── GenerateUUID() → generates unique identifiers
    └── encodeBase64JSON() → encodes config for sentinel
```

---

## Verification Using Chrome DevTools

### Step-by-Step Verification

#### 1. Open chatgpt.com and Start DevTools
```
1. Navigate to https://chatgpt.com
2. Press F12 to open Developer Tools
3. Go to Network tab
4. Filter by "Fetch/XHR" or "WS" (WebSocket)
5. Send a message in ChatGPT
```

#### 2. Verify Conduit Token Request

**In Network tab**:
```
Look for: f/conversation/prepare

Request Headers to verify:
  x-conduit-token: no-token
  x-oai-turn-trace-id: <uuid>
  x-openai-target-path: /backend-api/f/conversation/prepare
  x-openai-target-route: /backend-api/f/conversation/prepare

Request Body to verify:
  {
    "action": "next",
    "fork_from_shared_post": false,
    "parent_message_id": "client-created-root",
    "model": "gpt-5-5-thinking",
    "partial_query": {
      "parts": ["h"]  // First 5 chars of your message
    },
    "thinking_effort": "standard"
  }

Compare with: auth.go lines 16-37
```

#### 3. Verify Sentinel Prepare Request

**In Network tab**:
```
Look for: sentinel/chat-requirements/prepare

Request Headers to verify:
  x-openai-target-path: /backend-api/sentinel/chat-requirements/prepare
  x-openai-target-route: /backend-api/sentinel/chat-requirements/prepare

Request Body to verify:
  {
    "p": "gAAAAAC<base64_encoded>"
  }

Decode the base64 and verify it matches buildCfg() output:
  - Index 0: 3000
  - Index 1: Date string in JS format
  - Index 2: 4294967296
  - Index 4: User-Agent
  - Index 6: Build hash
  - Index 7: Language

Response to verify:
  {
    "proofofwork": {
      "required": true,
      "seed": "...",
      "difficulty": "0000"
    },
    "prepare_token": "..."
  }

Compare with: auth.go lines 78-112, utils.go lines 44-66
```

#### 4. Verify Sentinel Finalize Request

**In Network tab**:
```
Look for: sentinel/chat-requirements/finalize

Request Body to verify:
  {
    "prepare_token": "...",
    "proofofwork": "gAAAAAB<base64>~S"
  }

Response to verify:
  {
    "token": "<sentinel_token>",
    "expire_after": 300
  }

Compare with: auth.go lines 145-173
```

#### 5. Verify WebSocket Connection

**In Network tab**:
```
Look for: celsius/ws/user

Request Headers to verify:
  x-openai-target-path: /backend-api/celsius/ws/user
  x-openai-target-route: /backend-api/celsius/ws/user

Response to verify:
  {
    "websocket_url": "wss://r.chatgpt.com/backend-api/ws/..."
  }

Then look for "WS" tab and verify handshake:
  User-Agent: Mozilla/5.0...
  Origin: https://chatgpt.com

Messages sent after connection:
  1. {"id":1,"command":{"type":"connect",...}}
  2. {"id":2,"command":{"type":"subscribe","topic_id":"calpico-chatgpt"}}
  3. {"id":3,"command":{"type":"subscribe","topic_id":"conversations"}}
  4. {"id":4,"command":{"type":"subscribe","topic_id":"app_notifications"}}

Compare with: chat.go lines 136-198
```

#### 6. Verify Main Conversation Request

**In Network tab**:
```
Look for: f/conversation

Request Headers to verify:
  Accept: text/event-stream
  openai-sentinel-chat-requirements-token: <sentinel_token>
  x-conduit-token: <conduit_token>
  x-oai-turn-trace-id: <uuid>
  openai-sentinel-proof-token: <proof_token>  // if PoW required

Request Body to verify:
  {
    "action": "next",
    "messages": [{
      "id": "<uuid>",
      "author": {"role": "user"},
      "content": {"content_type": "text", "parts": ["your message"]},
      "metadata": {...}
    }],
    "model": "gpt-5-5-thinking",
    "conversation_mode": {"kind": "primary_assistant"},
    "thinking_effort": "standard"
  }

Response to verify (SSE stream):
  event: delta_encoding
  data: {"type":"delta_encoding","content_type":"text"}

  event: delta
  data: {"p":"/message/content/parts/0","o":"append","v":"text"}

  event: stream_handoff
  data: {"type":"stream_handoff","options":[{"type":"subscribe_ws_topic","topic_id":"..."}]}

  event: done
  data: [DONE]

Compare with: chat.go lines 208-334
```

#### 7. Verify WebSocket Handoff

**In Network tab → WS tab**:
```
Look for messages after stream_handoff:

Subscribe message:
  {
    "id": 5,
    "command": {
      "type": "subscribe",
      "topic_id": "conversation-turn-xxx",
      "offset": "0"
    }
  }

Reply messages:
  {
    "type": "reply",
    "reply": {
      "topic_id": "conversation-turn-xxx",
      "catchups": [
        {
          "payload": {
            "payload": {
              "encoded_item": "event: delta\ndata: {...}\n\n"
            }
          }
        }
      ]
    }
  }

Compare with: chat.go lines 487-559
```

### Timing Verification

Expected order and timing (typical values):
```
1. f/conversation/prepare      →  0ms
2. sentinel/chat-requirements/prepare →  50ms
3. PoW brute force              →  100-5000ms (varies)
4. sentinel/chat-requirements/finalize →  10ms
5. celsius/ws/user              →  20ms
6. WebSocket handshake          →  100ms
7. f/conversation (SSE start)   →  200ms
8. stream_handoff               →  variable
9. WebSocket streaming         →  continues
```

---

## What Changes & How to Detect

### High-Frequency Changes

#### 1. API Endpoints
**What changes**: Path names, version numbers, new endpoints
**How to detect**:
- Monitor 404/403 errors in responses
- Compare endpoint URLs with baseline
- Watch for new endpoints in DevTools
- Use anything-analyzer to detect new patterns

**Examples**:
```
Old: /backend-api/f/conversation
New: /backend-api/v2/f/conversation

Old: /backend-api/sentinel/chat-requirements/prepare
New: /backend-api/sentinel/v2/requirements
```

#### 2. Request Body Structure
**What changes**: Field names, required fields, removed fields
**How to detect**:
- Monitor 400 Bad Request errors
- Compare request body JSON structure
- Watch for new fields in DevTools
- Use AI analysis to detect schema changes

**Examples**:
```
Old: {"model": "gpt-5-5-thinking"}
New: {"model": "gpt-5-5-thinking", "model_version": "latest"}

Old: {"thinking_effort": "standard"}
New: {"thinking_effort": "standard", "thinking_mode": "auto"}
```

#### 3. Authentication Tokens
**What changes**: Token format, encryption, expiration
**How to detect**:
- Monitor 401 Unauthorized errors
- Check token length and format
- Verify token expiration times
- Use anything-analyzer to track token lifecycle

**Examples**:
```
Old: "gAAAAAC<base64>"
New: "gAAAAAB<base64>"  // Different prefix

Old: 300 seconds expiration
New: 600 seconds expiration
```

#### 4. PoW Algorithm
**What changes**: Difficulty, seed format, hash function
**How to detect**:
- Monitor PoW computation time
- Check difficulty string format
- Verify hash algorithm output
- Use mitmproxy to capture exact PoW responses

**Examples**:
```
Old: difficulty: "0000", seed: "abc123"
New: difficulty: "00000", seed: "xyz789"

Old: FNV-1a hash
New: SHA-256 hash
```

#### 5. HTTP Headers
**What changes**: New headers, changed header names, new header values
**How to detect**:
- Compare request headers with baseline
- Monitor for missing headers causing errors
- Use anything-analyzer to detect new security headers

**Examples**:
```
Old: x-conduit-token
New: x-oai-conduit-token

New header: x-openai-request-id: <uuid>
```

#### 6. Fingerprint Configuration
**What changes**: Array structure, values, field order
**How to detect**:
- Decode base64 in sentinel prepare request
- Compare array structure with baseline
- Use AI analysis to detect fingerprint changes

**Examples**:
```
Old: [3000, date, 4294967296, null, ua, "", build_hash, lang, ...]
New: [3000, date, 4294967296, null, ua, "", build_hash, lang, ..., new_field]

Old: Index 6: "prod-81e0c5cdf6140e8c5db714d613337f4aeab94029"
New: Index 6: "prod-92f1d6eeg7251f9dlec825e724448g5bfcb1513a"
```

#### 7. SSE Event Formats
**What changes**: Event types, data structure, delta encoding
**How to detect**:
- Monitor SSE events in DevTools
- Compare event types with baseline
- Check for new delta encoding formats

**Examples**:
```
Old: event: delta, data: {"p":"/message/content/parts/0","o":"append","v":"text"}
New: event: delta, data: {"path":"/message/content/parts/0","op":"append","value":"text"}

New event type: event: thinking_start
```

#### 8. WebSocket Protocol
**What changes**: Handoff logic, subscription topics, message format
**How to detect**:
- Monitor WebSocket messages in DevTools
- Compare subscription topics
- Check for new message types

**Examples**:
```
Old: topic_id: "conversation-turn-xxx"
New: topic_id: "v2/conversation-turn-xxx"

Old: {"type":"reply","reply":{...}}
New: {"type":"response_v2","response":{...}}
```

### Medium-Frequency Changes

#### 9. Browser Fingerprinting
**What changes**: User-Agent, build hash, sec-ch-ua headers
**How to detect**:
- Update default constants in client.go
- Monitor for 403 errors with fingerprint mismatch

#### 10. TLS Fingerprint
**What changes**: JA3/JA4 fingerprint, TLS versions, cipher suites
**How to detect**:
- Monitor for 403 errors after successful authentication
- Update imroc/req/v3 library for new Chrome versions

### Low-Frequency Changes

#### 11. WebSocket URL Format
**What changes**: Domain structure, path format
**How to detect**:
- Monitor celsius/ws/user response
- Update URL parsing logic

#### 12. Image Generation Protocol
**What changes**: Download URLs, asset_pointer format
**How to detect**:
- Monitor image download requests
- Update image.go logic

---

## Anything-Analyzer Integration

### Overview

**Anything-Analyzer** (Mouseww/anything-analyzer) is a comprehensive protocol analysis toolkit that provides:

1. **Browser Capture** via Chrome DevTools Protocol (CDP)
2. **MITM Proxy** for HTTPS traffic interception
3. **JS Hook Injection** for monitoring encryption calls
4. **AI Analysis** for automatic protocol reverse engineering
5. **MCP Server** for seamless AI agent integration

### Installation

```bash
# Download from GitHub Releases
# https://github.com/Mouseww/anything-analyzer/releases

# Windows
Anything-Analyzer-Setup-x.x.x.exe

# macOS
Anything-Analyzer-x.x.x-arm64.dmg

# Linux
Anything-Analyzer-x.x.x.AppImage
```

### Setup for ChatGPT Monitoring

#### 1. Configure LLM

```
Settings → LLM
Add OpenAI/Anthropic API key for analysis
```

#### 2. Browser Capture Setup

```
1. New Session → Name: "chatgpt-monitoring"
2. URL: https://chatgpt.com
3. Click "Start Capture"
4. In embedded browser, log in and send a message
5. Stop capture
```

#### 3. MITM Proxy Setup

```
Settings → MITM Proxy
1. Install CA Certificate (required for HTTPS)
2. Enable proxy (default: 127.0.0.1:8888)
3. Configure ChatGPT.com to use proxy or set system proxy
```

#### 4. AI Analysis

```
1. Select captured requests
2. Click "Analyze"
3. Choose analysis mode:
   - "自动识别" (Auto Detect) - General analysis
   - "API 逆向" (API Reverse Engineer) - For API changes
   - "JS 加密逆向" (JS Crypto Reverse) - For encryption changes
   - "安全审计" (Security Audit) - For security changes
4. Review generated report
5. Ask follow-up questions via AI chat
```

### MCP Server Integration

**Anything-Analyzer exposes an MCP Server** that can be connected by AI agents (Claude Desktop, Cursor, etc.):

```
Settings → MCP
1. Enable MCP Server
2. Note the server URL (usually stdio:// or http://localhost:port)
3. Configure Claude Desktop to connect:
   {
     "mcpServers": {
       "anything-analyzer": {
         "command": "path/to/anything-analyzer",
         "args": ["--mcp"]
       }
     }
   }
```

### MCP Tools Available

Once connected, AI agents can call these tools:

```json
{
  "tools": [
    {
      "name": "capture_browser_traffic",
      "description": "Start/stop browser capture via CDP",
      "inputSchema": {
        "type": "object",
        "properties": {
          "url": {"type": "string"},
          "duration": {"type": "number"}
        }
      }
    },
    {
      "name": "analyze_protocol",
      "description": "AI analysis of captured traffic",
      "inputSchema": {
        "type": "object",
        "properties": {
          "mode": {
            "type": "string",
            "enum": ["auto", "api_reverse", "crypto", "security"]
          },
          "focus": {
            "type": "string",
            "enum": ["authentication", "request_format", "response_format", "all"]
          }
        }
      }
    },
    {
      "name": "compare_with_baseline",
      "description": "Compare current capture with stored baseline",
      "inputSchema": {
        "type": "object",
        "properties": {
          "baseline_id": {"type": "string"}
        }
      }
    },
    {
      "name": "export_captured_data",
      "description": "Export captured requests/responses as JSON",
      "inputSchema": {
        "type": "object",
        "properties": {
          "format": {"type": "string", "enum": ["json", "har", "mitmproxy"]}
        }
      }
    }
  ]
}
```

### Automated Change Detection Workflow

```
┌──────────────────────────────────────────────────────────────────┐
│  Step 1: Baseline Capture                                        │
├──────────────────────────────────────────────────────────────────┤
│  1. Run anything-analyzer                                       │
│  2. Capture baseline traffic (when working)                    │
│  3. Export baseline as JSON/HAR                                │
│  4. Store baseline with timestamp and version                  │
└──────────────────────────────────────────────────────────────────┘
                         ↓
┌──────────────────────────────────────────────────────────────────┐
│  Step 2: Periodic Monitoring                                     │
├──────────────────────────────────────────────────────────────────┤
│  1. Schedule daily/weekly capture runs                          │
│  2. Use anything-analyzer's "compare_with_baseline" tool        │
│  3. Detect differences in:                                      │
│     - Endpoints                                                  │
│     - Request/response formats                                   │
│     - Headers                                                    │
│     - Authentication flows                                       │
│     - PoW parameters                                             │
└──────────────────────────────────────────────────────────────────┘
                         ↓
┌──────────────────────────────────────────────────────────────────┐
│  Step 3: AI Analysis of Changes                                 │
├──────────────────────────────────────────────────────────────────┤
│  1. Use "analyze_protocol" tool with mode "api_reverse"        │
│  2. Focus on changed areas (auth, request_format, etc.)        │
│  3. AI generates:                                               │
│     - Detailed change report                                    │
│     - Code update suggestions                                   │
│     - Migration guide                                           │
└──────────────────────────────────────────────────────────────────┘
                         ↓
┌──────────────────────────────────────────────────────────────────┐
│  Step 4: Auto-Update (via MCP + AI Agent)                       │
├──────────────────────────────────────────────────────────────────┤
│  1. AI Agent receives change report via MCP                     │
│  2. Agent generates code patches:                               │
│     - auth.go: Update token formats, PoW logic                 │
│     - chat.go: Update request bodies, SSE parsing               │
│     - client.go: Update headers                                 │
│     - utils.go: Update fingerprint config                       │
│  3. Agent applies patches                                       │
│  4. Agent runs tests to verify                                   │
│  5. Agent commits changes with descriptive message              │
└──────────────────────────────────────────────────────────────────┘
```

### Example AI Analysis Output

**Input**: Captured traffic showing new endpoint

**AI Analysis Report**:
```markdown
## API Change Detected

### New Endpoint
- **Old**: POST /backend-api/f/conversation
- **New**: POST /backend-api/v2/f/conversation

### Request Body Changes
- **Added field**: `model_version` (required)
- **Changed field**: `thinking_effort` now accepts: "low", "standard", "high"
- **Removed field**: `paragen_cot_summary_display_override`

### Header Changes
- **New header**: `x-openai-api-version: v2`
- **Changed header**: `openai-sentinel-chat-requirements-token` renamed to `x-oai-sentinel-token`

### Impact Assessment
- **Breaking change**: Yes, endpoint change breaks existing code
- **Migration required**: Update all `/f/conversation` calls to `/v2/f/conversation`
- **Risk level**: High (authentication also affected)

### Code Updates Required

#### 1. auth.go
```diff
- endpoint := "/backend-api/f/conversation/prepare"
+ endpoint := "/backend-api/v2/f/conversation/prepare"
```

#### 2. chat.go
```diff
+ headers["x-openai-api-version"] = "v2"
+ body["model_version"] = "latest"
- body["paragen_cot_summary_display_override"] = "allow"
```

#### 3. client.go
```diff
- headers["openai-sentinel-chat-requirements-token"] = token
+ headers["x-oai-sentinel-token"] = token
```

### Testing Recommendations
1. Test with new endpoint format
2. Verify model_version field handling
3. Test all thinking_effort values
4. Monitor for 404/400 errors
```

### JS Hook Integration

**Anything-Analyzer can inject JS hooks** to monitor encryption calls:

```javascript
// Auto-injected hooks for ChatGPT monitoring
// These hooks are injected into the browser context

// Hook 1: fetch interceptor
const originalFetch = window.fetch;
window.fetch = function(...args) {
  console.log('[FETCH] URL:', args[0]);
  console.log('[FETCH] Options:', args[1]);
  return originalFetch.apply(this, args);
};

// Hook 2: WebSocket interceptor
const originalWS = window.WebSocket;
window.WebSocket = function(...args) {
  console.log('[WS] URL:', args[0]);
  const ws = new originalWS(...args);
  const originalSend = ws.send;
  ws.send = function(data) {
    console.log('[WS] Send:', data);
    return originalSend.apply(this, arguments);
  };
  return ws;
};

// Hook 3: crypto.subtle interceptor
const originalSubtle = window.crypto.subtle;
window.crypto.subtle = new Proxy(originalSubtle, {
  get(target, prop) {
    if (prop === 'digest' || prop === 'encrypt' || prop === 'sign') {
      return function(...args) {
        console.log('[CRYPTO]', prop, args);
        return target[prop].apply(target, args);
      };
    }
    return target[prop];
  }
});
```

**These hooks capture**:
- All API requests (including internal requests)
- WebSocket messages
- Cryptographic operations (useful for detecting encryption changes)

---

## Mitmproxy Setup

### Installation

```bash
# Python package
pip install mitmproxy

# Or download binaries
# https://mitmproxy.org/downloads/
```

### Basic Setup

```bash
# Start interactive proxy
mitmproxy

# Start web interface
mitmweb

# Start CLI mode
mitmcli
```

### Custom Script for ChatGPT Monitoring

Create `chatgpt_monitor.py`:

```python
#!/usr/bin/env python3
from mitmproxy import http, ctx

class ChatGPTMonitor:
    def __init__(self):
        self.baseline = {}
        self.changes = []

    def request(self, flow: http.HTTPFlow):
        """Monitor outgoing requests"""
        url = flow.request.pretty_url

        # Only monitor chatgpt.com requests
        if "chatgpt.com" not in url:
            return

        # Log endpoint
        endpoint = flow.request.path
        ctx.log.info(f"[REQUEST] {flow.request.method} {endpoint}")

        # Check for new endpoints
        if endpoint not in self.baseline and "/backend-api/" in endpoint:
            self.changes.append({
                "type": "new_endpoint",
                "endpoint": endpoint,
                "method": flow.request.method,
                "headers": dict(flow.request.headers)
            })
            ctx.log.warn(f"[NEW ENDPOINT] {endpoint}")

        # Log authentication headers
        if "conduit-token" in str(flow.request.headers) or \
           "sentinel" in endpoint:
            ctx.log.info(f"[AUTH] Headers: {dict(flow.request.headers)}")

    def response(self, flow: http.HTTPFlow):
        """Monitor responses for errors and changes"""
        url = flow.request.pretty_url

        if "chatgpt.com" not in url:
            return

        # Check for errors
        if flow.response.status_code >= 400:
            ctx.log.error(f"[ERROR] {flow.request.method} {flow.request.path} - {flow.response.status_code}")
            ctx.log.error(f"[ERROR] Response: {flow.response.text[:200]}")

            # Log error details
            if flow.response.status_code in [401, 403]:
                self.changes.append({
                    "type": "auth_error",
                    "endpoint": flow.request.path,
                    "status": flow.response.status_code,
                    "response": flow.response.text[:500]
                })

        # Check sentinel responses
        if "sentinel" in flow.request.path:
            ctx.log.info(f"[SENTINEL] Response: {flow.response.text[:200]}")

    def websocket_message(self, flow: http.HTTPFlow):
        """Monitor WebSocket messages"""
        ctx.log.info(f"[WS] Message on {flow.request.path}")

addons = [ChatGPTMonitor()]
```

**Run with custom script**:
```bash
mitmproxy -s chatgpt_monitor.py
```

### Advanced Script with Baseline Comparison

Create `chatgpt_compare.py`:

```python
#!/usr/bin/env python3
from mitmproxy import http, ctx
import json
import hashlib
from datetime import datetime

class ChatGPTComparator:
    def __init__(self):
        self.baseline_file = "chatgpt_baseline.json"
        self.current_baseline = self.load_baseline()
        self.session = {
            "timestamp": datetime.now().isoformat(),
            "requests": [],
            "changes": []
        }

    def load_baseline(self):
        """Load baseline from file"""
        try:
            with open(self.baseline_file, "r") as f:
                return json.load(f)
        except FileNotFoundError:
            return {"endpoints": {}, "requests": {}}

    def save_baseline(self):
        """Save current state as baseline"""
        with open(self.baseline_file, "w") as f:
            json.dump(self.current_baseline, f, indent=2)

    def get_request_signature(self, flow: http.HTTPFlow):
        """Create unique signature for request"""
        sig_data = {
            "method": flow.request.method,
            "path": flow.request.path,
            "headers": dict(flow.request.headers),
            "body": flow.request.text if flow.request.text else ""
        }
        return hashlib.md5(json.dumps(sig_data, sort_keys=True).encode()).hexdigest()

    def request(self, flow: http.HTTPFlow):
        """Compare request with baseline"""
        url = flow.request.pretty_url
        if "chatgpt.com" not in url:
            return

        signature = self.get_request_signature(flow)
        endpoint = flow.request.path

        # Log request
        self.session["requests"].append({
            "endpoint": endpoint,
            "signature": signature,
            "timestamp": datetime.now().isoformat()
        })

        # Check if endpoint exists in baseline
        if endpoint not in self.current_baseline["endpoints"]:
            ctx.log.warn(f"[NEW ENDPOINT] {endpoint}")
            self.session["changes"].append({
                "type": "new_endpoint",
                "endpoint": endpoint
            })
            return

        # Compare with baseline
        baseline_sig = self.current_baseline["endpoints"][endpoint]
        if signature != baseline_sig:
            ctx.log.warn(f"[REQUEST CHANGED] {endpoint}")
            ctx.log.warn(f"  Old: {baseline_sig[:16]}...")
            ctx.log.warn(f"  New: {signature[:16]}...")

            self.session["changes"].append({
                "type": "request_changed",
                "endpoint": endpoint,
                "old_signature": baseline_sig,
                "new_signature": signature
            })

    def response(self, flow: http.HTTPFlow):
        """Monitor for breaking changes"""
        if "chatgpt.com" not in flow.request.pretty_url:
            return

        # Check for new error responses
        if flow.response.status_code >= 400:
            endpoint = flow.request.path
            if endpoint in self.current_baseline["endpoints"]:
                ctx.log.error(f"[BREAKING CHANGE] {endpoint} - {flow.response.status_code}")
                self.session["changes"].append({
                    "type": "breaking_change",
                    "endpoint": endpoint,
                    "status": flow.response.status_code
                })

    def done(self):
        """Save session and generate report"""
        # Update baseline with new requests
        for req in self.session["requests"]:
            if req["endpoint"] not in self.current_baseline["endpoints"]:
                self.current_baseline["endpoints"][req["endpoint"]] = req["signature"]

        self.save_baseline()

        # Generate report
        if self.session["changes"]:
            report_file = f"chatgpt_changes_{datetime.now().strftime('%Y%m%d_%H%M%S')}.json"
            with open(report_file, "w") as f:
                json.dump(self.session, f, indent=2)
            ctx.log.info(f"[REPORT] Changes saved to {report_file}")

addons = [ChatGPTComparator()]
```

**Run comparison script**:
```bash
mitmproxy -s chatgpt_compare.py
```

### Export Captured Traffic

```python
# Add to mitmproxy script
def done(self):
    """Export captured traffic as JSON"""
    export_file = "chatgpt_captured.json"
    with open(export_file, "w") as f:
        json.dump(self.session, f, indent=2)
    ctx.log.info(f"[EXPORT] Captured traffic saved to {export_file}")
```

---

## Auto-Update Implementation Strategy

### Phase 1: Monitoring Infrastructure

#### 1.1 Setup Anything-Analyzer MCP Server

```typescript
// Create MCP client to connect to anything-analyzer
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

const client = new Client({
  name: "chatgpt-sentinel-updater",
  version: "1.0.0"
}, {
  capabilities: {}
});

const transport = new StdioClientTransport({
  command: "anything-analyzer",
  args: ["--mcp"]
});

await client.connect(transport);
```

#### 1.2 Implement Change Detection Pipeline

```go
// cmd/auto-detect/main.go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "os/exec"
    "time"
)

type ChangeDetector struct {
    baselineFile  string
    anythingAnalyzer string
}

type ChangeReport struct {
    Timestamp     time.Time              `json:"timestamp"`
    Changes       []Change               `json:"changes"`
    Recommendations []string             `json:"recommendations"`
}

type Change struct {
    Type         string                 `json:"type"`
    Severity     string                 `json:"severity"`
    Description  string                 `json:"description"`
    AffectedFile string                 `json:"affected_file"`
    Details      map[string]interface{} `json:"details"`
}

func (d *ChangeDetector) DetectChanges(ctx context.Context) (*ChangeReport, error) {
    // Step 1: Start anything-analyzer browser capture
    cmd := exec.CommandContext(ctx, d.anythingAnalyzer, "--capture", "chatgpt.com")
    output, err := cmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("capture failed: %w", err)
    }

    // Step 2: Analyze captured traffic
    analysisCmd := exec.CommandContext(ctx, d.anythingAnalyzer, "--analyze", "api_reverse", string(output))
    analysis, err := analysisCmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("analysis failed: %w", err)
    }

    // Step 3: Parse analysis result
    var report ChangeReport
    if err := json.Unmarshal(analysis, &report); err != nil {
        return nil, fmt.Errorf("parse analysis: %w", err)
    }

    return &report, nil
}

func main() {
    detector := &ChangeDetector{
        baselineFile: "chatgpt_baseline.json",
        anythingAnalyzer: "anything-analyzer",
    }

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    report, err := detector.DetectChanges(ctx)
    if err != nil {
        log.Fatalf("Detection failed: %v", err)
    }

    if len(report.Changes) > 0 {
        log.Printf("Detected %d changes", len(report.Changes))
        for _, change := range report.Changes {
            log.Printf("- [%s] %s: %s", change.Severity, change.Type, change.Description)
        }

        // Step 4: Generate patches
        patches, err := detector.GeneratePatches(report)
        if err != nil {
            log.Fatalf("Patch generation failed: %v", err)
        }

        // Step 5: Apply patches
        for _, patch := range patches {
            if err := detector.ApplyPatch(patch); err != nil {
                log.Printf("Failed to apply patch to %s: %v", patch.File, err)
            }
        }
    } else {
        log.Println("No changes detected")
    }
}
```

### Phase 2: AI-Powered Code Generation

#### 2.1 Integrate with AI for Patch Generation

```go
// cmd/auto-detect/patcher.go
package main

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "strings"
)

type Patch struct {
    File    string `json:"file"`
    OldCode string `json:"old_code"`
    NewCode string `json:"new_code"`
    Reason  string `json:"reason"`
}

func (d *ChangeDetector) GeneratePatches(report *ChangeReport) ([]Patch, error) {
    var patches []Patch

    // Use AI to generate patches based on change report
    prompt := fmt.Sprintf(`
You are a Go developer tasked with updating the ChatGPT sentinel client.

Change Report:
%s

Generate code patches for the following files:
- auth.go (authentication logic)
- chat.go (conversation logic)
- client.go (HTTP client setup)
- utils.go (utilities and helpers)

For each change, provide:
1. The file to modify
2. The old code (exact string to replace)
3. The new code (exact replacement)
4. Reason for the change

Format as JSON array of Patch objects.
`, formatChangeReport(report))

    // Call AI API (OpenAI, Anthropic, or anything-analyzer's AI analysis)
    aiResponse, err := d.callAI(prompt)
    if err != nil {
        return nil, fmt.Errorf("AI call failed: %w", err)
    }

    // Parse AI response
    if err := json.Unmarshal([]byte(aiResponse), &patches); err != nil {
        return nil, fmt.Errorf("parse patches: %w", err)
    }

    return patches, nil
}

func (d *ChangeDetector) callAI(prompt string) (string, error) {
    // Implementation depends on AI provider
    // Could be OpenAI, Anthropic, or anything-analyzer's MCP server
    return "", nil
}

func (d *ChangeDetector) ApplyPatch(patch Patch) error {
    filepath := filepath.Join(".", patch.File)
    content, err := os.ReadFile(filepath)
    if err != nil {
        return fmt.Errorf("read file: %w", err)
    }

    contentStr := string(content)
    if !strings.Contains(contentStr, patch.OldCode) {
        return fmt.Errorf("old code not found in file")
    }

    newContent := strings.Replace(contentStr, patch.OldCode, patch.NewCode, 1)
    if err := os.WriteFile(filepath, []byte(newContent), 0644); err != nil {
        return fmt.Errorf("write file: %w", err)
    }

    fmt.Printf("Applied patch to %s: %s\n", patch.File, patch.Reason)
    return nil
}
```

### Phase 3: Automated Testing

#### 3.1 Integration Tests for Updates

```go
// cmd/auto-detect/tester.go
package main

import (
    "context"
    "testing"
)

func TestChangesApplied(t *testing.T) {
    // Run the auto-detect process
    detector := &ChangeDetector{
        baselineFile: "chatgpt_baseline.json",
    }

    report, err := detector.DetectChanges(context.Background())
    if err != nil {
        t.Fatalf("DetectChanges failed: %v", err)
    }

    if len(report.Changes) == 0 {
        t.Skip("No changes to test")
    }

    // Apply patches
    patches, err := detector.GeneratePatches(report)
    if err != nil {
        t.Fatalf("GeneratePatches failed: %v", err)
    }

    for _, patch := range patches {
        if err := detector.ApplyPatch(patch); err != nil {
            t.Errorf("ApplyPatch failed: %v", err)
        }
    }

    // Run existing tests
    if err := detector.RunTests(); err != nil {
        t.Errorf("Tests failed: %v", err)
    }
}

func (d *ChangeDetector) RunTests() error {
    // Run go tests
    cmd := exec.Command("go", "test", "./...")
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("tests failed: %w\n%s", err, string(output))
    }
    return nil
}
```

### Phase 4: Deployment Strategy

#### 4.1 Safe Rollout

```go
// cmd/auto-detect/rollout.go
package main

type RolloutStrategy struct {
    DryRun       bool
    Backup       bool
    Notify       bool
    Rollback     bool
}

func (d *ChangeDetector) RolloutChanges(strategy RolloutStrategy) error {
    // Step 1: Create backup
    if strategy.Backup {
        if err := d.CreateBackup(); err != nil {
            return fmt.Errorf("backup failed: %w", err)
        }
    }

    // Step 2: Apply changes
    report, err := d.DetectChanges(context.Background())
    if err != nil {
        return fmt.Errorf("detection failed: %w", err)
    }

    patches, err := d.GeneratePatches(report)
    if err != nil {
        return fmt.Errorf("patch generation failed: %w", err)
    }

    if strategy.DryRun {
        fmt.Println("DRY RUN - Not applying changes")
        for _, patch := range patches {
            fmt.Printf("Would apply: %s\n", patch.Reason)
        }
        return nil
    }

    for _, patch := range patches {
        if err := d.ApplyPatch(patch); err != nil {
            if strategy.Rollback {
                d.RollbackBackup()
            }
            return fmt.Errorf("apply patch failed: %w", err)
        }
    }

    // Step 3: Run tests
    if err := d.RunTests(); err != nil {
        if strategy.Rollback {
            d.RollbackBackup()
        }
        return fmt.Errorf("tests failed: %w", err)
    }

    // Step 4: Notify
    if strategy.Notify {
        d.NotifyUsers(report)
    }

    return nil
}
```

### Complete Auto-Update Workflow

```
┌──────────────────────────────────────────────────────────────────┐
│  1. Scheduled Trigger (Cron/Timer)                               │
└──────────────────────────────────────────────────────────────────┘
                         ↓
┌──────────────────────────────────────────────────────────────────┐
│  2. Anything-Analyzer Capture                                    │
│     - Start browser capture                                      │
│     - Navigate to chatgpt.com                                   │
│     - Send test message                                         │
│     - Stop capture                                              │
└──────────────────────────────────────────────────────────────────┘
                         ↓
┌──────────────────────────────────────────────────────────────────┐
│  3. AI Analysis (via MCP)                                       │
│     - Compare with baseline                                     │
│     - Detect changes in:                                        │
│       * Endpoints                                               │
│       * Request/response formats                                │
│       * Headers                                                 │
│       * Authentication                                          │
│     - Generate change report                                    │
└──────────────────────────────────────────────────────────────────┘
                         ↓
┌──────────────────────────────────────────────────────────────────┐
│  4. Code Patch Generation (via AI Agent)                        │
│     - Analyze change report                                     │
│     - Generate patches for affected files:                      │
│       * auth.go                                                  │
│       * chat.go                                                  │
│       * client.go                                                │
│       * utils.go                                                 │
│     - Provide rationale for each patch                          │
└──────────────────────────────────────────────────────────────────┘
                         ↓
┌──────────────────────────────────────────────────────────────────┐
│  5. Safe Application                                             │
│     - Create backup of current code                              │
│     - Apply patches to affected files                           │
│     - Run test suite                                            │
│     - If tests pass: commit changes                             │
│     - If tests fail: rollback backup                            │
└──────────────────────────────────────────────────────────────────┘
                         ↓
┌──────────────────────────────────────────────────────────────────┐
│  6. Update Baseline                                              │
│     - Store new baseline from captured traffic                   │
│     - Update version metadata                                    │
│     - Notify users of changes                                   │
└──────────────────────────────────────────────────────────────────┘
```

---

## Monitoring Checklist

### Daily Checks

- [ ] Run anything-analyzer capture for 5 minutes
- [ ] Compare with baseline for new endpoints
- [ ] Check for authentication errors (401/403)
- [ ] Monitor for new headers or changed header formats
- [ ] Verify PoW algorithm hasn't changed

### Weekly Checks

- [ ] Full capture and analysis session (30 minutes)
- [ ] Review all API endpoints for changes
- [ ] Check request/response body structures
- [ ] Verify SSE event formats are unchanged
- [ ] Test WebSocket handoff logic

### Monthly Checks

- [ ] Complete baseline comparison
- [ ] Review all changes with anything-analyzer AI analysis
- [ ] Update documentation if changes detected
- [ ] Run full test suite
- [ ] Check for deprecation notices

### On Error

When you encounter errors, immediately:

1. [ ] Capture full request/response with mitmproxy
2. [ ] Run anything-analyzer analysis on the error request
3. [ ] Compare with last known working baseline
4. [ ] Check GitHub issues for similar problems
5. [ ] Test with different models
6. [ ] Verify cookies and tokens are valid

### Critical Alerts

Trigger automated alerts for:

- **Authentication failures** (401/403 on token endpoints)
- **New endpoints** detected
- **Breaking changes** (400+ errors on previously working requests)
- **PoW algorithm changes** (difficulty or hash function)
- **Header requirement changes** (missing or new required headers)

---

## Quick Reference

### Files to Monitor

| File | What to Monitor | DevTools to Check |
|------|-----------------|-------------------|
| `auth.go` | Conduit token, sentinel token, PoW | f/conversation/prepare, sentinel/* |
| `chat.go` | Main request, SSE events, WebSocket | f/conversation, WS messages |
| `client.go` | Headers, user-agent, TLS fingerprint | Request headers |
| `utils.go` | Fingerprint config, hash functions | sentinel prepare body (decode base64) |
| `image.go` | Image download URLs | files/download/* |

### Key Endpoints

| Endpoint | Method | File | Function |
|----------|--------|------|----------|
| `/backend-api/f/conversation/prepare` | POST | auth.go | getConduitToken() |
| `/backend-api/sentinel/chat-requirements/prepare` | POST | auth.go | getSentinelToken() |
| `/backend-api/sentinel/chat-requirements/finalize` | POST | auth.go | getSentinelToken() |
| `/backend-api/celsius/ws/user` | GET | chat.go | getWsURL() |
| `/backend-api/f/conversation` | POST | chat.go | streamConversation() |
| `/backend-api/files/download/{fileId}` | GET | image.go | downloadImage() |

### Critical Headers

| Header | Value Pattern | File |
|--------|---------------|------|
| `Authorization` | `Bearer <jwt>` | client.go |
| `x-conduit-token` | `<conduit_token>` | chat.go |
| `openai-sentinel-chat-requirements-token` | `<sentinel_token>` | chat.go |
| `openai-sentinel-proof-token` | `<proof_token>` | chat.go |
| `x-oai-turn-trace-id` | `<uuid>` | chat.go |
| `User-Agent` | `Mozilla/5.0...` | client.go |
| `sec-ch-ua` | `"Chromium";v="146",...` | client.go |

---

## Conclusion

This architecture document provides:

1. **Complete understanding** of how ChatGPT requests are formed
2. **File-by-file breakdown** of responsibilities
3. **Step-by-step verification** using Chrome DevTools
4. **Change detection strategies** using anything-analyzer and mitmproxy
5. **Auto-update implementation plan** with AI-powered code generation
6. **Monitoring checklist** for maintaining compatibility

**Next Steps**:

1. Set up anything-analyzer MCP server for automated monitoring
2. Create baseline capture of current working traffic
3. Implement automated detection pipeline
4. Test auto-update workflow in development environment
5. Deploy to production with monitoring

**Remember**: The ChatGPT web API changes frequently. Regular monitoring and automated updates are essential for maintaining compatibility.

---

## Appendices

### Appendix A: Example Baseline JSON

```json
{
  "version": "1.0.0",
  "timestamp": "2025-06-06T18:00:00Z",
  "endpoints": {
    "/backend-api/f/conversation/prepare": {
      "method": "POST",
      "required_headers": [
        "x-conduit-token",
        "x-oai-turn-trace-id",
        "x-openai-target-path"
      ],
      "required_body_fields": [
        "action",
        "model",
        "partial_query"
      ]
    },
    "/backend-api/sentinel/chat-requirements/prepare": {
      "method": "POST",
      "required_headers": [
        "x-openai-target-path"
      ],
      "required_body_fields": [
        "p"
      ]
    }
  },
  "headers": {
    "openai-sentinel-chat-requirements-token": {
      "format": "jwt",
      "expiration": 300
    }
  },
  "pow": {
    "algorithm": "fnv-1a",
    "default_difficulty": "0000",
    "max_iterations": 500000
  }
}
```

### Appendix B: Example Change Report

```json
{
  "timestamp": "2025-06-07T10:30:00Z",
  "changes": [
    {
      "type": "endpoint_changed",
      "severity": "critical",
      "description": "Conversation endpoint path changed",
      "affected_file": "chat.go",
      "details": {
        "old": "/backend-api/f/conversation",
        "new": "/backend-api/v2/f/conversation"
      }
    },
    {
      "type": "new_header_required",
      "severity": "high",
      "description": "New header x-openai-api-version required",
      "affected_file": "client.go",
      "details": {
        "header": "x-openai-api-version",
        "value": "v2"
      }
    }
  ],
  "recommendations": [
    "Update chat.go:208 with new endpoint path",
    "Add x-openai-api-version header in client.go:132",
    "Run integration tests to verify changes",
    "Monitor for additional breaking changes"
  ]
}
```

### Appendix C: Useful Commands

```bash
# Run anything-analyzer MCP server
anything-analyzer --mcp

# Run mitmproxy with custom script
mitmproxy -s chatgpt_monitor.py

# Capture traffic and export as HAR
mitmproxy --set har_capture=true

# Run auto-detect in dry-run mode
go run cmd/auto-detect/main.go --dry-run

# Run tests after updates
go test -v ./...

# Check for git changes
git diff HEAD~1 HEAD -- auth.go chat.go client.go utils.go
```

### Appendix D: Further Reading

- [Anything-Analyzer Documentation](https://github.com/Mouseww/anything-analyzer)
- [Mitmproxy Documentation](https://docs.mitmproxy.org/)
- [Chrome DevTools Protocol](https://chromedevtools.github.io/devtools-protocol/)
- [ChatGPT Web API History](https://github.com/transitive-bullshit/chatgpt-api)

---

**Document Version**: 1.0.0
**Last Updated**: 2025-06-06
**Maintained By**: ChatGPT Sentinel Project