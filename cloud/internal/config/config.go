// Package config 读取云端服务的 JSON 配置文件。
package config

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

// Config 云端配置。
type Config struct {
	Listen   string `json:"listen"`    // HTTP/WS 监听地址
	DataDir  string `json:"data_dir"`  // 持久化目录
	LogLevel string `json:"log_level"` // debug/info/warn/error

	// DeviceTokens 预置设备注册令牌：device_id -> token。
	// 为空时表示「首次连接自动注册」，生产环境建议预置。
	DeviceTokens map[string]string `json:"device_tokens"`

	// AllowAutoRegister 是否允许未预置的设备首次接入自动注册（默认 true）。
	AllowAutoRegister bool `json:"allow_auto_register"`

	// AdminToken 管理接口鉴权，为空则关闭 /api/v1/devices。
	AdminToken string `json:"admin_token"`

	// TLSCert / TLSKey 配置后直接启用 HTTPS（也可由 Nginx 反代）。
	TLSCert string `json:"tls_cert"`
	TLSKey  string `json:"tls_key"`

	// RPCTimeout 转发请求等待设备响应的超时（秒）
	RPCTimeout int `json:"rpc_timeout"`

	APNS struct {
		TeamID  string `json:"team_id"`
		KeyID   string `json:"key_id"`
		KeyPath string `json:"key_path"`
		Topic   string `json:"topic"`
		Sandbox bool   `json:"sandbox"`
	} `json:"apns"`
}

// Default 默认配置。
func Default() *Config {
	c := &Config{
		Listen:            ":8443",
		DataDir:           "./data",
		LogLevel:          "info",
		RPCTimeout:        15,
		DeviceTokens:      map[string]string{},
		AllowAutoRegister: true,
	}
	return c
}

// Load 从 JSON 文件加载配置，缺失时使用默认值。
func Load(path string) (*Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	if c.Listen == "" {
		c.Listen = ":8443"
	}
	if c.DataDir == "" {
		c.DataDir = "./data"
	}
	if c.RPCTimeout <= 0 {
		c.RPCTimeout = 15
	}
	if c.DeviceTokens == nil {
		c.DeviceTokens = map[string]string{}
	}
	return c, nil
}

// Timeout 返回 RPC 超时时间。
func (c *Config) Timeout() time.Duration {
	return time.Duration(c.RPCTimeout) * time.Second
}
