package sentinel

import (
	"encoding/json"
	"fmt"
	"time"
)

// getConduitToken fetches a conduit_token (Step 1 of authentication).
//
// This is the FIRST step in the ChatGPT Sentinel authentication flow.
// The conduit token is required for the main conversation request.
//
// Chrome DevTools Verification:
//  1. Open chatgpt.com → F12 → Network tab
//  2. Send a message and look for: f/conversation/prepare
//  3. Verify request headers include:
//     - x-conduit-token: no-token
//     - x-oai-turn-trace-id: <uuid>
//     - x-openai-target-path: /backend-api/f/conversation/prepare
//  4. Verify request body structure matches below
//  5. Check response contains: {"status":"success", "conduit_token":"..."}
//
// What Might Change:
//   - Endpoint path: /backend-api/f/conversation/prepare → /backend-api/v2/prepare
//   - Required headers: x-conduit-token → x-oai-conduit-token
//   - Request body fields: new required fields like "client_version"
//   - Response structure: status field name or format
//
// Detection with Anything-Analyzer:
//   - Use "API 逆向" mode on captured traffic
//   - Monitor for 404/400 errors on prepare endpoint
//   - Compare request body structure with baseline
//
// Detection with Mitmproxy:
//
//	```python
//	def request(flow: http.HTTPFlow):
//	    if "f/conversation/prepare" in flow.request.path:
//	        # Log request for comparison
//	        ctx.log.info(f"Prepare request: {flow.request.text}")
//	```
func (c *Client) getConduitToken(model, turnTraceID, partialText string) (string, error) {
	if partialText == "" {
		partialText = "h"
	}

	body := map[string]interface{}{
		"action":                "next",
		"fork_from_shared_post": false,
		"parent_message_id":     "client-created-root",
		"model":                 model,
		"timezone_offset_min":   -480,
		"timezone":              "Asia/Shanghai",
		"conversation_mode":     map[string]string{"kind": "primary_assistant"},
		"system_hints":          []string{},
		"partial_query": map[string]interface{}{
			"id":     GenerateUUID(),
			"author": map[string]string{"role": "user"},
			"content": map[string]interface{}{
				"content_type": "text",
				"parts":        []string{partialText},
			},
		},
		"supports_buffering":     true,
		"supported_encodings":    []string{"v1"},
		"client_contextual_info": map[string]interface{}{"app_name": "chatgpt.com"},
		"thinking_effort":        "standard",
	}

	resp, err := c.httpClient.R().
		SetHeaders(map[string]string{
			"Accept":                "*/*",
			"Content-Type":          "application/json",
			"x-conduit-token":       "no-token",
			"x-oai-turn-trace-id":   turnTraceID,
			"x-openai-target-path":  "/backend-api/f/conversation/prepare",
			"x-openai-target-route": "/backend-api/f/conversation/prepare",
		}).
		SetBody(body).
		Post("/backend-api/f/conversation/prepare")
	if err != nil {
		return "", fmt.Errorf("conversation/prepare request: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("conversation/prepare %d: %s", resp.StatusCode, truncateStr(resp.String(), 200))
	}

	var result struct {
		Status       string `json:"status"`
		ConduitToken string `json:"conduit_token"`
	}
	if err := json.Unmarshal(resp.Bytes(), &result); err != nil {
		return "", fmt.Errorf("parse conduit response: %w", err)
	}

	c.logf("  [conduit] status=%s", result.Status)
	return result.ConduitToken, nil
}

// getSentinelToken fetches a sentinel token (Steps 2+3: prepare → PoW → finalize).
//
// This is the CORE authentication step that includes proof-of-work.
// The sentinel token is the primary authentication token for conversations.
//
// Chrome DevTools Verification:
//  1. Look for: sentinel/chat-requirements/prepare
//  2. Verify request body has: {"p": "gAAAAAC<base64>"}
//  3. Decode the base64 to verify fingerprint config array structure
//  4. Check response for proofofwork.required: true/false
//  5. If PoW required, look for sentinel/chat-requirements/finalize
//  6. Verify final response contains: {"token":"<sentinel_token>", "expire_after":300}
//
// What Might Change:
//   - PoW algorithm: FNV-1a hash → SHA-256, xxHash
//   - Difficulty format: "0000" → "00000" or numeric difficulty
//   - Seed format: simple string → complex object
//   - Fingerprint config: array structure changes, new fields added
//   - Token format: JWT → different encoding, new prefixes
//   - Expiration time: 300s → 600s or dynamic
//
// Detection with Anything-Analyzer:
//   - Use "JS 加密逆向" mode to track hash function changes
//   - Monitor crypto.subtle.digest() calls for algorithm changes
//   - Compare fingerprint config array with baseline
//   - Set up alert for new PoW difficulty levels
//
// Detection with Mitmproxy:
//
//	```python
//	def response(flow: http.HTTPFlow):
//	    if "sentinel/prepare" in flow.request.path:
//	        response = json.loads(flow.response.text)
//	        # Check for PoW changes
//	        if "proofofwork" in response:
//	            pow_data = response["proofofwork"]
//	            ctx.log.info(f"PoW difficulty: {pow_data.get('difficulty')}")
//	            ctx.log.info(f"PoW seed: {pow_data.get('seed')}")
//	```
func (c *Client) getSentinelToken() (sentinelToken, proofToken string, err error) {
	reqToken := NewPOWConfig(c.userAgent).RequirementsToken()

	prepBody := map[string]string{
		"p": reqToken,
	}

	resp, err := c.httpClient.R().
		SetHeaders(map[string]string{
			"Accept":                "*/*",
			"Content-Type":          "application/json",
			"x-openai-target-path":  "/backend-api/sentinel/chat-requirements/prepare",
			"x-openai-target-route": "/backend-api/sentinel/chat-requirements/prepare",
		}).
		SetBody(prepBody).
		Post("/backend-api/sentinel/chat-requirements/prepare")
	if err != nil {
		return "", "", fmt.Errorf("sentinel/prepare request: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("sentinel/prepare %d: %s", resp.StatusCode, truncateStr(resp.String(), 200))
	}

	var pd struct {
		Persona     string `json:"persona"`
		Proofofwork *struct {
			Required   bool   `json:"required"`
			Seed       string `json:"seed"`
			Difficulty string `json:"difficulty"`
		} `json:"proofofwork"`
		Turnstile *struct {
			Required bool `json:"required"`
		} `json:"turnstile"`
		PrepareToken string `json:"prepare_token"`
	}
	if err := json.Unmarshal(resp.Bytes(), &pd); err != nil {
		return "", "", fmt.Errorf("parse sentinel/prepare: %w", err)
	}

	powRequired := pd.Proofofwork != nil && pd.Proofofwork.Required
	turnstileRequired := pd.Turnstile != nil && pd.Turnstile.Required
	c.logf("  [sentinel] persona=%s, PoW=%v, turnstile=%v", pd.Persona, powRequired, turnstileRequired)

	if powRequired {
		seed := pd.Proofofwork.Seed
		difficulty := pd.Proofofwork.Difficulty
		s0 := time.Now()

		proofToken = SolveProofToken(seed, difficulty, c.userAgent)
		c.logf("  [pow] solved in %dms", time.Since(s0).Milliseconds())
	}

	fb := map[string]interface{}{
		"prepare_token": pd.PrepareToken,
	}
	if proofToken != "" {
		fb["proofofwork"] = proofToken
	}

	finResp, err := c.httpClient.R().
		SetHeaders(map[string]string{
			"Accept":                "*/*",
			"Content-Type":          "application/json",
			"x-openai-target-path":  "/backend-api/sentinel/chat-requirements/finalize",
			"x-openai-target-route": "/backend-api/sentinel/chat-requirements/finalize",
		}).
		SetBody(fb).
		Post("/backend-api/sentinel/chat-requirements/finalize")
	if err != nil {
		return "", "", fmt.Errorf("sentinel/finalize request: %w", err)
	}
	if finResp.StatusCode != 200 {
		return "", "", fmt.Errorf("sentinel/finalize %d: %s", finResp.StatusCode, truncateStr(finResp.String(), 200))
	}

	var fd struct {
		Token       string `json:"token"`
		ExpireAfter int    `json:"expire_after"`
	}
	if err := json.Unmarshal(finResp.Bytes(), &fd); err != nil {
		return "", "", fmt.Errorf("parse sentinel/finalize: %w", err)
	}
	if fd.Token == "" {
		return "", "", fmt.Errorf("no sentinel token: %s", truncateStr(finResp.String(), 200))
	}

	c.logf("  [finalize] expire=%ds", fd.ExpireAfter)
	return fd.Token, proofToken, nil
}
