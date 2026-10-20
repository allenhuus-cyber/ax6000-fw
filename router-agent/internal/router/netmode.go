package router

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ax6000/ragent/internal/model"
)

// ---- 上网方式（WAN）----

// WANDevice 探测当前 wan 口使用的物理设备名（DSA 下通常是 wan / eth1）。
func WANDevice() string {
	if d := UciGet("network.wan.device"); d != "" {
		return d
	}
	if d := UciGet("network.wan.ifname"); d != "" {
		return d
	}
	if d := UciGet("network.@device[0].name"); d != "" {
		return d
	}
	return "wan"
}

// GetWANConfig 读取当前上网方式与参数。
func GetWANConfig() (*model.WANConfig, error) {
	cfg := &model.WANConfig{}
	proto := UciGet("network.wan.proto")
	if proto == "" {
		proto = "dhcp"
	}
	switch proto {
	case "pppoe":
		cfg.Mode = "pppoe"
		cfg.Username = UciGet("network.wan.username")
		cfg.Password = UciGet("network.wan.password")
		cfg.ServiceName = UciGet("network.wan.service")
	case "static":
		cfg.Mode = "static"
		cfg.IP = UciGet("network.wan.ipaddr")
		cfg.Netmask = UciGet("network.wan.netmask")
		cfg.Gateway = UciGet("network.wan.gateway")
		cfg.DNS = uciList("network.wan.dns")
	case "dhcp":
		// 可能是有线 DHCP，也可能是无线中继（存在 wwan 且 wan 已停用）
		if UciGet("network.wwan") == "interface" || UciGet("network.wwan.proto") != "" {
			cfg.Mode = "relay"
		} else {
			cfg.Mode = "dhcp"
		}
	default:
		cfg.Mode = proto
	}
	if v := UciGet("network.wan.mtu"); v != "" {
		cfg.MTU, _ = strconv.Atoi(v)
	}
	cfg.MACClone = UciGet("network.wan.macaddr")
	if cfg.Mode == "relay" {
		cfg.RelaySSID, cfg.RelayPassword, cfg.RelayBand = RelayCurrent()
	}
	return cfg, nil
}

func uciList(key string) []string {
	out, err := Run("uci", "-q", "get", key)
	if err != nil || out == "" {
		return nil
	}
	// uci get 对 list 返回空格分隔
	return strings.Fields(strings.Trim(out, `"'`))
}

// GetWANStatus 通过 ubus 查询 wan 接口实时状态。
func GetWANStatus() (*model.WANStatus, error) {
	st := &model.WANStatus{}
	cfg, err := GetWANConfig()
	if err != nil {
		return nil, err
	}
	st.Mode = cfg.Mode
	iface := "wan"
	if cfg.Mode == "relay" {
		iface = "wwan"
	}
	m, err := UbusCall("network.interface."+iface, "status", nil)
	if err != nil {
		st.Error = err.Error()
		return st, nil
	}
	st.Protocol = cfg.Mode
	if v, ok := m["up"].(bool); ok {
		st.Up = v
	}
	if v, ok := m["device"].(string); ok {
		st.Device = v
	}
	if v, ok := m["uptime"].(float64); ok {
		st.Uptime = int64(v)
	}
	if ip := extractFirstIPv4(m); ip != "" {
		st.IP = ip
	}
	if route, ok := m["route"].([]interface{}); ok && len(route) > 0 {
		if rm, ok := route[0].(map[string]interface{}); ok {
			if g, ok := rm["nexthop"].(string); ok {
				st.Gateway = g
			}
		}
	}
	if dns, ok := m["dns-server"].([]interface{}); ok {
		for _, d := range dns {
			if s, ok := d.(string); ok {
				st.DNS = append(st.DNS, s)
			}
		}
	}
	if s, ok := m["data"].(map[string]interface{}); ok {
		if rx, ok := s["rx_bytes"].(float64); ok {
			st.RxBytes = int64(rx)
		}
		if tx, ok := s["tx_bytes"].(float64); ok {
			st.TxBytes = int64(tx)
		}
	}
	if stats, ok := m["statistics"].(map[string]interface{}); ok {
		if rx, ok := stats["rx_bytes"].(float64); ok {
			st.RxBytes = int64(rx)
		}
		if tx, ok := stats["tx_bytes"].(float64); ok {
			st.TxBytes = int64(tx)
		}
	}
	// MAC 地址：从 l2_device 或直接读网口
	dev := st.Device
	if dev == "" {
		dev = WANDevice()
	}
	if b, err := ReadFileFirstLine("/sys/class/net/" + dev + "/address"); err == nil {
		st.MAC = strings.TrimSpace(b)
	}
	return st, nil
}

// ApplyWAN 写入上网方式配置并生效。
// 会先清理互斥选项，再按 mode 写入对应参数，最后 reload network。
func ApplyWAN(cfg *model.WANConfig) error {
	switch cfg.Mode {
	case "pppoe", "dhcp", "static", "relay":
	default:
		return fmt.Errorf("unsupported wan mode: %s", cfg.Mode)
	}

	// 清理上一次可能残留的参数
	for _, k := range []string{
		"network.wan.username", "network.wan.password", "network.wan.service",
		"network.wan.ipaddr", "network.wan.netmask", "network.wan.gateway",
		"network.wan.dns", "network.wan.mtu", "network.wan.macaddr",
	} {
		_ = UciDelete(k)
	}

	switch cfg.Mode {
	case "pppoe":
		if cfg.Username == "" {
			return fmt.Errorf("pppoe 需要用户名")
		}
		if err := UciSet("network.wan.proto", "pppoe"); err != nil {
			return err
		}
		if err := UciSet("network.wan.username", cfg.Username); err != nil {
			return err
		}
		if err := UciSet("network.wan.password", cfg.Password); err != nil {
			return err
		}
		if cfg.ServiceName != "" {
			_ = UciSet("network.wan.service", cfg.ServiceName)
		}
		// 中继清理
		_ = DisableRelay()
	case "dhcp":
		if err := UciSet("network.wan.proto", "dhcp"); err != nil {
			return err
		}
		_ = DisableRelay()
	case "static":
		if cfg.IP == "" || cfg.Netmask == "" {
			return fmt.Errorf("静态地址需要 ip 与 netmask")
		}
		if err := UciSet("network.wan.proto", "static"); err != nil {
			return err
		}
		if err := UciSet("network.wan.ipaddr", cfg.IP); err != nil {
			return err
		}
		if err := UciSet("network.wan.netmask", cfg.Netmask); err != nil {
			return err
		}
		if cfg.Gateway != "" {
			_ = UciSet("network.wan.gateway", cfg.Gateway)
		}
		for _, d := range cfg.DNS {
			_, _ = Run("uci", "add_list", "network.wan.dns="+d)
		}
		_ = DisableRelay()
	case "relay":
		if cfg.RelaySSID == "" {
			return fmt.Errorf("无线中继需要 SSID")
		}
		if err := SetupRelay(cfg); err != nil {
			return err
		}
	}

	if cfg.MTU > 0 {
		_ = UciSet("network.wan.mtu", strconv.Itoa(cfg.MTU))
	}
	if cfg.MACClone != "" && cfg.Mode != "relay" {
		_ = UciSet("network.wan.macaddr", cfg.MACClone)
	}

	if err := UciCommit("network"); err != nil {
		return err
	}
	if err := UciCommit("wireless"); err != nil {
		_ = err
	}
	if err := UciCommit("firewall"); err != nil {
		_ = err
	}
	return ReloadNetwork()
}

// ---- LAN ----

// GetLANConfig 读取 LAN 配置。
func GetLANConfig() (*model.LANConfig, error) {
	cfg := &model.LANConfig{}
	cfg.IP = UciGet("network.lan.ipaddr")
	cfg.Netmask = UciGet("network.lan.netmask")
	cfg.DHCPEnabled = UciGet("dhcp.lan.ignore") != "1"
	if v := UciGet("dhcp.lan.start"); v != "" {
		cfg.DHCPStart, _ = strconv.Atoi(v)
	}
	if v := UciGet("dhcp.lan.limit"); v != "" {
		cfg.DHCPLimit, _ = strconv.Atoi(v)
	}
	cfg.DHCPLease = UciGet("dhcp.lan.leasetime")
	return cfg, nil
}

// ApplyLAN 写入 LAN 配置。
func ApplyLAN(cfg *model.LANConfig) error {
	if cfg.IP != "" {
		if err := UciSet("network.lan.ipaddr", cfg.IP); err != nil {
			return err
		}
	}
	if cfg.Netmask != "" {
		if err := UciSet("network.lan.netmask", cfg.Netmask); err != nil {
			return err
		}
	}
	if !cfg.DHCPEnabled {
		_ = UciSet("dhcp.lan.ignore", "1")
	} else {
		_ = UciDelete("dhcp.lan.ignore")
	}
	if cfg.DHCPStart > 0 {
		_ = UciSet("dhcp.lan.start", strconv.Itoa(cfg.DHCPStart))
	}
	if cfg.DHCPLimit > 0 {
		_ = UciSet("dhcp.lan.limit", strconv.Itoa(cfg.DHCPLimit))
	}
	if cfg.DHCPLease != "" {
		_ = UciSet("dhcp.lan.leasetime", cfg.DHCPLease)
	}
	if err := UciCommit("network"); err != nil {
		return err
	}
	if err := UciCommit("dhcp"); err != nil {
		return err
	}
	return ReloadNetwork()
}

// ---- 防火墙 zone 辅助 ----

// FindFirewallZone 返回 zone 的 UCI 路径，如 firewall.@zone[1]。
func FindFirewallZone(name string) string {
	m, err := UciShow("firewall")
	if err != nil {
		return ""
	}
	for i := 0; ; i++ {
		key := fmt.Sprintf("firewall.@zone[%d].name", i)
		v, ok := m[key]
		if !ok {
			return ""
		}
		if v == name {
			return fmt.Sprintf("firewall.@zone[%d]", i)
		}
	}
}

// ZoneNetworks 返回某 zone 的 network 列表。
func ZoneNetworks(zonePath string) []string {
	out, err := Run("uci", "-q", "get", zonePath+".network")
	if err != nil || out == "" {
		return nil
	}
	return strings.Fields(strings.Trim(out, `"'`))
}

// SetZoneNetworks 覆盖某 zone 的 network 列表。
func SetZoneNetworks(zonePath string, nets []string) error {
	_ = UciDelete(zonePath + ".network")
	for _, n := range nets {
		if _, err := Run("uci", "add_list", zonePath+".network="+n); err != nil {
			return err
		}
	}
	return UciCommit("firewall")
}
