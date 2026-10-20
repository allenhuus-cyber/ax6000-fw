// Package model 定义云端与设备、App 之间的信封协议（与固件侧 model.Envelope 保持一致）。
package model

import "encoding/json"

// Envelope 设备 <-> 云端 WebSocket 上的统一消息格式。
type Envelope struct {
	ID       string      `json:"id,omitempty"`
	Type     string      `json:"type"` // hello/rpc/event/result/error/ping/pong
	Method   string      `json:"method,omitempty"`
	Params   interface{} `json:"params,omitempty"`
	Result   interface{} `json:"result,omitempty"`
	Error    string      `json:"error,omitempty"`
	DeviceID string      `json:"device_id,omitempty"`
	Token    string      `json:"token,omitempty"`
	TS       int64       `json:"ts,omitempty"`
}

// 信封类型。
const (
	EnvHello  = "hello"
	EnvRPC    = "rpc"
	EnvEvent  = "event"
	EnvResult = "result"
	EnvError  = "error"
	EnvPing   = "ping"
	EnvPong   = "pong"
)

// PushPayload 设备上报事件时携带的推送素材。
type PushPayload struct {
	DeviceID string            `json:"device_id"`
	Event    string            `json:"event"`
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Data     map[string]string `json:"data,omitempty"`
	TS       int64             `json:"ts"`
}

// NotifyRegister App 注册 APNs 推送令牌。
type NotifyRegister struct {
	APNsToken      string `json:"apns_token"`
	DeviceID       string `json:"device_id"`
	AppID          string `json:"app_id"`
	Sandbox        bool   `json:"sandbox,omitempty"`
	NotifyNewHost  bool   `json:"notify_new_host,omitempty"`
	NotifyOffline  bool   `json:"notify_offline,omitempty"`
}

// Event 广播给 App 的事件结构。
type Event struct {
	Type string      `json:"type"`
	Name string      `json:"name"`
	TS   int64       `json:"ts"`
	Data interface{} `json:"data"`
}

// RPCRequest App 通过云端转发给设备的请求。
type RPCRequest struct {
	DeviceID string          `json:"device_id"`
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Body     json.RawMessage `json:"body,omitempty"`
}
