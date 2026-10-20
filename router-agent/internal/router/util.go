package router

import (
	"os"
	"strings"
)

// ReadFileFirstLine 读取文件首行，用于 /sys/class/net/<dev>/address 之类的伪文件。
func ReadFileFirstLine(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	if i := strings.IndexAny(s, "\n"); i >= 0 {
		s = s[:i]
	}
	return s, nil
}

// NormalizeMAC 统一 MAC 格式为小写无分隔符，用于终端标识。
func NormalizeMAC(mac string) string {
	s := strings.ToLower(strings.TrimSpace(mac))
	s = strings.NewReplacer(":", "", "-", "", ".", "").Replace(s)
	if len(s) != 12 {
		return strings.ToLower(strings.TrimSpace(mac))
	}
	return s
}

// PrettyMAC 输出 aa:bb:cc:dd:ee:ff 形式。
func PrettyMAC(mac string) string {
	n := NormalizeMAC(mac)
	if len(n) != 12 {
		return mac
	}
	var sb strings.Builder
	for i := 0; i < 12; i += 2 {
		if i > 0 {
			sb.WriteByte(':')
		}
		sb.WriteString(n[i : i+2])
	}
	return sb.String()
}
