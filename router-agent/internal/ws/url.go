package ws

import (
	"crypto/tls"
	"errors"
	"net"
	"strings"
)

type wsURL struct {
	scheme string
	host   string // 不含端口
	port   string
	path   string
}

// parseURL 极简解析，只处理 ws://host[:port]/path 形式，够用且避免引入 net/url 的边界情况。
func parseURL(raw string) (*wsURL, error) {
	u := wsURL{path: "/"}
	s := raw
	switch {
	case strings.HasPrefix(s, "ws://"):
		u.scheme = "ws"
		s = strings.TrimPrefix(s, "ws://")
	case strings.HasPrefix(s, "wss://"):
		u.scheme = "wss"
		s = strings.TrimPrefix(s, "wss://")
	default:
		return nil, errors.New("ws: unsupported scheme")
	}
	if i := strings.Index(s, "/"); i >= 0 {
		u.path = s[i:]
		if u.path == "" {
			u.path = "/"
		}
		s = s[:i]
	} else {
		u.path = "/"
	}
	u.host = s
	u.port = "80"
	if u.scheme == "wss" {
		u.port = "443"
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		// 仅当冒号后不是纯数字时才可能是 IPv6，这里简单处理常见场景
		hostPort := s[i+1:]
		if hostPort != "" && !strings.Contains(hostPort, "]") {
			u.host = s[:i]
			u.port = hostPort
		}
	}
	u.host = strings.Trim(u.host, "[]")
	if u.host == "" {
		return nil, errors.New("ws: empty host")
	}
	return &u, nil
}

func wrapTLS(conn net.Conn, host string) (net.Conn, error) {
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsConn.Handshake(); err != nil {
		conn.Close()
		return nil, err
	}
	return tlsConn, nil
}
