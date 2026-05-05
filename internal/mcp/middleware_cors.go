package mcp

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
)

const defaultCORSMaxAgeSeconds = 86400

var (
	defaultCORSMethods = []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodDelete,
		http.MethodOptions,
	}
	defaultCORSHeaders = []string{
		"Content-Type",
		"Accept",
		"Authorization",
		"Mcp-Protocol-Version",
		"Mcp-Session-Id",
		"Last-Event-ID",
		"Mcp-Method",
		"Mcp-Name",
	}
	defaultCORSExposedHeaders = []string{
		"Mcp-Session-Id",
		"WWW-Authenticate",
	}
)

func (s *Server) corsHTTPMiddleware(next http.Handler) http.Handler {
	cfg := &s.config.MCP.CORS
	if !cfg.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if !isCORSOriginAllowed(origin, cfg.AllowedOrigins) {
			http.Error(w, "cors origin is not allowed", http.StatusForbidden)
			return
		}

		writeCORSHeaders(
			w.Header(),
			origin,
			cfg.AllowCredentials,
			cfg.AllowedMethods,
			cfg.AllowedHeaders,
			cfg.ExposedHeaders,
			cfg.MaxAgeSeconds,
		)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeCORSHeaders(
	header http.Header,
	origin string,
	allowCredentials bool,
	allowedMethods []string,
	allowedHeaders []string,
	exposedHeaders []string,
	maxAgeSeconds int,
) {
	header.Set("Access-Control-Allow-Origin", origin)
	header.Add("Vary", "Origin")
	header.Set("Access-Control-Allow-Methods", strings.Join(allowedMethods, ", "))
	header.Set("Access-Control-Allow-Headers", strings.Join(allowedHeaders, ", "))
	header.Set("Access-Control-Expose-Headers", strings.Join(exposedHeaders, ", "))
	header.Set("Access-Control-Max-Age", strconv.Itoa(maxAgeSeconds))
	if allowCredentials {
		header.Set("Access-Control-Allow-Credentials", "true")
	}
}

func isCORSOriginAllowed(origin string, allowedOrigins []string) bool {
	return slices.Contains(allowedOrigins, "*") || slices.Contains(allowedOrigins, origin)
}
