package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/ax6000/ragent/internal/logx"
	"github.com/ax6000/ragent/internal/model"
	"github.com/ax6000/ragent/internal/ws"
)

// Hub 维护当前连接的 WebSocket 客户端，并向它们广播事件。
// 同时把事件转发给云端（用于 APNs 推送）。
type Hub struct {
	upgrader ws.Upgrader

	mu    sync.RWMutex
	conns map[*ws.Conn]struct{}

	// Forward 可选：把事件送给云端中继。
	Forward func(ev model.Event)
}

// NewHub 创建事件总线。
func NewHub() *Hub {
	return &Hub{
		conns: map[*ws.Conn]struct{}{},
		upgrader: ws.Upgrader{
			ReadBuffer:  2048,
			WriteBuffer: 8192,
		},
	}
}

// Broadcast 向所有在线客户端推送事件。
func (h *Hub) Broadcast(ev model.Event) {
	if ev.TS == 0 {
		ev.TS = nowUnix()
	}
	if ev.Type == "" {
		ev.Type = "event"
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.mu.RLock()
	conns := make([]*ws.Conn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	for _, c := range conns {
		if err := c.WriteText(string(b)); err != nil {
			h.Remove(c)
			_ = c.Close()
		}
	}
	if h.Forward != nil {
		h.Forward(ev)
	}
}

// Add 注册连接。
func (h *Hub) Add(c *ws.Conn) {
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
}

// Remove 注销连接。
func (h *Hub) Remove(c *ws.Conn) {
	h.mu.Lock()
	delete(h.conns, c)
	h.mu.Unlock()
}

// Count 当前连接数。
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}

// ServeWS 处理 WebSocket 事件流：鉴权已在上一层完成，这里只负责收发。
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	c, err := h.upgrader.Upgrade(w, r)
	if err != nil {
		logx.Warnf("ws", "upgrade failed: %v", err)
		http.Error(w, "websocket upgrade failed", http.StatusBadRequest)
		return
	}
	h.Add(c)
	logx.Infof("ws", "client connected: %s (total %d)", c.RemoteAddr(), h.Count())
	defer func() {
		h.Remove(c)
		_ = c.Close()
		logx.Infof("ws", "client disconnected: %s", c.RemoteAddr())
	}()

	// 客户端可发 {"type":"ping"}，这里仅维持连接并回 pong
	for {
		op, payload, err := c.ReadMessage()
		if err != nil {
			return
		}
		if op != ws.OpText {
			continue
		}
		var msg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		if msg.Type == "ping" {
			_ = c.WriteText(`{"type":"pong"}`)
		}
	}
}
