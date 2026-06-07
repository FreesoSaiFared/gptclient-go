package sentinel

import (
	"regexp"
	"testing"
)

func TestRuneSlice(t *testing.T) {
	if got := runeSlice("", 5); got != "" {
		t.Errorf("runeSlice empty: got %q", got)
	}
	if got := runeSlice("abc", 10); got != "abc" {
		t.Errorf("runeSlice short: got %q", got)
	}
	if got := runeSlice("hello世界", 5); got != "hello" {
		t.Errorf("runeSlice truncate: got %q", got)
	}
	if got := runeSlice("你好世界", 2); got != "你好" {
		t.Errorf("runeSlice unicode: got %q", got)
	}
}

func TestOrDefault(t *testing.T) {
	if got := orDefault("", "def"); got != "def" {
		t.Errorf("orDefault empty: got %q", got)
	}
	if got := orDefault("val", "def"); got != "val" {
		t.Errorf("orDefault non-empty: got %q", got)
	}
	if got := orDefault("val", ""); got != "val" {
		t.Errorf("orDefault with empty def: got %q", got)
	}
}

func TestTruncateStr(t *testing.T) {
	if got := truncateStr("hello", 10); got != "hello" {
		t.Errorf("truncateStr short: got %q", got)
	}
	if got := truncateStr("hello world", 5); got != "hello" {
		t.Errorf("truncateStr truncate: got %q", got)
	}
	if got := truncateStr("", 5); got != "" {
		t.Errorf("truncateStr empty: got %q", got)
	}
}

func TestExtractFileID(t *testing.T) {
	if got := extractFileID(""); got != "" {
		t.Errorf("extractFileID empty: got %q", got)
	}
	if got := extractFileID("sediment://file_abc123def"); got != "file_abc123def" {
		t.Errorf("extractFileID with sediment: got %q", got)
	}
	if got := extractFileID("no file id here"); got != "" {
		t.Errorf("extractFileID no match: got %q", got)
	}
	if got := extractFileID("prefix file_a1b2c3 suffix"); got != "file_a1b2c3" {
		t.Errorf("extractFileID embedded: got %q", got)
	}
}

func TestGetNestedString(t *testing.T) {
	m := map[string]interface{}{
		"a": map[string]interface{}{
			"b": map[string]interface{}{
				"c": "value",
			},
		},
	}
	if got := getNestedString(m, "a", "b", "c"); got != "value" {
		t.Errorf("getNestedString deep: got %q", got)
	}
	if got := getNestedString(m, "a", "missing", "c"); got != "" {
		t.Errorf("getNestedString missing: got %q", got)
	}
	if got := getNestedString(m, "x"); got != "" {
		t.Errorf("getNestedString top missing: got %q", got)
	}
}

func TestGetFirstStringPart(t *testing.T) {
	msg := map[string]interface{}{
		"content": map[string]interface{}{
			"parts": []interface{}{"hello", "world"},
		},
	}
	if got := getFirstStringPart(msg); got != "hello" {
		t.Errorf("getFirstStringPart: got %q", got)
	}
	emptyMsg := map[string]interface{}{
		"content": map[string]interface{}{
			"parts": []interface{}{},
		},
	}
	if got := getFirstStringPart(emptyMsg); got != "" {
		t.Errorf("getFirstStringPart empty: got %q", got)
	}
	noContent := map[string]interface{}{}
	if got := getFirstStringPart(noContent); got != "" {
		t.Errorf("getFirstStringPart no content: got %q", got)
	}
}

func TestParseStreamHandoff(t *testing.T) {
	evt := map[string]interface{}{
		"type": "stream_handoff",
		"options": []interface{}{
			map[string]interface{}{
				"type":     "subscribe_ws_topic",
				"topic_id": "conversation-turn-abc123",
			},
		},
	}
	ok, topicID := parseStreamHandoff(evt)
	if !ok || topicID != "conversation-turn-abc123" {
		t.Errorf("parseStreamHandoff: ok=%v topicID=%q", ok, topicID)
	}

	noOpts := map[string]interface{}{"type": "stream_handoff"}
	ok, topicID = parseStreamHandoff(noOpts)
	if ok || topicID != "" {
		t.Errorf("parseStreamHandoff no opts: ok=%v topicID=%q", ok, topicID)
	}

	wrongType := map[string]interface{}{
		"options": []interface{}{
			map[string]interface{}{"type": "other", "topic_id": "foo"},
		},
	}
	ok, topicID = parseStreamHandoff(wrongType)
	if ok || topicID != "" {
		t.Errorf("parseStreamHandoff wrong type: ok=%v topicID=%q", ok, topicID)
	}
}

func TestParseWSFrames(t *testing.T) {
	// Array of frames
	raw := []byte(`[{"type":"message"},{"type":"reply"}]`)
	frames := parseWSFrames(raw)
	if len(frames) != 2 {
		t.Fatalf("parseWSFrames array: got %d frames", len(frames))
	}
	if frames[0]["type"] != "message" || frames[1]["type"] != "reply" {
		t.Errorf("parseWSFrames array content: got %v", frames)
	}

	// Single object
	raw = []byte(`{"type":"message"}`)
	frames = parseWSFrames(raw)
	if len(frames) != 1 {
		t.Fatalf("parseWSFrames single: got %d frames", len(frames))
	}
	if frames[0]["type"] != "message" {
		t.Errorf("parseWSFrames single content: got %v", frames)
	}

	// Empty
	frames = parseWSFrames([]byte{})
	if frames != nil {
		t.Errorf("parseWSFrames empty: got %v", frames)
	}

	// Invalid JSON
	frames = parseWSFrames([]byte(`not json`))
	if frames != nil {
		t.Errorf("parseWSFrames invalid: got %v", frames)
	}
}

func TestGenerateUUID(t *testing.T) {
	id := GenerateUUID()
	if len(id) != 36 {
		t.Errorf("GenerateUUID length: got %d, want 36", len(id))
	}
	// Check format: 8-4-4-4-12
	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if !uuidRe.MatchString(id) {
		t.Errorf("GenerateUUID format: got %q", id)
	}
	// Two calls should produce different UUIDs
	id2 := GenerateUUID()
	if id == id2 {
		t.Errorf("GenerateUUID: two calls produced the same UUID %q", id)
	}
}
