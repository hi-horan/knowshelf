package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	tokenauth "knowshelf/internal/pkg/auth"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) authHTTPMiddleware(next http.Handler) http.Handler {
	if !s.authEnabled() {
		return next
	}
	return sdkauth.RequireBearerToken(s.verifyBearerToken, nil)(next)
}

func (s *Server) scopeMiddleware() mcpsdk.Middleware {
	return func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			if !s.authEnabled() {
				return next(ctx, method, req)
			}
			tokenInfo := requestTokenInfo(ctx, req)
			if tokenInfo == nil {
				return nil, fmt.Errorf("authenticate mcp request: token info is missing")
			}
			if err := authorizeMCPMethod(tokenInfo, method, req); err != nil {
				s.log().LogAttrs(ctx, slog.LevelWarn, "mcp authorization failed",
					slog.String("method", method),
					slog.String("tool", requestToolName(req)),
					slog.String("subject", tokenInfo.UserID),
					slog.String("error", err.Error()))
				return nil, err
			}
			return next(ctx, method, req)
		}
	}
}

func (s *Server) authEnabled() bool {
	return s.config.MCP.Auth.Enabled
}

func (s *Server) verifyBearerToken(_ context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
	claims, err := tokenauth.VerifyToken(s.config.MCP.Auth.Secret, token)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", sdkauth.ErrInvalidToken, err)
	}
	return &sdkauth.TokenInfo{
		Scopes:     claims.Scopes,
		Expiration: time.Unix(claims.ExpiresAt, 0),
		UserID:     claims.Subject,
	}, nil
}

func requestTokenInfo(ctx context.Context, req mcpsdk.Request) *sdkauth.TokenInfo {
	if req.GetExtra() != nil && req.GetExtra().TokenInfo != nil {
		return req.GetExtra().TokenInfo
	}
	return sdkauth.TokenInfoFromContext(ctx)
}

func authorizeMCPMethod(tokenInfo *sdkauth.TokenInfo, method string, req mcpsdk.Request) error {
	required := requiredScopes(method, req)
	if len(required) == 0 {
		return nil
	}
	if tokenInfoHasAnyScope(tokenInfo, required) {
		return nil
	}
	return fmt.Errorf("mcp scope denied: method %q requires one of %s", method, strings.Join(required, ","))
}

func tokenInfoHasAnyScope(tokenInfo *sdkauth.TokenInfo, required []string) bool {
	if tokenInfo == nil {
		return false
	}
	for _, requiredScope := range required {
		for _, scope := range tokenInfo.Scopes {
			if scope == tokenauth.ScopeAll || scope == requiredScope {
				return true
			}
		}
	}
	return false
}

func requiredScopes(method string, req mcpsdk.Request) []string {
	switch method {
	case "initialize", "ping", "notifications/initialized", "notifications/cancelled":
		return nil
	case "tools/list":
		return []string{tokenauth.ScopeRead, tokenauth.ScopeImport}
	case "tools/call":
		return requiredToolScopes(requestToolName(req))
	case "resources/list", "resources/templates/list", "resources/read", "prompts/list", "prompts/get", "completion/complete":
		return []string{tokenauth.ScopeRead}
	default:
		return []string{tokenauth.ScopeRead}
	}
}

func requiredToolScopes(toolName string) []string {
	switch toolName {
	case "query", "listAllBooks", "get", "status":
		return []string{tokenauth.ScopeRead}
	case "import":
		return []string{tokenauth.ScopeImport}
	default:
		return []string{tokenauth.ScopeAll}
	}
}
