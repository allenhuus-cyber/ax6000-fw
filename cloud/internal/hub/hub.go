// Package hub 管理云端两侧的长连接：设备连接与 App 连接，并提供同步 RPC 转发。
package hub

import (
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/ax6000/cloud/internal/model"
	"github.com/ax6000/cloud/internal/ws"
)

// Hub 连接注册表。
type Hub struct {
	mu       sync.RWMutex
	devices  map[string]*ws.Conn
	apps     map[string]map[*ws.Conn]struct{}
	pending  map[string]chan *model.Envelope
	seq      uint64
}

// New 创建 Hub。
func New() *Hub {
	return &Hub{
		devices: map[string]*ws.Conn{},
		apps:    map[string]map[*ws.Conn]struct{}{},
		pending: map[string]chan *model.Envelope{},
	}
}

// ---- 设备连接 ----

// SetDevice 登记设备连接（同一 device_id 只保留最新一条）。
func (h *Hub) SetDevice(id string, c *ws.Conn) {
	h.mu.Lock()
	if old, ok := h.devices[id]; ok && old != c {
		_ = old.Close()
	}
	h.devices[id] = c
	h.mu.Unlock()
}

// UnsetDevice 移除设备连接（仅当传入连接仍是当前连接时）。
func (h *Hub) UnsetDevice(id string, c *ws.Conn) {
	h.mu.Lock()
	if cur, ok := h.devices[id]; ok && cur == c {
		delete(h.devices, id)
	}
	h.mu.Unlock()
}

// DeviceOnline 设备是否在线。
func (h *Hub) DeviceOnline(id string) bool {
	h.mu.RLock()
	_, ok := h.devices[id]
	h.mu.RUnlock()
	return ok
}

// SendDevice 向设备发送信封。
func (h *Hub) SendDevice(id string, env model.Envelope) error {
	h.mu.RLock()
	c, ok := h.devices[id]
	h.mu.RUnlock()
	if !ok {
		return ErrOffline
	}
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return c.WriteText(string(b))
}

// ---- App 连接 ----

// AddApp 登记 App 连接（一个设备可有多个 App 连接）。
func (h *Hub) AddApp(deviceID string, c *ws.Conn) {
	h.mu.Lock()
	if h.apps[deviceID] == nil {
		h.apps[deviceID] = map[*ws.Conn]struct{}{}
	}
	h.apps[deviceID][c] = struct{}{}
	h.mu.Unlock()
}

// RemoveApp 移除 App 连接。
func (h *Hub) RemoveApp(deviceID string, c *ws.Conn) {
	h.mu.Lock()
	if set, ok := h.apps[deviceID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.apps, deviceID)
		}
	}
	h.mu.Unlock()
}

// BroadcastApp 向关注该设备的所有 App 连接广播事件。
func (h *Hub) BroadcastApp(deviceID string, msg string) {
	h.mu.RLock()
	conns := make([]*ws.Conn, 0, 4)
	for c := range h.apps[deviceID] {
		conns = append(conns, c)
	}
	h.mu.RUnlock()
	for _, c := range conns {
		if err := c.WriteText(msg); err != nil {
			h.RemoveApp(deviceID, c)
			_ = c.Close()
		}
	}
}

// AppCount 某设备的 App 连接数。
func (h *Hub) AppCount(deviceID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.apps[deviceID])
}

// ---- 同步 RPC ----

// NewID 生成单调递增的请求 ID。
func (h *Hub) NewID() string {
	return "r" + itoa(atomic.AddUint64(&h.seq, 1))
}

// Register 登记等待响应的通道。
func (h *Hub) Register(id string) chan *model.Envelope {
	ch := make(chan *model.Envelope, 1)
	h.mu.Lock()
	h.pending[id] = ch
	h.mu.Unlock()
	return ch
}

// Unregister 注销等待。
func (h *Hub) Unregister(id string) {
	h.mu.Lock()
	delete(h.pending, id)
	h.mu.Unlock()
}

// Deliver 把设备的应答投递给等待方。
func (h *Hub) Deliver(id string, env *model.Envelope) {
	h.mu.RLock()
	ch, ok := h.pending[id]
	h.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case ch <- env:
	default:
	}
}

// ErrOffline 设备离线。
var ErrOffline = errors.New("device offline")

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [24]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
