package sentinel

// Config holds the client configuration.
type Config struct {
	BearerToken        string // Required: ChatGPT Bearer Token (JWT)
	CookieString       string // Optional: Cookie string
	Model              string // Optional: Default model "gpt-5-5-thinking"
	DeviceID           string // Optional: Device ID; auto-generates UUID if empty
	BuildHash          string // Optional: Client build hash
	BuildNumber        string // Optional: Client build number
	UserAgent          string // Optional: User-Agent string
	Language           string // Optional: Language, default "zh-CN"
	ImageDir           string // Optional: Image save directory, default "images"
	TempMode           bool   // Optional: Temporary mode (don't save conversation history)
	BaseURL            string // Optional: Backend base URL, default "https://chatgpt.com"
	DisableImpersonate bool   // Optional: Disable Chrome TLS fingerprint impersonation (for testing)
}

// ChatResult holds the result of a single chat turn.
type ChatResult struct {
	Text               string // Full text of the assistant reply
	ConversationID     string // Conversation ID
	LastAssistantMsgID string // Last assistant message ID (for multi-turn chaining)
	ImageTaskID        string // DALL-E image task trigger flag (if any)
	ImageFileID        string // Image file ID (extracted from asset_pointer, e.g. file_xxx)
	ImagePath          string // Local path of the downloaded image (if any)
}

// SessionInfo is a snapshot of the current session state.
type SessionInfo struct {
	ConversationID  string
	ParentMessageID string
	Model           string
	TempMode        bool
	TurnCount       int
}

// StreamHandler is the streaming callback invoked for each text delta.
type StreamHandler func(delta string)

// LogFunc is the signature for log output functions.
type LogFunc func(format string, args ...interface{})
