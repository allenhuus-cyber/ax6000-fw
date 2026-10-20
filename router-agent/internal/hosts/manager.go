// Package hosts 负责终端（联网设备）的发现、在线状态跟踪、事件通知与策略下发。
package hosts

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ax6000/ragent/internal/logx"
	"github.com/ax6000/ragent/internal/model"
	"github.com/ax6000/ragent/internal/store"
)

// PolicyApplier 把终端策略（拉黑/限速/App 限制）落到具体的过滤引擎上。
// 由 filter 包实现，hosts 不直接依赖 nftables / oaf，便于测试。
type PolicyApplier interface {
	ApplyBlock(mac string, blocked bool) error
	ApplyRateLimit(mac string, kbps int) error
	ApplyAppRules(mac string, apps []string) error
}

// Profile 是终端的持久化配置（用户备注、限速、黑名单等）。
type Profile struct {
	Alias       string   `json:"alias"`
	Blocked     bool     `json:"blocked"`
	RateLimit   int      `json:"rate_limit_kbps"`
	BlockedApps []string `json:"blocked_apps"`
	Notify      bool     `json:"notify"` // 该终端上线时是否推送提醒
	FirstSeen   int64    `json:"first_seen"`
	LastSeen    int64    `json:"last_seen"`
}

// Manager 维护终端表并产出上下线事件。
type Manager struct {
	store    *store.File
	applier  PolicyApplier
	onEvent  func(model.Event)
	bridgeIf string

	// 离线判定阈值
	wirelessOfflineAfter int64 // 秒
	wiredOfflineAfter    int64 // 秒

	mu       sync.Mutex
	known    map[string]*model.Host
	profiles map[string]*Profile
	pending  []model.Event // 加锁期间收集、解锁后派发
	lastFlush time.Time    // 上次落盘时间
}

// New 创建终端管理器。onEvent 会被并发调用（已加锁外调用），实现需保证非阻塞。
func New(st *store.File, applier PolicyApplier, onEvent func(model.Event)) *Manager {
	m := &Manager{
		store:                st,
		applier:              applier,
		onEvent:              onEvent,
		bridgeIf:             "br-lan",
		wirelessOfflineAfter: 90,
		wiredOfflineAfter:    300,
		known:                map[string]*model.Host{},
		profiles:             map[string]*Profile{},
	}
	m.load()
	return m
}

// SetOfflineThreshold 调整离线判定阈值（秒）。
func (m *Manager) SetOfflineThreshold(wireless, wired int64) {
	if wireless > 0 {
		m.wirelessOfflineAfter = wireless
	}
	if wired > 0 {
		m.wiredOfflineAfter = wired
	}
}

const profilePrefix = "profile."

func (m *Manager) load() {
	for _, key := range m.store.Keys() {
		if len(key) <= len(profilePrefix) || key[:len(profilePrefix)] != profilePrefix {
			continue
		}
		var p Profile
		if err := m.store.Get(key, &p); err != nil {
			continue
		}
		mac := key[len(profilePrefix):]
		m.profiles[mac] = &p
		h := &model.Host{
			MAC:         mac,
			Alias:       p.Alias,
			Blocked:     p.Blocked,
			RateLimit:   p.RateLimit,
			BlockedApps: p.BlockedApps,
			FirstSeen:   p.FirstSeen,
			LastSeen:    p.LastSeen,
		}
		m.known[mac] = h
	}
}

func (m *Manager) profile(mac string) *Profile {
	p, ok := m.profiles[mac]
	if !ok {
		p = &Profile{}
		m.profiles[mac] = p
	}
	return p
}

func (m *Manager) saveProfile(mac string) error {
	return m.store.Set(profilePrefix+mac, *m.profile(mac))
}

// ---- 扫描 ----

// ScanOnce 执行一次全量扫描并产出事件。
func (m *Manager) ScanOnce() {
	now := time.Now().Unix()
	wl := scanWireless()
	dhcp := scanDHCP()
	arp := scanARP()

	m.mu.Lock()
	seen := map[string]bool{}

	// 1) 无线客户端（以 hostapd 关联状态为准）
	for mac, c := range wl {
		h := m.upsertLocked(mac, now)
		h.Online = true
		h.LastSeen = now
		h.Type = "wireless"
		h.Iface = c.Iface
		h.RxBytes = c.RxBytes
		h.TxBytes = c.TxBytes
		if l, ok := dhcp[mac]; ok {
			h.IP = l.IP
			if l.Hostname != "" {
				h.Name = l.Hostname
			}
		}
		h.Vendor = LookupVendor(mac)
		seen[mac] = true
	}

	// 2) 有线终端（ARP 可达）
	for mac, a := range arp {
		if _, ok := wl[mac]; ok {
			continue
		}
		h := m.upsertLocked(mac, now)
		h.Online = true
		h.LastSeen = now
		h.Type = "wired"
		h.Iface = m.bridgeIf
		h.IP = a.IP
		if l, ok := dhcp[mac]; ok && l.Hostname != "" {
			h.Name = l.Hostname
		}
		h.Vendor = LookupVendor(mac)
		seen[mac] = true
	}

	// 3) 只有 DHCP 租约、当前未活动：保留档案，不标记在线
	for mac, l := range dhcp {
		if seen[mac] {
			continue
		}
		h := m.upsertLocked(mac, now)
		if l.Hostname != "" {
			h.Name = l.Hostname
		}
		if h.IP == "" {
			h.IP = l.IP
		}
		if h.Vendor == "" {
			h.Vendor = LookupVendor(mac)
		}
	}

	// 4) 离线判定
	var events []model.Event
	for mac, h := range m.known {
		if seen[mac] {
			continue
		}
		threshold := m.wiredOfflineAfter
		if h.Type == "wireless" {
			threshold = m.wirelessOfflineAfter
		}
		if h.Online && now-h.LastSeen >= threshold {
			h.Online = false
			events = append(events, model.Event{
				Type: "event",
				Name: model.EventHostOffline,
				TS:   now,
				Data: model.HostEventData{Host: *h},
			})
		}
	}

	// 5) 上线 / 新设备事件（在 upsert 时收集）
	events = append(events, m.pending...)
	m.pending = nil
	m.mu.Unlock()

	for _, ev := range events {
		m.emit(ev)
	}

	// 持久化 LastSeen（低频，避免频繁写盘）
	m.flushProfiles()
}

// flushProfiles 节流落盘：终端档案变化不频繁，避免每轮扫描都写闪存。
func (m *Manager) flushProfiles() {
	m.mu.Lock()
	now := time.Now()
	if now.Sub(m.lastFlush) < time.Minute {
		m.mu.Unlock()
		return
	}
	m.lastFlush = now
	snapshot := make(map[string]Profile, len(m.profiles))
	for mac, p := range m.profiles {
		snapshot[mac] = *p
	}
	m.mu.Unlock()
	for mac, p := range snapshot {
		_ = m.store.Set(profilePrefix+mac, p)
	}
}

// upsertLocked 返回终端记录，首次出现时写入 FirstSeen 并产生 new/online 事件。
func (m *Manager) upsertLocked(mac string, now int64) *model.Host {
	h, ok := m.known[mac]
	if !ok {
		// 新设备：只发 host.new（App 端视为「首次上线」，避免与 online 重复提醒）
		h = &model.Host{MAC: mac, FirstSeen: now}
		m.known[mac] = h
		m.profile(mac).FirstSeen = now
		m.pending = append(m.pending, model.Event{
			Type: "event",
			Name: model.EventHostNew,
			TS:   now,
			Data: model.HostEventData{Host: *h},
		})
	} else if !h.Online {
		m.pending = append(m.pending, model.Event{
			Type: "event",
			Name: model.EventHostOnline,
			TS:   now,
			Data: model.HostEventData{Host: *h},
		})
	}
	p := m.profile(mac)
	p.LastSeen = now
	return h
}

func (m *Manager) emit(ev model.Event) {
	if m.onEvent == nil {
		return
	}
	m.onEvent(ev)
}

// Loop 按固定周期扫描，直到 ctx 取消。
func (m *Manager) Loop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logx.Errorf("hosts", "scan panic: %v", r)
					}
				}()
				m.ScanOnce()
			}()
		}
	}
}

// ---- 查询 ----

// List 返回全部已知终端（含离线），按在线优先、最近活跃排序。
func (m *Manager) List() []model.Host {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.Host, 0, len(m.known))
	for _, h := range m.known {
		cp := *h
		cp.MAC = h.MAC
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		return out[i].LastSeen > out[j].LastSeen
	})
	return out
}

// Get 查询单个终端。
func (m *Manager) Get(mac string) (model.Host, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.known[mac]
	if !ok {
		return model.Host{}, false
	}
	return *h, true
}

// LookupByIP 按 IP 反查 MAC（DNS 过滤需要把源 IP 还原成终端）。
func (m *Manager) LookupByIP(ip string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for mac, h := range m.known {
		if h.IP == ip {
			return mac
		}
	}
	return ""
}

// Online 返回在线终端数量。
func (m *Manager) Online() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, h := range m.known {
		if h.Online {
			n++
		}
	}
	return n
}

// ---- 策略 ----

// SetAlias 设置备注名。
func (m *Manager) SetAlias(mac, alias string) error {
	m.mu.Lock()
	p := m.profile(mac)
	p.Alias = alias
	if h, ok := m.known[mac]; ok {
		h.Alias = alias
	}
	m.mu.Unlock()
	return m.saveProfile(mac)
}

// SetNotify 开关上线提醒。
func (m *Manager) SetNotify(mac string, enabled bool) error {
	m.mu.Lock()
	m.profile(mac).Notify = enabled
	m.mu.Unlock()
	return m.saveProfile(mac)
}

// SetBlocked 拉黑/放行终端。
func (m *Manager) SetBlocked(mac string, blocked bool) error {
	if m.applier != nil {
		if err := m.applier.ApplyBlock(mac, blocked); err != nil {
			return err
		}
	}
	m.mu.Lock()
	p := m.profile(mac)
	p.Blocked = blocked
	if h, ok := m.known[mac]; ok {
		h.Blocked = blocked
	}
	m.mu.Unlock()
	return m.saveProfile(mac)
}

// SetRateLimit 设置限速（kbps，0 表示不限速）。
func (m *Manager) SetRateLimit(mac string, kbps int) error {
	if m.applier != nil {
		if err := m.applier.ApplyRateLimit(mac, kbps); err != nil {
			return err
		}
	}
	m.mu.Lock()
	p := m.profile(mac)
	p.RateLimit = kbps
	if h, ok := m.known[mac]; ok {
		h.RateLimit = kbps
	}
	m.mu.Unlock()
	return m.saveProfile(mac)
}

// SetBlockedApps 设置该终端被禁止的 App 列表。
func (m *Manager) SetBlockedApps(mac string, apps []string) error {
	if m.applier != nil {
		if err := m.applier.ApplyAppRules(mac, apps); err != nil {
			return err
		}
	}
	m.mu.Lock()
	p := m.profile(mac)
	p.BlockedApps = apps
	if h, ok := m.known[mac]; ok {
		h.BlockedApps = apps
	}
	m.mu.Unlock()
	return m.saveProfile(mac)
}
