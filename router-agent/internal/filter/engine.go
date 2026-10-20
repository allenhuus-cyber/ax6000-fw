// Package filter 实现终端策略引擎：拉黑、限速、App 访问限制。
//
// App 限制采用双引擎：
//   - DPI 引擎 OpenAppFilter（内核态识别，可覆盖游戏等无法枚举域名的 App）；
//   - 域名引擎（内置 DNS 代理 + nftables 重定向），作为兜底并可单独工作。
//
// 两者对同一终端同时生效，最大化拦截成功率。
package filter

import (
	"sort"
	"strings"
	"sync"

	"github.com/ax6000/ragent/internal/apps"
	"github.com/ax6000/ragent/internal/logx"
	"github.com/ax6000/ragent/internal/model"
	"github.com/ax6000/ragent/internal/router"
	"github.com/ax6000/ragent/internal/store"
)

// Manager 聚合各引擎，并作为 hosts.PolicyApplier 的实现。
type Manager struct {
	store *store.File
	oaf   *OAF
	nft   *NFT
	tc    *TC
	proxy *DNSProxy

	// ipToMAC 由 hosts 注入，用于把 DNS 请求的源 IP 还原成终端 MAC。
	ipToMAC func(ip string) string

	mu       sync.Mutex
	rules    map[string][]string // mac -> 被禁止的 app id
	blocked  map[string]bool
	limits   map[string]int
	classIDs map[string]int
}

// New 创建过滤管理器。st 用于持久化规则，dnsPort 为内置 DNS 代理端口。
func New(st *store.File, dnsPort int) *Manager {
	m := &Manager{
		store:    st,
		oaf:      NewOAF(),
		nft:      NewNFT("/etc/ragent/nft.conf", dnsPort),
		tc:       NewTC("br-lan"),
		rules:    map[string][]string{},
		blocked:  map[string]bool{},
		limits:   map[string]int{},
		classIDs: map[string]int{},
	}
	m.tc.Restore(m.loadMap("filter.class_ids"))
	m.loadState()
	m.proxy = NewDNSProxy(dnsPort, "", m.shouldBlock)
	if m.oaf == nil {
		logx.Warnf("filter", "OpenAppFilter 未安装，仅使用域名引擎（请在固件中集成 appfilter 内核模块）")
	}
	return m
}

// SetIPResolver 注入 IP -> MAC 的查询函数（DNS 代理需要）。
func (m *Manager) SetIPResolver(fn func(ip string) string) {
	m.ipToMAC = fn
}

// Start 启动内置 DNS 代理。
func (m *Manager) Start() error {
	return m.proxy.Start()
}

// Stop 停止代理并清理 nftables 表。
func (m *Manager) Stop() {
	m.proxy.Stop()
	_ = m.nft.Clear()
}

// ---- hosts.PolicyApplier ----

// ApplyBlock 拉黑 / 放行终端。
func (m *Manager) ApplyBlock(mac string, blocked bool) error {
	m.mu.Lock()
	m.blocked[mac] = blocked
	m.mu.Unlock()
	if err := m.store.Set("filter.blocked", m.blocked); err != nil {
		return err
	}
	return m.syncNFT()
}

// ApplyRateLimit 设置限速（kbps，0 为解除）。
func (m *Manager) ApplyRateLimit(mac string, kbps int) error {
	if err := m.tc.Apply(mac, kbps); err != nil {
		logx.Warnf("filter", "rate limit %s: %v", mac, err)
		return err
	}
	m.mu.Lock()
	m.limits[mac] = kbps
	m.classIDs = m.tc.Snapshot()
	m.mu.Unlock()
	if err := m.store.Set("filter.limits", m.limits); err != nil {
		return err
	}
	return m.store.Set("filter.class_ids", m.classIDs)
}

// ApplyAppRules 设置某终端禁止访问的 App 列表（双引擎生效）。
func (m *Manager) ApplyAppRules(mac string, appIDs []string) error {
	clean := normalizeAppIDs(appIDs)
	m.mu.Lock()
	if len(clean) == 0 {
		delete(m.rules, mac)
	} else {
		m.rules[mac] = clean
	}
	m.mu.Unlock()
	if err := m.store.Set("filter.rules", m.rules); err != nil {
		return err
	}
	if m.oaf != nil {
		if err := m.oaf.Apply(router.PrettyMAC(mac), clean); err != nil {
			logx.Warnf("filter", "oaf apply %s: %v", mac, err)
		}
	}
	return m.syncNFT()
}

// ---- 查询 ----

// Rules 返回某终端被禁止的 App。
func (m *Manager) Rules(mac string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.rules[mac]...)
}

// Catalog 返回可管控的 App 列表，标注每个 App 当前可用的引擎。
func (m *Manager) Catalog() []model.AppInfo {
	var oafIDs map[string]int
	if m.oaf != nil {
		oafIDs = m.oaf.Catalog()
	}
	out := make([]model.AppInfo, 0, len(apps.Catalog))
	for _, def := range apps.Catalog {
		info := model.AppInfo{
			ID:       def.ID,
			Name:     def.Name,
			Category: string(def.Category),
		}
		if len(def.Domains) > 0 {
			info.Engines = append(info.Engines, "dns")
		}
		if _, ok := oafIDs[def.ID]; ok {
			info.Engines = append(info.Engines, "oaf")
		}
		if len(info.Engines) == 0 {
			info.Engines = []string{"dns"}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// EngineStatus 反馈各引擎可用性，便于 App 端提示。
func (m *Manager) EngineStatus() map[string]bool {
	return map[string]bool{
		"oaf":    m.oaf != nil,
		"nft":    m.nft.Available(),
		"tc":     m.tc.Available(),
		"dns":    true,
	}
}

// ---- 内部 ----

// shouldBlock 供 DNS 代理回调：判断某 IP 的终端是否禁止访问该域名。
func (m *Manager) shouldBlock(clientIP, domain string) bool {
	if m.ipToMAC == nil {
		return false
	}
	mac := m.ipToMAC(clientIP)
	if mac == "" {
		return false
	}
	m.mu.Lock()
	ids := m.rules[mac]
	m.mu.Unlock()
	if len(ids) == 0 {
		return false
	}
	return apps.MatchDomain(domain, ids)
}

func (m *Manager) syncNFT() error {
	m.mu.Lock()
	var blockedMACs, filterMACs []string
	for mac, v := range m.blocked {
		if v {
			blockedMACs = append(blockedMACs, router.PrettyMAC(mac))
		}
	}
	for mac := range m.rules {
		filterMACs = append(filterMACs, router.PrettyMAC(mac))
	}
	m.mu.Unlock()
	sort.Strings(blockedMACs)
	sort.Strings(filterMACs)
	if err := m.nft.Apply(State{BlockedMACs: blockedMACs, FilterMACs: filterMACs}); err != nil {
		logx.Errorf("filter", "sync nft: %v", err)
		return err
	}
	return nil
}

func (m *Manager) loadState() {
	_ = m.store.Get("filter.rules", &m.rules)
	_ = m.store.Get("filter.blocked", &m.blocked)
	_ = m.store.Get("filter.limits", &m.limits)
	if m.rules == nil {
		m.rules = map[string][]string{}
	}
	if m.blocked == nil {
		m.blocked = map[string]bool{}
	}
	if m.limits == nil {
		m.limits = map[string]int{}
	}
}

func (m *Manager) loadMap(key string) map[string]int {
	out := map[string]int{}
	_ = m.store.Get(key, &out)
	return out
}

// RestoreOnBoot 开机时把持久化的规则重新下发到内核（幂等）。
func (m *Manager) RestoreOnBoot() error {
	if err := m.syncNFT(); err != nil {
		return err
	}
	// 重新下发限速与 DPI 规则
	for mac, kbps := range m.limits {
		if kbps > 0 {
			_ = m.tc.Apply(mac, kbps)
		}
	}
	if m.oaf != nil {
		for mac, ids := range m.rules {
			_ = m.oaf.Apply(router.PrettyMAC(mac), ids)
		}
	}
	return nil
}

func normalizeAppIDs(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if _, ok := apps.ByID(id); !ok {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
