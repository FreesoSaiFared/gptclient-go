package sentinel

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imroc/req/v3"
	_ "modernc.org/sqlite"
)

// CookieSourceConfig configures automatic cookie extraction.
type CookieSourceConfig struct {
	Enabled bool   `json:"enabled"`
	File    string `json:"file,omitempty"`
	Browser string `json:"browser,omitempty"`
	Profile string `json:"profile,omitempty"`
	Domain  string `json:"domain,omitempty"`
	Keyring string `json:"keyring,omitempty"`
}

// RuntimeConfig holds the full runtime configuration including cookie sources.
type RuntimeConfig struct {
	BearerToken  string             `json:"bearerToken"`
	CookieString string             `json:"cookieString"`
	Cookies      CookieSourceConfig `json:"cookies,omitempty"`
}

// CookieRecord represents a single cookie.
type CookieRecord struct {
	Domain   string
	Path     string
	Name     string
	Value    string
	Secure   bool
	Expires  *time.Time
	HTTPOnly bool
}

// LoadRuntimeConfig reads and parses a JSON config file into RuntimeConfig.
func LoadRuntimeConfig(path string) (RuntimeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("read config: %w", err)
	}
	var cfg RuntimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return RuntimeConfig{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// ResolveCookieString resolves the cookie header string using the resolution order:
//  1. If explicit is non-empty, use it exactly.
//  2. Else if src.Enabled and src.File is set, load a Netscape cookies.txt file.
//  3. Else if src.Enabled and src.Browser is set, extract browser cookies.
//  4. Else return empty string.
func ResolveCookieString(ctx context.Context, explicit string, src CookieSourceConfig) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if !src.Enabled {
		return "", nil
	}

	domain := orDefault(src.Domain, "chatgpt.com")

	var records []CookieRecord
	var err error

	if src.File != "" {
		records, err = LoadNetscapeCookieFile(src.File)
		if err != nil {
			return "", fmt.Errorf("load cookie file %s: %w", src.File, err)
		}
	} else if src.Browser != "" {
		records, err = ExtractBrowserCookies(ctx, src)
		if err != nil {
			return "", fmt.Errorf("extract browser cookies from %s: %w", src.Browser, err)
		}
	} else {
		return "", nil
	}

	url := "https://" + domain + "/"
	return CookieHeaderForURL(records, url), nil
}

// LoadNetscapeCookieFile parses a standard 7-column Netscape cookies.txt file.
func LoadNetscapeCookieFile(path string) ([]CookieRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open cookie file: %w", err)
	}
	defer f.Close()

	return ParseNetscapeCookies(f)
}

// ParseNetscapeCookies parses Netscape cookie format from a reader.
func ParseNetscapeCookies(r io.Reader) ([]CookieRecord, error) {
	var records []CookieRecord
	lineNum := 0

	// Read line by line
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read cookie data: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		lineNum++
		line = strings.TrimRight(line, "\r")

		// Skip blank lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Skip comments (but handle #HttpOnly_ prefix)
		if strings.HasPrefix(line, "#HttpOnly_") {
			line = strings.TrimPrefix(line, "#HttpOnly_")
			// Fall through to parse the rest as a cookie line
			parts := strings.Fields(line)
			if len(parts) < 7 {
				continue // skip malformed
			}
			rec, err := parseNetscapeFields(parts)
			if err != nil {
				continue
			}
			rec.HTTPOnly = true
			records = append(records, rec)
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 7 {
			continue // skip malformed lines
		}

		rec, err := parseNetscapeFields(parts)
		if err != nil {
			continue
		}
		records = append(records, rec)
	}

	return records, nil
}

func parseNetscapeFields(parts []string) (CookieRecord, error) {
	domain := parts[0]
	// parts[1] is include_subdomains ("TRUE"/"FALSE") — we infer from domain prefix
	path := parts[2]
	secure := strings.EqualFold(parts[3], "TRUE")
	expiryStr := parts[4]
	name := parts[5]
	value := parts[6]

	if name == "" {
		return CookieRecord{}, fmt.Errorf("empty cookie name")
	}

	var expires *time.Time
	if expiryStr != "" && expiryStr != "0" {
		expirySec, err := strconv.ParseInt(expiryStr, 10, 64)
		if err == nil && expirySec > 0 {
			t := time.Unix(expirySec, 0)
			expires = &t
		}
	}

	return CookieRecord{
		Domain:  domain,
		Path:    path,
		Name:    name,
		Value:   value,
		Secure:  secure,
		Expires: expires,
	}, nil
}

// CookieHeaderForURL builds a Cookie HTTP header value for the given URL
// from the provided cookie records. Only cookies matching the URL's domain
// and path are included. Expired cookies are skipped. Results are sorted
// by name for deterministic output.
func CookieHeaderForURL(records []CookieRecord, rawURL string) string {
	domain, path, secure := parseURLForCookies(rawURL)

	var matched []CookieRecord
	now := time.Now()
	for _, rec := range records {
		if rec.Name == "" {
			continue
		}
		if rec.Expires != nil && rec.Expires.Before(now) {
			continue
		}
		if !cookieDomainMatch(rec.Domain, domain) {
			continue
		}
		if !cookiePathMatch(rec.Path, path) {
			continue
		}
		if rec.Secure && !secure {
			continue
		}
		matched = append(matched, rec)
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Name < matched[j].Name
	})

	var parts []string
	for _, m := range matched {
		parts = append(parts, m.Name+"="+m.Value)
	}
	return strings.Join(parts, "; ")
}

func parseURLForCookies(rawURL string) (domain, path string, secure bool) {
	secure = strings.HasPrefix(rawURL, "https://")
	u := rawURL
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	if idx := strings.Index(u, "/"); idx >= 0 {
		domain = u[:idx]
		path = u[idx:]
	} else {
		domain = u
		path = "/"
	}
	if path == "" {
		path = "/"
	}
	return
}

// cookieDomainMatch checks if the cookie domain matches the request domain.
// A cookie domain "chatgpt.com" matches "chatgpt.com".
// A cookie domain ".chatgpt.com" also matches "chatgpt.com" and "sub.chatgpt.com".
func cookieDomainMatch(cookieDomain, requestDomain string) bool {
	cd := strings.ToLower(cookieDomain)
	rd := strings.ToLower(requestDomain)

	// Exact match
	if cd == rd {
		return true
	}
	// Cookie domain with leading dot matches the domain and subdomains
	if strings.HasPrefix(cd, ".") {
		suffix := cd[1:]
		return rd == suffix || strings.HasSuffix(rd, "."+suffix)
	}
	return false
}

// cookiePathMatch checks if the cookie path is a prefix of the request path.
func cookiePathMatch(cookiePath, requestPath string) bool {
	cp := orDefault(cookiePath, "/")
	rp := orDefault(requestPath, "/")

	if cp == "/" {
		return true
	}
	if rp == cp {
		return true
	}
	if strings.HasPrefix(rp, cp) && (len(cp) > 0 && cp[len(cp)-1] == '/' || len(rp) > len(cp) && rp[len(cp)] == '/') {
		return true
	}
	return false
}

// ExtractBrowserCookies extracts cookies from the specified browser.
func ExtractBrowserCookies(ctx context.Context, src CookieSourceConfig) ([]CookieRecord, error) {
	browser := strings.ToLower(src.Browser)
	switch browser {
	case "firefox":
		return extractFirefoxCookies(ctx, src.Profile)
	case "chrome", "chrome-canary", "chrome-unstable", "chrome-beta", "chrome-for-testing", "chromium", "brave", "edge", "opera", "vivaldi":
		// Resolve keyring password for v11 cookie decryption
		keyringName := browser
		if strings.HasPrefix(browser, "chrome-") {
			keyringName = "chrome"
		}
		keyringPass, err := getLinuxKeyringPassword(keyringName)
		if err != nil {
			keyringPass = "" // Fall back to empty; v11 decryption will fail gracefully
		}
		return extractChromiumCookies(ctx, browser, src.Profile, keyringPass)
	case "safari":
		return nil, fmt.Errorf("safari cookie extraction is not implemented in this Go port")
	default:
		return nil, fmt.Errorf("unknown browser: %s (supported: firefox, chrome, chrome-canary, chrome-unstable, chrome-beta, chrome-for-testing, chromium, brave, edge, opera, vivaldi)", src.Browser)
	}
}

// --- Firefox ---

func extractFirefoxCookies(ctx context.Context, profile string) ([]CookieRecord, error) {
	searchRoots := firefoxBrowserDirs()
	if profile != "" {
		if isPath(profile) {
			searchRoots = []string{profile}
		} else {
			var expanded []string
			for _, root := range firefoxBrowserDirs() {
				expanded = append(expanded, filepath.Join(root, profile))
			}
			searchRoots = expanded
		}
	}

	cookieDBPath, err := findNewestFile(searchRoots, "cookies.sqlite")
	if err != nil {
		return nil, fmt.Errorf("find firefox cookies database: %w", err)
	}

	return readFirefoxCookies(cookieDBPath)
}

func firefoxBrowserDirs() []string {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	home, _ := os.UserHomeDir()
	if xdg == "" && home != "" {
		xdg = filepath.Join(home, ".config")
	}

	var dirs []string
	if xdg != "" {
		dirs = append(dirs, filepath.Join(xdg, "mozilla/firefox"))
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".mozilla/firefox"))
		dirs = append(dirs, filepath.Join(home, ".var/app/org.mozilla.firefox/config/mozilla/firefox"))
		dirs = append(dirs, filepath.Join(home, ".var/app/org.mozilla.firefox/.mozilla/firefox"))
		dirs = append(dirs, filepath.Join(home, "snap/firefox/common/.mozilla/firefox"))
	}
	return dirs
}

func readFirefoxCookies(dbPath string) ([]CookieRecord, error) {
	tmpDir, err := os.MkdirTemp("", "sentinel-firefox-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpDB := filepath.Join(tmpDir, "cookies.sqlite")
	if err := copyFile(dbPath, tmpDB); err != nil {
		return nil, fmt.Errorf("copy firefox cookies db: %w", err)
	}

	db, err := sql.Open("sqlite", tmpDB)
	if err != nil {
		return nil, fmt.Errorf("open firefox cookies db: %w", err)
	}
	defer db.Close()

	var schemaVersion int
	row := db.QueryRow("PRAGMA user_version")
	if err := row.Scan(&schemaVersion); err != nil {
		return nil, fmt.Errorf("query firefox schema version: %w", err)
	}

	rows, err := db.Query("SELECT host, name, value, path, expiry, isSecure FROM moz_cookies")
	if err != nil {
		return nil, fmt.Errorf("query moz_cookies: %w", err)
	}
	defer rows.Close()

	var records []CookieRecord
	for rows.Next() {
		var host, name, value, path string
		var expiry float64
		var isSecure bool
		if err := rows.Scan(&host, &name, &value, &path, &expiry, &isSecure); err != nil {
			continue
		}

		// Firefox 142+ (schema >= 16) stores expiry in milliseconds
		if schemaVersion >= 16 && expiry > 0 {
			expiry /= 1000
		}

		var expires *time.Time
		if expiry > 0 {
			t := time.Unix(int64(expiry), 0)
			expires = &t
		}

		records = append(records, CookieRecord{
			Domain:  host,
			Path:    path,
			Name:    name,
			Value:   value,
			Secure:  isSecure,
			Expires: expires,
		})
	}
	return records, nil
}

// --- Chromium ---

var chromiumBrowserDirs = map[string]string{
	"chrome":             "google-chrome",
	"chrome-canary":      "google-chrome-canary",
	"chrome-unstable":    "google-chrome-unstable",
	"chrome-beta":        "google-chrome-beta",
	"chrome-for-testing": "google-chrome-for-testing",
	"chromium":           "chromium",
	"brave":              "BraveSoftware/Brave-Browser",
	"edge":               "microsoft-edge",
	"opera":              "opera",
	"vivaldi":            "vivaldi",
}

func extractChromiumCookies(ctx context.Context, browser, profile, keyringPass string) ([]CookieRecord, error) {
	browserDirName, ok := chromiumBrowserDirs[browser]
	if !ok {
		return nil, fmt.Errorf("unknown chromium browser: %s", browser)
	}

	searchRoots := chromiumSearchRoots(browserDirName)

	var searchPaths []string
	if profile != "" {
		if isPath(profile) {
			searchPaths = []string{profile}
		} else {
			for _, root := range searchRoots {
				searchPaths = append(searchPaths, filepath.Join(root, profile))
			}
		}
	} else {
		searchPaths = searchRoots
	}

	cookieDBPath, err := findNewestFile(searchPaths, "Cookies")
	if err != nil {
		return nil, fmt.Errorf("find %s cookies database: %w", browser, err)
	}

	return readChromiumCookies(cookieDBPath, keyringPass)
}

// chromiumSearchRoots returns the list of directories to search for a Chromium
// browser's data, including XDG config and snap installations.
func chromiumSearchRoots(browserDirName string) []string {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	home, _ := os.UserHomeDir()
	if xdg == "" && home != "" {
		xdg = filepath.Join(home, ".config")
	}

	roots := []string{
		filepath.Join(xdg, browserDirName),
	}

	// Snap installations use a different path structure
	if home != "" {
		snapPath := filepath.Join(home, "snap", browserDirName, "common", browserDirName)
		roots = append(roots, snapPath)
	}

	return roots
}

func readChromiumCookies(dbPath string, keyringPass string) ([]CookieRecord, error) {
	tmpDir, err := os.MkdirTemp("", "sentinel-chromium-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpDB := filepath.Join(tmpDir, "Cookies")
	if err := copyFile(dbPath, tmpDB); err != nil {
		return nil, fmt.Errorf("copy chromium cookies db: %w", err)
	}

	db, err := sql.Open("sqlite", tmpDB)
	if err != nil {
		return nil, fmt.Errorf("open chromium cookies db: %w", err)
	}
	defer db.Close()

	// Read meta version
	var metaVersion int
	row := db.QueryRow(`SELECT value FROM meta WHERE key = 'version'`)
	if err := row.Scan(&metaVersion); err != nil {
		metaVersion = 0 // older DBs may not have meta table
	}

	// Detect secure column name
	secureCol := "is_secure"
	colRows, err := db.Query("PRAGMA table_info(cookies)")
	if err == nil {
		for colRows.Next() {
			var cid int
			var cname, ctype string
			var notnull int
			var dfltValue interface{}
			var pk int
			if colRows.Scan(&cid, &cname, &ctype, &notnull, &dfltValue, &pk) == nil {
				if cname == "secure" {
					secureCol = "secure"
				}
			}
		}
		colRows.Close()
	}

	query := fmt.Sprintf(
		"SELECT host_key, name, value, encrypted_value, path, expires_utc, %s FROM cookies",
		secureCol,
	)
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query cookies: %w", err)
	}
	defer rows.Close()

	var records []CookieRecord
	for rows.Next() {
		var hostKey, name, value, path string
		var encryptedValue []byte
		var expiresUTC int64
		var isSecure bool

		if err := rows.Scan(&hostKey, &name, &value, &encryptedValue, &path, &expiresUTC, &isSecure); err != nil {
			continue
		}

		cookieValue := value

		// If value is empty but encrypted_value exists, try decrypting
		if value == "" && len(encryptedValue) > 0 {
			decrypted, err := decryptChromiumValue(encryptedValue, metaVersion, keyringPass)
			if err != nil {
				// Skip cookies we can't decrypt rather than failing entirely
				continue
			}
			cookieValue = decrypted
		}

		// Skip cookies with values containing non-printable characters
		// (indicates failed decryption or binary data not suitable for HTTP headers)
		if !isValidCookieValue(cookieValue) {
			continue
		}

		var expires *time.Time
		// Chromium stores expires_utc as microseconds since Windows epoch (1601-01-01)
		// Convert to Unix timestamp: subtract 11644473600 seconds, divide by 10^6
		if expiresUTC > 0 {
			unixSec := expiresUTC/1000000 - 11644473600
			if unixSec > 0 {
				t := time.Unix(unixSec, 0)
				expires = &t
			}
		}

		records = append(records, CookieRecord{
			Domain:  hostKey,
			Path:    path,
			Name:    name,
			Value:   cookieValue,
			Secure:  isSecure,
			Expires: expires,
		})
	}
	return records, nil
}

// decryptChromiumValue decrypts a Chromium encrypted cookie value on Linux.
func decryptChromiumValue(encryptedValue []byte, metaVersion int, keyringPass string) (string, error) {
	if len(encryptedValue) < 4 {
		return "", fmt.Errorf("encrypted value too short")
	}

	prefix := string(encryptedValue[:3])

	switch prefix {
	case "v10":
		return decryptChromiumV10(encryptedValue[3:], metaVersion)
	case "v11":
		if keyringPass == "" {
			return "", fmt.Errorf("encrypted Chromium v11 cookies require keyring access; install libsecret-tools and ensure the keyring is unlocked")
		}
		return decryptChromiumV11(encryptedValue[3:], metaVersion, keyringPass)
	default:
		// Try as plaintext
		return string(encryptedValue), nil
	}
}

// decryptChromiumV10 decrypts v10 AES-CBC encrypted Chromium cookies on Linux.
// Uses PBKDF2-HMAC-SHA1 with salt "saltysalt", 1 iteration, 16-byte key.
// Tries password "peanuts" first, then empty password.
func decryptChromiumV10(ciphertext []byte, metaVersion int) (string, error) {
	passwords := []string{"peanuts", ""}

	for _, password := range passwords {
		result, err := aesCBCDecryptPBKDF2(ciphertext, password, metaVersion)
		if err == nil {
			return result, nil
		}
	}

	return "", fmt.Errorf("failed to decrypt Chromium v10 cookie (tried peanuts and empty password)")
}

// decryptChromiumV11 decrypts v11 AES-CBC encrypted Chromium cookies on Linux.
// Uses the keyring password retrieved from libsecret/gnome-keyring.
// The keyring stores a base64-encoded password which is used as the PBKDF2 input.
func decryptChromiumV11(ciphertext []byte, metaVersion int, keyringPass string) (string, error) {
	result, err := aesCBCDecryptPBKDF2(ciphertext, keyringPass, metaVersion)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt Chromium v11 cookie with keyring password: %w", err)
	}
	return result, nil
}

// aesCBCDecryptPBKDF2 decrypts AES-CBC encrypted data using PBKDF2-HMAC-SHA1 derived key.
func aesCBCDecryptPBKDF2(ciphertext []byte, password string, metaVersion int) (string, error) {
	// Derive key using PBKDF2-HMAC-SHA1
	key := pbkdf2SHA1([]byte(password), []byte("saltysalt"), 1, 16)

	// IV: 16 space bytes
	iv := make([]byte, 16)
	for i := range iv {
		iv[i] = ' '
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create AES cipher: %w", err)
	}

	if len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext not a multiple of block size")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	// PKCS#7 unpad
	plaintext, err = pkcs7Unpad(plaintext)
	if err != nil {
		return "", fmt.Errorf("pkcs7 unpad: %w", err)
	}

	// If meta version >= 24, remove the first 32 bytes
	if metaVersion >= 24 && len(plaintext) > 32 {
		plaintext = plaintext[32:]
	}

	return string(plaintext), nil
}

// pbkdf2SHA1 implements PBKDF2-HMAC-SHA1.
func pbkdf2SHA1(password, salt []byte, iterations, keyLen int) []byte {
	// PRF is HMAC-SHA1, output length is 20 bytes
	const hashLen = 20

	blocksNeeded := (keyLen + hashLen - 1) / hashLen
	var dk []byte

	for blockNum := 1; blockNum <= blocksNeeded; blockNum++ {
		// U1 = PRF(password, salt || INT_32_BE(blockNum))
		h := hmacSHA1(password, append(salt, uint32ToBytes(uint32(blockNum))...))
		u := h
		result := make([]byte, hashLen)
		copy(result, u)

		for i := 1; i < iterations; i++ {
			u = hmacSHA1(password, u)
			for j := range result {
				result[j] ^= u[j]
			}
		}

		dk = append(dk, result...)
	}

	return dk[:keyLen]
}

func hmacSHA1(key, data []byte) []byte {
	// Simplified HMAC-SHA1 implementation
	const blockSize = 64

	if len(key) > blockSize {
		h := sha1.Sum(key)
		key = h[:]
	}

	keyPadded := make([]byte, blockSize)
	copy(keyPadded, key)

	var ipad, opad [64]byte
	for i := range ipad {
		ipad[i] = keyPadded[i] ^ 0x36
		opad[i] = keyPadded[i] ^ 0x5c
	}

	inner := sha1.Sum(append(ipad[:], data...))
	outer := sha1.Sum(append(opad[:], inner[:]...))
	return outer[:]
}

func uint32ToBytes(n uint32) []byte {
	return []byte{
		byte(n >> 24),
		byte(n >> 16),
		byte(n >> 8),
		byte(n),
	}
}

// pkcs7Unpad removes PKCS#7 padding.
func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > aes.BlockSize {
		return nil, fmt.Errorf("invalid padding length: %d", padLen)
	}
	for i := len(data) - padLen; i < len(data); i++ {
		if data[i] != byte(padLen) {
			return nil, fmt.Errorf("invalid padding")
		}
	}
	return data[:len(data)-padLen], nil
}

// --- Utility ---

func isPath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~")
}

// isValidCookieValue returns true if the cookie value contains only printable
// characters and is safe to include in an HTTP Cookie header. Rejects values
// with control characters (except TAB) or other non-HTTP-safe bytes.
func isValidCookieValue(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\t' {
			return false
		}
		if r == 0x7f {
			return false
		}
	}
	return true
}

// findNewestFile recursively searches directories for the newest file matching name.
func findNewestFile(roots []string, filename string) (string, error) {
	var bestPath string
	var bestTime time.Time

	for _, root := range roots {
		root, _ = filepath.Abs(os.ExpandEnv(root))

		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			if filepath.Base(path) == filename {
				if info.ModTime().After(bestTime) {
					bestTime = info.ModTime()
					bestPath = path
				}
			}
			return nil
		})
	}

	if bestPath == "" {
		return "", fmt.Errorf("no %s found in %v", filename, roots)
	}
	return bestPath, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// EncryptChromiumV10ForTest encrypts a value using the v10 Chromium scheme for testing.
func EncryptChromiumV10ForTest(plaintext string, password string, metaVersion int) ([]byte, error) {
	// Add 32-byte prefix if metaVersion >= 24
	data := []byte(plaintext)
	if metaVersion >= 24 {
		prefix := make([]byte, 32)
		data = append(prefix, data...)
	}

	// Pad to AES block size
	data = pkcs7Pad(data, aes.BlockSize)

	// Derive key
	key := pbkdf2SHA1([]byte(password), []byte("saltysalt"), 1, 16)

	// IV: 16 space bytes
	iv := make([]byte, 16)
	for i := range iv {
		iv[i] = ' '
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	ciphertext := make([]byte, len(data))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, data)

	result := append([]byte("v10"), ciphertext...)
	return result, nil
}

// EncryptChromiumV11ForTest encrypts a cookie value using the v11 scheme
// (same as v10 but with the keyring password) for use in tests.
func EncryptChromiumV11ForTest(plaintext string, keyringPassword string, metaVersion int) ([]byte, error) {
	data := []byte(plaintext)
	if metaVersion >= 24 {
		prefix := make([]byte, 32)
		data = append(prefix, data...)
	}

	data = pkcs7Pad(data, aes.BlockSize)

	key := pbkdf2SHA1([]byte(keyringPassword), []byte("saltysalt"), 1, 16)

	iv := make([]byte, 16)
	for i := range iv {
		iv[i] = ' '
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	ciphertext := make([]byte, len(data))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, data)

	result := append([]byte("v11"), ciphertext...)
	return result, nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padLen := blockSize - (len(data) % blockSize)
	padding := make([]byte, padLen)
	for i := range padding {
		padding[i] = byte(padLen)
	}
	return append(data, padding...)
}

// --- Linux Keyring Access ---

// getLinuxKeyringPassword retrieves the Chromium encryption password from the
// system keyring (libsecret/gnome-keyring) using the secret-tool command.
// The browser name is used as the "application" attribute for the lookup.
func getLinuxKeyringPassword(browser string) (string, error) {
	// secret-tool lookup application chrome
	cmd := exec.Command("secret-tool", "lookup", "application", browser)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("secret-tool lookup application %s: %w", browser, err)
	}
	password := strings.TrimSpace(string(output))
	if password == "" {
		return "", fmt.Errorf("empty password from keyring for %s", browser)
	}
	return password, nil
}

// RefreshAccessToken uses the resolved cookie string to obtain a fresh
// ChatGPT access token from /api/auth/session. It uses the req library's
// Chrome TLS fingerprint impersonation to avoid Cloudflare blocks.
// Returns the new access token or an error.
func RefreshAccessToken(cookieString string) (string, error) {
	if cookieString == "" {
		return "", fmt.Errorf("no cookie string available for token refresh")
	}

	client := newRefreshClient()

	resp, err := client.R().
		SetHeader("Cookie", cookieString).
		SetHeader("Accept", "application/json").
		Get("https://chatgpt.com/api/auth/session")
	if err != nil {
		return "", fmt.Errorf("request /api/auth/session: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("/api/auth/session returned status %d", resp.StatusCode)
	}

	var session struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(resp.Bytes(), &session); err != nil {
		return "", fmt.Errorf("parse session response: %w", err)
	}

	if session.AccessToken == "" {
		return "", fmt.Errorf("no accessToken in session response (session may be expired, re-login in browser)")
	}

	return session.AccessToken, nil
}

// newRefreshClient creates an HTTP client with Chrome TLS fingerprint
// impersonation for use by RefreshAccessToken. Uses the same impersonation
// as the main Client but without auth-related transports.
func newRefreshClient() *req.Client {
	c := req.C().
		SetUserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36").
		ImpersonateChrome()
	return c
}

// --- Browser Probing ---

// BrowserProbeResult holds information about a browser's cookie availability.
type BrowserProbeResult struct {
	Browser       string
	Available     bool
	CookieCount   int
	CookieBytes   int
	AccountPlan   string // "plus", "free", or "" if unknown
	AccountEmail  string // email if discoverable, or ""
	Error         string // error message if probe failed
	KeyringAccess bool   // whether v11 keyring decryption succeeded
}

// ProbeAllBrowsers probes all known browsers for chatgpt.com cookies
// and returns results for each. This does NOT modify any files.
func ProbeAllBrowsers(ctx context.Context) []BrowserProbeResult {
	browsers := []string{"chrome", "chrome-canary", "chrome-unstable", "chrome-beta", "chrome-for-testing", "chromium", "brave", "firefox", "edge", "opera", "vivaldi"}
	var results []BrowserProbeResult
	for _, b := range browsers {
		results = append(results, ProbeBrowser(ctx, b, ""))
	}
	return results
}

// ProbeBrowser probes a single browser for chatgpt.com cookies and
// attempts to refresh the access token to determine account info.
func ProbeBrowser(ctx context.Context, browser, profile string) BrowserProbeResult {
	result := BrowserProbeResult{Browser: browser}

	src := CookieSourceConfig{
		Enabled: true,
		Browser: browser,
		Profile: profile,
		Domain:  "chatgpt.com",
	}

	// Try extracting cookies
	records, err := ExtractBrowserCookies(ctx, src)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	// Filter for chatgpt.com
	filtered := filterCookiesForDomain(records, "chatgpt.com")
	result.Available = len(filtered) > 0
	result.CookieCount = len(filtered)

	// Build cookie header to measure bytes
	header := CookieHeaderForURL(filtered, "https://chatgpt.com/")
	result.CookieBytes = len(header)

	// Check keyring access: did we get any v11 cookies?
	// If cookie count > 0 and no errors, keyring is working
	result.KeyringAccess = result.CookieCount > 0

	// Try to get account info by refreshing the token
	if header != "" {
		token, err := RefreshAccessToken(header)
		if err == nil && token != "" {
			// Decode JWT payload to extract account info
			result.AccountPlan, result.AccountEmail = extractAccountInfo(token)
		}
		// Even if refresh fails, the browser still has cookies available
	}

	return result
}

// filterCookiesForDomain returns cookies that match the given domain.
func filterCookiesForDomain(records []CookieRecord, domain string) []CookieRecord {
	var filtered []CookieRecord
	for _, r := range records {
		if cookieDomainMatch(r.Domain, domain) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// extractAccountInfo decodes a JWT payload to extract plan type and email.
func extractAccountInfo(token string) (plan, email string) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ""
	}
	payload := parts[1]
	payload += strings.Repeat("=", (4-len(payload)%4)%4)
	decoded, err := base64UrlDecode(payload)
	if err != nil {
		return "", ""
	}
	var claims struct {
		A struct {
			Plan string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
		P struct {
			Email string `json:"email"`
		} `json:"https://api.openai.com/profile"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return "", ""
	}
	return claims.A.Plan, claims.P.Email
}

// base64UrlDecode decodes base64url-encoded data.
func base64UrlDecode(s string) ([]byte, error) {
	// Replace URL-safe characters
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	return base64Decode(s)
}

// base64Decode decodes standard base64 data with padding.
func base64Decode(s string) ([]byte, error) {
	// Simple base64 decoder
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	// Build decode map
	decMap := make([]byte, 256)
	for i := range decMap {
		decMap[i] = 0xFF
	}
	for i := 0; i < len(alphabet); i++ {
		decMap[alphabet[i]] = byte(i)
	}

	// Remove whitespace
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)

	// Remove padding
	s = strings.TrimRight(s, "=")

	if len(s)%4 == 1 {
		return nil, fmt.Errorf("invalid base64 length")
	}

	var result []byte
	var val byte
	var bits int
	for _, c := range []byte(s) {
		if c >= 255 || decMap[c] == 0xFF {
			continue
		}
		val = (val << 6) | decMap[c]
		bits += 6
		if bits >= 8 {
			bits -= 8
			result = append(result, (val>>bits)&0xFF)
		}
	}
	return result, nil
}
