package router

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

// ---- 系统状态 ----

// UptimeSeconds 读取 /proc/uptime 的第一字段。
func UptimeSeconds() int64 {
	f, err := os.Open("/proc/uptime")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0
	}
	fields := strings.Fields(sc.Text())
	if len(fields) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return int64(v)
}

// CPUUsage 采样两次 /proc/stat 计算总体 CPU 使用率（百分比）。
// 首次调用返回 0，需要间隔采样；这里内部做 200ms 间隔采样。
func CPUUsage() int {
	read := func() (total, idle uint64) {
		f, err := os.Open("/proc/stat")
		if err != nil {
			return 0, 0
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "cpu ") {
				continue
			}
			fs := strings.Fields(line)
			// user nice system idle iowait irq softirq steal
			var vals [8]uint64
			for i := 0; i < len(vals) && i+1 < len(fs); i++ {
				v, _ := strconv.ParseUint(fs[i+1], 10, 64)
				vals[i] = v
			}
			idle = vals[3] + vals[4]
			for _, v := range vals {
				total += v
			}
			return
		}
		return
	}
	t1, i1 := read()
	time.Sleep(200 * time.Millisecond)
	t2, i2 := read()
	dt := t2 - t1
	if dt == 0 {
		return 0
	}
	used := dt - (i2 - i1)
	pct := int((used * 100) / dt)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

type MemInfo struct {
	Total int64
	Free  int64
}

// Mem 读取 /proc/meminfo 的 MemTotal / MemAvailable（单位 KB）。
func Mem() MemInfo {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemInfo{}
	}
	defer f.Close()
	var info MemInfo
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			info.Total = parseKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			info.Free = parseKB(line)
		}
	}
	return info
}

func parseKB(line string) int64 {
	fs := strings.Fields(line)
	if len(fs) < 2 {
		return 0
	}
	v, _ := strconv.ParseInt(fs[1], 10, 64)
	return v * 1024
}

// LANAddress 从 ubus 获取 br-lan 的 IPv4 地址。
func LANAddress() string {
	m, err := UbusCall("network.interface.lan", "status", nil)
	if err != nil {
		return ""
	}
	return extractFirstIPv4(m)
}

func extractFirstIPv4(m map[string]interface{}) string {
	addrs, ok := m["ipv4-address"].([]interface{})
	if !ok {
		return ""
	}
	for _, a := range addrs {
		am, ok := a.(map[string]interface{})
		if !ok {
			continue
		}
		if ip, ok := am["address"].(string); ok {
			return ip
		}
	}
	return ""
}

// FirmwareVersion 读取 /etc/openwrt_release 或 os-release 中的版本串。
func FirmwareVersion() string {
	for _, path := range []string{"/etc/openwrt_release", "/etc/os-release", "/usr/lib/os-release"} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "DISTRIB_DESCRIPTION=") {
				return strings.Trim(line[len("DISTRIB_DESCRIPTION="):], `'"`)
			}
			if strings.HasPrefix(line, "OPENWRT_RELEASE=") {
				return strings.Trim(line[len("OPENWRT_RELEASE="):], `'"`)
			}
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				return strings.Trim(line[len("PRETTY_NAME="):], `'"`)
			}
		}
	}
	return "unknown"
}
