package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware 跨域中间件
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Requested-With")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// AuthMiddleware 鉴权中间件
// 若配置了 AUTHORIZATION 环境变量，则验证请求头中的 Bearer Token 是否匹配
// 若未配置，则跳过鉴权（直接将 Bearer Token 视为 ChatGPT token）
// 若配置了 FallbackBearerToken，则支持 dummy keys (sk-sentinel-local, sk-dummy) for config-based auth
func AuthMiddleware(cfg *ServerConfig, pool *TokenPool) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		rawToken := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(auth, "Bearer "), "Bearer"))

		// When AUTHORIZATION is configured, check raw token match first (before cleanToken filters non-JWT)
		if cfg.Authorization != "" {
			if rawToken == "" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{
					Error: ErrorDetail{
						Message: "Missing Authorization header",
						Type:    "invalid_request_error",
						Code:    "missing_auth",
					},
				})
				return
			}
			// sk-dummy/sk-sentinel-local are not valid API keys
			if rawToken == "sk-sentinel-local" || rawToken == "sk-dummy" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{
					Error: ErrorDetail{
						Message: "Invalid API key",
						Type:    "invalid_request_error",
						Code:    "invalid_api_key",
					},
				})
				return
			}
			// Token must match the configured API key
			if rawToken != cfg.Authorization {
				c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{
					Error: ErrorDetail{
						Message: "Invalid API key",
						Type:    "invalid_request_error",
						Code:    "invalid_api_key",
					},
				})
				return
			}
			// Token matches AUTHORIZATION - try pool first
			chatgptToken, ok := pool.Pick()
			if !ok {
				// Pool empty, try config fallback
				if cfg.FallbackBearerToken != "" {
					c.Set("chatgpt_token", cfg.FallbackBearerToken)
					c.Set("from_pool", false)
					c.Set("from_config_fallback", true)
					c.Next()
					return
				}
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, ErrorResponse{
					Error: ErrorDetail{
						Message: "Token pool is empty. Please upload tokens or provide one in the request.",
						Type:    "server_error",
						Code:    "no_token",
					},
				})
				return
			}
			c.Set("chatgpt_token", chatgptToken)
			c.Set("from_pool", true)
			c.Next()
			return
		}

		// AUTHORIZATION not configured - local development mode
		token := cleanToken(rawToken)

		// Check for dummy key fallback (only when FallbackBearerToken available)
		if (rawToken == "sk-sentinel-local" || rawToken == "sk-dummy" || token == "") && cfg.FallbackBearerToken != "" {
			c.Set("chatgpt_token", cfg.FallbackBearerToken)
			c.Set("from_pool", false)
			c.Set("from_config_fallback", true)
			c.Next()
			return
		}

		// Empty token with no fallback and no authorization = use pool
		if token == "" {
			chatgptToken, ok := pool.Pick()
			if !ok {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, ErrorResponse{
					Error: ErrorDetail{
						Message: "Token pool is empty. Please upload tokens or provide one in the request.",
						Type:    "server_error",
						Code:    "no_token",
					},
				})
				return
			}
			c.Set("chatgpt_token", chatgptToken)
			c.Set("from_pool", true)
			c.Next()
			return
		}

		// Pass through real ChatGPT JWT token
		c.Set("chatgpt_token", token)
		c.Set("from_pool", false)
		c.Next()
	}
}

// extractChatGPTToken 从 gin Context 中取出 chatgpt_token
func extractChatGPTToken(c *gin.Context) string {
	v, _ := c.Get("chatgpt_token")
	t, _ := v.(string)
	return t
}
