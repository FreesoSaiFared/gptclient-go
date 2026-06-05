package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	sentinel "sentinel-go"
)

func main() {
	configPath := flag.String("config", "config.json", "Config file path")
	browser := flag.String("browser", "", "Browser to extract cookies from (chrome, chromium, brave, edge, opera, vivaldi, firefox)")
	profile := flag.String("profile", "", "Browser profile name or path")
	cookieFile := flag.String("cookie-file", "", "Netscape cookies.txt file path")
	domain := flag.String("domain", "chatgpt.com", "Domain to filter cookies for")
	writeConfig := flag.Bool("write", false, "Write resolved cookie string to config file")
	printStatus := flag.Bool("print", false, "Print redacted cookie status summary")
	flag.Parse()

	if *browser == "" && *cookieFile == "" {
		fmt.Fprintln(os.Stderr, "Error: specify -browser or -cookie-file")
		flag.Usage()
		os.Exit(1)
	}

	// Build cookie source config
	src := sentinel.CookieSourceConfig{
		Enabled: true,
		Browser: *browser,
		Profile: *profile,
		File:    *cookieFile,
		Domain:  *domain,
	}

	// Resolve cookie string
	cookieString, err := sentinel.ResolveCookieString(context.Background(), "", src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving cookies: %v\n", err)
		os.Exit(1)
	}

	if *printStatus {
		count := 0
		bytes := len(cookieString)
		if cookieString != "" {
			// Count cookies by semicolons
			parts := splitCookieParts(cookieString)
			count = len(parts)
		}
		fmt.Printf("resolved cookie header for %s: %d cookies, %d bytes\n", *domain, count, bytes)
	}

	if *writeConfig {
		cfg, readErr := sentinel.LoadRuntimeConfig(*configPath)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read existing config: %v\n", readErr)
			cfg = sentinel.RuntimeConfig{}
		}

		// Update cookie string and cookies block
		cfg.CookieString = cookieString
		cfg.Cookies = src

		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling config: %v\n", err)
			os.Exit(1)
		}

		if err := os.WriteFile(*configPath, data, 0600); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Updated %s with resolved cookie string (%d bytes)\n", *configPath, len(cookieString))
	}

	if !*printStatus && !*writeConfig {
		fmt.Fprintf(os.Stderr, "No action specified. Use -print and/or -write.\n")
		os.Exit(1)
	}
}

func splitCookieParts(header string) []string {
	if header == "" {
		return nil
	}
	var parts []string
	for _, p := range splitSemicolons(header) {
		p = trimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitSemicolons(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ';' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
