package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type CORSOptions struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           time.Duration
}

func CORSMiddleware(opts CORSOptions) gin.HandlerFunc {
	origins := normalizeHeaderValues(opts.AllowedOrigins)
	if len(origins) == 0 {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	methods := normalizeHeaderValuesWithDefaults(opts.AllowedMethods, []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	})
	headers := normalizeHeaderValuesWithDefaults(opts.AllowedHeaders, []string{
		"Authorization",
		"Content-Type",
		"Accept",
		"Origin",
		"X-Requested-With",
		"Idempotency-Key",
	})
	exposedHeaders := normalizeHeaderValues(opts.ExposedHeaders)
	maxAge := opts.MaxAge
	if maxAge <= 0 {
		maxAge = 12 * time.Hour
	}

	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			c.Next()
			return
		}

		allowedOrigin := matchAllowedOrigin(origin, origins)
		if allowedOrigin == "" {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		setVaryHeader(c.Writer.Header(), "Origin")
		c.Header("Access-Control-Allow-Origin", allowedOrigin)
		if opts.AllowCredentials && allowedOrigin != "*" {
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		if len(exposedHeaders) > 0 {
			c.Header("Access-Control-Expose-Headers", strings.Join(exposedHeaders, ", "))
		}

		if c.Request.Method == http.MethodOptions {
			setVaryHeader(c.Writer.Header(), "Access-Control-Request-Method")
			setVaryHeader(c.Writer.Header(), "Access-Control-Request-Headers")
			c.Header("Access-Control-Allow-Methods", strings.Join(methods, ", "))
			c.Header("Access-Control-Allow-Headers", strings.Join(headers, ", "))
			c.Header("Access-Control-Max-Age", formatMaxAgeSeconds(maxAge))
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func matchAllowedOrigin(origin string, allowedOrigins []string) string {
	for _, allowedOrigin := range allowedOrigins {
		if allowedOrigin == "*" {
			return "*"
		}
		if strings.EqualFold(origin, allowedOrigin) {
			return origin
		}
	}
	return ""
}

func normalizeHeaderValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func normalizeHeaderValuesWithDefaults(values []string, defaults []string) []string {
	normalized := normalizeHeaderValues(values)
	if len(normalized) > 0 {
		return normalized
	}
	return normalizeHeaderValues(defaults)
}

func setVaryHeader(header http.Header, value string) {
	existing := header.Values("Vary")
	for _, entry := range existing {
		for _, part := range strings.Split(entry, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

func formatMaxAgeSeconds(maxAge time.Duration) string {
	seconds := int(maxAge / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	return strconv.Itoa(seconds)
}
