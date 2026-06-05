package sentinel

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// --- Netscape Parser Tests ---

func TestParseNetscapeCookies_Normal(t *testing.T) {
	input := `.chatgpt.com	TRUE	/	TRUE	0	__Secure-token	abc123
chatgpt.com	FALSE	/	FALSE	1735689600	session_id	xyz789
`
	records, err := ParseNetscapeCookies(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseNetscapeCookies: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(records))
	}
	if records[0].Domain != ".chatgpt.com" {
		t.Errorf("domain: got %q", records[0].Domain)
	}
	if records[0].Name != "__Secure-token" {
		t.Errorf("name: got %q", records[0].Name)
	}
	if records[0].Secure != true {
		t.Errorf("secure: got %v", records[0].Secure)
	}
	if records[1].Domain != "chatgpt.com" {
		t.Errorf("domain[1]: got %q", records[1].Domain)
	}
	if records[1].Expires == nil {
		t.Error("expected non-nil expires for second cookie")
	}
}

func TestParseNetscapeCookies_HttpOnly(t *testing.T) {
	input := `#HttpOnly_.chatgpt.com	TRUE	/	TRUE	0	httponly_cookie	val1
`
	records, err := ParseNetscapeCookies(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseNetscapeCookies: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(records))
	}
	if records[0].HTTPOnly != true {
		t.Error("expected HTTPOnly=true")
	}
	if records[0].Domain != ".chatgpt.com" {
		t.Errorf("domain: got %q", records[0].Domain)
	}
	if records[0].Name != "httponly_cookie" {
		t.Errorf("name: got %q", records[0].Name)
	}
}

func TestParseNetscapeCookies_Comments(t *testing.T) {
	input := `# This is a comment
# Another comment

.chatgpt.com	TRUE	/	TRUE	0	test	val
`
	records, err := ParseNetscapeCookies(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseNetscapeCookies: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(records))
	}
}

func TestParseNetscapeCookies_ExpiryZero(t *testing.T) {
	input := `chatgpt.com	FALSE	/	FALSE	0	session	testval
`
	records, err := ParseNetscapeCookies(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseNetscapeCookies: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(records))
	}
	if records[0].Expires != nil {
		t.Errorf("expected nil Expires for expiry=0, got %v", records[0].Expires)
	}
}

func TestParseNetscapeCookies_FilterChatGPT(t *testing.T) {
	input := `.chatgpt.com	TRUE	/	TRUE	0	gpt_cookie	gptval
auth.openai.com	TRUE	/	TRUE	0	auth_cookie	authval
.unrelated.com	TRUE	/	TRUE	0	other	otherval
`
	records, err := ParseNetscapeCookies(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseNetscapeCookies: %v", err)
	}

	header := CookieHeaderForURL(records, "https://chatgpt.com/")
	// Should include chatgpt.com cookie but not auth.openai.com or unrelated.com
	if !strings.Contains(header, "gpt_cookie=gptval") {
		t.Errorf("expected gpt_cookie in header, got %q", header)
	}
	if strings.Contains(header, "auth_cookie") {
		t.Errorf("should not include auth_cookie, got %q", header)
	}
	if strings.Contains(header, "other") {
		t.Errorf("should not include unrelated cookie, got %q", header)
	}
}

// --- CookieHeaderForURL Tests ---

func TestCookieHeaderForURL_ExactDomain(t *testing.T) {
	records := []CookieRecord{
		{Domain: "chatgpt.com", Path: "/", Name: "a", Value: "1", Secure: true},
	}
	header := CookieHeaderForURL(records, "https://chatgpt.com/")
	if header != "a=1" {
		t.Errorf("got %q, want %q", header, "a=1")
	}
}

func TestCookieHeaderForURL_DotDomain(t *testing.T) {
	records := []CookieRecord{
		{Domain: ".chatgpt.com", Path: "/", Name: "a", Value: "1", Secure: true},
	}
	header := CookieHeaderForURL(records, "https://chatgpt.com/")
	if header != "a=1" {
		t.Errorf("got %q, want %q", header, "a=1")
	}
}

func TestCookieHeaderForURL_Subdomain(t *testing.T) {
	records := []CookieRecord{
		{Domain: ".chatgpt.com", Path: "/", Name: "a", Value: "1", Secure: true},
	}
	header := CookieHeaderForURL(records, "https://sub.chatgpt.com/")
	if header != "a=1" {
		t.Errorf("got %q, want %q", header, "a=1")
	}
}

func TestCookieHeaderForURL_ExcludeUnrelated(t *testing.T) {
	records := []CookieRecord{
		{Domain: "auth.openai.com", Path: "/", Name: "b", Value: "2", Secure: true},
	}
	header := CookieHeaderForURL(records, "https://chatgpt.com/")
	if header != "" {
		t.Errorf("expected empty header for unrelated domain, got %q", header)
	}
}

func TestCookieHeaderForURL_ExcludeExpired(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour)
	records := []CookieRecord{
		{Domain: "chatgpt.com", Path: "/", Name: "expired", Value: "old", Secure: true, Expires: &past},
	}
	header := CookieHeaderForURL(records, "https://chatgpt.com/")
	if header != "" {
		t.Errorf("expected empty header for expired cookie, got %q", header)
	}
}

func TestCookieHeaderForURL_DeterministicOrder(t *testing.T) {
	records := []CookieRecord{
		{Domain: "chatgpt.com", Path: "/", Name: "zebra", Value: "z", Secure: true},
		{Domain: "chatgpt.com", Path: "/", Name: "alpha", Value: "a", Secure: true},
		{Domain: "chatgpt.com", Path: "/", Name: "middle", Value: "m", Secure: true},
	}
	header := CookieHeaderForURL(records, "https://chatgpt.com/")
	expected := "alpha=a; middle=m; zebra=z"
	if header != expected {
		t.Errorf("got %q, want %q", header, expected)
	}
}

func TestCookieHeaderForURL_SecureCookieOnHTTP(t *testing.T) {
	records := []CookieRecord{
		{Domain: "chatgpt.com", Path: "/", Name: "secure_only", Value: "val", Secure: true},
	}
	header := CookieHeaderForURL(records, "http://chatgpt.com/")
	if header != "" {
		t.Errorf("expected empty header for secure cookie on HTTP, got %q", header)
	}
}

func TestCookieHeaderForURL_EmptyNameSkipped(t *testing.T) {
	records := []CookieRecord{
		{Domain: "chatgpt.com", Path: "/", Name: "", Value: "skip", Secure: true},
		{Domain: "chatgpt.com", Path: "/", Name: "valid", Value: "keep", Secure: true},
	}
	header := CookieHeaderForURL(records, "https://chatgpt.com/")
	if header != "valid=keep" {
		t.Errorf("got %q, want %q", header, "valid=keep")
	}
}

// --- Firefox Extraction Tests ---

func TestExtractFirefox_CookieDB(t *testing.T) {
	tmpDir := t.TempDir()
	profileDir := filepath.Join(tmpDir, "profile")
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(profileDir, "cookies.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("create sqlite: %v", err)
	}

	// Create schema
	_, err = db.Exec(`CREATE TABLE moz_cookies (
		id INTEGER PRIMARY KEY,
		host TEXT,
		name TEXT,
		value TEXT,
		path TEXT,
		expiry REAL,
		isSecure INTEGER
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert test cookie
	_, err = db.Exec(`INSERT INTO moz_cookies (host, name, value, path, expiry, isSecure) VALUES (?, ?, ?, ?, ?, ?)`,
		".chatgpt.com", "test_ff", "ff_value", "/", 0.0, 1)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Insert a cookie for unrelated domain
	_, err = db.Exec(`INSERT INTO moz_cookies (host, name, value, path, expiry, isSecure) VALUES (?, ?, ?, ?, ?, ?)`,
		"other.example.com", "unrelated", "skip", "/", 0.0, 0)
	if err != nil {
		t.Fatalf("insert unrelated: %v", err)
	}

	// Set PRAGMA user_version for schema detection
	_, _ = db.Exec("PRAGMA user_version = 10")
	db.Close()

	records, err := readFirefoxCookies(dbPath)
	if err != nil {
		t.Fatalf("readFirefoxCookies: %v", err)
	}

	header := CookieHeaderForURL(records, "https://chatgpt.com/")
	if !strings.Contains(header, "test_ff=ff_value") {
		t.Errorf("expected test_ff cookie in header, got %q", header)
	}
	if strings.Contains(header, "unrelated") {
		t.Errorf("should not include unrelated domain cookie, got %q", header)
	}
}

func TestExtractFirefox_Schema16_MillisecondExpiry(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "cookies.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("create sqlite: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE moz_cookies (
		id INTEGER PRIMARY KEY,
		host TEXT, name TEXT, value TEXT, path TEXT,
		expiry REAL, isSecure INTEGER
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert cookie with millisecond expiry (future time)
	futureMs := float64(time.Now().Add(24*time.Hour).Unix() * 1000)
	_, err = db.Exec(`INSERT INTO moz_cookies (host, name, value, path, expiry, isSecure) VALUES (?, ?, ?, ?, ?, ?)`,
		".chatgpt.com", "ms_cookie", "val", "/", futureMs, 1)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	_, _ = db.Exec("PRAGMA user_version = 16")
	db.Close()

	records, err := readFirefoxCookies(dbPath)
	if err != nil {
		t.Fatalf("readFirefoxCookies: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Expires == nil {
		t.Fatal("expected non-nil expires")
	}
	// The expires should be in the future
	if records[0].Expires.Before(time.Now()) {
		t.Errorf("expiry should be in the future after ms conversion, got %v", records[0].Expires)
	}
}

// --- Chromium Extraction Tests ---

func createChromiumTestDB(t *testing.T, tmpDir string, metaVersion int, cookies []struct {
	HostKey, Name, Value string
	EncryptedValue       []byte
	Path                 string
	ExpiresUTC           int64
	IsSecure             bool
}) string {
	t.Helper()

	// Create Default/Network/Cookies structure
	netDir := filepath.Join(tmpDir, "Default", "Network")
	if err := os.MkdirAll(netDir, 0755); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(netDir, "Cookies")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("create sqlite: %v", err)
	}

	// Create meta table
	_, err = db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`)
	if err != nil {
		t.Fatalf("create meta: %v", err)
	}
	_, err = db.Exec(`INSERT INTO meta (key, value) VALUES ('version', ?)`, fmt.Sprintf("%d", metaVersion))
	if err != nil {
		t.Fatalf("insert meta: %v", err)
	}

	// Create cookies table
	_, err = db.Exec(`CREATE TABLE cookies (
		creation_utc INTEGER NOT NULL,
		host_key TEXT NOT NULL,
		top_frame_site_key TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL,
		value TEXT NOT NULL DEFAULT '',
		encrypted_value BLOB NOT NULL DEFAULT X'',
		path TEXT NOT NULL,
		expires_utc INTEGER NOT NULL,
		is_secure INTEGER NOT NULL,
		is_httponly INTEGER NOT NULL DEFAULT 0,
		last_access_utc INTEGER NOT NULL DEFAULT 0,
		has_expires INTEGER NOT NULL DEFAULT 1,
		is_persistent INTEGER NOT NULL DEFAULT 0,
		priority INTEGER NOT NULL DEFAULT 1,
		samesite INTEGER NOT NULL DEFAULT -1,
		source_scheme INTEGER NOT NULL DEFAULT 0,
		source_port INTEGER NOT NULL DEFAULT -1,
		is_same_party INTEGER NOT NULL DEFAULT 0,
		last_update_utc INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		t.Fatalf("create cookies table: %v", err)
	}

	for i, c := range cookies {
		ev := c.EncryptedValue
		if ev == nil {
			ev = []byte{}
		}
		_, err = db.Exec(`INSERT INTO cookies (creation_utc, host_key, top_frame_site_key, name, value, encrypted_value, path, expires_utc, is_secure)
			VALUES (?, ?, '', ?, ?, ?, ?, ?, ?)`,
			i+1, c.HostKey, c.Name, c.Value, ev, c.Path, c.ExpiresUTC, c.IsSecure)
		if err != nil {
			t.Fatalf("insert cookie %d: %v", i, err)
		}
	}

	db.Close()
	return dbPath
}

func TestExtractChromium_Plaintext(t *testing.T) {
	tmpDir := t.TempDir()

	// Chromium epoch: microseconds since 1601-01-01. A future time.
	// Convert a future Unix timestamp to Chromium epoch
	futureUnix := time.Now().Add(24 * time.Hour).Unix()
	chromiumExpiry := (futureUnix + 11644473600) * 1000000

	cookies := []struct {
		HostKey, Name, Value string
		EncryptedValue       []byte
		Path                 string
		ExpiresUTC           int64
		IsSecure             bool
	}{
		{".chatgpt.com", "test_cr", "chrome_value", nil, "/", chromiumExpiry, true},
		{"other.example.com", "unrelated", "skip", nil, "/", chromiumExpiry, false},
	}

	dbPath := createChromiumTestDB(t, tmpDir, 20, cookies)

	records, err := readChromiumCookies(dbPath, "")
	if err != nil {
		t.Fatalf("readChromiumCookies: %v", err)
	}

	header := CookieHeaderForURL(records, "https://chatgpt.com/")
	if !strings.Contains(header, "test_cr=chrome_value") {
		t.Errorf("expected test_cr in header, got %q", header)
	}
	if strings.Contains(header, "unrelated") {
		t.Errorf("should not include unrelated domain, got %q", header)
	}
}

// --- Chromium V10 Decryption Tests ---

func TestDecryptChromiumV10_Peanuts(t *testing.T) {
	plaintext := "decrypted_value"
	encrypted, err := EncryptChromiumV10ForTest(plaintext, "peanuts", 0)
	if err != nil {
		t.Fatalf("EncryptChromiumV10ForTest: %v", err)
	}

	result, err := decryptChromiumValue(encrypted, 0, "")
	if err != nil {
		t.Fatalf("decryptChromiumValue: %v", err)
	}
	if result != plaintext {
		t.Errorf("got %q, want %q", result, plaintext)
	}
}

func TestDecryptChromiumV10_EmptyPassword(t *testing.T) {
	plaintext := "empty_password_value"
	encrypted, err := EncryptChromiumV10ForTest(plaintext, "", 0)
	if err != nil {
		t.Fatalf("EncryptChromiumV10ForTest: %v", err)
	}

	result, err := decryptChromiumValue(encrypted, 0, "")
	if err != nil {
		t.Fatalf("decryptChromiumValue: %v", err)
	}
	if result != plaintext {
		t.Errorf("got %q, want %q", result, plaintext)
	}
}

func TestDecryptChromiumV10_MetaVersion24(t *testing.T) {
	plaintext := "v24_value"
	encrypted, err := EncryptChromiumV10ForTest(plaintext, "peanuts", 24)
	if err != nil {
		t.Fatalf("EncryptChromiumV10ForTest: %v", err)
	}

	result, err := decryptChromiumValue(encrypted, 24, "")
	if err != nil {
		t.Fatalf("decryptChromiumValue: %v", err)
	}
	if result != plaintext {
		t.Errorf("got %q, want %q", result, plaintext)
	}
}

func TestDecryptChromiumV11_NoKeyringPassword(t *testing.T) {
	encrypted := []byte("v11" + "someencrypteddata")
	_, err := decryptChromiumValue(encrypted, 0, "")
	if err == nil {
		t.Error("expected error for v11 encrypted cookies with no keyring password")
	}
	if !strings.Contains(err.Error(), "keyring") {
		t.Errorf("error should mention keyring, got: %v", err)
	}
}

func TestDecryptChromiumV11_WithKeyringPassword(t *testing.T) {
	keyringPass := "ibW0UW57hj7DrW7l335e7g=="
	plaintext := "v11_decrypted_value"

	encrypted, err := EncryptChromiumV11ForTest(plaintext, keyringPass, 0)
	if err != nil {
		t.Fatalf("EncryptChromiumV11ForTest: %v", err)
	}

	result, err := decryptChromiumValue(encrypted, 0, keyringPass)
	if err != nil {
		t.Fatalf("decryptChromiumValue with keyring password: %v", err)
	}
	if result != plaintext {
		t.Errorf("got %q, want %q", result, plaintext)
	}
}

func TestDecryptChromiumV11_MetaVersion24(t *testing.T) {
	keyringPass := "test_keyring_pass"
	plaintext := "v11_mv24_value"

	encrypted, err := EncryptChromiumV11ForTest(plaintext, keyringPass, 24)
	if err != nil {
		t.Fatalf("EncryptChromiumV11ForTest: %v", err)
	}

	result, err := decryptChromiumValue(encrypted, 24, keyringPass)
	if err != nil {
		t.Fatalf("decryptChromiumValue with keyring password: %v", err)
	}
	if result != plaintext {
		t.Errorf("got %q, want %q", result, plaintext)
	}
}

func TestDecryptChromiumV10_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	plaintext := "secret_cookie_val"
	encrypted, err := EncryptChromiumV10ForTest(plaintext, "peanuts", 0)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	cookies := []struct {
		HostKey, Name, Value string
		EncryptedValue       []byte
		Path                 string
		ExpiresUTC           int64
		IsSecure             bool
	}{
		{".chatgpt.com", "enc_cookie", "", encrypted, "/", 0, true},
	}

	dbPath := createChromiumTestDB(t, tmpDir, 0, cookies)

	records, err := readChromiumCookies(dbPath, "")
	if err != nil {
		t.Fatalf("readChromiumCookies: %v", err)
	}

	header := CookieHeaderForURL(records, "https://chatgpt.com/")
	if !strings.Contains(header, "enc_cookie=secret_cookie_val") {
		t.Errorf("expected decrypted value in header, got %q", header)
	}
}

// --- ResolveCookieString Tests ---

func TestResolveCookieString_ExplicitWins(t *testing.T) {
	result, err := ResolveCookieString(context.Background(), "manual=cookie", CookieSourceConfig{
		Enabled: true,
		File:    "/nonexistent/path",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "manual=cookie" {
		t.Errorf("got %q, want %q", result, "manual=cookie")
	}
}

func TestResolveCookieString_Disabled(t *testing.T) {
	result, err := ResolveCookieString(context.Background(), "", CookieSourceConfig{
		Enabled: false,
		Browser: "chrome",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty for disabled cookies, got %q", result)
	}
}

func TestResolveCookieString_EnabledBadFile(t *testing.T) {
	_, err := ResolveCookieString(context.Background(), "", CookieSourceConfig{
		Enabled: true,
		File:    "/nonexistent/cookies.txt",
	})
	if err == nil {
		t.Error("expected error for nonexistent cookie file")
	}
}

func TestResolveCookieString_EnabledBadBrowser(t *testing.T) {
	_, err := ResolveCookieString(context.Background(), "", CookieSourceConfig{
		Enabled: true,
		Browser: "nonexistent_browser",
	})
	if err == nil {
		t.Error("expected error for unknown browser")
	}
}

func TestResolveCookieString_FileSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.txt")
	content := ".chatgpt.com	TRUE	/	TRUE	0	from_file	fileval\n"
	if err := os.WriteFile(cookieFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ResolveCookieString(context.Background(), "", CookieSourceConfig{
		Enabled: true,
		File:    cookieFile,
		Domain:  "chatgpt.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "from_file=fileval" {
		t.Errorf("got %q, want %q", result, "from_file=fileval")
	}
}

func TestResolveCookieString_EnabledNoFileOrBrowser(t *testing.T) {
	result, err := ResolveCookieString(context.Background(), "", CookieSourceConfig{
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty when enabled but no file/browser, got %q", result)
	}
}

// --- LoadRuntimeConfig Tests ---

func TestLoadRuntimeConfig_OldFormat(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	content := `{"bearerToken":"test-token","cookieString":"manual=cookie"}`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRuntimeConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig: %v", err)
	}
	if cfg.BearerToken != "test-token" {
		t.Errorf("BearerToken: got %q", cfg.BearerToken)
	}
	if cfg.CookieString != "manual=cookie" {
		t.Errorf("CookieString: got %q", cfg.CookieString)
	}
	if cfg.Cookies.Enabled {
		t.Error("Cookies.Enabled should be false for old format")
	}
}

func TestLoadRuntimeConfig_NewFormat(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	content := `{
		"bearerToken":"test-token",
		"cookieString":"",
		"cookies":{
			"enabled":true,
			"browser":"chrome",
			"profile":"Default",
			"domain":"chatgpt.com",
			"keyring":"auto"
		}
	}`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRuntimeConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig: %v", err)
	}
	if cfg.BearerToken != "test-token" {
		t.Errorf("BearerToken: got %q", cfg.BearerToken)
	}
	if !cfg.Cookies.Enabled {
		t.Error("Cookies.Enabled should be true")
	}
	if cfg.Cookies.Browser != "chrome" {
		t.Errorf("Cookies.Browser: got %q", cfg.Cookies.Browser)
	}
	if cfg.Cookies.Profile != "Default" {
		t.Errorf("Cookies.Profile: got %q", cfg.Cookies.Profile)
	}
	if cfg.Cookies.Domain != "chatgpt.com" {
		t.Errorf("Cookies.Domain: got %q", cfg.Cookies.Domain)
	}
	if cfg.Cookies.Keyring != "auto" {
		t.Errorf("Cookies.Keyring: got %q", cfg.Cookies.Keyring)
	}
}

func TestLoadRuntimeConfig_Nonexistent(t *testing.T) {
	_, err := LoadRuntimeConfig("/nonexistent/config.json")
	if err == nil {
		t.Error("expected error for nonexistent config")
	}
}

// --- CookieDomainMatch Tests ---

func TestCookieDomainMatch(t *testing.T) {
	tests := []struct {
		cookie, request string
		match           bool
	}{
		{"chatgpt.com", "chatgpt.com", true},
		{".chatgpt.com", "chatgpt.com", true},
		{".chatgpt.com", "sub.chatgpt.com", true},
		{"chatgpt.com", "sub.chatgpt.com", false},
		{"auth.openai.com", "chatgpt.com", false},
		{".openai.com", "chatgpt.com", false},
	}
	for _, tt := range tests {
		got := cookieDomainMatch(tt.cookie, tt.request)
		if got != tt.match {
			t.Errorf("cookieDomainMatch(%q, %q) = %v, want %v", tt.cookie, tt.request, got, tt.match)
		}
	}
}

// --- CookiePathMatch Tests ---

func TestCookiePathMatch(t *testing.T) {
	tests := []struct {
		cookie, request string
		match           bool
	}{
		{"/", "/", true},
		{"/", "/anything", true},
		{"/api", "/api", true},
		{"/api", "/api/v1", true},
		{"/api", "/other", false},
		{"/api/", "/api/v1", true},
	}
	for _, tt := range tests {
		got := cookiePathMatch(tt.cookie, tt.request)
		if got != tt.match {
			t.Errorf("cookiePathMatch(%q, %q) = %v, want %v", tt.cookie, tt.request, got, tt.match)
		}
	}
}

// --- Safari not implemented ---

func TestExtractBrowserCookies_SafariUnsupported(t *testing.T) {
	_, err := ExtractBrowserCookies(context.Background(), CookieSourceConfig{
		Browser: "safari",
	})
	if err == nil {
		t.Error("expected error for safari")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error should mention not implemented, got: %v", err)
	}
}

// --- Sort stability ---

func TestCookieHeaderForURL_SortedByName(t *testing.T) {
	records := []CookieRecord{
		{Domain: "chatgpt.com", Path: "/", Name: "z_cookie", Value: "z", Secure: true},
		{Domain: "chatgpt.com", Path: "/", Name: "a_cookie", Value: "a", Secure: true},
		{Domain: ".chatgpt.com", Path: "/", Name: "m_cookie", Value: "m", Secure: true},
	}

	// Run multiple times to ensure deterministic ordering
	for i := 0; i < 10; i++ {
		header := CookieHeaderForURL(records, "https://chatgpt.com/")
		parts := strings.Split(header, "; ")
		names := make([]string, len(parts))
		for j, p := range parts {
			names[j] = strings.SplitN(p, "=", 2)[0]
		}
		if !sort.StringsAreSorted(names) {
			t.Errorf("iteration %d: cookies not sorted by name: %v", i, names)
		}
	}
}

// --- PBKDF2 and AES roundtrip ---

func TestAESCBCEncryptDecryptRoundtrip(t *testing.T) {
	passwords := []string{"peanuts", ""}
	for _, pw := range passwords {
		for _, metaVersion := range []int{0, 24} {
			plaintext := "test_value_" + pw
			encrypted, err := EncryptChromiumV10ForTest(plaintext, pw, metaVersion)
			if err != nil {
				t.Fatalf("encrypt (pw=%q, mv=%d): %v", pw, metaVersion, err)
			}
			decrypted, err := decryptChromiumValue(encrypted, metaVersion, "")
			if err != nil {
				t.Fatalf("decrypt (pw=%q, mv=%d): %v", pw, metaVersion, err)
			}
			if decrypted != plaintext {
				t.Errorf("roundtrip (pw=%q, mv=%d): got %q, want %q", pw, metaVersion, decrypted, plaintext)
			}
		}
	}
}
