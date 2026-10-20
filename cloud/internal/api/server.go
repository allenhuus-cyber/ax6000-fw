// Package api 云端对外接口：设备长连接、App 长连接、RPC 转发与推送注册。
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ax6000/cloud/internal/apns"
	"github.com/ax6000/cloud/internal/config"
	"github.com/ax6000/cloud/internal/hub"
	"github.com/ax6000/cloud/internal/model"
	"github.com/ax6000/cloud/internal/store"
	"github.com/ax6000/cloud/internal/ws"
)

// Server 云端 HTTP/WebSocket 服务。
type Server struct {
	cfg   *config.Config
	st    *store.Store
	hub   *hub.Hub
	apns  *apns.Client
	up    ws.Upgrader
	mux   *http.ServeMux
}

// New 创建服务。apns 可为 nil（推送关闭）。
func New(cfg *config.Config, st *store.Store, h *hub.Hub, a *apns.Client) *Server {
	s := &Server{
		cfg: cfg,
		st:  st,
		hub: h,
		apns: a,
		up: ws.Upgrader{
			ReadBuffer:  2048,
			WriteBuffer: 8192,
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/v1/devices", s.admin(s.handleDevices))
	mux.HandleFunc("POST /api/v1/rpc", s.handleRPC)
	mux.HandleFunc("POST /api/v1/notify/register", s.handleNotifyRegister)
	mux.HandleFunc("POST /api/v1/notify/unregister", s.handleNotifyUnregister)
	mux.HandleFunc("GET /device", s.handleDeviceWS)
	mux.HandleFunc("GET /app", s.handleAppWS)
	mux.HandleFunc("/", s.handleNotFound)
	s.mux = mux
	return s
}

// Handler 返回 http.Handler。
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	fail(w, http.StatusNotFound, "not found")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	devices := s.st.ListDevices()
	online := 0
	for _, d := range devices {
		if s.hub.DeviceOnline(d.ID) {
			online++
		}
	}
	ok(w, map[string]interface{}{
		"status":       "ok",
		"devices":      len(devices),
		"devices_online": online,
		"apns":         s.apns != nil,
		"ts":           time.Now().Unix(),
	})
}

// admin 简易管理鉴权（可选：未配置 AdminToken 时关闭管理接口）。
func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AdminToken == "" {
			fail(w, http.StatusForbidden, "admin api disabled")
			return
		}
		if r.Header.Get("X-Admin-Token") != s.cfg.AdminToken {
			fail(w, http.StatusUnauthorized, "bad admin token")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	list := s.st.ListDevices()
	out := make([]map[string]interface{}, 0, len(list))
	for _, d := range list {
		out = append(out, map[string]interface{}{
			"id":        d.ID,
			"name":      d.Name,
			"online":    s.hub.DeviceOnline(d.ID),
			"last_seen": d.LastSeen.Unix(),
			"apps":      s.hub.AppCount(d.ID),
		})
	}
	ok(w, map[string]interface{}{"devices": out})
}

// ---- RPC 转发 ----

// POST /api/v1/rpc —— 把 App 的请求转发到指定设备，并等待其响应。
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	var req model.RPCRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.DeviceID == "" || req.Path == "" {
		fail(w, http.StatusBadRequest, "device_id and path required")
		return
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	if !s.hub.DeviceOnline(req.DeviceID) {
		fail(w, http.StatusBadGateway, "路由器离线")
		return
	}
	id := s.hub.NewID()
	ch := s.hub.Register(id)
	defer s.hub.Unregister(id)

	env := model.Envelope{
		ID:       id,
		Type:     model.EnvRPC,
		Method:   "rpc",
		DeviceID: req.DeviceID,
		Token:    bearer(r.Header.Get("Authorization")), // 透传设备签发的 JWT，由设备侧校验
		Params: map[string]interface{}{
			"method": strings.ToUpper(req.Method),
			"path":   req.Path,
			"body":   req.Body,
		},
	}
	if err := s.hub.SendDevice(req.DeviceID, env); err != nil {
		fail(w, http.StatusBadGateway, "路由器离线")
		return
	}
	select {
	case resp := <-ch:
		if resp == nil {
			fail(w, http.StatusGatewayTimeout, "no response")
			return
		}
		if resp.Type == model.EnvError || resp.Error != "" {
			fail(w, http.StatusBadGateway, resp.Error)
			return
		}
		ok(w, resp.Result)
	case <-time.After(s.cfg.Timeout()):
		fail(w, http.StatusGatewayTimeout, "路由器响应超时")
	}
}

// ---- 推送注册 ----

// POST /api/v1/notify/register —— 先让设备校验 App 身份，再保存 APNs token。
func (s *Server) handleNotifyRegister(w http.ResponseWriter, r *http.Request) {
	var req model.NotifyRegister
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.DeviceID == "" || req.APNsToken == "" {
		fail(w, http.StatusBadRequest, "device_id and apns_token required")
		return
	}
	if s.hub.DeviceOnline(req.DeviceID) {
		// 交由设备确认该 App 确实持有合法凭证
		code, body := s.rpc(req.DeviceID, "POST", "/api/v1/notify/register", mustJSON(req), r.Header.Get("Authorization"))
		if code >= 400 {
			fail(w, code, string(body))
			return
		}
	}
	s.st.AddToken(req.DeviceID, store.PushToken{
		Token:   req.APNsToken,
		Sandbox: req.Sandbox,
		AppID:   req.AppID,
	})
	ok(w, map[string]interface{}{"result": "ok"})
}

// POST /api/v1/notify/unregister
func (s *Server) handleNotifyUnregister(w http.ResponseWriter, r *http.Request) {
	var req model.NotifyRegister
	if !decodeJSON(w, r, &req) {
		return
	}
	s.st.RemoveToken(req.DeviceID, req.APNsToken)
	ok(w, map[string]interface{}{"result": "ok"})
}

// rpc 同步转发一次请求。
func (s *Server) rpc(deviceID, method, path string, body []byte, auth string) (int, []byte) {
	if !s.hub.DeviceOnline(deviceID) {
		return http.StatusBadGateway, []byte(`{"error":"路由器离线"}`)
	}
	id := s.hub.NewID()
	ch := s.hub.Register(id)
	defer s.hub.Unregister(id)
	env := model.Envelope{
		ID:       id,
		Type:     model.EnvRPC,
		Method:   "rpc",
		DeviceID: deviceID,
		Token:    bearer(auth),
		Params: map[string]interface{}{
			"method": method,
			"path":   path,
			"body":   json.RawMessage(body),
		},
	}
	if err := s.hub.SendDevice(deviceID, env); err != nil {
		return http.StatusBadGateway, []byte(`{"error":"路由器离线"}`)
	}
	select {
	case resp := <-ch:
		if resp == nil {
			return http.StatusGatewayTimeout, []byte(`{"error":"timeout"}`)
		}
		if resp.Error != "" {
			b, _ := json.Marshal(map[string]string{"error": resp.Error})
			return http.StatusBadGateway, b
		}
		b, _ := json.Marshal(resp.Result)
		return http.StatusOK, b
	case <-time.After(s.cfg.Timeout()):
		return http.StatusGatewayTimeout, []byte(`{"error":"timeout"}`)
	}
}

// ---- WebSocket ----

// GET /device —— 路由器连接入口。
func (s *Server) handleDeviceWS(w http.ResponseWriter, r *http.Request) {
	c, err := s.up.Upgrade(w, r)
	if err != nil {
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return
	}
	// 首帧必须是 hello
	op, payload, err := c.ReadMessage()
	if err != nil || op != ws.OpText {
		_ = c.Close()
		return
	}
	var env model.Envelope
	if err := json.Unmarshal(payload, &env); err != nil || env.Type != model.EnvHello || env.DeviceID == "" {
		_ = c.WriteText(`{"type":"error","error":"hello required"}`)
		_ = c.Close()
		return
	}
	if !s.deviceAllowed(env.DeviceID, env.Token) {
		_ = c.WriteText(`{"type":"error","error":"unauthorized device"}`)
		_ = c.Close()
		return
	}

	s.hub.SetDevice(env.DeviceID, c)
	s.st.PutDevice(&store.Device{
		ID:       env.DeviceID,
		Token:    env.Token,
		Online:   true,
		LastSeen: time.Now(),
		RemoteIP: c.RemoteAddr().String(),
	})
	_ = c.WriteText(`{"type":"hello","result":"ok"}`)

	defer func() {
		s.hub.UnsetDevice(env.DeviceID, c)
		if d, ok := s.st.GetDevice(env.DeviceID); ok {
			d.Online = false
			s.st.PutDevice(d)
		}
		_ = c.Close()
	}()

	for {
		op, payload, err := c.ReadMessage()
		if err != nil {
			return
		}
		if op != ws.OpText {
			continue
		}
		var msg model.Envelope
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case model.EnvResult, model.EnvError:
			s.hub.Deliver(msg.ID, &msg)
		case model.EnvEvent:
			s.onDeviceEvent(env.DeviceID, msg)
		case model.EnvPing:
			_ = c.WriteText(`{"type":"pong"}`)
		}
	}
}

// GET /app?device_id=xxx —— App 事件流。
func (s *Server) handleAppWS(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	if deviceID == "" {
		http.Error(w, "device_id required", http.StatusBadRequest)
		return
	}
	c, err := s.up.Upgrade(w, r)
	if err != nil {
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return
	}
	s.hub.AddApp(deviceID, c)
	defer func() {
		s.hub.RemoveApp(deviceID, c)
		_ = c.Close()
	}()
	_ = c.WriteText(`{"type":"hello","result":"ok"}`)
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

// onDeviceEvent 把设备事件广播给 App，并触发 APNs 推送。
func (s *Server) onDeviceEvent(deviceID string, env model.Envelope) {
	// 1) 广播给在线 App
	payload, _ := json.Marshal(env.Params)
	var push model.PushPayload
	_ = json.Unmarshal(payload, &push)
	if push.DeviceID == "" {
		push.DeviceID = deviceID
	}
	if push.TS == 0 {
		push.TS = time.Now().Unix()
	}
	event := model.Event{
		Type: "event",
		Name: env.Method,
		TS:   push.TS,
		Data: env.Params,
	}
	b, _ := json.Marshal(event)
	s.hub.BroadcastApp(deviceID, string(b))

	// 2) APNs 推送
	if s.apns == nil {
		return
	}
	tokens := s.st.Tokens(deviceID)
	for _, t := range tokens {
		err := s.apns.Push(t.Token, apns.Payload{
			Title: push.Title,
			Body:  push.Body,
			Data: map[string]string{
				"device_id": deviceID,
				"event":     push.Event,
			},
		})
		if err != nil {
			// 令牌失效（如用户卸载 App）时清理
			if strings.Contains(err.Error(), "410") || strings.Contains(err.Error(), "400") {
				s.st.RemoveToken(deviceID, t.Token)
			}
		}
	}
}

// deviceAllowed 校验设备注册令牌：
// 预置了 device_id 的必须匹配，否则按「首次接入自动注册」处理。
func (s *Server) deviceAllowed(deviceID, token string) bool {
	if want, ok := s.cfg.DeviceTokens[deviceID]; ok && want != "" {
		return want == token
	}
	if !s.cfg.AllowAutoRegister {
		return false
	}
	if token == "" {
		return false
	}
	return true
}

// ---- 工具 ----

func ok(w http.ResponseWriter, v interface{}) { writeJSON(w, http.StatusOK, v) }

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]interface{}{"error": msg, "code": code})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if r.Body == nil {
		fail(w, http.StatusBadRequest, "empty body")
		return false
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "bad json")
		return false
	}
	return true
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func bearer(h string) string {
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}
