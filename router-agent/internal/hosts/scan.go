package hosts

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/ax6000/ragent/internal/router"
)

// 各种数据来源的采样结果。终端的最终视图由 Scan 合并得到。

type wirelessClient struct {
	MAC     string
	Iface   string
	Signal  int
	RxBytes int64
	TxBytes int64
}

// scanWireless 通过 hostapd 的 ubus 接口读取已关联的无线客户端。
func scanWireless() map[string]*wirelessClient {
	out := map[string]*wirelessClient{}
	for _, iface := range router.APInterfaces() {
		m, err := router.UbusCall("hostapd."+iface, "get_clients", nil)
		if err != nil {
			continue
		}
		clients, ok := m["clients"].(map[string]interface{})
		if !ok {
			continue
		}
		for rawMAC, v := range clients {
			cm, ok := v.(map[string]interface{})
			if !ok {
				cm = map[string]interface{}{}
			}
			c := &wirelessClient{
				MAC:   router.NormalizeMAC(rawMAC),
				Iface: iface,
			}
			if s, ok := cm["signal"].(float64); ok {
				c.Signal = int(s)
			}
			if b, ok := cm["bytes"].(map[string]interface{}); ok {
				if rx, ok := b["rx"].(float64); ok {
					c.RxBytes = int64(rx)
				}
				if tx, ok := b["tx"].(float64); ok {
					c.TxBytes = int64(tx)
				}
			}
			out[c.MAC] = c
		}
	}
	return out
}

type leaseInfo struct {
	IP       string
	Hostname string
}

// scanDHCP 合并 dnsmasq 租约文件与 odhcpd 的 ubus 租约，得到 MAC -> IP/主机名。
func scanDHCP() map[string]*leaseInfo {
	out := map[string]*leaseInfo{}
	scanDnsmasqLeases(out)
	scanOdhcpdLeases(out)
	return out
}

// dnsmasq 租约文件格式（/tmp/dhcp.leases）：
// <expiry unix> <mac> <ip> <hostname> <client-id>
func scanDnsmasqLeases(out map[string]*leaseInfo) {
	f, err := os.Open("/tmp/dhcp.leases")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 4 {
			continue
		}
		mac := router.NormalizeMAC(fs[1])
		info := &leaseInfo{IP: fs[2]}
		if fs[3] != "*" {
			info.Hostname = fs[3]
		}
		out[mac] = info
	}
}

// odhcpd 租约：ubus call dhcp ipv4leases
// {"device":{"br-lan":{"leases":[{"mac":"..","ip":"..","hostname":".."}]}}}
func scanOdhcpdLeases(out map[string]*leaseInfo) {
	var resp struct {
		Device map[string]struct {
			Leases []struct {
				MAC      string `json:"mac"`
				IP       string `json:"ip"`
				Hostname string `json:"hostname"`
			} `json:"leases"`
		} `json:"device"`
	}
	if err := router.RunJSON(&resp, "ubus", "-t", "5", "call", "dhcp", "ipv4leases"); err != nil {
		return
	}
	for _, dev := range resp.Device {
		for _, l := range dev.Leases {
			if l.MAC == "" {
				continue
			}
			mac := router.NormalizeMAC(l.MAC)
			if _, ok := out[mac]; ok && l.IP == "" {
				continue
			}
			out[mac] = &leaseInfo{IP: l.IP, Hostname: l.Hostname}
		}
	}
}

type arpEntry struct {
	IP        string
	Device    string
	Reachable bool
}

// scanARP 解析 /proc/net/arp，用于发现有线终端与确认在线状态。
func scanARP() map[string]*arpEntry {
	out := map[string]*arpEntry{}
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // 跳过表头
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 6 {
			continue
		}
		mac := router.NormalizeMAC(fs[3])
		if len(mac) != 12 || mac == "000000000000" {
			continue
		}
		flags, _ := strconv.ParseUint(fs[2], 0, 32)
		out[mac] = &arpEntry{
			IP:        fs[0],
			Device:    fs[5],
			Reachable: flags&0x2 != 0, // ATF_COM
		}
	}
	return out
}

// scanBridgeFDB 读取桥接转发表，补充"只发过少量包、ARP 未命中"的有线终端。
func scanBridgeFDB(br string) map[string]bool {
	if br == "" {
		br = "br-lan"
	}
	out := map[string]bool{}
	res, err := router.Run("bridge", "fdb", "show", "br", br)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(res, "\n") {
		fs := strings.Fields(line)
		if len(fs) < 2 {
			continue
		}
		// 跳过本机端口的 permanent / self 表项
		local := false
		for _, f := range fs[1:] {
			if f == "permanent" || f == "self" {
				local = true
			}
		}
		if local {
			continue
		}
		mac := router.NormalizeMAC(fs[0])
		if len(mac) != 12 || mac == "000000000000" {
			continue
		}
		out[mac] = true
	}
	return out
}
