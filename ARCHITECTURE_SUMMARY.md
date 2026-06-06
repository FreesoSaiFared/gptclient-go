# ChatGPT Sentinel - Architecture Documentation Summary

## What Was Created

### 1. ARCHITECTURE.md (500+ lines)
Complete documentation covering:
- **Complete Request Flow**: 4-step authentication + conversation process
- **File Responsibility Matrix**: What each file does and key functions
- **Chrome DevTools Verification**: Step-by-step verification guide
- **What Changes & How to Detect**: Comprehensive change detection guide
- **Anything-Analyzer Integration**: MCP server setup and workflow
- **Mitmproxy Setup**: Custom scripts for monitoring
- **Auto-Update Strategy**: Phase-by-phase implementation plan
- **Monitoring Checklist**: Daily, weekly, monthly checks

### 2. Inline Code Comments
Added detailed comments to core source files:

#### auth.go
- `getConduitToken()` - Step 1 of authentication
- `getSentinelToken()` - Steps 2+3: prepare → PoW → finalize

Each comment includes:
- What the code does
- Chrome DevTools verification steps
- What might change
- Detection methods (anything-analyzer, mitmproxy)

#### chat.go
- `ChatStream()` - Main entry point
- `getWsURL()` - WebSocket URL fetch
- `dialChatWS()` - WebSocket connection setup
- `streamConversation()` - Main conversation request
- `subscribeWSStream()` - WebSocket streaming

#### client.go
- `commonHeaders()` - HTTP header generation with browser fingerprinting

#### utils.go
- `buildCfg()` - Fingerprint configuration array
- `fnvHash()` - FNV-1a hash for proof-of-work

#### image.go
- `PollAndDownloadImage()` - Image polling and download

## Quick Reference

### Request Flow (4 Steps)

```
1. Conduit Token
   POST /backend-api/f/conversation/prepare
   → conduit_token

2. Sentinel Token + PoW
   POST /backend-api/sentinel/chat-requirements/prepare
   → Brute force FNV-1a hash
   POST /backend-api/sentinel/chat-requirements/finalize
   → sentinel_token + proof_token

3. WebSocket Connection
   GET /backend-api/celsius/ws/user
   → websocket_url
   → WebSocket handshake + 4 subscriptions

4. Main Conversation
   POST /backend-api/f/conversation
   → SSE stream + WebSocket handoff
```

### Files & Their Responsibilities

| File | Main Functions | Key Endpoints |
|------|---------------|---------------|
| **auth.go** | `getConduitToken()`, `getSentinelToken()` | f/conversation/prepare, sentinel/* |
| **chat.go** | `ChatStream()`, `streamConversation()`, `dialChatWS()` | f/conversation, celsius/ws/user |
| **client.go** | `commonHeaders()`, `NewClient()` | All requests (headers) |
| **utils.go** | `buildCfg()`, `fnvHash()` | sentinel prepare body |
| **image.go** | `PollAndDownloadImage()`, `downloadImage()` | conversation/{id}, files/download/{id} |

### High-Frequency Changes

These change most often:
1. **API Endpoints** - Path names, versioning
2. **Request Body Structure** - New fields, removed fields
3. **Authentication Tokens** - Format, encryption
4. **PoW Algorithm** - Difficulty, hash function
5. **HTTP Headers** - New headers, name changes
6. **Fingerprint Config** - Array structure, values
7. **SSE Events** - Event types, delta format
8. **WebSocket Protocol** - Handoff logic, topics

### Verification Using Chrome DevTools

**Quick Steps:**
1. Open chatgpt.com → F12 → Network tab
2. Send a message
3. Look for these requests in order:
   - `f/conversation/prepare` → conduit token
   - `sentinel/chat-requirements/prepare` → PoW challenge
   - `sentinel/chat-requirements/finalize` → sentinel token
   - `celsius/ws/user` → WebSocket URL
   - `f/conversation` → main conversation (SSE)
4. Switch to WS tab → verify WebSocket messages
5. Compare headers, bodies, responses with documentation

### Change Detection Tools

#### Anything-Analyzer (Recommended)
- **Browser capture** via CDP
- **MITM proxy** for HTTPS traffic
- **JS Hook injection** for encryption monitoring
- **AI analysis** for automatic reverse engineering
- **MCP Server** for AI agent integration

**Setup:**
```bash
# Download from GitHub Releases
https://github.com/Mouseww/anything-analyzer/releases

# Configure LLM in Settings
# Start browser capture
# Use AI analysis mode "API 逆向" for protocol changes
```

**MCP Integration:**
```json
{
  "mcpServers": {
    "anything-analyzer": {
      "command": "anything-analyzer",
      "args": ["--mcp"]
    }
  }
}
```

#### Mitmproxy
- **MITM proxy** for traffic interception
- **Custom scripts** for change detection
- **HAR export** for analysis

**Setup:**
```bash
pip install mitmproxy

# Run with custom script
mitmproxy -s chatgpt_monitor.py

# Export as HAR
mitmproxy --set har_capture=true
```

**Custom Script:**
```python
from mitmproxy import http, ctx

class ChatGPTMonitor:
    def request(self, flow: http.HTTPFlow):
        if "chatgpt.com" in flow.request.pretty_url:
            ctx.log.info(f"[REQUEST] {flow.request.path}")

    def response(self, flow: http.HTTPFlow):
        if flow.response.status_code >= 400:
            ctx.log.error(f"[ERROR] {flow.request.path} - {flow.response.status_code}")

addons = [ChatGPTMonitor()]
```

### Auto-Update Workflow

```
┌─────────────────────────────────────────────────────┐
│  1. Anything-Analyzer Capture                        │
│     - Start browser capture                           │
│     - Navigate to chatgpt.com                        │
│     - Send test message                              │
│     - Stop capture                                   │
└─────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────┐
│  2. AI Analysis (via MCP)                            │
│     - Compare with baseline                          │
│     - Detect changes                                 │
│     - Generate change report                         │
└─────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────┐
│  3. Code Patch Generation                            │
│     - AI agent analyzes report                       │
│     - Generates patches for affected files           │
│     - Provides rationale for each patch              │
└─────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────┐
│  4. Safe Application                                 │
│     - Create backup                                  │
│     - Apply patches                                  │
│     - Run tests                                      │
│     - Rollback if tests fail                         │
└─────────────────────────────────────────────────────┘
```

### Monitoring Checklist

**Daily:**
- [ ] Run anything-analyzer capture (5 minutes)
- [ ] Check for new endpoints
- [ ] Monitor for auth errors (401/403)
- [ ] Verify PoW algorithm unchanged

**Weekly:**
- [ ] Full capture & analysis (30 minutes)
- [ ] Review all API endpoints
- [ ] Check request/response structures
- [ ] Verify SSE event formats

**Monthly:**
- [ ] Complete baseline comparison
- [ ] Review all changes with AI analysis
- [ ] Update documentation
- [ ] Run full test suite

**On Error:**
1. Capture full request/response with mitmproxy
2. Run anything-analyzer analysis
3. Compare with baseline
4. Check GitHub issues
5. Test with different models

### Key Files to Monitor

| File | What to Monitor | DevTools Endpoint |
|------|-----------------|-------------------|
| `auth.go` | Token formats, PoW logic | f/conversation/prepare, sentinel/* |
| `chat.go` | Main request, SSE, WebSocket | f/conversation, WS messages |
| `client.go` | Headers, user-agent | Request headers |
| `utils.go` | Fingerprint, hash functions | sentinel prepare (decode base64) |
| `image.go` | Image download URLs | files/download/* |

### Next Steps

1. **Set up anything-analyzer MCP server**
   - Download and install anything-analyzer
   - Configure LLM API key
   - Enable MCP server
   - Test with Claude Desktop/Cursor

2. **Create baseline capture**
   - Run full capture session
   - Export as JSON/HAR
   - Store with timestamp and version

3. **Implement automated detection**
   - Create change detection pipeline
   - Set up scheduled monitoring
   - Configure alerts for breaking changes

4. **Test auto-update workflow**
   - Implement AI patch generation
   - Create safe rollback mechanism
   - Test in development environment

5. **Deploy with monitoring**
   - Set up production monitoring
   - Configure automated alerts
   - Document change response procedures

### Useful Commands

```bash
# View architecture documentation
cat ARCHITECTURE.md

# Run anything-analyzer MCP server
anything-analyzer --mcp

# Run mitmproxy with custom script
mitmproxy -s chatgpt_monitor.py

# Capture traffic as HAR
mitmproxy --set har_capture=true

# Run tests
go test -v ./...

# Check recent changes
git diff HEAD~1 HEAD -- auth.go chat.go client.go utils.go

# View file comments
grep -n "Chrome DevTools Verification" auth.go chat.go client.go utils.go image.go
```

### References

- **ARCHITECTURE.md** - Complete architecture documentation
- **anything-analyzer** - https://github.com/Mouseww/anything-analyzer
- **mitmproxy** - https://docs.mitmproxy.org/
- **Chrome DevTools** - https://chromedevtools.github.io/devtools-protocol/

### Example Change Detection

**Scenario:** ChatGPT updates conversation endpoint

**Detection:**
```python
# anything-analyzer detects new endpoint
{
  "type": "endpoint_changed",
  "severity": "critical",
  "old": "/backend-api/f/conversation",
  "new": "/backend-api/v2/f/conversation"
}

# AI generates patch
{
  "file": "chat.go",
  "line": 226,
  "old": 'Post("/backend-api/f/conversation")',
  "new": 'Post("/backend-api/v2/f/conversation")',
  "reason": "Endpoint path updated by ChatGPT"
}
```

**Application:**
```bash
# Auto-update applies patch
sed -i 's|/backend-api/f/conversation|/backend-api/v2/f/conversation|g' chat.go

# Run tests
go test -v ./...

# If tests pass, commit changes
git add chat.go
git commit -m "fix: update conversation endpoint to v2"
```

## Summary

This documentation provides:

✅ **Complete understanding** of ChatGPT request formation
✅ **File-by-file breakdown** of responsibilities  
✅ **Step-by-step verification** using Chrome DevTools
✅ **Change detection strategies** using anything-analyzer and mitmproxy
✅ **Auto-update implementation plan** with AI-powered code generation
✅ **Monitoring checklist** for maintaining compatibility

**The code is now self-documenting with inline comments that guide verification and change detection.**

---

**Version**: 1.0.0  
**Created**: 2025-06-06  
**Commit**: 2096c5e