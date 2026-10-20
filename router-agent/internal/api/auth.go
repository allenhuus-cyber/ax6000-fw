// Package api 提供本地 HTTP + WebSocket 控制接口（供 iOS App 直连与云端中继复用）。
package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims JWT 负载。
type Claims map[string]interface{}

var (
	// ErrInvalidToken 令牌无效或已过期。
	ErrInvalidToken = errors.New("invalid token")
)

// SignJWT 签发 HS256 JWT。
func SignJWT(secret string, ttl time.Duration, claims Claims) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	c := Claims{}
	for k, v := range claims {
		c[k] = v
	}
	c["exp"] = time.Now().Add(ttl).Unix()
	c["iat"] = time.Now().Unix()

	h, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	p, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	signing := b64(h) + "." + b64(p)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	sig := mac.Sum(nil)
	return signing + "." + b64(sig), nil
}

// VerifyJWT 校验令牌并返回负载。
func VerifyJWT(token, secret string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(mac.Sum(nil), mustDecode(parts[2])) {
		return nil, ErrInvalidToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, ErrInvalidToken
	}
	if exp, ok := c["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			return nil, ErrInvalidToken
		}
	}
	return c, nil
}

func mustDecode(s string) []byte {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// TokenFromRequest 从 Authorization 头或 query 参数解析令牌，缺失返回空串。
func TokenFromRequest(headers map[string][]string, query map[string][]string) string {
	if v, ok := headers["Authorization"]; ok && len(v) > 0 {
		return strings.TrimSpace(strings.TrimPrefix(v[0], "Bearer "))
	}
	if v, ok := query["token"]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}
