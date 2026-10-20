// Package apns 实现 Apple Push Notification service 的 HTTP/2 客户端。
// 依赖仅标准库：crypto/ecdsa 做 ES256 签名，net/http 自动协商 HTTP/2。
package apns

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Config APNs 配置（对应 Apple Developer 的 .p8 密钥）。
type Config struct {
	TeamID  string // Developer Team ID
	KeyID   string // APNs Key ID
	KeyPath string // AuthKey_XXXX.p8 路径
	Topic   string // App Bundle ID
	Sandbox bool   // true 使用开发环境
}

// Client APNs 客户端。
type Client struct {
	cfg    Config
	key    *ecdsa.PrivateKey
	http   *http.Client
	mu     sync.Mutex
	token  string
	expiry time.Time
}

// New 加载私钥并创建客户端。配置不完整时返回 nil（推送功能关闭）。
func New(cfg Config) (*Client, error) {
	if cfg.TeamID == "" || cfg.KeyID == "" || cfg.KeyPath == "" || cfg.Topic == "" {
		return nil, errors.New("apns 配置不完整")
	}
	b, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("apns: 无法解析 p8 私钥")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("apns: 私钥不是 EC 类型")
	}
	tr := &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &Client{
		cfg: cfg,
		key: key,
		http: &http.Client{
			Transport: tr,
			Timeout:   15 * time.Second,
		},
	}, nil
}

// Payload 推送内容。
type Payload struct {
	Title string
	Body  string
	Data  map[string]string
}

type apsBody struct {
	Alert struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	} `json:"alert"`
	Sound          string `json:"sound"`
	ContentAvail   int    `json:"content-available"`
	MutableContent int    `json:"mutable-content"`
	Category       string `json:"category,omitempty"`
}

// Push 向单个 device token 推送。
func (c *Client) Push(deviceToken string, p Payload) error {
	var aps apsBody
	aps.Alert.Title = p.Title
	aps.Alert.Body = p.Body
	aps.Sound = "default"
	aps.ContentAvail = 1
	aps.MutableContent = 1
	aps.Category = "ROUTER_EVENT"

	body := map[string]interface{}{"aps": aps}
	for k, v := range p.Data {
		body[k] = v
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}

	jwt, err := c.authToken()
	if err != nil {
		return err
	}
	host := "https://api.push.apple.com"
	if c.cfg.Sandbox {
		host = "https://api.sandbox.push.apple.com"
	}
	url := host + "/3/device/" + strings.TrimSpace(deviceToken)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", c.cfg.Topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("apns-expiration", "0")
	req.Header.Set("content-type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	msg, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("apns %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
}

// authToken 生成并缓存 ES256 提供者令牌（有效期 50 分钟）。
func (c *Client) authToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expiry) {
		return c.token, nil
	}
	header := map[string]string{"alg": "ES256", "kid": c.cfg.KeyID}
	claims := map[string]interface{}{
		"iss": c.cfg.TeamID,
		"iat": time.Now().Unix(),
	}
	h, _ := json.Marshal(header)
	p, _ := json.Marshal(claims)
	signing := b64(h) + "." + b64(p)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, c.key, sum[:])
	if err != nil {
		return "", err
	}
	sig := append(pad(r), pad(s)...)
	c.token = signing + "." + b64(sig)
	c.expiry = time.Now().Add(50 * time.Minute)
	return c.token, nil
}

func pad(v *big.Int) []byte {
	b := v.Bytes()
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
