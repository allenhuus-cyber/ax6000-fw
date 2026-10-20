// Package store 云端持久化：设备注册表与推送令牌表（JSON 文件）。
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Device 已注册的路由器。
type Device struct {
	ID       string    `json:"id"`
	Token    string    `json:"token"`
	Name     string    `json:"name"`
	LastSeen time.Time `json:"last_seen"`
	Online   bool      `json:"online"`
	RemoteIP string    `json:"remote_ip,omitempty"`
}

// PushToken App 上报的 APNs device token。
type PushToken struct {
	Token     string    `json:"token"`
	Sandbox   bool      `json:"sandbox"`
	AppID     string    `json:"app_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ErrNotFound 记录不存在。
var ErrNotFound = errors.New("not found")

// Store 云端存储。
type Store struct {
	dir string

	mu      sync.RWMutex
	devices map[string]*Device
	tokens  map[string][]PushToken
}

// Open 打开存储目录并加载数据。
func Open(dir string) (*Store, error) {
	s := &Store{
		dir:     dir,
		devices: map[string]*Device{},
		tokens:  map[string][]PushToken{},
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	_ = s.load()
	return s, nil
}

func (s *Store) load() error {
	if b, err := os.ReadFile(filepath.Join(s.dir, "devices.json")); err == nil {
		_ = json.Unmarshal(b, &s.devices)
	}
	if b, err := os.ReadFile(filepath.Join(s.dir, "push_tokens.json")); err == nil {
		_ = json.Unmarshal(b, &s.tokens)
	}
	if s.devices == nil {
		s.devices = map[string]*Device{}
	}
	if s.tokens == nil {
		s.tokens = map[string][]PushToken{}
	}
	return nil
}

// Save 落盘。
func (s *Store) Save() error {
	s.mu.RLock()
	d, _ := json.MarshalIndent(s.devices, "", "  ")
	t, _ := json.MarshalIndent(s.tokens, "", "  ")
	s.mu.RUnlock()
	if err := writeAtomic(filepath.Join(s.dir, "devices.json"), d); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.dir, "push_tokens.json"), t)
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---- 设备 ----

// GetDevice 查询设备。
func (s *Store) GetDevice(id string) (*Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[id]
	if !ok {
		return nil, false
	}
	cp := *d
	return &cp, true
}

// PutDevice 写入设备记录。
func (s *Store) PutDevice(d *Device) {
	s.mu.Lock()
	s.devices[d.ID] = d
	s.mu.Unlock()
	_ = s.Save()
}

// ListDevices 列出全部设备。
func (s *Store) ListDevices() []*Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Device, 0, len(s.devices))
	for _, d := range s.devices {
		cp := *d
		out = append(out, &cp)
	}
	return out
}

// ---- 推送令牌 ----

// AddToken 绑定 APNs token 到设备（去重）。
func (s *Store) AddToken(deviceID string, t PushToken) {
	s.mu.Lock()
	list := s.tokens[deviceID]
	for i, exist := range list {
		if exist.Token == t.Token {
			list[i] = t
			s.tokens[deviceID] = list
			s.mu.Unlock()
			_ = s.Save()
			return
		}
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	s.tokens[deviceID] = append(list, t)
	s.mu.Unlock()
	_ = s.Save()
}

// RemoveToken 解绑。
func (s *Store) RemoveToken(deviceID, token string) {
	s.mu.Lock()
	list := s.tokens[deviceID]
	next := list[:0]
	for _, t := range list {
		if t.Token != token {
			next = append(next, t)
		}
	}
	if len(next) == 0 {
		delete(s.tokens, deviceID)
	} else {
		s.tokens[deviceID] = next
	}
	s.mu.Unlock()
	_ = s.Save()
}

// Tokens 返回设备的全部推送令牌。
func (s *Store) Tokens(deviceID string) []PushToken {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PushToken, len(s.tokens[deviceID]))
	copy(out, s.tokens[deviceID])
	return out
}
