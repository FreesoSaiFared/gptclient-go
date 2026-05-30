package server

import (
	"testing"
)

func TestParseTokenLine(t *testing.T) {
	// st: 前缀
	at, st := parseTokenLine("st:abc123sessiontokenvaluexxxxxxxxxxxxxxxx")
	if at != "" || st != "abc123sessiontokenvaluexxxxxxxxxxxxxxxx" {
		t.Fatalf("st line: at=%q st=%q", at, st)
	}
	// 纯 JWT Access Token
	at, st = parseTokenLine("eyJhbGciOiJSUzI1NiJ9.eyJleHAiOjE3MDAwMDAwMDB9.sig")
	if at == "" || st != "" {
		t.Fatalf("jwt line: at=%q st=%q", at, st)
	}
	// JSON 整段：同时有 accessToken 和 sessionToken
	jsonLine := `{"accessToken":"eyJhbGciOiJSUzI1NiJ9.eyJleHAiOjE3MDAwMDAwMDB9.sig","sessionToken":"mysessiontokenvalue1234567890abcdefghij"}`
	at, st = parseTokenLine(jsonLine)
	if at == "" {
		t.Fatalf("json line: missing at, got at=%q st=%q", at, st)
	}
	if st != "mysessiontokenvalue1234567890abcdefghij" {
		t.Fatalf("json line: missing st, got st=%q", st)
	}
	// JSON 只有 accessToken（老格式）
	jsonOnlyAT := `{"accessToken":"eyJhbGciOiJSUzI1NiJ9.eyJleHAiOjE3MDAwMDAwMDB9.sig"}`
	at, st = parseTokenLine(jsonOnlyAT)
	if at == "" || st != "" {
		t.Fatalf("json only-AT line: at=%q st=%q", at, st)
	}
}

func TestNormalizeSessionToken(t *testing.T) {
	got := normalizeSessionToken("__Secure-next-auth.session-token=secretvalue")
	if got != "secretvalue" {
		t.Fatalf("got %q", got)
	}
}

func TestParseJWTExp(t *testing.T) {
	exp := parseJWTExp("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjQxMDI0NDQ4MDB9.x")
	if exp.Unix() != 4102444800 {
		t.Fatalf("exp unix=%d", exp.Unix())
	}
}
