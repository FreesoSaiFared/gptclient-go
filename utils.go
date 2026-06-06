package sentinel

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// GenerateUUID generates a v4 UUID.
func GenerateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// encodeBase64JSON JSON-encodes a value and then Base64-encodes it (corresponds to JS O0 function).
func encodeBase64JSON(v interface{}) string {
	data, _ := json.Marshal(v)
	return base64.StdEncoding.EncodeToString(data)
}

// fnvHash corresponds to JS rEe function: FNV-1a variant hash.
//
// This is used for proof-of-work computation in the sentinel authentication.
// The hash must start with a specific prefix to be accepted.
//
// Chrome DevTools Verification:
//   1. Look for: sentinel/chat-requirements/prepare
//   2. Check if proofofwork.required is true
//   3. Note the difficulty value (e.g., "0000")
//   4. Look for: sentinel/chat-requirements/finalize after delay
//   5. Verify the proof_token has prefix "gAAAAAB"
//   6. The hash computation happens between prepare and finalize
//
// What Might Change:
//   - Hash algorithm: FNV-1a → SHA-256, xxHash, BLAKE3
//   - Hash constants: Offset basis, prime, finalization steps
//   - Difficulty comparison: String comparison → numeric comparison
//   - PoW format: "gAAAAAB" prefix → different prefix or no prefix
//   - Maximum iterations: 500000 → higher or dynamic limit
//
// Detection with Anything-Analyzer:
//   - Use "JS 加密逆向" mode to monitor crypto functions
//   - Hook into JavaScript crypto operations
//   - Compare hash outputs with different inputs
//   - Set up alerts for algorithm changes
//
// Detection with Mitmproxy:
//   ```python
//   def request(flow: http.HTTPFlow):
//       if "sentinel/finalize" in flow.request.path:
//           body = json.loads(flow.request.text)
//           proof = body.get("proofofwork", "")
//           if proof and not proof.startswith("gAAAAAB"):
//               ctx.log.warn(f"[CHANGE] Proof token prefix changed: {proof[:10]}")
//   ```
//
// Algorithm Migration:
//   If hash function changes:
//   1. Capture new algorithm from JavaScript source
//   2. Implement in Go with same behavior
//   3. Verify output matches JavaScript for same inputs
//   4. Update difficulty comparison logic if needed
//   5. Test with different PoW scenarios
func fnvHash(s string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	h ^= h >> 16
	h *= 2246822507
	h ^= h >> 13
	h *= 3266489909
	h ^= h >> 16
	return fmt.Sprintf("%08x", h)
}

// buildCfg constructs the fingerprint configuration array (corresponds to JS buildCfg).
//
// This array is encoded and sent to the sentinel/prepare endpoint.
// It acts as a browser fingerprint to verify the client is legitimate.
//
// Chrome DevTools Verification:
//   1. Look for: sentinel/chat-requirements/prepare
//   2. Copy the request body: {"p": "gAAAAAC<base64>"}
//   3. Decode the base64 (after "gAAAAAC")
//   4. Parse the JSON and verify array structure:
//      - Index 0: 3000 (constant)
//      - Index 1: JS Date.toString() format
//      - Index 4: User-Agent string
//      - Index 6: Build hash (prod-*)
//      - Index 7: Language code (zh-CN)
//      - Index 13: Performance timing (float)
//      - Index 14: UUID (session ID)
//   5. Compare with this function's output
//
// What Might Change:
//   - Array length: New fields added or removed
//   - Field values: Constants change, new defaults
//   - Field order: Index positions shift
//   - Build hash format: New hash algorithm
//   - Performance timing: Different precision or calculation
//   - Date format: Changes to JavaScript Date.toString()
//
// Detection with Anything-Analyzer:
//   - Use "API 逆向" mode on sentinel/prepare requests
//   - Decode base64 and compare array structure with baseline
//   - Set up alerts for array length changes
//   - Monitor for new field names in decoded JSON
//
// Detection with Mitmproxy:
//   ```python
//   import base64, json
//   def request(flow: http.HTTPFlow):
//       if "sentinel/prepare" in flow.request.path:
//           body = json.loads(flow.request.text)
//           encoded = body["p"][7:]  # Remove "gAAAAAC" prefix
//           decoded = base64.b64decode(encoded)
//           cfg = json.loads(decoded)
//           ctx.log.info(f"[FINGERPRINT] Array length: {len(cfg)}")
//           if len(cfg) != 25:
//               ctx.log.warn(f"[CHANGE] Fingerprint array length changed!")
//   ```
//
// Browser Updates:
//   - Update buildHash from ChatGPT web client source code
//   - Update buildNumber from deployment metadata
//   - Update language support if new languages added
//   - Update sec-ch-ua headers in client.go to match browser version
func buildCfg(ua, buildHash, lang, sid string, t0 int64, perfNow float64) []interface{} {
	return []interface{}{
		3000,
		jsDateString(time.Now()),
		int64(4294967296),
		nil,
		ua,
		"",
		buildHash,
		lang,
		"zh-CN,en,en-GB,en-US",
		nil,
		"credentials\u2252[object Navigator]",
		"location",
		"fetch",
		perfNow,
		sid,
		"",
		28,
		t0,
		0, 0, 0, 0, 0, 0, 0,
	}
}

// jsDateString simulates JavaScript Date.toString() output format.
func jsDateString(t time.Time) string {
	days := [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	months := [...]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun",
		"Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	name, offset := t.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	h := offset / 3600
	m := (offset % 3600) / 60
	return fmt.Sprintf("%s %s %02d %d %02d:%02d:%02d GMT%s%02d%02d (%s)",
		days[t.Weekday()], months[t.Month()-1], t.Day(), t.Year(),
		t.Hour(), t.Minute(), t.Second(), sign, h, m, name)
}

func perfNowMs(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000.0
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func runeSlice(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) > maxRunes {
		r = r[:maxRunes]
	}
	return string(r)
}

func orDefault(val, def string) string {
	if val != "" {
		return val
	}
	return def
}

// getNestedString retrieves a string value from a nested map by key path.
func getNestedString(m map[string]interface{}, keys ...string) string {
	current := m
	for i, key := range keys {
		if i == len(keys)-1 {
			s, _ := current[key].(string)
			return s
		}
		next, ok := current[key].(map[string]interface{})
		if !ok {
			return ""
		}
		current = next
	}
	return ""
}

// getFirstStringPart retrieves content.parts[0] as a string from a message map.
func getFirstStringPart(msg map[string]interface{}) string {
	content, ok := msg["content"].(map[string]interface{})
	if !ok {
		return ""
	}
	parts, ok := content["parts"].([]interface{})
	if !ok || len(parts) == 0 {
		return ""
	}
	s, _ := parts[0].(string)
	return s
}

var fileIDRegexp = regexp.MustCompile(`file_[a-f0-9]+`)

func extractFileID(pointer string) string {
	if pointer == "" {
		return ""
	}
	return fileIDRegexp.FindString(pointer)
}
