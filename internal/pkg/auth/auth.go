package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ScopeAll = "mcp:*"
	ScopeRead = "mcp:read"
	ScopeImport = "mcp:import"
)

var (
	ErrEmptySecret  = errors.New("auth secret is required")
	errInvalidToken = errors.New("invalid bearer token")
)

type tokenHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

type TokenClaims struct {
	Subject   string   `json:"sub"`
	Scopes    []string `json:"scopes"`
	IssuedAt  int64    `json:"iat"`
	ExpiresAt int64    `json:"exp"`
}

func NewTokenClaims(subject string, scopes []string, now time.Time, ttl time.Duration) (TokenClaims, error) {
	if subject == "" {
		return TokenClaims{}, errors.New("token subject is required")
	}
	if ttl <= 0 {
		return TokenClaims{}, errors.New("token ttl must be greater than zero")
	}
	now = now.UTC()
	return TokenClaims{
		Subject:   subject,
		Scopes:    scopes,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
	}, nil
}

func GenerateToken(secret string, claims TokenClaims) (string, error) {
	if err := validateClaimsForSigning(claims); err != nil {
		return "", err
	}
	headerPart, err := encodeTokenJSON(tokenHeader{Algorithm: "HS256", Type: "JWT"})
	if err != nil {
		return "", err
	}
	payloadPart, err := encodeTokenJSON(claims)
	if err != nil {
		return "", err
	}
	signingInput := headerPart + "." + payloadPart
	signature := signToken(secret, signingInput)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func VerifyToken(secret string, token string) (TokenClaims, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return TokenClaims{}, ErrEmptySecret
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return TokenClaims{}, errInvalidToken
	}
	signingInput := parts[0] + "." + parts[1]
	expectedSignature := signToken(secret, signingInput)
	actualSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return TokenClaims{}, errInvalidToken
	}
	if !hmac.Equal(actualSignature, expectedSignature) {
		return TokenClaims{}, errInvalidToken
	}
	var header tokenHeader
	if err := decodeTokenJSON(parts[0], &header); err != nil {
		return TokenClaims{}, errInvalidToken
	}
	if header.Algorithm != "HS256" || header.Type != "JWT" {
		return TokenClaims{}, errInvalidToken
	}
	var claims TokenClaims
	if err := decodeTokenJSON(parts[1], &claims); err != nil {
		return TokenClaims{}, errInvalidToken
	}
	if err := validateClaimsForSigning(claims); err != nil {
		return TokenClaims{}, fmt.Errorf("%w: %v", errInvalidToken, err)
	}
	return claims, nil
}

func ValidateToken(secret string, token string, now time.Time) (TokenClaims, error) {
	claims, err := VerifyToken(secret, token)
	if err != nil {
		return TokenClaims{}, err
	}
	if claims.ExpiresAt <= now.UTC().Unix() {
		return TokenClaims{}, fmt.Errorf("%w: expired", errInvalidToken)
	}
	return claims, nil
}

func (c TokenClaims) HasScope(required string) bool {
	for _, scope := range c.Scopes {
		if scope == ScopeAll || scope == required {
			return true
		}
	}
	return false
}

func (c TokenClaims) HasAnyScope(required []string) bool {
	for _, scope := range required {
		if c.HasScope(scope) {
			return true
		}
	}
	return false
}

func validateClaimsForSigning(claims TokenClaims) error {
	if claims.Subject == "" {
		return errors.New("token subject is required")
	}
	if len(claims.Scopes) == 0 {
		return errors.New("token scope is required")
	}
	if claims.IssuedAt <= 0 {
		return errors.New("token issued-at time is required")
	}
	if claims.ExpiresAt <= claims.IssuedAt {
		return errors.New("token expiration must be after issued-at time")
	}
	return nil
}

func encodeTokenJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeTokenJSON(part string, value any) error {
	data, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func signToken(secret string, signingInput string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	return mac.Sum(nil)
}
