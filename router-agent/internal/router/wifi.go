package router

import (
	"errors"
	"strings"

	"github.com/ax6000/ragent/internal/model"
)

// errNoRadio 表示固件里没有找到可用的无线设备（无线驱动未加载或配置缺失）。
var errNoRadio = errors.New("no wifi-device found in wireless config")

// ---- 无线 ----

// RadioInfo 描述一个物理无线设备。
type RadioInfo struct {
	Name   string // UCI 段名，如 radio1
	Band   string // "2g" / "5g" / "6g"
	Phy    string // phy0 / phy1
	Iface  string // 当前接口名（wlan0/wlan1），中继接口另算
	SSID   string
}

// Radios 列出所有 wifi-device。
func Radios() []RadioInfo {
	m, err := UciShow("wireless")
	if err != nil {
		return nil
	}
	var list []RadioInfo
	for k, v := range m {
		// 形如 wireless.radio1=wifi-device
		parts := strings.Split(k, ".")
		if len(parts) != 2 || v != "wifi-device" {
			continue
		}
		name := parts[1]
		if strings.HasPrefix(name, "@") {
			continue
		}
		r := RadioInfo{Name: name}
		band := ""
		if b, ok := m["wireless."+name+".band"]; ok {
			band = strings.ToLower(b)
		}
		if band == "" {
			// 老配置用 hwmode：11a/11g
			switch strings.ToLower(m["wireless."+name+".hwmode"]) {
			case "11a", "11ac", "11axa":
				band = "5g"
			default:
				band = "2g"
			}
		}
		r.Band = band
		if p, ok := m["wireless."+name+".phy"]; ok {
			r.Phy = p
		}
		list = append(list, r)
	}
	// 关联 AP 接口名与 SSID
	for i := range list {
		for k, v := range m {
			if v != "wifi-iface" {
				continue
			}
			parts := strings.Split(k, ".")
			if len(parts) != 2 {
				continue
			}
			sec := parts[1]
			if m["wireless."+sec+".device"] != list[i].Name {
				continue
			}
			if m["wireless."+sec+".mode"] != "ap" {
				continue
			}
			if ssid, ok := m["wireless."+sec+".ssid"]; ok {
				list[i].SSID = ssid
			}
			if ifn, ok := m["wireless."+sec+".ifname"]; ok && ifn != "" {
				list[i].Iface = ifn
			}
		}
		if list[i].Iface == "" {
			list[i].Iface = defaultIfaceName(list[i].Phy)
		}
	}
	return list
}

func defaultIfaceName(phy string) string {
	switch phy {
	case "phy0":
		return "wlan0"
	case "phy1":
		return "wlan1"
	case "phy2":
		return "wlan2"
	}
	return ""
}

// APInterfaces 返回当前处于 AP 模式的接口名列表（用于读取无线客户端）。
func APInterfaces() []string {
	var out []string
	for _, r := range Radios() {
		if r.Iface != "" {
			out = append(out, r.Iface)
		}
	}
	return out
}

func pickRadio(band string) string {
	rs := Radios()
	if len(rs) == 0 {
		return ""
	}
	for _, r := range rs {
		if r.Band == band {
			return r.Name
		}
	}
	if band == "2g" {
		for _, r := range rs {
			if r.Band != "5g" {
				return r.Name
			}
		}
	}
	return rs[len(rs)-1].Name
}

// ---- 无线中继（WISP / Client+AP）----

// SetupRelay 配置无线中继：新增 sta 接口 + wwan 网络接口 + 防火墙调整。
func SetupRelay(cfg *model.WANConfig) error {
	radio := pickRadio(firstNonEmpty(cfg.RelayBand, "5g"))
	if radio == "" {
		return errNoRadio
	}

	sec := findStaSection()
	if sec == "" {
		newSec, err := UciAdd("wireless", "wifi-iface")
		if err != nil {
			return err
		}
		sec = strings.TrimSpace(newSec)
	}
	sets := map[string]string{
		"device":    radio,
		"mode":      "sta",
		"network":   "wwan",
		"disabled":  "0",
		"ssid":      cfg.RelaySSID,
		"encryption": "psk2",
	}
	if cfg.RelayPassword == "" {
		sets["encryption"] = "none"
		_ = UciDelete("wireless." + sec + ".key")
	} else {
		sets["key"] = cfg.RelayPassword
	}
	for k, v := range sets {
		if err := UciSet("wireless."+sec+"."+k, v); err != nil {
			return err
		}
	}
	_ = UciSet("wireless."+sec+".mode", "sta")

	_ = UciSetNamed("network", "wwan", "interface")
	_ = UciSet("network.wwan.proto", "dhcp")

	// 防火墙：把 wwan 放进 wan zone，去掉 wan（避免有线口空转报错）
	if zp := FindFirewallZone("wan"); zp != "" {
		nets := ZoneNetworks(zp)
		var next []string
		for _, n := range nets {
			if n != "wan" {
				next = append(next, n)
			}
		}
		next = append(next, "wwan")
		_ = SetZoneNetworks(zp, next)
	}
	// 有线 WAN 停用
	_ = UciSet("network.wan.proto", "none")
	_ = UciDelete("network.wan6")
	return nil
}

// DisableRelay 关闭无线中继，恢复有线 WAN 为默认 DHCP 上行。
func DisableRelay() error {
	if sec := findStaSection(); sec != "" {
		_ = UciSet("wireless."+sec+".disabled", "1")
	}
	_ = UciDelete("network.wwan")
	if zp := FindFirewallZone("wan"); zp != "" {
		nets := ZoneNetworks(zp)
		var next []string
		hasWAN := false
		for _, n := range nets {
			if n == "wwan" {
				continue
			}
			if n == "wan" {
				hasWAN = true
			}
			next = append(next, n)
		}
		if !hasWAN {
			next = append(next, "wan")
		}
		_ = SetZoneNetworks(zp, next)
	}
	if UciGet("network.wan.proto") == "none" {
		_ = UciSet("network.wan.proto", "dhcp")
	}
	return nil
}

// RelayCurrent 返回当前中继的 SSID/密码/频段。
func RelayCurrent() (ssid, key, band string) {
	m, err := UciShow("wireless")
	if err != nil {
		return
	}
	for k, v := range m {
		if v != "wifi-iface" {
			continue
		}
		parts := strings.Split(k, ".")
		if len(parts) != 2 {
			continue
		}
		sec := parts[1]
		if m["wireless."+sec+".mode"] != "sta" {
			continue
		}
		ssid = m["wireless."+sec+".ssid"]
		key = m["wireless."+sec+".key"]
		dev := m["wireless."+sec+".device"]
		for _, r := range Radios() {
			if r.Name == dev {
				band = r.Band
			}
		}
		return
	}
	return
}

func findStaSection() string {
	m, err := UciShow("wireless")
	if err != nil {
		return ""
	}
	for k, v := range m {
		if v != "wifi-iface" {
			continue
		}
		parts := strings.Split(k, ".")
		if len(parts) != 2 {
			continue
		}
		sec := parts[1]
		if m["wireless."+sec+".mode"] == "sta" {
			return sec
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- AP 扫描 ----

// ScanResult 周边热点扫描结果。
type ScanResult struct {
	SSID       string `json:"ssid"`
	BSSID      string `json:"bssid"`
	Channel    int    `json:"channel"`
	Signal     int    `json:"signal"` // dBm，负值
	Encryption string `json:"encryption"`
	Band       string `json:"band"`
}

// Scan 使用 iwinfo 扫描周边热点（ubus 接口，无需 root 权限拼装命令行）。
func Scan(radio string) ([]ScanResult, error) {
	if radio == "" {
		radio = pickRadio("5g")
	}
	var resp struct {
		Results []struct {
			SSID     string `json:"ssid"`
			BSSID    string `json:"bssid"`
			Channel  int    `json:"channel"`
			Signal   int    `json:"signal"`
			Encryption struct {
				Enabled    bool   `json:"enabled"`
				WPA        int    `json:"wpa"`
				AuthSuites []string `json:"auth_suites"`
			} `json:"encryption"`
		} `json:"results"`
	}
	if err := RunJSON(&resp, "ubus", "-t", "20", "call", "iwinfo", "scan", `{"device":"`+radio+`"}`); err != nil {
		return nil, err
	}
	out := make([]ScanResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		enc := "none"
		if r.Encryption.Enabled {
			enc = "psk2"
		}
		out = append(out, ScanResult{
			SSID:       r.SSID,
			BSSID:      r.BSSID,
			Channel:    r.Channel,
			Signal:     r.Signal,
			Encryption: enc,
			Band:       bandByChannel(r.Channel),
		})
	}
	return out, nil
}

func bandByChannel(ch int) string {
	if ch > 14 {
		return "5g"
	}
	return "2g"
}
