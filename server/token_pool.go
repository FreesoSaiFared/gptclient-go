package server

import (
	"bufio"
	"errors"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var accessTokenRegex = regexp.MustCompile(`"accessToken"\s*:\s*"([^"]+)"`)
var sessionTokenRegex = regexp.MustCompile(`"sessionToken"\s*:\s*"([^"]+)"`)

// cleanToken 从 JSON 中提取 accessToken；否则返回原串（非 JSON 碎片时）。
func cleanToken(t string) string {
	t = strings.TrimSpace(t)
	if match := accessTokenRegex.FindStringSubmatch(t); len(match) == 2 {
		return match[1]
	}
	if strings.Contains(t, "{") || strings.Contains(t, "}") {
		return ""
	}
	return t
}

// extractSessionJSON 从完整 session JSON 同时提取 accessToken 和 sessionToken。
// 两者都找到时返回 (at, st)；只找到 AT 时返回 (at, "")。
func extractSessionJSON(line string) (at, st string) {
	if !strings.Contains(line, "{") {
		return "", ""
	}
	if m := accessTokenRegex.FindStringSubmatch(line); len(m) == 2 {
		at = m[1]
	}
	if m := sessionTokenRegex.FindStringSubmatch(line); len(m) == 2 {
		st = normalizeSessionToken(m[1])
	}
	return at, st
}

type poolEntry struct {
	at        string
	st        string
	expiresAt time.Time
}

func (e *poolEntry) markKey() string {
	if e.st != "" {
		return "st:" + e.st
	}
	return e.at
}

// TokenPool Token 池：支持 AT 直连，或通过 ST 自动续期。
type TokenPool struct {
	mu           sync.Mutex
	entries      []poolEntry
	errorKeys    map[string]bool
	roundIdx     int
	tokensFile   string
	refreshAhead time.Duration
}

// NewTokenPool 创建并从文件加载 Token 池。
func NewTokenPool(tokensFile string, refreshAhead time.Duration) *TokenPool {
	if refreshAhead <= 0 {
		refreshAhead = 5 * time.Minute
	}
	tp := &TokenPool{
		errorKeys:    make(map[string]bool),
		tokensFile:   tokensFile,
		refreshAhead: refreshAhead,
	}
	tp.loadFromFile()
	return tp
}

func (tp *TokenPool) loadFromFile() {
	f, err := os.Open(tp.tokensFile)
	if err != nil {
		return
	}
	defer f.Close()

	var atN, stN int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		at, st := parseTokenLine(scanner.Text())
		if at == "" && st == "" {
			continue
		}
		e := poolEntry{at: at, st: st}
		if at != "" {
			e.expiresAt = parseJWTExp(at)
			atN++
		}
		if st != "" {
			stN++
		}
		tp.entries = append(tp.entries, e)
	}
	log.Printf("[token-pool] 已加载 %d 条凭证 (AT=%d, ST=%d)", len(tp.entries), atN, stN)
}

func (tp *TokenPool) refreshEntry(e *poolEntry) (string, error) {
	if e.st == "" {
		if e.at == "" {
			return "", errors.New("no token")
		}
		return e.at, nil
	}
	at, exp, err := RefreshATFromSession(e.st)
	if err != nil {
		return "", err
	}
	e.at = at
	e.expiresAt = exp
	log.Printf("[token-pool] ST→AT 刷新成功, 过期时间 %s", exp.Format(time.RFC3339))
	return at, nil
}

func (tp *TokenPool) ensureFresh(e *poolEntry) (string, error) {
	if e.st != "" {
		need := e.at == "" || e.expiresAt.IsZero() || time.Now().Add(tp.refreshAhead).After(e.expiresAt)
		if need {
			return tp.refreshEntry(e)
		}
		return e.at, nil
	}
	if e.at == "" {
		return "", errors.New("empty access token")
	}
	if e.expiresAt.IsZero() {
		e.expiresAt = parseJWTExp(e.at)
	}
	return e.at, nil
}

// Pick 轮询选取可用 AT；含 ST 的条目会在过期前自动刷新。
func (tp *TokenPool) Pick() (string, bool) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	n := len(tp.entries)
	if n == 0 {
		return "", false
	}

	for i := 0; i < n; i++ {
		idx := (tp.roundIdx + i) % n
		e := &tp.entries[idx]
		if tp.errorKeys[e.markKey()] {
			continue
		}
		at, err := tp.ensureFresh(e)
		if err != nil {
			log.Printf("[token-pool] 刷新失败 key=%s: %v", e.markKey(), err)
			tp.errorKeys[e.markKey()] = true
			continue
		}
		tp.roundIdx = (idx + 1) % n
		return at, true
	}
	return "", false
}

// TryRefreshAT 强制用 ST 刷新与 currentAT 对应的条目（401 重试用）。
func (tp *TokenPool) TryRefreshAT(currentAT string) (string, bool) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	for i := range tp.entries {
		e := &tp.entries[i]
		if e.st == "" {
			continue
		}
		if e.at != "" && e.at != currentAT {
			continue
		}
		at, err := tp.refreshEntry(e)
		if err != nil {
			log.Printf("[token-pool] 强制刷新失败: %v", err)
			return "", false
		}
		delete(tp.errorKeys, e.markKey())
		return at, true
	}
	return "", false
}

// Add 添加凭证；ST 会以 st: 前缀写入文件。
func (tp *TokenPool) Add(lines ...string) int {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	added := 0
	f, _ := os.OpenFile(tp.tokensFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()

	existing := make(map[string]bool, len(tp.entries))
	for _, e := range tp.entries {
		existing[e.markKey()] = true
	}

	for _, raw := range lines {
		at, st := parseTokenLine(raw)
		if at == "" && st == "" {
			continue
		}
		e := poolEntry{at: at, st: st}
		if at != "" {
			e.expiresAt = parseJWTExp(at)
		}
		key := e.markKey()
		if existing[key] {
			continue
		}
		tp.entries = append(tp.entries, e)
		existing[key] = true
		added++
		if f != nil {
			if st != "" {
				// 有 ST：只持久化 st: 行（下次启动时 AT 靠 ST 换，无需存旧 AT）
				_, _ = f.WriteString("st:" + st + "\n")
			} else {
				_, _ = f.WriteString(at + "\n")
			}
		}
	}
	return added
}

// Clear 清空池与文件。
func (tp *TokenPool) Clear() {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.entries = nil
	tp.errorKeys = make(map[string]bool)
	tp.roundIdx = 0
	_ = os.WriteFile(tp.tokensFile, []byte{}, 0644)
}

// MarkError 标记失效（按当前 AT 匹配条目）。
func (tp *TokenPool) MarkError(at string) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	for i := range tp.entries {
		if tp.entries[i].at == at {
			tp.errorKeys[tp.entries[i].markKey()] = true
			return
		}
	}
	tp.errorKeys[at] = true
}

// Stats 返回 total / valid / errored。
func (tp *TokenPool) Stats() (total, valid, errored int) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	total = len(tp.entries)
	errored = len(tp.errorKeys)
	valid = total - errored
	if valid < 0 {
		valid = 0
	}
	return
}

// ErrorTokens 返回失效条目的 markKey 列表。
func (tp *TokenPool) ErrorTokens() []string {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	result := make([]string, 0, len(tp.errorKeys))
	for k := range tp.errorKeys {
		result = append(result, k)
	}
	return result
}
