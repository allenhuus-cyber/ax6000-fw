package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ax6000/ragent/internal/config"
	"github.com/ax6000/ragent/internal/filter"
	"github.com/ax6000/ragent/internal/hosts"
	"github.com/ax6000/ragent/internal/model"
	"github.com/ax6000/ragent/internal/router"
	"github.com/ax6000/ragent/internal/store"
)

// Server 是所有控制接口的入口：HTTP（App 直连）与 RPC（云端转发）共用同一套 handler。
type Server struct {
	cfg    *config.Config
	store  *store.File
	hosts  *hosts.Manager
	filter *filter.Manager
	hub    *Hub

	// Notify 由云端链路实现：App 在局域网内注册 APNs token 后交给云端。
	Notify func(req model.NotifyRegister) error

	mux *http.ServeMux
}

// New 创建 API Server。
func New(cfg *config.Config, st *store.File, hm *hosts.Manager, fm *filter.Manager, hub *Hub) *Server {
	s := &Server{cfg: cfg, store: st, hosts: hm, filter: fm, hub: hub}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/pair", s.handlePair)
	mux.HandleFunc("GET /api/v1/status", s.auth(s.handleStatus))
	mux.HandleFunc("GET /api/v1/network/wan", s.auth(s.handleWANGet))
	mux.HandleFunc("PUT /api/v1/network/wan", s.auth(s.handleWANPut))
	mux.HandleFunc("GET /api/v1/network/lan", s.auth(s.handleLANGet))
	mux.HandleFunc("PUT /api/v1/network/lan", s.auth(s.handleLANPut))
	mux.HandleFunc("GET /api/v1/network/scan", s.auth(s.handleScan))
	mux.HandleFunc("GET /api/v1/hosts", s.auth(s.handleHostsList))
	mux.HandleFunc("GET /api/v1/hosts/{mac}", s.auth(s.handleHostGet))
	mux.HandleFunc("POST /api/v1/hosts/{mac}", s.auth(s.handleHostUpdate))
	mux.HandleFunc("POST /api/v1/hosts/{mac}/apps", s.auth(s.handleHostApps))
	mux.HandleFunc("GET /api/v1/apps", s.auth(s.handleApps))
	mux.HandleFunc("POST /api/v1/notify/register", s.auth(s.handleNotifyRegister))
	mux.HandleFunc("GET /api/v1/ws", s.authWS(s.handleWS))
	mux.HandleFunc("/", s.handleNotFound)
	s.mux = mux
	return s
}

// Handler 返回 http.Handler（供 ListenAndServe 使用）。
func (s *Server) Handler() http.Handler { return s.mux }

// auth 校验 JWT 的中间件。
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := TokenFromRequest(r.Header, r.URL.Query())
		if token == "" {
			fail(w, http.StatusUnauthorized, "missing token")
			return
		}
		if _, err := VerifyJWT(token, s.cfg.Secret); err != nil {
			fail(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		next(w, r)
	}
}

// authWS WebSocket 用 query 参数携带 token。
func (s *Server) authWS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			fail(w, http.StatusUnauthorized, "missing token")
			return
		}
		if _, err := VerifyJWT(token, s.cfg.Secret); err != nil {
			fail(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		next(w, r)
	}
}

// ---- 内部 RPC（云端转发到本地）----

// recorder 捕获 handler 的输出，用于把 HTTP handler 当 RPC 调用。
type recorder struct {
	code   int
	header http.Header
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header {
	if r.header == nil {
		r.header = http.Header{}
	}
	return r.header
}

func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }

func (r *recorder) WriteHeader(code int) { r.code = code }

// Invoke 以内部令牌调用本地 handler（云端未透传 App 令牌时使用）。
func (s *Server) Invoke(method, path string, body []byte) (int, []byte) {
	return s.InvokeWithToken(method, path, body, mustInternalToken(s.cfg.Secret))
}

// InvokeWithToken 以指定令牌调用本地 handler，保证端到端鉴权不被中继绕过。
func (s *Server) InvokeWithToken(method, path string, body []byte, token string) (int, []byte) {
	req, err := http.NewRequest(method, "http://ragent.local"+path, bytes.NewReader(body))
	if err != nil {
		return http.StatusBadRequest, []byte(`{"error":"bad request"}`)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := &recorder{code: http.StatusOK}
	s.mux.ServeHTTP(rec, req)
	return rec.code, rec.body.Bytes()
}

func mustInternalToken(secret string) string {
	t, err := SignJWT(secret, 5*time.Minute, Claims{"sub": "cloud-relay"})
	if err != nil {
		return ""
	}
	return t
}

// ---- handlers ----

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	fail(w, http.StatusNotFound, "not found: "+r.URL.Path)
}

// POST /api/v1/auth/login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req model.LoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.DeviceID != "" && req.DeviceID != s.cfg.DeviceID {
		fail(w, http.StatusForbidden, "device_id mismatch")
		return
	}
	if req.Secret != s.cfg.Secret {
		fail(w, http.StatusUnauthorized, "bad secret")
		return
	}
	token, err := SignJWT(s.cfg.Secret, 12*time.Hour, Claims{
		"sub":       req.Client,
		"device_id": s.cfg.DeviceID,
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, "sign token failed")
		return
	}
	ok(w, model.LoginResponse{
		Token:     token,
		ExpiresIn: int64((12 * time.Hour).Seconds()),
		DeviceID:  s.cfg.DeviceID,
		Name:      s.cfg.Name,
	})
}

// POST /api/v1/pair —— 用路由器管理账号换取配对信息（device_id + secret）。
// 通过 ubus rpcd session 校验，避免明文处理 root 密码。
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Password == "" {
		fail(w, http.StatusBadRequest, "username and password required")
		return
	}
	payload, err := json.Marshal(map[string]string{
		"username": req.Username,
		"password": req.Password,
		"timeout":  "600",
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	var resp struct {
		UBUSRPCSession string `json:"ubus_rpc_session"`
		Expires        int    `json:"expires"`
		Data           struct {
			Username string `json:"username"`
		} `json:"data"`
	}
	if err := router.RunJSON(&resp, "ubus", "-t", "5", "call", "session", "login", string(payload)); err != nil {
		fail(w, http.StatusUnauthorized, "路由器账号或密码错误")
		return
	}
	if resp.UBUSRPCSession == "" {
		fail(w, http.StatusUnauthorized, "登录失败")
		return
	}
	ok(w, map[string]interface{}{
		"device_id": s.cfg.DeviceID,
		"secret":    s.cfg.Secret,
		"name":      s.cfg.Name,
		"lan_addr":  router.LANAddress(),
		"api_port":  s.cfg.Listen,
		"cloud":     s.cfg.CloudEnabled,
	})
}

// GET /api/v1/status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	mem := router.Mem()
	wan, err := router.GetWANStatus()
	if err != nil {
		wan = &model.WANStatus{}
	}
	st := model.RouterStatus{
		DeviceID:   s.cfg.DeviceID,
		Name:       s.cfg.Name,
		Firmware:   router.FirmwareVersion(),
		Uptime:     router.UptimeSeconds(),
		CPUUsage:   router.CPUUsage(),
		MemTotal:   mem.Total,
		MemFree:    mem.Free,
		LANAddr:    router.LANAddress(),
		WANStatus:  *wan,
		HostTotal:  len(s.hosts.List()),
		HostOnline: s.hosts.Online(),
	}
	ok(w, st)
}

// GET /api/v1/network/wan
func (s *Server) handleWANGet(w http.ResponseWriter, r *http.Request) {
	cfg, err := router.GetWANConfig()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, cfg)
}

// PUT /api/v1/network/wan
func (s *Server) handleWANPut(w http.ResponseWriter, r *http.Request) {
	var cfg model.WANConfig
	if !decodeJSON(w, r, &cfg) {
		return
	}
	// 未提供的密码保持原值（App 通常只改其他字段）
	if cfg.Password == "" && cfg.Mode == "pppoe" {
		if old, err := router.GetWANConfig(); err == nil {
			cfg.Password = old.Password
		}
	}
	if err := router.ApplyWAN(&cfg); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	go func() {
		// 等待网络收敛后再上报状态变化
		select {
		case <-time.After(4 * time.Second):
		}
		st, err := router.GetWANStatus()
		if err != nil {
			return
		}
		s.hub.Broadcast(model.Event{
			Type: "event",
			Name: model.EventWANChange,
			TS:   nowUnix(),
			Data: st,
		})
	}()
	ok(w, map[string]interface{}{"result": "ok"})
}

// GET /api/v1/network/lan
func (s *Server) handleLANGet(w http.ResponseWriter, r *http.Request) {
	cfg, err := router.GetLANConfig()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, cfg)
}

// PUT /api/v1/network/lan
func (s *Server) handleLANPut(w http.ResponseWriter, r *http.Request) {
	var cfg model.LANConfig
	if !decodeJSON(w, r, &cfg) {
		return
	}
	if err := router.ApplyLAN(&cfg); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, map[string]interface{}{"result": "ok"})
}

// GET /api/v1/network/scan?radio=radio1 —— 扫描周边热点（用于配置无线中继）
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	list, err := router.Scan(r.URL.Query().Get("radio"))
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]interface{}{"results": list})
}

// GET /api/v1/hosts
func (s *Server) handleHostsList(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{"hosts": s.hosts.List(), "ts": nowUnix()})
}

// GET /api/v1/hosts/{mac}
func (s *Server) handleHostGet(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMACParam(r.PathValue("mac"))
	h, found := s.hosts.Get(mac)
	if !found {
		fail(w, http.StatusNotFound, "host not found")
		return
	}
	ok(w, h)
}

// POST /api/v1/hosts/{mac}
func (s *Server) handleHostUpdate(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMACParam(r.PathValue("mac"))
	var req struct {
		Alias     *string `json:"alias"`
		Blocked   *bool   `json:"blocked"`
		RateLimit *int    `json:"rate_limit_kbps"`
		Notify    *bool   `json:"notify"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Alias != nil {
		if err := s.hosts.SetAlias(mac, *req.Alias); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.Blocked != nil {
		if err := s.hosts.SetBlocked(mac, *req.Blocked); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.RateLimit != nil {
		if err := s.hosts.SetRateLimit(mac, *req.RateLimit); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.Notify != nil {
		if err := s.hosts.SetNotify(mac, *req.Notify); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	h, _ := s.hosts.Get(mac)
	ok(w, h)
}

// POST /api/v1/hosts/{mac}/apps —— 设置该终端禁止访问的 App
func (s *Server) handleHostApps(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMACParam(r.PathValue("mac"))
	var req struct {
		Apps []string `json:"apps"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.hosts.SetBlockedApps(mac, req.Apps); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast(model.Event{
		Type: "event",
		Name: model.EventFilterApply,
		TS:   nowUnix(),
		Data: map[string]interface{}{"mac": mac, "apps": req.Apps},
	})
	ok(w, map[string]interface{}{"mac": mac, "apps": req.Apps})
}

// GET /api/v1/apps —— 可管控 App 清单与引擎可用性
func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{
		"apps":    s.filter.Catalog(),
		"engines": s.filter.EngineStatus(),
	})
}

// POST /api/v1/notify/register —— App 注册推送（APNs token 转交云端）
func (s *Server) handleNotifyRegister(w http.ResponseWriter, r *http.Request) {
	var req model.NotifyRegister
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.APNsToken == "" {
		fail(w, http.StatusBadRequest, "apns_token required")
		return
	}
	req.DeviceID = s.cfg.DeviceID
	if s.Notify == nil {
		fail(w, http.StatusServiceUnavailable, "云端中继未启用，无法开启远程提醒")
		return
	}
	if err := s.Notify(req); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	_ = s.store.Set("notify."+req.APNsToken, req)
	ok(w, map[string]interface{}{"result": "ok"})
}

// GET /api/v1/ws —— 事件流
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	s.hub.ServeWS(w, r)
}

// ---- 事件入口 ----

// OnHostEvent 终端事件回调：本地广播 + 生成推送内容交给云端。
func (s *Server) OnHostEvent(ev model.Event) {
	if ev.Type == "" {
		ev.Type = "event"
	}
	if ev.TS == 0 {
		ev.TS = nowUnix()
	}
	s.hub.Broadcast(ev)
}

// PushText 生成终端事件的推送文案。
func PushText(ev model.Event) (title, body string) {
	data, ok := ev.Data.(model.HostEventData)
	if !ok {
		return "路由器提醒", "有一条新的网络事件"
	}
	name := data.Host.Alias
	if name == "" {
		name = data.Host.Name
	}
	if name == "" {
		name = data.Host.MAC
	}
	switch ev.Name {
	case model.EventHostNew:
		return "新设备接入", name + " 首次连接到你的网络"
	case model.EventHostOnline:
		return "设备上线", name + " 已连接到网络"
	case model.EventHostOffline:
		return "设备离线", name + " 已断开连接"
	}
	return "路由器提醒", name
}

func normalizeMACParam(v string) string {
	return strings.ToLower(router.NormalizeMAC(v))
}
