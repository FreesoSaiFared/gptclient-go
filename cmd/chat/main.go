package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	sentinel "sentinel-go"
)

func main() {
	configPath := flag.String("config", "config.json", "Config file path")
	model := flag.String("model", "gpt-5-5-thinking", "Model name")
	temp := flag.Bool("temp", false, "Temporary mode (don't save history)")
	baseURL := flag.String("base-url", "", "Backend base URL (empty defaults to https://chatgpt.com)")
	cookieFile := flag.String("cookie-file", "", "Netscape cookies.txt file path (overrides config)")
	browser := flag.String("browser", "", "Browser to extract cookies from (overrides config)")
	profile := flag.String("profile", "", "Browser profile name or path (overrides config)")
	cookieDomain := flag.String("cookie-domain", "", "Domain to filter cookies for (default chatgpt.com)")
	printCookieStatus := flag.Bool("print-cookie-status", false, "Print cookie resolution status")
	flag.Parse()

	// Load runtime config
	cfg, err := sentinel.LoadRuntimeConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	if cfg.BearerToken == "" {
		cfg.BearerToken = "REPLACE_WITH_JWT"
	}

	// Override cookie source from CLI flags
	if *cookieFile != "" {
		cfg.Cookies.File = *cookieFile
		cfg.Cookies.Enabled = true
	}
	if *browser != "" {
		cfg.Cookies.Browser = *browser
		cfg.Cookies.Enabled = true
	}
	if *profile != "" {
		cfg.Cookies.Profile = *profile
	}
	if *cookieDomain != "" {
		cfg.Cookies.Domain = *cookieDomain
	}

	// Resolve cookie string
	cookieString, err := sentinel.ResolveCookieString(context.Background(), cfg.CookieString, cfg.Cookies)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to resolve cookies: %v\n", err)
		cookieString = cfg.CookieString // fall back to manual cookie string
	}

	if *printCookieStatus {
		if cookieString != "" {
			n := strings.Count(cookieString, ";") + 1
			fmt.Fprintf(os.Stderr, "Cookie status: resolved %d cookies, %d bytes\n", n, len(cookieString))
		} else {
			fmt.Fprintln(os.Stderr, "Cookie status: no cookies resolved")
		}
	}

	// Try to auto-refresh the access token if cookies are available
	bearerToken := cfg.BearerToken
	needsRefresh := cookieString != "" && (bearerToken == "REPLACE_WITH_JWT" || bearerToken == "" || isTokenExpired(bearerToken))
	if needsRefresh {
		fmt.Fprintln(os.Stderr, "Attempting auto-refresh from cookies...")
		fresh, err := sentinel.RefreshAccessToken(cookieString)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: auto-refresh failed: %v\n", err)
			if bearerToken == "REPLACE_WITH_JWT" || bearerToken == "" {
				fmt.Fprintln(os.Stderr, "No bearer token available. Please set bearerToken in config.json or ensure browser cookies are valid.")
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "The expired token will still be tried; expect 401 errors.")
		} else {
			bearerToken = fresh
			fmt.Fprintf(os.Stderr, "Auto-refresh succeeded: got new token (%d chars)\n", len(fresh))
		}
	}

	if bearerToken == "" || bearerToken == "REPLACE_WITH_JWT" {
		fmt.Fprintln(os.Stderr, "No credentials configured. Please edit config.json or provide browser cookies.")
		os.Exit(1)
	}

	client := sentinel.NewClient(sentinel.Config{
		BearerToken:        bearerToken,
		CookieString:       cookieString,
		Model:              *model,
		TempMode:           *temp,
		BaseURL:            *baseURL,
		DisableImpersonate: *baseURL != "",
	})

	args := flag.Args()
	if len(args) > 0 {
		userMsg := strings.Join(args, " ")
		fmt.Printf("\nYou: %s\n\nChatGPT:\n\n", userMsg)
		_, err := client.ChatStream(userMsg, func(delta string) {
			fmt.Print(delta)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n[error] %v\n", err)
			os.Exit(1)
		}
		fmt.Println()
		return
	}

	startRepl(client, cookieString)
}

// isTokenExpired checks if a JWT bearer token has expired by decoding the
// payload without verifying the signature. Returns true if the token is
// expired or if it cannot be parsed.
func isTokenExpired(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false // not a JWT, can't tell
	}
	payload := parts[1]
	// Add base64url padding
	payload += strings.Repeat("=", (4-len(payload)%4)%4)
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		// Try std encoding as fallback
		decoded, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return false
		}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return false
	}
	if claims.Exp == 0 {
		return false
	}
	return time.Now().Unix() > claims.Exp
}

func startRepl(client *sentinel.Client, cookieString string) {
	reader := bufio.NewReader(os.Stdin)

	info := client.GetSessionInfo()
	fmt.Println("=== ChatGPT Multi-turn Conversation (Go) ===")
	fmt.Printf("Model: %s | Temp mode: %s\n", info.Model, boolStr(info.TempMode))
	fmt.Println("Commands: /new (new conversation) /model <name> (switch model) /temp (toggle temp mode) /info (session info) /exit (quit)")
	fmt.Println()

	for {
		fmt.Print("You> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		switch {
		case input == "/exit" || input == "/quit":
			fmt.Println("Goodbye!")
			return

		case input == "/new":
			client.ResetSession()
			fmt.Print("[ok] New conversation started, context reset\n\n")

		case strings.HasPrefix(input, "/model"):
			parts := strings.Fields(input)
			if len(parts) > 1 {
				client.SetModel(parts[1])
				fmt.Printf("[ok] Model switched to: %s\n\n", parts[1])
			} else {
				fmt.Printf("[current model] %s\n", client.GetModel())
				fmt.Print("  Available: gpt-5-5-thinking, gpt-5-5, gpt-4o, o4-mini-high\n\n")
			}

		case input == "/temp":
			info := client.GetSessionInfo()
			client.SetTempMode(!info.TempMode)
			newInfo := client.GetSessionInfo()
			if newInfo.TempMode {
				fmt.Print("[ok] Temp mode: on (don't save history / don't update memory)\n\n")
			} else {
				fmt.Print("[ok] Temp mode: off (normal save)\n\n")
			}

		case input == "/info":
			info := client.GetSessionInfo()
			cid := info.ConversationID
			if cid == "" {
				cid = "(none, new conversation)"
			}
			fmt.Printf("  conversation_id  : %s\n", cid)
			fmt.Printf("  parent_message_id: %s\n", info.ParentMessageID)
			fmt.Printf("  model            : %s\n", info.Model)
			fmt.Printf("  temp_mode        : %s\n", boolStr(info.TempMode))
			fmt.Printf("  turn             : %d\n\n", info.TurnCount)

		case input == "/refresh":
			if cookieString == "" {
				fmt.Print("[error] No cookies available for token refresh\n\n")
				continue
			}
			fmt.Print("Refreshing access token...\n")
			fresh, err := sentinel.RefreshAccessToken(cookieString)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[error] Refresh failed: %v\n\n", err)
			} else {
				client.SetBearerToken(fresh)
				fmt.Printf("[ok] Token refreshed (%d chars)\n\n", len(fresh))
			}

		default:
			fmt.Print("\nChatGPT:\n\n")
			_, err := client.ChatStream(input, func(delta string) {
				fmt.Print(delta)
			})
			if err != nil {
				// If the error is token_expired and we have cookies, try auto-refresh
				if strings.Contains(err.Error(), "token_expired") && cookieString != "" {
					fmt.Fprintln(os.Stderr, "\nToken expired, attempting auto-refresh...")
					fresh, refreshErr := sentinel.RefreshAccessToken(cookieString)
					if refreshErr != nil {
						fmt.Fprintf(os.Stderr, "[error] Auto-refresh failed: %v\n\n", refreshErr)
					} else {
						client.SetBearerToken(fresh)
						fmt.Fprintln(os.Stderr, "Auto-refresh succeeded, retrying...")
						fmt.Print("\nChatGPT:\n\n")
						_, retryErr := client.ChatStream(input, func(delta string) {
							fmt.Print(delta)
						})
						if retryErr != nil {
							fmt.Fprintf(os.Stderr, "\n[error] %v\n\n", retryErr)
						} else {
							fmt.Println()
						}
					}
				} else {
					fmt.Fprintf(os.Stderr, "\n[error] %v\n\n", err)
				}
			} else {
				fmt.Println()
			}
		}
	}
}

func boolStr(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
