// Package model 定义设备端与云端、App 之间共享的数据结构。
// 三端（固件 / 云端 / iOS）使用同一份 JSON 契约，字段命名保持 snake_case。
package model

// ---- 认证 ----

type LoginRequest struct {
	DeviceID string `json:"device_id"`
	Secret   string `json:"secret"`
	Client   string `json:"client,omitempty"` // "ios" / "web"
}

type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
	DeviceID  string `json:"device_id"`
	Name      string `json:"name"`
}

// ---- 路由器状态 ----

type RouterStatus struct {
	DeviceID  string `json:"device_id"`
	Name      string `json:"name"`
	Firmware  string `json:"firmware"`
	Uptime    int64  `json:"uptime"` // 秒
	CPUUsage  int    `json:"cpu_usage"`
	MemTotal  int64  `json:"mem_total"`
	MemFree   int64  `json:"mem_free"`
	LANAddr   string `json:"lan_addr"`
	WANStatus WANStatus `json:"wan"`
	HostTotal int    `json:"host_total"`
	HostOnline int   `json:"host_online"`
}

type WANStatus struct {
	Mode      string `json:"mode"` // pppoe/dhcp/static/relay/none
	Protocol  string `json:"protocol"`
	Up        bool   `json:"up"`
	IP        string `json:"ip,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
	DNS       []string `json:"dns,omitempty"`
	MAC       string `json:"mac,omitempty"`
	Device    string `json:"device,omitempty"` // eth1 / pppoe-wan
	Uptime    int64  `json:"uptime,omitempty"`
	RxBytes   int64  `json:"rx_bytes,omitempty"`
	TxBytes   int64  `json:"tx_bytes,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ---- 上网方式配置 ----

type WANConfig struct {
	Mode     string   `json:"mode"` // pppoe | dhcp | static | relay
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
	IP       string   `json:"ip,omitempty"`
	Netmask  string   `json:"netmask,omitempty"`
	Gateway  string   `json:"gateway,omitempty"`
	DNS      []string `json:"dns,omitempty"`
	MTU      int      `json:"mtu,omitempty"`
	MACClone string   `json:"mac_clone,omitempty"`
	// 无线中继（mode=relay）参数
	RelaySSID     string `json:"relay_ssid,omitempty"`
	RelayPassword string `json:"relay_password,omitempty"`
	RelayBand     string `json:"relay_band,omitempty"` // "2g" / "5g"
	ServiceName   string `json:"service_name,omitempty"`
}

type LANConfig struct {
	IP      string `json:"ip"`
	Netmask string `json:"netmask"`
	DHCPEnabled bool   `json:"dhcp_enabled"`
	DHCPStart   int    `json:"dhcp_start,omitempty"`
	DHCPLimit   int    `json:"dhcp_limit,omitempty"`
	DHCPLease   string `json:"dhcp_lease,omitempty"`
}

// ---- 终端 ----

type Host struct {
	MAC        string `json:"mac"`
	IP         string `json:"ip,omitempty"`
	Name       string `json:"name,omitempty"`      // 上报主机名
	Alias      string `json:"alias,omitempty"`     // 用户备注
	Vendor     string `json:"vendor,omitempty"`
	Iface      string `json:"iface,omitempty"`     // wlan0 / wlan1 / br-lan(有线)
	Type       string `json:"type"`                // wireless | wired
	Online     bool   `json:"online"`
	FirstSeen  int64  `json:"first_seen"`
	LastSeen   int64  `json:"last_seen"`
	RxBytes    int64  `json:"rx_bytes,omitempty"`
	TxBytes    int64  `json:"tx_bytes,omitempty"`
	Blocked    bool   `json:"blocked"`
	RateLimit  int    `json:"rate_limit_kbps"`     // 0 表示不限速
	BlockedApps []string `json:"blocked_apps"`     // app id 列表
}

// ---- App 过滤 ----

type AppInfo struct {
	ID       string `json:"id"`        // 我们的稳定标识，如 "douyin"
	Name     string `json:"name"`      // 中文名
	Category string `json:"category"`  // video/social/game/shopping/music/...
	Engines  []string `json:"engines"` // 支持的引擎：oaf / dns
}

// ---- 事件 ----

type Event struct {
	Type string      `json:"type"` // "event"
	Name string      `json:"name"`
	TS   int64       `json:"ts"`
	Data interface{} `json:"data"`
}

const (
	EventHostOnline  = "host.online"
	EventHostOffline = "host.offline"
	EventHostNew     = "host.new"      // 首次出现（新设备入网）
	EventWANChange   = "wan.change"
	EventFilterApply = "filter.apply"
	EventStatusTick  = "status.tick"
)

type HostEventData struct {
	Host Host `json:"host"`
}

// ---- 云端中继协议 ----

// Envelope 是设备与云端之间 WebSocket 上的统一信封。
type Envelope struct {
	ID     string      `json:"id,omitempty"`     // 请求 ID，用于应答关联
	Type   string      `json:"type"`             // hello / rpc / event / result / error / ping
	Method string      `json:"method,omitempty"` // rpc 方法
	Params interface{} `json:"params,omitempty"`
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
	DeviceID string    `json:"device_id,omitempty"`
	Token  string      `json:"token,omitempty"`
	TS     int64       `json:"ts,omitempty"`
}

const (
	EnvHello  = "hello"
	EnvRPC    = "rpc"
	EnvEvent  = "event"
	EnvResult = "result"
	EnvError  = "error"
	EnvPing   = "ping"
	EnvPong   = "pong"
)

// PushPayload 由设备上报给云端、用于生成 APNs 推送的内容。
type PushPayload struct {
	DeviceID string `json:"device_id"`
	Event    string `json:"event"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Data     map[string]string `json:"data,omitempty"`
	TS       int64  `json:"ts"`
}

// NotifyRegister 设备把 App 的 APNs device token 转交给云端（App 在局域网内先注册）。
type NotifyRegister struct {
	APNsToken  string `json:"apns_token"`
	DeviceID   string `json:"device_id"`
	AppID      string `json:"app_id"`
	Sandbox    bool   `json:"sandbox,omitempty"`
	NotifyNewHost   bool `json:"notify_new_host,omitempty"`
	NotifyOffline   bool `json:"notify_offline,omitempty"`
}
