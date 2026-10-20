package filter

import (
	"bufio"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ax6000/ragent/internal/logx"
)

// DNSProxy 是一个极简 DNS 转发代理，用于按终端做域名拦截。
//
// 工作流程：
//  1. nftables 把受控终端的 53 端口流量重定向到本代理；
//  2. 代理解析查询域名，命中该终端的黑名单时直接返回 NXDOMAIN；
//  3. 否则原样转发到上游 DNS，并把应答回给终端。
//
// 已知边界：无法拦截 DoH/DoT（443/853）以及直接使用 IP 直连的应用，
// 这类流量由 DPI 引擎（OpenAppFilter）覆盖。
type DNSProxy struct {
	Port     int
	Upstream string
	Block    func(clientIP, domain string) bool

	mu   sync.Mutex
	conn *net.UDPConn
	done chan struct{}
	wg   sync.WaitGroup
}

// NewDNSProxy 创建代理。upstream 为空时自动探测。
func NewDNSProxy(port int, upstream string, block func(string, string) bool) *DNSProxy {
	if port == 0 {
		port = 5353
	}
	if upstream == "" {
		upstream = DetectUpstream()
	}
	return &DNSProxy{Port: port, Upstream: upstream, Block: block, done: make(chan struct{})}
}

// Start 绑定端口并开始服务。
func (p *DNSProxy) Start() error {
	addr, err := net.ResolveUDPAddr("udp", ":"+strconv.Itoa(p.Port))
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.conn = conn
	p.mu.Unlock()
	logx.Infof("dns", "dns proxy listening on :%d upstream=%s", p.Port, p.Upstream)
	p.wg.Add(1)
	go p.serve(conn)
	return nil
}

// Stop 关闭代理。
func (p *DNSProxy) Stop() {
	close(p.done)
	p.mu.Lock()
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
	p.mu.Unlock()
	p.wg.Wait()
}

func (p *DNSProxy) serve(conn *net.UDPConn) {
	defer p.wg.Done()
	buf := make([]byte, 1500)
	for {
		select {
		case <-p.done:
			return
		default:
		}
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-p.done:
				return
			default:
				continue
			}
		}
		if n < 12 {
			continue
		}
		msg := make([]byte, n)
		copy(msg, buf[:n])
		p.handle(conn, msg, from)
	}
}

func (p *DNSProxy) handle(conn *net.UDPConn, msg []byte, from *net.UDPAddr) {
	name, _, err := ParseQuestion(msg)
	if err == nil && p.Block != nil && p.Block(from.IP.String(), name) {
		if resp := NXDomain(msg); resp != nil {
			_, _ = conn.WriteToUDP(resp, from)
			logx.Debugf("dns", "blocked %s for %s", name, from.IP)
			return
		}
	}
	p.forward(conn, msg, from)
}

func (p *DNSProxy) forward(conn *net.UDPConn, msg []byte, from *net.UDPAddr) {
	up, err := net.ResolveUDPAddr("udp", p.Upstream)
	if err != nil {
		return
	}
	uc, err := net.DialUDP("udp", nil, up)
	if err != nil {
		return
	}
	defer uc.Close()
	_ = uc.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err := uc.Write(msg); err != nil {
		return
	}
	rb := make([]byte, 1500)
	n, err := uc.Read(rb)
	if err != nil || n < 12 {
		return
	}
	_, _ = conn.WriteToUDP(rb[:n], from)
}

// ---- DNS 报文解析 ----

// ParseQuestion 解析查询报文，返回域名与 question 段结束偏移。
func ParseQuestion(msg []byte) (string, int, error) {
	if len(msg) < 12 {
		return "", 0, errors.New("dns: message too short")
	}
	off := 12
	var labels []string
	for {
		if off >= len(msg) {
			return "", 0, errors.New("dns: truncated name")
		}
		l := int(msg[off])
		if l == 0 {
			off++
			break
		}
		if l&0xC0 != 0 {
			return "", 0, errors.New("dns: compression not supported in question")
		}
		off++
		if off+l > len(msg) {
			return "", 0, errors.New("dns: bad label length")
		}
		labels = append(labels, string(msg[off:off+l]))
		off += l
	}
	end := off + 4 // QTYPE(2) + QCLASS(2)
	if end > len(msg) {
		return "", 0, errors.New("dns: missing qtype/qclass")
	}
	return strings.ToLower(strings.Join(labels, ".")), end, nil
}

// NXDomain 基于查询报文构造 NXDOMAIN 应答（rcode=3，不携带 answer）。
func NXDomain(query []byte) []byte {
	_, end, err := ParseQuestion(query)
	if err != nil {
		return nil
	}
	resp := make([]byte, end)
	copy(resp, query[:end])
	binary.BigEndian.PutUint16(resp[2:], 0x8183) // QR=1,RD=1,RA=1,RCODE=3
	binary.BigEndian.PutUint16(resp[4:], 1)      // QDCOUNT
	binary.BigEndian.PutUint16(resp[6:], 0)      // ANCOUNT
	binary.BigEndian.PutUint16(resp[8:], 0)      // NSCOUNT
	binary.BigEndian.PutUint16(resp[10:], 0)     // ARCOUNT
	return resp
}

// DetectUpstream 从路由器自身的 resolv.conf 推导上游 DNS。
func DetectUpstream() string {
	candidates := []string{
		"/tmp/resolv.conf.d/resolv.conf.auto",
		"/tmp/resolv.conf.auto",
		"/etc/resolv.conf",
	}
	for _, path := range candidates {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if !strings.HasPrefix(line, "nameserver") {
				continue
			}
			fs := strings.Fields(line)
			if len(fs) < 2 {
				continue
			}
			ip := fs[1]
			f.Close()
			// 回环地址说明本机即 DNS 服务器，避免转发成环
			if strings.HasPrefix(ip, "127.") {
				return "223.5.5.5:53"
			}
			if strings.Contains(ip, ":") && !strings.Contains(ip, ".") {
				return "[" + ip + "]:53"
			}
			return ip + ":53"
		}
		f.Close()
	}
	return "223.5.5.5:53"
}
