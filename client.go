package sentinel

import (
	"log"
	"time"

	"github.com/imroc/req/v3"
)

const (
	defaultUA          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36 Edg/147.0.0.0"
	defaultBuildHash   = "prod-81e0c5cdf6140e8c5db714d613337f4aeab94029"
	defaultBuildNumber = "6128297"
	defaultLang        = "zh-CN"
	defaultModel       = "gpt-5-5-thinking"
)

// Client is a ChatGPT conversation client that encapsulates the full
// Sentinel authentication + SSE conversation flow.
type Client struct {
	httpClient  *req.Client
	bearerToken string
	cookieStr   string
	userAgent   string
	deviceID    string
	buildHash   string
	buildNumber string
	language    string
	sessionID   string
	imageDir    string
	startTime   time.Time

	conversationID  string
	parentMessageID string
	model           string
	tempMode        bool
	turnCount       int

	// Logf is the log output function; set to nil to disable logging. Defaults to log.Printf.
	Logf LogFunc

	// DisableAutoImage when true prevents Chat/ChatStream from blocking to wait for
	// image downloads. Suitable for DLL/external call scenarios where the caller
	// handles image downloading asynchronously.
	DisableAutoImage bool
}

// NewClient creates a new ChatGPT client.
func NewClient(cfg Config) *Client {
	c := &Client{
		bearerToken:     cfg.BearerToken,
		cookieStr:       cfg.CookieString,
		userAgent:       orDefault(cfg.UserAgent, defaultUA),
		deviceID:        orDefault(cfg.DeviceID, GenerateUUID()),
		buildHash:       orDefault(cfg.BuildHash, defaultBuildHash),
		buildNumber:     orDefault(cfg.BuildNumber, defaultBuildNumber),
		language:        orDefault(cfg.Language, defaultLang),
		imageDir:        orDefault(cfg.ImageDir, "images"),
		model:           orDefault(cfg.Model, defaultModel),
		parentMessageID: "client-created-root",
		sessionID:       GenerateUUID(),
		startTime:       time.Now(),
		tempMode:        cfg.TempMode,
		Logf:            log.Printf,
	}

	baseURL := orDefault(cfg.BaseURL, "https://chatgpt.com")

	httpC := req.C().
		SetBaseURL(baseURL).
		SetCommonHeaders(c.commonHeaders())

	if !cfg.DisableImpersonate {
		httpC = httpC.ImpersonateChrome()
	}

	c.httpClient = httpC
	return c
}

// HTTPClient returns the underlying req.Client for advanced customization.
func (c *Client) HTTPClient() *req.Client {
	return c.httpClient
}

// ResetSession resets the conversation context (starts a new conversation).
func (c *Client) ResetSession() {
	c.conversationID = ""
	c.parentMessageID = "client-created-root"
	c.turnCount = 0
}

// SetModel switches the model.
func (c *Client) SetModel(model string) { c.model = model }

// GetModel returns the current model.
func (c *Client) GetModel() string { return c.model }

// SetBearerToken updates the bearer token used for authentication.
// This is useful for refreshing an expired token without creating a new Client.
func (c *Client) SetBearerToken(token string) { c.bearerToken = token }

// SetTempMode sets temporary mode.
func (c *Client) SetTempMode(enabled bool) { c.tempMode = enabled }

// SetDisableAutoImage sets whether auto image downloading is disabled (for DLL use cases).
func (c *Client) SetDisableAutoImage(disabled bool) { c.DisableAutoImage = disabled }

// SetConversationID restores to a specific conversation.
func (c *Client) SetConversationID(id string) { c.conversationID = id }

// SetParentMessageID sets the parent message ID (to specify reply position).
func (c *Client) SetParentMessageID(id string) { c.parentMessageID = id }

// GetSessionInfo returns the current session state.
func (c *Client) GetSessionInfo() SessionInfo {
	return SessionInfo{
		ConversationID:  c.conversationID,
		ParentMessageID: c.parentMessageID,
		Model:           c.model,
		TempMode:        c.tempMode,
		TurnCount:       c.turnCount,
	}
}

func (c *Client) logf(format string, args ...interface{}) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// commonHeaders generates the HTTP headers required for ChatGPT API requests.
//
// These headers mimic a Chrome browser on Windows to avoid detection.
// They include authentication, browser fingerprinting, and client information.
//
// Chrome DevTools Verification:
//   1. Open chatgpt.com → F12 → Network tab
//   2. Click on any ChatGPT API request
//   3. Compare Request Headers with this function's output
//   4. Critical headers to verify:
//      - Authorization: Bearer <jwt_token>
//      - User-Agent: Must match current Chrome version
//      - sec-ch-ua*: Client Hints headers (security detection)
//      - oai-* headers: OpenAI-specific headers
//      - Cookie: If cookies are enabled
//
// What Might Change:
//   - User-Agent: Chrome version updates (147.0 → 148.0)
//   - sec-ch-ua*: Client hints format changes, new headers added
//   - oai-* headers: New OpenAI headers, name changes (oai-device-id → oai-device-id-v2)
//   - Cookie format: New required cookies, cookie encryption
//   - Build hash: Updates with new ChatGPT web client builds
//   - Build number: Increments with each deployment
//
// Detection with Anything-Analyzer:
//   - Use "安全审计" mode to detect new security headers
//   - Monitor for 403 errors (often due to header mismatch)
//   - Compare sec-ch-ua headers with latest Chrome version
//   - Set up alerts for User-Agent version changes
//
// Detection with Mitmproxy:
//   ```python
//   def request(flow: http.HTTPFlow):
//       if "chatgpt.com" in flow.request.pretty_host:
//           # Check for new headers
//           baseline_headers = ["Authorization", "User-Agent", "sec-ch-ua", "oai-device-id"]
//           for header in flow.request.headers:
//               if header not in baseline_headers and "oai-" in header:
//                   ctx.log.warn(f"[NEW HEADER] {header}: {flow.request.headers[header]}")
//   ```
//
// Browser Fingerprint Updates:
//   - Update defaultUA when Chrome releases new versions
//   - Update sec-ch-ua values to match Chrome version
//   - Update buildHash from ChatGPT web client source
//   - Update buildNumber from latest deployment
func (c *Client) commonHeaders() map[string]string {
	h := map[string]string{
		"Authorization":               "Bearer " + c.bearerToken,
		"User-Agent":                  c.userAgent,
		"Accept-Language":             c.language + ",zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6",
		"oai-language":                c.language,
		"oai-device-id":               c.deviceID,
		"oai-session-id":              c.sessionID,
		"oai-client-version":          c.buildHash,
		"oai-client-build-number":     c.buildNumber,
		"Origin":                      "https://chatgpt.com",
		"Referer":                     "https://chatgpt.com/",
		"sec-ch-ua":                   `"Chromium";v="146", "Not-A.Brand";v="24", "Microsoft Edge";v="146"`,
		"sec-ch-ua-mobile":            "?0",
		"sec-ch-ua-platform":          `"Windows"`,
		"sec-ch-ua-platform-version":  `"19.0.0"`,
		"sec-ch-ua-arch":              `"x86"`,
		"sec-ch-ua-bitness":           `"64"`,
		"sec-ch-ua-model":             `""`,
		"sec-ch-ua-full-version":      `"146.0.3856.72"`,
		"sec-ch-ua-full-version-list": `"Chromium";v="146.0.7680.154", "Not-A.Brand";v="24.0.0.0", "Microsoft Edge";v="146.0.3856.72"`,
		"sec-fetch-dest":              "empty",
		"sec-fetch-mode":              "cors",
		"sec-fetch-site":              "same-origin",
	}
	if c.cookieStr != "" {
		h["Cookie"] = c.cookieStr
	}
	return h
}
