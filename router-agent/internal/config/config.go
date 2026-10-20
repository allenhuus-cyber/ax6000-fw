// Package config 负责读取配置与维护设备身份（device_id / secret）。
//
// 优先级：环境变量 > /etc/config/ragent(UCI) > 内置默认值。
package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"strconv"
	"time"

	"github.com/ax6000/ragent/internal/router"
	"github.com/ax6000/ragent/internal/store"
)

// Config 运行期配置。
type Config struct {
	Listen       string        // HTTP 监听地址，默认 :8080
	DataDir      string        // 数据目录，默认 /etc/ragent
	DNSPort      int           // 内置 DNS 过滤代理端口，默认 5353
	ScanInterval time.Duration // 终端扫描周期，默认 10s
	DeviceID     string
	Secret       string // App 配对密钥（扫码获得）
	Name         string // 路由器展示名

	CloudEnabled bool   // 是否连接云端中继
	CloudURL     string // 云端 WebSocket 地址，如 wss://cloud.example.com/device
	CloudToken   string // 设备注册令牌（首次配对时由云端签发）

	LogLevel string
}

// Default 返回默认配置。
func Default() *Config {
	return &Config{
		Listen:       ":8080",
		DataDir:      "/etc/ragent",
		DNSPort:      5353,
		ScanInterval: 10 * time.Second,
		Name:         "Redmi AX6000",
		LogLevel:     "info",
	}
}

// Load 读取配置并补齐设备身份。
func Load() (*Config, *store.File, error) {
	c := Default()

	// UCI: /etc/config/ragent
	if v := router.UciGet("ragent.main.listen"); v != "" {
		c.Listen = v
	}
	if v := router.UciGet("ragent.main.data_dir"); v != "" {
		c.DataDir = v
	}
	if v := router.UciGet("ragent.main.dns_port"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.DNSPort = n
		}
	}
	if v := router.UciGet("ragent.main.scan_interval"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.ScanInterval = time.Duration(n) * time.Second
		}
	}
	if v := router.UciGet("ragent.main.name"); v != "" {
		c.Name = v
	}
	if v := router.UciGet("ragent.main.log_level"); v != "" {
		c.LogLevel = v
	}
	if v := router.UciGet("ragent.cloud.enabled"); v != "" {
		c.CloudEnabled = v == "1" || v == "true"
	}
	if v := router.UciGet("ragent.cloud.url"); v != "" {
		c.CloudURL = v
	}
	if v := router.UciGet("ragent.cloud.token"); v != "" {
		c.CloudToken = v
	}

	// 环境变量覆盖（容器/调试场景）
	if v := os.Getenv("RAGENT_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv("RAGENT_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("RAGENT_CLOUD_URL"); v != "" {
		c.CloudURL = v
		c.CloudEnabled = true
	}
	if v := os.Getenv("RAGENT_CLOUD_TOKEN"); v != "" {
		c.CloudToken = v
	}
	if v := os.Getenv("RAGENT_NAME"); v != "" {
		c.Name = v
	}

	st, err := store.Open(c.DataDir + "/state.json")
	if err != nil {
		return nil, nil, err
	}
	if err := c.ensureIdentity(st); err != nil {
		return nil, nil, err
	}
	return c, st, nil
}

// ensureIdentity 保证 device_id / secret 存在且稳定。
func (c *Config) ensureIdentity(st *store.File) error {
	var ident struct {
		DeviceID string `json:"device_id"`
		Secret   string `json:"secret"`
	}
	if err := st.Get("identity", &ident); err == nil && ident.DeviceID != "" {
		c.DeviceID = ident.DeviceID
		c.Secret = ident.Secret
		return nil
	}
	if c.DeviceID == "" {
		c.DeviceID = deriveDeviceID()
	}
	if c.Secret == "" {
		s, err := randomSecret()
		if err != nil {
			return err
		}
		c.Secret = s
	}
	return st.Set("identity", map[string]string{
		"device_id": c.DeviceID,
		"secret":    c.Secret,
	})
}

// deriveDeviceID 基于 LAN MAC 生成稳定的设备 ID（重装系统后保持一致）。
func deriveDeviceID() string {
	mac := ""
	for _, dev := range []string{"br-lan", "eth0", "wlan0"} {
		if v, err := router.ReadFileFirstLine("/sys/class/net/" + dev + "/address"); err == nil && v != "" {
			mac = v
			break
		}
	}
	if mac == "" {
		mac = router.UciGet("network.lan.macaddr")
	}
	if mac == "" {
		var err error
		mac, err = randomSecret()
		if err != nil {
			mac = strconv.FormatInt(time.Now().UnixNano(), 10)
		}
	}
	sum := sha256.Sum256([]byte("ragent:" + mac))
	return "ax6-" + hex.EncodeToString(sum[:8])
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
