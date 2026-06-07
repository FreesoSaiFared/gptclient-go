package sentinel

import (
	"testing"
)

func TestProcessDeltaSSE_FormatA_TopLevelAppend(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	var deltas []string
	handler := func(d string) { deltas = append(deltas, d) }

	evt := map[string]interface{}{
		"p": "/message/content/parts/0",
		"o": "append",
		"v": "Hello",
	}
	c := &Client{}
	c.processDeltaSSE(evt, result, &lastText, handler)

	if lastText != "Hello" {
		t.Errorf("lastText: got %q, want %q", lastText, "Hello")
	}
	if len(deltas) != 1 || deltas[0] != "Hello" {
		t.Errorf("deltas: got %v", deltas)
	}
}

func TestProcessDeltaSSE_FormatA_MultipleAppends(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	var deltas []string
	handler := func(d string) { deltas = append(deltas, d) }

	c := &Client{}

	// First append
	evt1 := map[string]interface{}{
		"p": "/message/content/parts/0",
		"o": "append",
		"v": "Hello",
	}
	c.processDeltaSSE(evt1, result, &lastText, handler)

	// Second append
	evt2 := map[string]interface{}{
		"p": "/message/content/parts/0",
		"o": "append",
		"v": " world",
	}
	c.processDeltaSSE(evt2, result, &lastText, handler)

	if lastText != "Hello world" {
		t.Errorf("lastText: got %q, want %q", lastText, "Hello world")
	}
	if len(deltas) != 2 || deltas[0] != "Hello" || deltas[1] != " world" {
		t.Errorf("deltas: got %v", deltas)
	}
}

func TestProcessDeltaSSE_FormatB_SimpleV(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	var deltas []string
	handler := func(d string) { deltas = append(deltas, d) }

	evt := map[string]interface{}{
		"v": " world",
	}
	c := &Client{}
	c.processDeltaSSE(evt, result, &lastText, handler)

	if lastText != " world" {
		t.Errorf("lastText: got %q, want %q", lastText, " world")
	}
	if len(deltas) != 1 || deltas[0] != " world" {
		t.Errorf("deltas: got %v", deltas)
	}
}

func TestProcessDeltaSSE_FormatB_EmptyVIgnored(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	handler := func(d string) {}

	evt := map[string]interface{}{
		"v": "",
	}
	c := &Client{}
	c.processDeltaSSE(evt, result, &lastText, handler)

	if lastText != "" {
		t.Errorf("lastText should be empty for empty v, got %q", lastText)
	}
}

func TestProcessDeltaSSE_FormatC_AssistantFinal(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	handler := func(d string) {}

	evt := map[string]interface{}{
		"p": "",
		"o": "add",
		"v": map[string]interface{}{
			"message": map[string]interface{}{
				"author": map[string]interface{}{
					"role": "assistant",
				},
				"id":      "msg-assistant-123",
				"channel": "final",
				"content": map[string]interface{}{
					"content_type": "text",
					"parts":        []interface{}{"Complete response text"},
				},
			},
		},
	}
	c := &Client{}
	c.processDeltaSSE(evt, result, &lastText, handler)

	if result.LastAssistantMsgID != "msg-assistant-123" {
		t.Errorf("LastAssistantMsgID: got %q, want %q", result.LastAssistantMsgID, "msg-assistant-123")
	}
	if lastText != "Complete response text" {
		t.Errorf("lastText: got %q, want %q", lastText, "Complete response text")
	}
}

func TestProcessDeltaSSE_FormatC_AssistantFinal_NoOverrideIfShorter(t *testing.T) {
	result := &ChatResult{}
	lastText := "Already accumulated longer text here"
	handler := func(d string) {}

	evt := map[string]interface{}{
		"p": "",
		"o": "add",
		"v": map[string]interface{}{
			"message": map[string]interface{}{
				"author": map[string]interface{}{
					"role": "assistant",
				},
				"id":      "msg-assistant-123",
				"channel": "final",
				"content": map[string]interface{}{
					"content_type": "text",
					"parts":        []interface{}{"Short"},
				},
			},
		},
	}
	c := &Client{}
	c.processDeltaSSE(evt, result, &lastText, handler)

	// Should NOT override because "Short" (5) < len("Already accumulated longer text here") (35)
	if lastText != "Already accumulated longer text here" {
		t.Errorf("lastText should not be overridden by shorter text, got %q", lastText)
	}
}

func TestProcessDeltaSSE_FormatD_PatchArray(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	var deltas []string
	handler := func(d string) { deltas = append(deltas, d) }

	evt := map[string]interface{}{
		"p": "",
		"o": "patch",
		"v": []interface{}{
			map[string]interface{}{
				"p": "/message/content/parts/0",
				"o": "append",
				"v": "chunk1",
			},
			map[string]interface{}{
				"p": "/message/content/parts/0",
				"o": "append",
				"v": "chunk2",
			},
		},
	}
	c := &Client{}
	c.processDeltaSSE(evt, result, &lastText, handler)

	if lastText != "chunk1chunk2" {
		t.Errorf("lastText: got %q, want %q", lastText, "chunk1chunk2")
	}
	if len(deltas) != 2 {
		t.Fatalf("deltas: got %d, want 2", len(deltas))
	}
	if deltas[0] != "chunk1" || deltas[1] != "chunk2" {
		t.Errorf("deltas: got %v", deltas)
	}
}

func TestProcessDeltaSSE_AssistantMsgID(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	handler := func(d string) {}

	evt := map[string]interface{}{
		"v": map[string]interface{}{
			"message": map[string]interface{}{
				"author": map[string]interface{}{"role": "assistant"},
				"id":     "msg-id-xyz",
			},
		},
	}
	c := &Client{}
	c.processDeltaSSE(evt, result, &lastText, handler)

	if result.LastAssistantMsgID != "msg-id-xyz" {
		t.Errorf("LastAssistantMsgID: got %q, want %q", result.LastAssistantMsgID, "msg-id-xyz")
	}
}

func TestProcessDeltaSSE_ToolMessage_ImageTaskID(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	handler := func(d string) {}

	evt := map[string]interface{}{
		"v": map[string]interface{}{
			"message": map[string]interface{}{
				"author": map[string]interface{}{"role": "tool"},
				"id":     "msg-tool-1",
				"metadata": map[string]interface{}{
					"image_gen_task_id": "task-123",
				},
			},
		},
	}
	c := &Client{}
	c.processDeltaSSE(evt, result, &lastText, handler)

	if result.ImageTaskID != "task-123" {
		t.Errorf("ImageTaskID: got %q, want %q", result.ImageTaskID, "task-123")
	}
}

func TestProcessFullSSE(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	var deltas []string
	handler := func(d string) { deltas = append(deltas, d) }

	evt := map[string]interface{}{
		"message": map[string]interface{}{
			"author": map[string]interface{}{
				"role": "assistant",
			},
			"id": "msg-full-123",
			"content": map[string]interface{}{
				"content_type": "text",
				"parts":        []interface{}{"Hello from full SSE"},
			},
		},
	}
	c := &Client{}
	c.processFullSSE(evt, result, &lastText, handler)

	if lastText != "Hello from full SSE" {
		t.Errorf("lastText: got %q, want %q", lastText, "Hello from full SSE")
	}
	if result.LastAssistantMsgID != "msg-full-123" {
		t.Errorf("LastAssistantMsgID: got %q", result.LastAssistantMsgID)
	}
	if len(deltas) != 1 || deltas[0] != "Hello from full SSE" {
		t.Errorf("deltas: got %v", deltas)
	}
}

func TestProcessFullSSE_IncrementalUpdate(t *testing.T) {
	result := &ChatResult{}
	lastText := "Hello"
	var deltas []string
	handler := func(d string) { deltas = append(deltas, d) }

	evt := map[string]interface{}{
		"message": map[string]interface{}{
			"author": map[string]interface{}{"role": "assistant"},
			"id":     "msg-2",
			"content": map[string]interface{}{
				"parts": []interface{}{"Hello world"},
			},
		},
	}
	c := &Client{}
	c.processFullSSE(evt, result, &lastText, handler)

	if lastText != "Hello world" {
		t.Errorf("lastText: got %q, want %q", lastText, "Hello world")
	}
	// Only the delta should be sent
	if len(deltas) != 1 || deltas[0] != " world" {
		t.Errorf("deltas: got %v, want [ world]", deltas)
	}
}

func TestProcessFullSSE_NonAssistant(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	handler := func(d string) {}

	evt := map[string]interface{}{
		"message": map[string]interface{}{
			"author": map[string]interface{}{"role": "user"},
			"id":     "msg-user-123",
			"content": map[string]interface{}{
				"parts": []interface{}{"User message"},
			},
		},
	}
	c := &Client{}
	c.processFullSSE(evt, result, &lastText, handler)

	if lastText != "" {
		t.Errorf("lastText should be empty for user message, got %q", lastText)
	}
}

func TestProcessFullSSE_NoMessage(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	handler := func(d string) {}

	evt := map[string]interface{}{
		"type": "other",
	}
	c := &Client{}
	c.processFullSSE(evt, result, &lastText, handler)

	if lastText != "" {
		t.Errorf("lastText should be empty for event without message, got %q", lastText)
	}
}

func TestProcessWSMessage(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	handler := func(d string) {}
	useDelta := false
	currentEvt := ""

	sseData := "data: {\"message\":{\"author\":{\"role\":\"assistant\"},\"id\":\"ws-msg-1\",\"content\":{\"content_type\":\"text\",\"parts\":[\"WS reply\"]}}}\n\n"
	frame := map[string]interface{}{
		"payload": map[string]interface{}{
			"payload": map[string]interface{}{
				"encoded_item": sseData,
			},
		},
	}

	c := &Client{}
	done := c.processWSMessage(frame, result, ChatOptions{}, &lastText, handler, &useDelta, &currentEvt)

	if done {
		t.Error("processWSMessage should not be done yet")
	}
	if lastText != "WS reply" {
		t.Errorf("lastText: got %q, want %q", lastText, "WS reply")
	}
	if result.LastAssistantMsgID != "ws-msg-1" {
		t.Errorf("LastAssistantMsgID: got %q", result.LastAssistantMsgID)
	}
}

func TestProcessWSMessage_Done(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	handler := func(d string) {}
	useDelta := false
	currentEvt := ""

	sseData := "data: [DONE]\n\n"
	frame := map[string]interface{}{
		"payload": map[string]interface{}{
			"payload": map[string]interface{}{
				"encoded_item": sseData,
			},
		},
	}

	c := &Client{}
	done := c.processWSMessage(frame, result, ChatOptions{}, &lastText, handler, &useDelta, &currentEvt)

	if !done {
		t.Error("processWSMessage should return true for [DONE]")
	}
}

func TestProcessWSMessage_DeltaEncoding(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	var deltas []string
	handler := func(d string) { deltas = append(deltas, d) }
	useDelta := false
	currentEvt := ""

	sseData := "event: delta_encoding\nevent: delta\ndata: {\"v\":\"delta text\"}\n\n"
	frame := map[string]interface{}{
		"payload": map[string]interface{}{
			"payload": map[string]interface{}{
				"encoded_item": sseData,
			},
		},
	}

	c := &Client{}
	c.processWSMessage(frame, result, ChatOptions{}, &lastText, handler, &useDelta, &currentEvt)

	if !useDelta {
		t.Error("useDelta should be true after delta_encoding event")
	}
	if lastText != "delta text" {
		t.Errorf("lastText: got %q, want %q", lastText, "delta text")
	}
}

func TestProcessWSMessage_ConversationID(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	handler := func(d string) {}
	useDelta := false
	currentEvt := ""

	sseData := "data: {\"conversation_id\":\"conv-xyz-789\",\"message\":{\"author\":{\"role\":\"assistant\"},\"id\":\"msg-1\",\"content\":{\"parts\":[\"text\"]}}}\n\n"
	frame := map[string]interface{}{
		"payload": map[string]interface{}{
			"payload": map[string]interface{}{
				"encoded_item": sseData,
			},
		},
	}

	c := &Client{}
	c.processWSMessage(frame, result, ChatOptions{}, &lastText, handler, &useDelta, &currentEvt)

	if result.ConversationID != "conv-xyz-789" {
		t.Errorf("ConversationID: got %q, want %q", result.ConversationID, "conv-xyz-789")
	}
}

func TestProcessWSMessage_MissingPayload(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	handler := func(d string) {}
	useDelta := false
	currentEvt := ""

	frame := map[string]interface{}{
		"type": "message",
	}

	c := &Client{}
	done := c.processWSMessage(frame, result, ChatOptions{}, &lastText, handler, &useDelta, &currentEvt)

	if !done {
		t.Error("processWSMessage should return false for missing payload")
	}
}

func TestProcessWSMessage_EmptyEncodedItem(t *testing.T) {
	result := &ChatResult{}
	var lastText string
	handler := func(d string) {}
	useDelta := false
	currentEvt := ""

	frame := map[string]interface{}{
		"payload": map[string]interface{}{
			"payload": map[string]interface{}{
				"encoded_item": "",
			},
		},
	}

	c := &Client{}
	done := c.processWSMessage(frame, result, ChatOptions{}, &lastText, handler, &useDelta, &currentEvt)

	if !done {
		t.Error("processWSMessage should return false for empty encoded_item")
	}
}
