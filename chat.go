package sentinel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Chat sends a single conversation turn and returns the complete result (non-streaming).
func (c *Client) Chat(userMsg string) (*ChatResult, error) {
	return c.ChatStream(userMsg, nil)
}

// ChatStream sends a single conversation turn, receiving incremental text via the handler callback.
func (c *Client) ChatStream(userMsg string, handler StreamHandler) (*ChatResult, error) {
	turnTraceID := GenerateUUID()

	c.logf("[step 1] Fetching conduit token...")
	conduitToken, err := c.getConduitToken(c.model, turnTraceID, runeSlice(userMsg, 5))
	if err != nil {
		return nil, fmt.Errorf("get conduit token: %w", err)
	}

	c.logf("[step 2] Fetching sentinel token...")
	sentinelToken, proofToken, err := c.getSentinelToken()
	if err != nil {
		return nil, fmt.Errorf("get sentinel token: %w", err)
	}

	c.logf("[step 2.5] Establishing WebSocket connection...")
	wsConn, err := c.dialChatWS()
	if err != nil {
		return nil, fmt.Errorf("dial ws: %w", err)
	}
	defer wsConn.Close()

	msgID := GenerateUUID()
	body := map[string]interface{}{
		"action": "next",
		"messages": []map[string]interface{}{
			{
				"id":          msgID,
				"author":      map[string]string{"role": "user"},
				"create_time": float64(time.Now().UnixMilli()) / 1000.0,
				"content": map[string]interface{}{
					"content_type": "text",
					"parts":        []string{userMsg},
				},
				"metadata": map[string]interface{}{
					"developer_mode_connector_ids": []string{},
					"selected_sources":             []string{},
					"selected_github_repos":        []string{},
					"selected_all_github_repos":    false,
					"serialization_metadata":       map[string]interface{}{"custom_symbol_offsets": []interface{}{}},
				},
			},
		},
		"parent_message_id":        c.parentMessageID,
		"model":                    c.model,
		"timezone_offset_min":      -480,
		"timezone":                 "Asia/Shanghai",
		"conversation_mode":        map[string]string{"kind": "primary_assistant"},
		"enable_message_followups": true,
		"system_hints":             []string{},
		"supports_buffering":       true,
		"supported_encodings":      []string{"v1"},
		"client_contextual_info": map[string]interface{}{
			"is_dark_mode":      false,
			"time_since_loaded": int(math.Round(perfNowMs(c.startTime) / 1000.0)),
			"page_height":       1014,
			"page_width":        1055,
			"pixel_ratio":       1,
			"screen_height":     1080,
			"screen_width":      1920,
			"app_name":          "chatgpt.com",
		},
		"history_and_training_disabled":        c.tempMode,
		"paragen_cot_summary_display_override": "allow",
		"force_parallel_switch":                "auto",
		"thinking_effort":                      "standard",
	}
	if c.conversationID != "" {
		body["conversation_id"] = c.conversationID
	}

	convDesc := c.conversationID
	if convDesc == "" {
		convDesc = "(new conversation)"
	}
	c.logf("[step 3] Sending conversation: model=%s, conversation=%s, turn=%d", c.model, convDesc, c.turnCount+1)

	result, err := c.streamConversation(body, sentinelToken, proofToken, conduitToken, turnTraceID, wsConn, handler)
	if err != nil {
		return nil, err
	}

	if result.ConversationID != "" {
		c.conversationID = result.ConversationID
	}
	if result.LastAssistantMsgID != "" {
		c.parentMessageID = result.LastAssistantMsgID
	}
	c.turnCount++

	c.logf("[info] conversation_id=%s, turn=%d, reply=%d chars",
		c.conversationID, c.turnCount, len([]rune(result.Text)))

	if !c.DisableAutoImage {
		if result.ImageFileID != "" && result.ConversationID != "" {
			// Download image directly from WebSocket asset_pointer file ID, no polling needed
			c.logf("[image] Downloading image directly: %s", result.ImageFileID)
			imgPath, err := c.DownloadImageByFileID(result.ImageFileID, result.ConversationID)
			if err != nil {
				c.logf("[image] Download failed: %v", err)
			}
			result.ImagePath = imgPath
		} else if result.ImageTaskID != "" && result.ConversationID != "" {
			// Fallback: poll conversation details for asset_pointer
			imgPath, _ := c.PollAndDownloadImage(result.ConversationID)
			result.ImagePath = imgPath
		}
	}

	return result, nil
}

// getWsURL calls celsius/ws/user to get the WebSocket connection URL.
func (c *Client) getWsURL() (string, error) {
	resp, err := c.httpClient.R().
		SetHeaders(map[string]string{
			"Accept":                "*/*",
			"x-openai-target-path":  "/backend-api/celsius/ws/user",
			"x-openai-target-route": "/backend-api/celsius/ws/user",
		}).
		Get("/backend-api/celsius/ws/user")
	if err != nil {
		return "", fmt.Errorf("celsius/ws/user request: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("celsius/ws/user %d: %s", resp.StatusCode, truncateStr(resp.String(), 200))
	}
	var result struct {
		WebsocketURL string `json:"websocket_url"`
	}
	if err := json.Unmarshal(resp.Bytes(), &result); err != nil {
		return "", fmt.Errorf("parse celsius/ws/user: %w", err)
	}
	if result.WebsocketURL == "" {
		return "", fmt.Errorf("empty websocket_url")
	}
	return result.WebsocketURL, nil
}

// dialChatWS fetches the WS URL, completes the handshake and initial subscription, and returns a ready connection.
func (c *Client) dialChatWS() (*websocket.Conn, error) {
	wsURL, err := c.getWsURL()
	if err != nil {
		return nil, err
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}
	hdrs := http.Header{}
	hdrs.Set("User-Agent", c.userAgent)
	hdrs.Set("Origin", "https://chatgpt.com")

	conn, _, err := dialer.Dial(wsURL, hdrs)
	if err != nil {
		return nil, fmt.Errorf("ws dial: %w", err)
	}

	// Initialization: connect + subscribe to three base topics
	initMsg := []map[string]interface{}{
		{"id": 1, "command": map[string]interface{}{
			"type":     "connect",
			"presence": map[string]string{"type": "presence", "state": "background"},
		}},
		{"id": 2, "command": map[string]interface{}{"type": "subscribe", "topic_id": "calpico-chatgpt"}},
		{"id": 3, "command": map[string]interface{}{"type": "subscribe", "topic_id": "conversations"}},
		{"id": 4, "command": map[string]interface{}{"type": "subscribe", "topic_id": "app_notifications"}},
	}
	if err := conn.WriteJSON(initMsg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ws init send: %w", err)
	}

	// Don't wait for init reply; the subscribeWSStream read loop handles all frames uniformly
	return conn, nil
}

// wsIDCounter auto-increments WebSocket command IDs (cross-call).
var wsIDCounter int64 = 4

func nextWsID() int64 {
	return atomic.AddInt64(&wsIDCounter, 1)
}

// streamConversation posts to f/conversation, parses stream_handoff, then continues via WebSocket.
func (c *Client) streamConversation(body interface{}, sentinelToken, proofToken, conduitToken, turnTraceID string, wsConn *websocket.Conn, handler StreamHandler) (*ChatResult, error) {
	headers := map[string]string{
		"Accept":       "text/event-stream",
		"Content-Type": "application/json",
		"openai-sentinel-chat-requirements-token": sentinelToken,
		"x-conduit-token":                         conduitToken,
		"x-oai-turn-trace-id":                     turnTraceID,
		"x-openai-target-path":                    "/backend-api/f/conversation",
		"x-openai-target-route":                   "/backend-api/f/conversation",
	}
	if proofToken != "" {
		headers["openai-sentinel-proof-token"] = proofToken
	}

	resp, err := c.httpClient.R().
		SetHeaders(headers).
		SetBody(body).
		DisableAutoReadResponse().
		Post("/backend-api/f/conversation")
	if err != nil {
		return nil, fmt.Errorf("conversation request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("conversation %d: %s", resp.StatusCode, truncateStr(string(b), 500))
	}

	result := &ChatResult{}
	var lastText string
	var useDeltaEncoding bool
	var currentEvent string
	var handoffTopicID string

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimSpace(line[7:])
			if currentEvent == "delta_encoding" {
				useDeltaEncoding = true
			}
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimSpace(line[6:])
		if payload == "" || payload == "[DONE]" || payload == `"v1"` {
			continue
		}

		var evt map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			currentEvent = ""
			continue
		}

		if cid, ok := evt["conversation_id"].(string); ok && cid != "" {
			result.ConversationID = cid
		}

		evtType, _ := evt["type"].(string)
		switch evtType {
		case "resume_conversation_token":
			currentEvent = ""
			continue
		case "stream_handoff":
			_, topicID := parseStreamHandoff(evt)
			if topicID != "" {
				handoffTopicID = topicID
			}
			currentEvent = ""
			continue
		}

		// server_ste_metadata event (image/thinking scenarios): extract turn_exchange_id (fallback)
		if currentEvent == "server_ste_metadata" {
			if tid, ok := evt["turn_exchange_id"].(string); ok && tid != "" && handoffTopicID == "" {
				handoffTopicID = "conversation-turn-" + tid
			}
		}

		checkImageTaskID(evt, result)
		if useDeltaEncoding && currentEvent == "delta" {
			c.processDeltaSSE(evt, result, &lastText, handler)
		} else {
			c.processFullSSE(evt, result, &lastText, handler)
		}
		currentEvent = ""
	}

	// If we already got the reply via SSE directly, return it
	if lastText != "" {
		result.Text = lastText
		return result, nil
	}

	// Image generation scenario: conversation-turn topic carries streaming thinking (delta),
	// conversation-update carries snapshots and image asset_pointers
	if !c.DisableAutoImage && result.ImageTaskID != "" && wsConn != nil && result.ConversationID != "" {
		if handoffTopicID != "" {
			c.logf("[image-ws] Subscribing to %s and listening for conversation-update...", handoffTopicID)
			if err := c.subscribeWSImageCombined(wsConn, handoffTopicID, result.ConversationID, result, &lastText, handler); err != nil {
				c.logf("[image-ws] ws ended: %v", err)
			}
		} else {
			c.logf("[image-ws] Listening on WebSocket conversation-update for image progress...")
			if err := c.subscribeWSConvUpdate(wsConn, result.ConversationID, result, handler); err != nil {
				c.logf("[image-ws] ws listening ended: %v", err)
			}
		}
	} else if handoffTopicID != "" && wsConn != nil {
		// Normal text scenario: continue streaming via topic SSE
		c.logf("[handoff] Subscribing to WebSocket topic: %s", handoffTopicID)
		if err := c.subscribeWSStream(wsConn, handoffTopicID, result, &lastText, handler); err != nil {
			return nil, fmt.Errorf("ws stream: %w", err)
		}
	}

	result.Text = lastText
	return result, nil
}

// parseWSFrames parses WebSocket text frames into a frame list (supports JSON array or single object).
func parseWSFrames(raw []byte) []map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '[' {
		var frames []map[string]interface{}
		if err := json.Unmarshal(raw, &frames); err != nil {
			return nil
		}
		return frames
	}
	var single map[string]interface{}
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil
	}
	return []map[string]interface{}{single}
}

// processConvUpdatePayload processes a conversation-update payload:
// outputs analysis text, and if an image is found, writes result.ImageFileID and returns true.
func (c *Client) processConvUpdatePayload(payload map[string]interface{}, result *ChatResult, handler StreamHandler) bool {
	updateContent, ok := payload["update_content"].(map[string]interface{})
	if !ok {
		return false
	}
	messages, ok := updateContent["messages"].([]interface{})
	if !ok {
		return false
	}

	for _, msgI := range messages {
		msg, ok := msgI.(map[string]interface{})
		if !ok {
			continue
		}
		author, _ := msg["author"].(map[string]interface{})
		role, _ := author["role"].(string)
		channel, _ := msg["channel"].(string)
		msgContent, _ := msg["content"].(map[string]interface{})
		parts, _ := msgContent["parts"].([]interface{})

		if channel == "analysis" {
			hasText := false
			for _, part := range parts {
				if text, ok := part.(string); ok && text != "" {
					if handler != nil {
						handler(text)
					}
					hasText = true
				}
			}
			if hasText && handler != nil {
				handler("\n")
			}
			continue
		}

		if role == "tool" {
			for _, part := range parts {
				partMap, ok := part.(map[string]interface{})
				if !ok {
					continue
				}
				if partMap["content_type"] == "image_asset_pointer" {
					assetPtr, _ := partMap["asset_pointer"].(string)
					if fileID := strings.TrimPrefix(assetPtr, "sediment://"); fileID != assetPtr && fileID != "" {
						c.logf("[image-ws] Received image asset_pointer: %s", fileID)
						result.ImageFileID = fileID
						return true
					}
				}
			}
		}
	}
	return false
}

// subscribeWSImageCombined handles image generation: subscribes to conversation-turn-* to consume
// streaming delta, while also processing conversation-update to get the image.
func (c *Client) subscribeWSImageCombined(conn *websocket.Conn, turnTopicID, conversationID string, result *ChatResult, lastText *string, handler StreamHandler) error {
	subID := nextWsID()
	subMsg := []map[string]interface{}{
		{"id": subID, "command": map[string]interface{}{
			"type":     "subscribe",
			"topic_id": turnTopicID,
			"offset":   "0",
		}},
	}
	if err := conn.WriteJSON(subMsg); err != nil {
		return fmt.Errorf("ws subscribe send: %w", err)
	}

	var useDeltaEncoding bool
	var currentEvent string

	conn.SetReadDeadline(time.Now().Add(180 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	for result.ImageFileID == "" {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("ws read: %w", err)
		}
		conn.SetReadDeadline(time.Now().Add(180 * time.Second))

		frames := parseWSFrames(raw)
		for _, frame := range frames {
			fType, _ := frame["type"].(string)
			switch fType {
			case "conversation-update":
				payload, ok := frame["payload"].(map[string]interface{})
				if !ok {
					continue
				}
				if cid, _ := payload["conversation_id"].(string); cid != conversationID {
					continue
				}
				if c.processConvUpdatePayload(payload, result, handler) {
					return nil
				}
			case "reply":
				reply, ok := frame["reply"].(map[string]interface{})
				if !ok {
					continue
				}
				replyTopicID, _ := reply["topic_id"].(string)
				if replyTopicID != turnTopicID {
					continue
				}
				catchups, _ := reply["catchups"].([]interface{})
				c.logf("[ws] reply catchups=%d", len(catchups))
				for _, cu := range catchups {
					if msg, ok := cu.(map[string]interface{}); ok {
						_ = c.processWSMessage(msg, result, lastText, handler, &useDeltaEncoding, &currentEvent)
					}
				}
			case "message":
				frameTopic, _ := frame["topic_id"].(string)
				if frameTopic != turnTopicID {
					continue
				}
				_ = c.processWSMessage(frame, result, lastText, handler, &useDeltaEncoding, &currentEvent)
			}
		}
	}
	return nil
}

// subscribeWSStream subscribes to a topic via an existing WebSocket connection and
// consumes SSE data from encoded_item entries.
func (c *Client) subscribeWSStream(conn *websocket.Conn, topicID string, result *ChatResult, lastText *string, handler StreamHandler) error {
	subID := nextWsID()
	subMsg := []map[string]interface{}{
		{"id": subID, "command": map[string]interface{}{
			"type":     "subscribe",
			"topic_id": topicID,
			"offset":   "0",
		}},
	}
	if err := conn.WriteJSON(subMsg); err != nil {
		return fmt.Errorf("ws subscribe send: %w", err)
	}

	var useDeltaEncoding bool
	var currentEvent string
	done := false

	conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	for !done {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("ws read: %w", err)
		}

		conn.SetReadDeadline(time.Now().Add(120 * time.Second))

		frames := parseWSFrames(raw)
		if len(frames) == 0 {
			continue
		}

		for _, frame := range frames {
			fType, _ := frame["type"].(string)

			if fType == "reply" {
				reply, ok := frame["reply"].(map[string]interface{})
				if !ok {
					continue
				}
				replyTopicID, _ := reply["topic_id"].(string)
				if replyTopicID != topicID {
					continue
				}
				catchups, _ := reply["catchups"].([]interface{})
				c.logf("[ws] reply catchups=%d", len(catchups))
				for _, cu := range catchups {
					if msg, ok := cu.(map[string]interface{}); ok {
						d := c.processWSMessage(msg, result, lastText, handler, &useDeltaEncoding, &currentEvent)
						if d {
							done = true
						}
					}
				}
				continue
			}

			if fType == "message" {
				frameTopic, _ := frame["topic_id"].(string)
				if frameTopic != topicID {
					continue
				}
				d := c.processWSMessage(frame, result, lastText, handler, &useDeltaEncoding, &currentEvent)
				if d {
					done = true
				}
			}
		}
	}

	return nil
}

// subscribeWSConvUpdate listens for WebSocket conversation-update messages
// (image scenario, when no turn topic is available).
func (c *Client) subscribeWSConvUpdate(conn *websocket.Conn, conversationID string, result *ChatResult, handler StreamHandler) error {
	conn.SetReadDeadline(time.Now().Add(180 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("ws read: %w", err)
		}
		conn.SetReadDeadline(time.Now().Add(180 * time.Second))

		for _, frame := range parseWSFrames(raw) {
			if fType, _ := frame["type"].(string); fType != "conversation-update" {
				continue
			}
			payload, ok := frame["payload"].(map[string]interface{})
			if !ok {
				continue
			}
			if cid, _ := payload["conversation_id"].(string); cid != conversationID {
				continue
			}
			if c.processConvUpdatePayload(payload, result, handler) {
				return nil
			}
		}
	}
}

// processWSMessage processes a single WebSocket message frame.
// Returns true when the stream is done ([DONE] received).
func (c *Client) processWSMessage(frame map[string]interface{}, result *ChatResult, lastText *string, handler StreamHandler, useDeltaEncoding *bool, currentEvent *string) bool {
	payload1, ok := frame["payload"].(map[string]interface{})
	if !ok {
		return false
	}
	payload2, ok := payload1["payload"].(map[string]interface{})
	if !ok {
		return false
	}
	encoded, ok := payload2["encoded_item"].(string)
	if !ok || encoded == "" {
		return false
	}

	// encoded_item is SSE-formatted text; parse it line by line
	for _, line := range strings.Split(encoded, "\n") {
		line = strings.TrimRight(line, "\r")

		if strings.HasPrefix(line, "event: ") {
			*currentEvent = strings.TrimSpace(line[7:])
			if *currentEvent == "delta_encoding" {
				*useDeltaEncoding = true
			}
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		ssePayload := strings.TrimSpace(line[6:])
		if ssePayload == "" || ssePayload == `"v1"` {
			continue
		}
		if ssePayload == "[DONE]" {
			return true
		}

		var evt map[string]interface{}
		if err := json.Unmarshal([]byte(ssePayload), &evt); err != nil {
			*currentEvent = ""
			continue
		}

		if cid, ok := evt["conversation_id"].(string); ok && cid != "" {
			result.ConversationID = cid
		}

		evtType, _ := evt["type"].(string)
		if evtType == "resume_conversation_token" || evtType == "stream_handoff" {
			*currentEvent = ""
			continue
		}

		checkImageTaskID(evt, result)
		if *useDeltaEncoding && *currentEvent == "delta" {
			c.processDeltaSSE(evt, result, lastText, handler)
		} else {
			c.processFullSSE(evt, result, lastText, handler)
		}
		*currentEvent = ""
	}
	return false
}

// parseStreamHandoff extracts the resume_sse_endpoint topic_id from a stream_handoff event.
func parseStreamHandoff(evt map[string]interface{}) (bool, string) {
	options, ok := evt["options"].([]interface{})
	if !ok {
		return false, ""
	}
	for _, optRaw := range options {
		opt, ok := optRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if typ, _ := opt["type"].(string); typ == "subscribe_ws_topic" {
			topicID, _ := opt["topic_id"].(string)
			return topicID != "", topicID
		}
	}
	return false, ""
}

// checkImageTaskID extracts the image task ID from an SSE event
// (supports both legacy image_gen_task_id and new ghostrider format).
func checkImageTaskID(evt map[string]interface{}, result *ChatResult) {
	extractFromMeta := func(meta map[string]interface{}) {
		if tid, ok := meta["image_gen_task_id"].(string); ok && tid != "" {
			result.ImageTaskID = tid
			return
		}
		if result.ImageTaskID == "" {
			if _, ok := meta["ghostrider"]; ok {
				result.ImageTaskID = "ghostrider"
			}
		}
	}

	if v, ok := evt["v"].(map[string]interface{}); ok {
		if msg, ok := v["message"].(map[string]interface{}); ok {
			if meta, ok := msg["metadata"].(map[string]interface{}); ok {
				extractFromMeta(meta)
			}
		}
	}
}

// processDeltaSSE processes delta-encoding SSE events.
// ChatGPT delta format has multiple variants:
//
//	A) Top-level patch: {"p":"/message/content/parts/0","o":"append","v":"text"}
//	B) Simple append: {"v":"text"} (p/o omitted, implicit append to parts/0)
//	C) Message object add: {"p":"","o":"add","v":{"message":{...}}}
//	D) Completion patch array: {"p":"","o":"patch","v":[...patches...]}
func (c *Client) processDeltaSSE(evt map[string]interface{}, result *ChatResult, lastText *string, handler StreamHandler) {
	pPath, _ := evt["p"].(string)
	pOp, _ := evt["o"].(string)

	// Format A: top-level append patch
	if pPath == "/message/content/parts/0" && pOp == "append" {
		if text, ok := evt["v"].(string); ok && text != "" {
			*lastText += text
			if handler != nil {
				handler(text)
			}
		}
		return
	}

	v := evt["v"]

	// Format B: only v field, and it's a string → implicit append
	_, hasP := evt["p"]
	_, hasO := evt["o"]
	if !hasP && !hasO {
		if text, ok := v.(string); ok && text != "" {
			*lastText += text
			if handler != nil {
				handler(text)
			}
			return
		}
	}

	// Format C: v is a map containing message (message object init or final channel)
	if vMap, ok := v.(map[string]interface{}); ok {
		if msgRaw, exists := vMap["message"]; exists {
			if msg, ok := msgRaw.(map[string]interface{}); ok {
				author := getNestedString(msg, "author", "role")
				channel, _ := msg["channel"].(string)
				msgID, _ := msg["id"].(string)

				if author == "assistant" && msgID != "" {
					result.LastAssistantMsgID = msgID
				}
				if author == "tool" {
					if meta, ok := msg["metadata"].(map[string]interface{}); ok {
						if tid, ok := meta["image_gen_task_id"].(string); ok && tid != "" {
							result.ImageTaskID = tid
						}
						// New ghostrider async image generation: no image_gen_task_id;
						// use "ghostrider" as the trigger flag
						if result.ImageTaskID == "" {
							if _, ok := meta["ghostrider"]; ok {
								result.ImageTaskID = "ghostrider"
							}
						}
					}
				}
				// Full text on the final channel (usually a final confirmation;
				// at this point lastText should already have accumulated the complete text)
				if author == "assistant" && channel == "final" {
					if text := getFirstStringPart(msg); text != "" && len(text) > len(*lastText) {
						delta := text[len(*lastText):]
						*lastText = text
						if handler != nil && delta != "" {
							handler(delta)
						}
					}
				}
			}
		}
	}

	// Format D: v is a patches array (batch patch)
	if patches, ok := v.([]interface{}); ok {
		for _, p := range patches {
			if patch, ok := p.(map[string]interface{}); ok {
				pp, _ := patch["p"].(string)
				po, _ := patch["o"].(string)
				if pp == "/message/content/parts/0" && po == "append" {
					if text, ok := patch["v"].(string); ok && text != "" {
						*lastText += text
						if handler != nil {
							handler(text)
						}
					}
				}
			}
		}
	}
}

// processFullSSE processes non-delta-encoding SSE events.
func (c *Client) processFullSSE(evt map[string]interface{}, result *ChatResult, lastText *string, handler StreamHandler) {
	msgRaw, exists := evt["message"]
	if !exists {
		return
	}
	msg, ok := msgRaw.(map[string]interface{})
	if !ok {
		return
	}

	author := getNestedString(msg, "author", "role")
	msgID, _ := msg["id"].(string)

	if author == "assistant" && msgID != "" {
		result.LastAssistantMsgID = msgID
	}

	if meta, ok := msg["metadata"].(map[string]interface{}); ok {
		if tid, ok := meta["image_gen_task_id"].(string); ok && tid != "" {
			result.ImageTaskID = tid
		}
	}

	if author == "assistant" {
		if text := getFirstStringPart(msg); text != "" && len(text) > len(*lastText) {
			delta := text[len(*lastText):]
			if handler != nil {
				handler(delta)
			}
			*lastText = text
		}
	}
}
