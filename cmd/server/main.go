package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	sentinel "sentinel-go"
	"sentinel-go/server"
)

func main() {
	configPath := flag.String("config", "config.json", "Config file path")
	addr := flag.String("addr", ":7777", "Listen address")
	model := flag.String("model", "gpt-5-5-thinking", "Default model")
	baseURL := flag.String("base-url", "", "Base URL for ChatGPT API (default: https://chatgpt.com)")
	flag.Parse()

	cfg := server.LoadConfig()
	cfg.Port = strings.TrimPrefix(*addr, ":")

	// Load RuntimeConfig for fallback token support
	var runtimeCfg sentinel.RuntimeConfig
	if *configPath != "" {
		var err error
		runtimeCfg, err = sentinel.LoadRuntimeConfig(*configPath)
		if err != nil {
			log.Printf("[startup] Config file not found or invalid: %s", *configPath)
		}
	}

	// Set fallback bearer token from config
	if runtimeCfg.BearerToken != "" && runtimeCfg.BearerToken != "REPLACE_WITH_JWT" {
		cfg.FallbackBearerToken = extractAccessToken(runtimeCfg.BearerToken)
		cfg.FallbackCookieString = runtimeCfg.CookieString
	}

	if *model != "" {
		cfg.DefaultModel = *model
	}
	if *baseURL != "" {
		cfg.BaseURL = *baseURL
	}

	log.Printf("============================================")
	log.Printf("  sentinel-go API Server")
	log.Printf("  Port           : %s", cfg.Port)
	log.Printf("  Default Model  : %s", cfg.DefaultModel)
	log.Printf("  Temp Mode      : %v", cfg.TempMode)
	log.Printf("  Tokens File    : %s", cfg.TokensFile)
	log.Printf("  Session TTL    : %d min", cfg.SessionTTLMinutes)
	log.Printf("  Base URL       : %s", cfg.BaseURL)
	if cfg.Authorization != "" {
		log.Printf("  Authorization  : configured (pool mode)")
	} else if cfg.FallbackBearerToken != "" {
		log.Printf("  Config Fallback: enabled (no AUTHORIZATION header required)")
	} else {
		log.Printf("  Authorization  : not set (direct token mode)")
	}
	log.Printf("============================================")

	pool := server.NewTokenPool(cfg.TokensFile, time.Duration(cfg.TokenRefreshAheadSec)*time.Second)
	total, valid, _ := pool.Stats()
	log.Printf("[startup] Token pool: total=%d, valid=%d", total, valid)

	session := server.NewSessionManager(&cfg)
	log.Printf("[startup] Session manager initialized (TTL=%d min)", cfg.SessionTTLMinutes)

	r := server.NewRouter(&cfg, pool, session)

	listenAddr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("[startup] Listening on http://0.0.0.0%s", listenAddr)
	log.Printf("[startup] API endpoint: http://0.0.0.0%s/v1/chat/completions", listenAddr)

	if err := r.Run(listenAddr); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// extractAccessToken extracts accessToken from session JSON or returns raw token
func extractAccessToken(bearerToken string) string {
	var sessionData struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal([]byte(bearerToken), &sessionData); err == nil && sessionData.AccessToken != "" {
		return sessionData.AccessToken
	}
	return bearerToken
}
