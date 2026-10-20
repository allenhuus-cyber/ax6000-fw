// Package cloudlink 实现设备到云端中继的长连接客户端。
//
// 链路：iOS App <-> 云端 <-> 路由器。
//   - 设备主动外连（无需公网 IP / 端口映射）；
//   - 云端把 App 的 REST 请求封装成 rpc 信封转发给设备，设备本地执行后回传；
//   - 设备把终端上下线等事件上报云端，云端转成 APNs 推送。
package cloudlink

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/ax6000/ragent/internal/config"
	"github.com/ax6000/ragent/internal/logx"
	"github.com/ax6000/ragent/internal/model"
	"github.com/ax6000/ragent/internal/ws"
)

// Invoker 本地 API 调用入口（由 api.Server 实现），避免与 api 包循环依赖。
type Invoker interface {
	// InvokeWithToken 以 App 的真实令牌执行本地 handler，保证中继不能绕过设备鉴权。
	InvokeWithToken(method, path string, body []byte, token string) (int, []byte)
}

// Client 云端长连接客户端。
type Client struct {
	cfg  *config.Config
	api  Invoker
	url  string
	stop chan struct{}
	wg   sync.WaitGroup

	mu    sync.Mutex
	conn  *ws.Conn
	ready bool
}

// New 创建客户端。
func New(cfg *config.Config, api Invoker) *Client {
	return &Client{cfg: cfg, api: api, url: cfg.CloudURL, stop: make(chan struct{})}
}

// Ready 连接是否可用。
func (c *Client) Ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready
}

// Close 停止重连并关闭连接。
func (c *Client) Close() {
	select {
	case <-c.stop:
		return
	default:
		close(c.stop)
	}
	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.mu.Unlock()
	c.wg.Wait()
}

// Run 保持连接，断线自动重连（指数退避，上限 60s）。
func (c *Client) Run() {
	if c.url == "" {
		logx.Warnf("cloud", "未配置云端地址，远程管理不可用")
		return
	}
	backoff := 3 * time.Second
	for {
		select {
		case <-c.stop:
			return
		default:
		}
		if err := c.connect(); err != nil {
			logx.Warnf("cloud", "connect failed: %v (retry in %s)", err, backoff)
			select {
			case <-c.stop:
				return
			case <-time.After(backoff):
			}
			if backoff < 60*time.Second {
				backoff *= 2
				if backoff > 60*time.Second {
					backoff = 60 * time.Second
				}
			}
			continue
		}
		backoff = 3 * time.Second
		c.serve()
	}
}

func (c *Client) connect() error {
	d := ws.Dialer{Timeout: 10 * time.Second}
	conn, _, err := d.Dial(c.url)
	if err != nil {
		return err
	}
	hello := model.Envelope{
		Type:     model.EnvHello,
		DeviceID: c.cfg.DeviceID,
		Token:    c.cfg.CloudToken,
		TS:       time.Now().Unix(),
	}
	b, _ := json.Marshal(hello)
	if err := conn.WriteText(string(b)); err != nil {
		_ = conn.Close()
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.ready = true
	c.mu.Unlock()
	logx.Infof("cloud", "connected to %s", c.url)
	return nil
}

func (c *Client) serve() {
	c.wg.Add(1)
	defer c.wg.Done()

	// 心跳
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-c.stop:
				return
			case <-ticker.C:
				c.mu.Lock()
				conn := c.conn
				c.mu.Unlock()
				if conn == nil {
					return
				}
				if err := conn.Ping([]byte("ragent")); err != nil {
					logx.Warnf("cloud", "ping failed: %v", err)
					return
				}
			}
		}
	}()

	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return
	}

	for {
		select {
		case <-c.stop:
			return
		default:
		}
		op, payload, err := conn.ReadMessage()
		if err != nil {
			logx.Warnf("cloud", "read error: %v", err)
			c.mu.Lock()
			c.ready = false
			if c.conn == conn {
				c.conn = nil
			}
			c.mu.Unlock()
			_ = conn.Close()
			return
		}
		if op != ws.OpText {
			continue
		}
		var env model.Envelope
		if err := json.Unmarshal(payload, &env); err != nil {
			continue
		}
		switch env.Type {
		case model.EnvRPC:
			go c.handleRPC(conn, env)
		case model.EnvPing:
			_ = c.send(conn, model.Envelope{Type: model.EnvPong, TS: time.Now().Unix()})
		}
	}
}

// handleRPC 执行云端转发过来的 API 调用。
func (c *Client) handleRPC(conn *ws.Conn, env model.Envelope) {
	var params struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	}
	if env.Params != nil {
		b, _ := json.Marshal(env.Params)
		_ = json.Unmarshal(b, &params)
	}
	if params.Path == "" {
		_ = c.send(conn, model.Envelope{Type: model.EnvError, ID: env.ID, Error: "missing path"})
		return
	}
	method := params.Method
	if method == "" {
		method = "GET"
	}
	code, body := c.api.InvokeWithToken(method, params.Path, []byte(params.Body), env.Token)
	var result interface{}
	_ = json.Unmarshal(body, &result)
	resp := model.Envelope{
		Type:   model.EnvResult,
		ID:     env.ID,
		Result: result,
	}
	if code >= 400 {
		resp.Type = model.EnvError
		resp.Error = string(body)
	}
	_ = c.send(conn, resp)
}

// SendEvent 上报事件（云端据此做 APNs 推送）。
func (c *Client) SendEvent(ev model.Event) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("cloud: not connected")
	}
	title, body := eventText(ev)
	push := model.PushPayload{
		DeviceID: c.cfg.DeviceID,
		Event:    ev.Name,
		Title:    title,
		Body:     body,
		TS:       ev.TS,
	}
	return c.send(conn, model.Envelope{
		Type:     model.EnvEvent,
		DeviceID: c.cfg.DeviceID,
		Method:   ev.Name,
		Params:   push,
		TS:       ev.TS,
	})
}

// RegisterNotify 把 App 的 APNs token 上报云端。
func (c *Client) RegisterNotify(req model.NotifyRegister) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("云端未连接，请确认路由器已联网且已开启远程管理")
	}
	return c.send(conn, model.Envelope{
		Type:     model.EnvRPC,
		Method:   "notify.register",
		DeviceID: c.cfg.DeviceID,
		Params:   req,
		TS:       time.Now().Unix(),
	})
}

func (c *Client) send(conn *ws.Conn, env model.Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return conn.WriteText(string(b))
}

// eventText 生成推送文案（与 api.PushText 保持一致，独立实现以避免包依赖）。
func eventText(ev model.Event) (string, string) {
	b, err := json.Marshal(ev.Data)
	if err != nil {
		return "路由器提醒", "有一条新的网络事件"
	}
	var d model.HostEventData
	_ = json.Unmarshal(b, &d)
	name := d.Host.Alias
	if name == "" {
		name = d.Host.Name
	}
	if name == "" {
		name = d.Host.MAC
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
