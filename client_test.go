package sentinel

import (
	"net/http/httptest"
	"testing"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient(Config{
		BearerToken: "test-token",
	})
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	info := c.GetSessionInfo()
	if info.Model != defaultModel {
		t.Errorf("default model: got %q, want %q", info.Model, defaultModel)
	}
	if info.ParentMessageID != "client-created-root" {
		t.Errorf("default parentMessageID: got %q", info.ParentMessageID)
	}
	if info.TurnCount != 0 {
		t.Errorf("default turnCount: got %d", info.TurnCount)
	}
	if info.TempMode != false {
		t.Errorf("default tempMode: got %v", info.TempMode)
	}
}

func TestResetSession(t *testing.T) {
	c := NewClient(Config{BearerToken: "test"})
	c.conversationID = "conv-123"
	c.parentMessageID = "msg-456"
	c.turnCount = 5
	c.ResetSession()
	info := c.GetSessionInfo()
	if info.ConversationID != "" {
		t.Errorf("conversationID after reset: got %q", info.ConversationID)
	}
	if info.ParentMessageID != "client-created-root" {
		t.Errorf("parentMessageID after reset: got %q", info.ParentMessageID)
	}
	if info.TurnCount != 0 {
		t.Errorf("turnCount after reset: got %d", info.TurnCount)
	}
}

func TestSetModel(t *testing.T) {
	c := NewClient(Config{BearerToken: "test"})
	c.SetModel("gpt-4o")
	if m := c.GetModel(); m != "gpt-4o" {
		t.Errorf("GetModel: got %q", m)
	}
	info := c.GetSessionInfo()
	if info.Model != "gpt-4o" {
		t.Errorf("GetSessionInfo.Model: got %q", info.Model)
	}
}

func TestSetTempMode(t *testing.T) {
	c := NewClient(Config{BearerToken: "test"})
	c.SetTempMode(true)
	info := c.GetSessionInfo()
	if !info.TempMode {
		t.Errorf("TempMode: got %v, want true", info.TempMode)
	}
	c.SetTempMode(false)
	info = c.GetSessionInfo()
	if info.TempMode {
		t.Errorf("TempMode: got %v, want false", info.TempMode)
	}
}

func TestSetDisableAutoImage(t *testing.T) {
	c := NewClient(Config{BearerToken: "test"})
	if c.DisableAutoImage {
		t.Error("DisableAutoImage should be false by default")
	}
	c.SetDisableAutoImage(true)
	if !c.DisableAutoImage {
		t.Error("DisableAutoImage should be true after SetDisableAutoImage(true)")
	}
	c.SetDisableAutoImage(false)
	if c.DisableAutoImage {
		t.Error("DisableAutoImage should be false after SetDisableAutoImage(false)")
	}
}

func TestGetSessionInfo(t *testing.T) {
	c := NewClient(Config{
		BearerToken: "test",
		Model:       "gpt-4o",
		TempMode:    true,
	})
	info := c.GetSessionInfo()
	if info.Model != "gpt-4o" {
		t.Errorf("Model: got %q, want %q", info.Model, "gpt-4o")
	}
	if !info.TempMode {
		t.Errorf("TempMode: got %v, want true", info.TempMode)
	}
	if info.ConversationID != "" {
		t.Errorf("ConversationID: got %q, want empty", info.ConversationID)
	}
	if info.ParentMessageID != "client-created-root" {
		t.Errorf("ParentMessageID: got %q", info.ParentMessageID)
	}
}

func TestConfigBaseURL(t *testing.T) {
	ts := httptest.NewServer(nil)
	defer ts.Close()

	c := NewClient(Config{
		BearerToken:        "test",
		BaseURL:            ts.URL,
		DisableImpersonate: true,
	})
	if c.httpClient == nil {
		t.Fatal("httpClient with custom baseURL is nil")
	}
}

func TestConfigDisableImpersonate(t *testing.T) {
	ts := httptest.NewServer(nil)
	defer ts.Close()

	c := NewClient(Config{
		BearerToken:        "test",
		BaseURL:            ts.URL,
		DisableImpersonate: true,
	})
	if c.httpClient == nil {
		t.Fatal("httpClient is nil with DisableImpersonate")
	}
	// Verify we can make a request (even if it fails, should not panic)
	resp, err := c.httpClient.R().Get("/test")
	if err != nil {
		t.Logf("Request error (expected for nil handler): %v", err)
	} else {
		t.Logf("Response status: %d", resp.StatusCode)
	}
}

func TestNewClientCustomFields(t *testing.T) {
	c := NewClient(Config{
		BearerToken:  "my-token",
		CookieString: "cookie-val",
		Model:        "gpt-4o-mini",
		DeviceID:     "my-device",
		BuildHash:    "custom-hash",
		BuildNumber:  "custom-num",
		UserAgent:    "custom-ua",
		Language:     "en-US",
		ImageDir:     "custom-images",
		TempMode:     true,
	})
	if c.bearerToken != "my-token" {
		t.Errorf("bearerToken: got %q", c.bearerToken)
	}
	if c.cookieStr != "cookie-val" {
		t.Errorf("cookieStr: got %q", c.cookieStr)
	}
	if c.userAgent != "custom-ua" {
		t.Errorf("userAgent: got %q", c.userAgent)
	}
	if c.deviceID != "my-device" {
		t.Errorf("deviceID: got %q", c.deviceID)
	}
	if c.buildHash != "custom-hash" {
		t.Errorf("buildHash: got %q", c.buildHash)
	}
	if c.buildNumber != "custom-num" {
		t.Errorf("buildNumber: got %q", c.buildNumber)
	}
	if c.language != "en-US" {
		t.Errorf("language: got %q", c.language)
	}
	if c.imageDir != "custom-images" {
		t.Errorf("imageDir: got %q", c.imageDir)
	}
	info := c.GetSessionInfo()
	if info.Model != "gpt-4o-mini" {
		t.Errorf("Model: got %q", info.Model)
	}
	if !info.TempMode {
		t.Errorf("TempMode: got %v, want true", info.TempMode)
	}
}

func TestNewClientDefaultBaseURL(t *testing.T) {
	c := NewClient(Config{BearerToken: "test"})
	// When BaseURL is empty, client should use https://chatgpt.com
	// We can't directly inspect the base URL from req.Client, but the client
	// should be created successfully
	if c.httpClient == nil {
		t.Fatal("httpClient is nil with default baseURL")
	}
}

func TestSetConversationID(t *testing.T) {
	c := NewClient(Config{BearerToken: "test"})
	c.SetConversationID("conv-abc")
	info := c.GetSessionInfo()
	if info.ConversationID != "conv-abc" {
		t.Errorf("ConversationID: got %q, want %q", info.ConversationID, "conv-abc")
	}
}

func TestSetParentMessageID(t *testing.T) {
	c := NewClient(Config{BearerToken: "test"})
	c.SetParentMessageID("msg-xyz")
	info := c.GetSessionInfo()
	if info.ParentMessageID != "msg-xyz" {
		t.Errorf("ParentMessageID: got %q, want %q", info.ParentMessageID, "msg-xyz")
	}
}
