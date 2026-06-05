package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	sentinel "sentinel-go"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

func main() {
	configPath := "config.json"
	addr := ":7777"
	model := "gpt-5-5-thinking"

	// Parse simple args
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-config":
			if i+1 < len(args) {
				configPath = args[i+1]
				i++
			}
		case "-addr":
			if i+1 < len(args) {
				addr = args[i+1]
				i++
			}
		case "-model":
			if i+1 < len(args) {
				model = args[i+1]
				i++
			}
		case "-h", "-help", "--help":
			printUsage()
			return
		}
	}

	printBanner()

	// Step 1: Load config
	fmt.Printf("%s  Loading config from %s%s\n", colorDim, configPath, colorReset)
	cfg, err := sentinel.LoadRuntimeConfig(configPath)
	if err != nil {
		fmt.Printf("  %sNo config found — starting fresh%s\n", colorYellow, colorReset)
		cfg = sentinel.RuntimeConfig{}
	}

	// Step 2: Check if current token is valid
	fmt.Printf("\n%s  Checking authentication...%s\n", colorCyan, colorReset)
	tokenValid := false
	tokenPlan := ""
	if cfg.BearerToken != "" && cfg.BearerToken != "REPLACE_WITH_JWT" {
		exp := jwtExpiry(cfg.BearerToken)
		if exp > 0 && time.Now().Unix() < exp {
			tokenValid = true
			tokenPlan, _ = extractPlanFromToken(cfg.BearerToken)
			fmt.Printf("  %s  Bearer token: VALID%s (expires %s", colorGreen, colorReset, formatExpiry(exp))
			if tokenPlan != "" {
				fmt.Printf(", %s plan", tokenPlan)
			}
			fmt.Println(")")
		} else {
			fmt.Printf("  %s  Bearer token: EXPIRED%s\n", colorRed, colorReset)
		}
	} else {
		fmt.Printf("  %s  Bearer token: NOT SET%s\n", colorYellow, colorReset)
	}

	// Step 3: If token not valid, probe browsers
	var cookieString string
	var selectedBrowser string

	if !tokenValid {
		fmt.Printf("\n%s  Probing browsers for chatgpt.com cookies...%s\n\n", colorCyan, colorReset)

		results := sentinel.ProbeAllBrowsers(context.Background())

		// Display results as a table
		available := printProbeResults(results)

		if len(available) == 0 {
			fmt.Printf("\n  %sNo browser cookies found.%s\n", colorRed, colorReset)
			fmt.Println("\n  Options:")
			fmt.Println("    1. Log into chatgpt.com in your browser and try again")
			fmt.Println("    2. Export cookies.txt and set cookies.file in config.json")
			fmt.Println("    3. Set bearerToken manually in config.json")
			os.Exit(1)
		}

		// If only one option, auto-select it
		if len(available) == 1 {
			selectedBrowser = available[0].Browser
			fmt.Printf("\n  %sAuto-selecting: %s%s (only option)\n", colorGreen, selectedBrowser, colorReset)
		} else {
			// Let user choose
			selectedBrowser = promptSelection(available)
			if selectedBrowser == "" {
				fmt.Println("\n  No selection made. Exiting.")
				os.Exit(1)
			}
		}

		// Extract cookies and refresh token
		fmt.Printf("\n%s  Extracting cookies from %s...%s\n", colorCyan, selectedBrowser, colorReset)
		src := sentinel.CookieSourceConfig{
			Enabled: true,
			Browser: selectedBrowser,
			Domain:  "chatgpt.com",
		}
		cookieString, err = sentinel.ResolveCookieString(context.Background(), "", src)
		if err != nil {
			fmt.Printf("  %sFailed: %v%s\n", colorRed, err, colorReset)
			os.Exit(1)
		}

		fmt.Printf("  %s  Cookies: %d bytes resolved%s\n", colorGreen, len(cookieString), colorReset)

		// Refresh access token
		fmt.Printf("  %s  Refreshing access token...%s\n", colorCyan, colorReset)
		token, err := sentinel.RefreshAccessToken(cookieString)
		if err != nil {
			fmt.Printf("  %sToken refresh failed: %v%s\n", colorRed, err, colorReset)
			fmt.Println("  Your browser session may be expired. Try logging into chatgpt.com again.")
			os.Exit(1)
		}

		plan, email := extractAccountInfo(token)
		fmt.Printf("  %s  Access token: OK%s (%d chars", colorGreen, colorReset, len(token))
		if plan != "" {
			fmt.Printf(", %s plan", plan)
		}
		if email != "" {
			fmt.Printf(", %s", email)
		}
		fmt.Println(")")

		// Save config
		cfg.BearerToken = token
		cfg.CookieString = cookieString
		cfg.Cookies = src
		if err := saveConfig(configPath, cfg); err != nil {
			fmt.Printf("  %sWarning: could not save config: %v%s\n", colorYellow, err, colorReset)
		} else {
			fmt.Printf("  %s  Config saved to %s%s\n", colorDim, configPath, colorReset)
		}
	} else {
		// Token is valid, resolve cookies for the session anyway
		if cfg.Cookies.Enabled && cfg.Cookies.Browser != "" {
			cookieString, _ = sentinel.ResolveCookieString(context.Background(), cfg.CookieString, cfg.Cookies)
		} else {
			cookieString = cfg.CookieString
		}
	}

	// Step 4: Start the server
	fmt.Printf("\n%s  Starting sentinel server...%s\n\n", colorGreen, colorReset)
	fmt.Printf("  %sModel:%s  %s\n", colorBold, colorReset, model)
	fmt.Printf("  %sAddr:%s    %s\n", colorBold, colorReset, addr)
	fmt.Printf("  %sConfig:%s  %s\n", colorBold, colorReset, configPath)
	fmt.Println()

	// Build and exec the server
	startServer(configPath, addr, model)
}

func printBanner() {
	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════════╗")
	fmt.Println("  ║       ⬡ Sentinel — ChatGPT Gateway       ║")
	fmt.Println("  ║         OpenAI-Compatible Server          ║")
	fmt.Println("  ╚══════════════════════════════════════════╝")
	fmt.Println()
}

func printProbeResults(results []sentinel.BrowserProbeResult) []sentinel.BrowserProbeResult {
	var available []sentinel.BrowserProbeResult

	fmt.Printf("  %-4s %-12s %-8s %-10s %-8s %-25s %s\n",
		"#", "Browser", "Cookies", "Size", "Keyring", "Account", "Status")
	fmt.Printf("  %-4s %-12s %-8s %-10s %-8s %-25s %s\n",
		"---", "--------", "-------", "--------", "-------", "-------------------------", "------")

	idx := 1
	for _, r := range results {
		if !r.Available && r.Error != "" {
			if strings.Contains(r.Error, "no Cookies found") ||
				strings.Contains(r.Error, "find") ||
				strings.Contains(r.Error, "no cookies.sqlite") {
				fmt.Printf("  %-4s %-12s —        —          —       %-25s %s\n",
					"", r.Browser, "", "not found")
				continue
			}
			if strings.Contains(r.Error, "secret-tool") {
				fmt.Printf("  %-4s %-12s ?        —          NO      %-25s %s\n",
					"", r.Browser, "", "keyring error")
				continue
			}
			continue
		}
		if !r.Available {
			continue
		}

		available = append(available, r)

		keyring := "YES"
		if !r.KeyringAccess {
			keyring = "partial"
		}

		account := r.AccountPlan
		if r.AccountEmail != "" {
			account = r.AccountPlan + " (" + r.AccountEmail + ")"
		}
		if account == "" {
			account = "unknown"
		}

		fmt.Printf("  %s%d%s   %-12s %s%-8d%s %-10s %-8s %-25s %sOK%s\n",
			colorBold, idx, colorReset,
			r.Browser,
			colorGreen, r.CookieCount, colorReset,
			fmt.Sprintf("%d bytes", r.CookieBytes),
			keyring, account,
			colorGreen, colorReset)
		idx++
	}

	return available
}

func promptSelection(available []sentinel.BrowserProbeResult) string {
	fmt.Printf("\n  %sSelect a browser [%d-%d]:%s ", colorBold, 1, len(available), colorReset)

	var input string
	fmt.Scanln(&input)

	input = strings.TrimSpace(input)
	num, err := strconv.Atoi(input)
	if err != nil || num < 1 || num > len(available) {
		fmt.Printf("  %sInvalid selection%s\n", colorRed, colorReset)
		return ""
	}

	selected := available[num-1].Browser
	fmt.Printf("  %sSelected: %s%s\n", colorGreen, selected, colorReset)
	return selected
}

func startServer(configPath, addr, model string) {
	// Use syscall.Exec to replace the current process with the server
	args := []string{
		"go", "run", "./cmd/server",
		"-config", configPath,
		"-addr", addr,
		"-model", model,
	}
	// Look up the go binary
	goPath, err := findGoBinary()
	if err != nil {
		fmt.Printf("  %sError: %v%s\n", colorRed, err, colorReset)
		os.Exit(1)
	}
	syscall.Exec(goPath, args, os.Environ())
}

// --- JWT helpers ---

func jwtExpiry(token string) int64 {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0
	}
	payload := parts[1]
	payload += strings.Repeat("=", (4-len(payload)%4)%4)
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return 0
		}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return 0
	}
	return claims.Exp
}

func extractPlanFromToken(token string) (string, string) {
	return extractAccountInfo(token)
}

func formatExpiry(unix int64) string {
	t := time.Unix(unix, 0)
	dur := time.Until(t)
	if dur < 0 {
		return "expired"
	}
	if dur < time.Hour {
		return fmt.Sprintf("%d min from now", int(dur.Minutes()))
	}
	if dur < 24*time.Hour {
		return fmt.Sprintf("%d hours from now", int(dur.Hours()))
	}
	return fmt.Sprintf("%s", t.Format("Jan 2"))
}

func extractAccountInfo(token string) (plan, email string) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ""
	}
	payload := parts[1]
	payload += strings.Repeat("=", (4-len(payload)%4)%4)
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return "", ""
		}
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

func saveConfig(path string, cfg sentinel.RuntimeConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func findGoBinary() (string, error) {
	// Check PATH for go
	path := os.Getenv("PATH")
	for _, dir := range strings.Split(path, ":") {
		candidate := strings.TrimSpace(dir) + "/go"
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	// Check common locations
	for _, loc := range []string{"/usr/local/go/bin/go", "/usr/bin/go", os.Getenv("HOME") + "/go/bin/go"} {
		if _, err := os.Stat(loc); err == nil {
			return loc, nil
		}
	}
	return "", fmt.Errorf("go binary not found in PATH")
}

func printUsage() {
	fmt.Println("sentinel start — Launch the ChatGPT gateway server")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  go run ./cmd/start [options]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -config PATH    Config file path (default: config.json)")
	fmt.Println("  -addr ADDR      Listen address (default: :7777)")
	fmt.Println("  -model NAME     Default model (default: gpt-5-5-thinking)")
	fmt.Println("  -h, --help      Show this help")
	fmt.Println()
	fmt.Println("The start command will:")
	fmt.Println("  1. Check if the current bearer token is valid")
	fmt.Println("  2. If not, probe all browsers for chatgpt.com cookies")
	fmt.Println("  3. Display account info for each browser found")
	fmt.Println("  4. Let you select which browser to use")
	fmt.Println("  5. Auto-refresh the access token")
	fmt.Println("  6. Save the config and start the server")
}
