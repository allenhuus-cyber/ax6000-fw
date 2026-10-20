// Package ws 提供一个零依赖的 RFC6455 WebSocket 实现（服务端 Upgrade + 客户端 Dial）。
//
// 之所以不引入 gorilla/websocket：固件侧需要交叉编译到 MT7986A(aarch64)，
// 同时希望本仓库在离线环境下 `go build ./...` 直接通过，不产生 go.sum。
//
// 支持：
//   - 文本/二进制消息、分片重组（continuation）
//   - ping/pong 自动应答
//   - close 握手
//   - 客户端帧掩码（RFC 要求客户端必须掩码）
package ws

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	OpContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	OpClose        = 0x8
	OpPing         = 0x9
	OpPong         = 0xA
)

const GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

var (
	ErrHandshake   = errors.New("ws: handshake failed")
	ErrClosed      = errors.New("ws: connection closed")
	ErrBadFrame    = errors.New("ws: malformed frame")
	ErrTooLarge    = errors.New("ws: message too large")
	ErrNotUpgraded = errors.New("ws: unexpected opcode")
)

// MaxMessageSize 单条消息上限，防止内存被打爆（8MB 足够承载终端列表/配置）。
const MaxMessageSize = 8 << 20

// Conn 是一条 WebSocket 连接。
type Conn struct {
	conn     net.Conn
	br       *bufio.Reader
	bw       *bufio.Writer
	isClient bool

	writeMu sync.Mutex
	readMu  sync.Mutex

	// 分片重组状态
	fragOpcode int
	fragBuf    []byte

	closed   bool
	closeErr error
}

// ---------- 服务端 ----------

// Upgrader 控制服务端握手行为。
type Upgrader struct {
	// ReadBuffer/WriteBuffer 大小，0 表示默认 4KB
	ReadBuffer  int
	WriteBuffer int
	// CheckOrigin 返回 false 则拒绝跨域请求；nil 表示全部允许
	CheckOrigin func(r *http.Request) bool
	// WriteTimeout 每次写消息的超时，0 表示不超时
	WriteTimeout time.Duration
}

func (u *Upgrader) bufSize(v int) int {
	if v <= 0 {
		return 4096
	}
	return v
}

// Upgrade 将 hijack 的 HTTP 连接升级为 WebSocket。
func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, ErrHandshake
	}
	if !headerContains(r.Header.Get("Connection"), "Upgrade") {
		return nil, ErrHandshake
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, ErrHandshake
	}
	if u.CheckOrigin != nil && !u.CheckOrigin(r) {
		return nil, ErrHandshake
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("ws: ResponseWriter does not support hijack")
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	if brw != nil {
		brw.Flush()
	}

	accept := computeAccept(key)
	var sb strings.Builder
	sb.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	sb.WriteString("Upgrade: websocket\r\n")
	sb.WriteString("Connection: Upgrade\r\n")
	sb.WriteString("Sec-WebSocket-Accept: " + accept + "\r\n")
	sb.WriteString("\r\n")
	if _, err := conn.Write([]byte(sb.String())); err != nil {
		conn.Close()
		return nil, err
	}

	c := &Conn{
		conn:     conn,
		br:       bufio.NewReaderSize(conn, u.bufSize(u.ReadBuffer)),
		bw:       bufio.NewWriterSize(conn, u.bufSize(u.WriteBuffer)),
		isClient: false,
	}
	return c, nil
}

func computeAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(GUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func headerContains(v, token string) bool {
	for _, part := range strings.Split(v, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// ---------- 客户端 ----------

// Dialer 用于客户端建连。
type Dialer struct {
	Timeout      time.Duration
	ReadBuffer   int
	WriteBuffer  int
	ExtraHeaders http.Header
}

// Dial 发起 ws:// 或 wss:// 连接。wss 需要传入 tls.Config 包装后的 net.Conn，
// 这里简化：scheme 为 wss 时使用默认 TLS 配置直连。
func (d *Dialer) Dial(rawURL string) (*Conn, *http.Response, error) {
	u, err := parseURL(rawURL)
	if err != nil {
		return nil, nil, err
	}
	timeout := d.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	addr := u.host + ":" + u.port
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, nil, err
	}
	if u.scheme == "wss" {
		conn, err = wrapTLS(conn, u.host)
		if err != nil {
			return nil, nil, err
		}
	}

	key, err := randomKey()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}

	var sb strings.Builder
	sb.WriteString("GET " + u.path + " HTTP/1.1\r\n")
	sb.WriteString("Host: " + u.host + "\r\n")
	sb.WriteString("Upgrade: websocket\r\n")
	sb.WriteString("Connection: Upgrade\r\n")
	sb.WriteString("Sec-WebSocket-Key: " + key + "\r\n")
	sb.WriteString("Sec-WebSocket-Version: 13\r\n")
	for k, vs := range d.ExtraHeaders {
		for _, v := range vs {
			sb.WriteString(k + ": " + v + "\r\n")
		}
	}
	sb.WriteString("\r\n")
	if _, err := conn.Write([]byte(sb.String())); err != nil {
		conn.Close()
		return nil, nil, err
	}

	br := bufio.NewReaderSize(conn, bufSizeOr(d.ReadBuffer))
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if resp.StatusCode != 101 {
		conn.Close()
		return nil, resp, ErrHandshake
	}
	if !strings.EqualFold(resp.Header.Get("Sec-WebSocket-Accept"), computeAccept(key)) {
		conn.Close()
		return nil, resp, ErrHandshake
	}
	c := &Conn{
		conn:     conn,
		br:       br,
		bw:       bufio.NewWriterSize(conn, bufSizeOr(d.WriteBuffer)),
		isClient: true,
	}
	return c, resp, nil
}

func bufSizeOr(v int) int {
	if v <= 0 {
		return 4096
	}
	return v
}

func randomKey() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// ---------- 读写 ----------

// frame 是一个完整的 WebSocket 帧（未做分片合并）。
type frame struct {
	opcode  int
	payload []byte
	fin     bool
}

func (c *Conn) readFrame() (frame, error) {
	h := make([]byte, 2)
	if _, err := io.ReadFull(c.br, h); err != nil {
		return frame{}, err
	}
	fin := h[0]&0x80 != 0
	opcode := int(h[0] & 0x0F)
	masked := h[1]&0x80 != 0
	length := uint64(h[1] & 0x7F)

	switch {
	case length == 126:
		b := make([]byte, 2)
		if _, err := io.ReadFull(c.br, b); err != nil {
			return frame{}, err
		}
		length = uint64(binary.BigEndian.Uint16(b))
	case length == 127:
		b := make([]byte, 8)
		if _, err := io.ReadFull(c.br, b); err != nil {
			return frame{}, err
		}
		length = binary.BigEndian.Uint64(b)
	}
	if length > MaxMessageSize {
		return frame{}, ErrTooLarge
	}
	if masked != c.isClient {
		// 服务端收到的帧必须来自客户端（有掩码）；客户端收到的帧必须无掩码
		return frame{}, ErrBadFrame
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(c.br, maskKey[:]); err != nil {
			return frame{}, err
		}
	}
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return frame{}, err
		}
		if masked {
			mask(payload, maskKey)
		}
	}
	return frame{opcode: opcode, payload: payload, fin: fin}, nil
}

func mask(b []byte, key [4]byte) {
	for i := range b {
		b[i] ^= key[i%4]
	}
}

// ReadMessage 读取一条完整消息（自动处理分片、ping/pong）。
// 返回 opcode（OpText / OpBinary）与负载。连接被关闭时返回 ErrClosed 或 io.EOF。
func (c *Conn) ReadMessage() (int, []byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	for {
		f, err := c.readFrame()
		if err != nil {
			if err == io.EOF {
				return 0, nil, io.EOF
			}
			return 0, nil, err
		}
		switch f.opcode {
		case OpContinuation:
			if c.fragOpcode == 0 {
				return 0, nil, ErrBadFrame
			}
			c.appendFrag(f.payload)
			if f.fin {
				msg := c.takeFrag()
				op := c.fragOpcode
				c.fragOpcode = 0
				return op, msg, nil
			}
		case OpText, OpBinary:
			if c.fragOpcode != 0 {
				return 0, nil, ErrBadFrame
			}
			if !f.fin {
				c.fragOpcode = f.opcode
				c.fragBuf = append(c.fragBuf[:0], f.payload...)
				continue
			}
			return f.opcode, f.payload, nil
		case OpPing:
			// 自动回 pong，不打断上层读循环
			_ = c.writeFrame(OpPong, f.payload)
			continue
		case OpPong:
			continue
		case OpClose:
			c.closed = true
			_ = c.writeFrame(OpClose, nil)
			return 0, nil, ErrClosed
		default:
			return 0, nil, ErrNotUpgraded
		}
	}
}

func (c *Conn) appendFrag(p []byte) {
	c.fragBuf = append(c.fragBuf, p...)
	if len(c.fragBuf) > MaxMessageSize {
		c.fragBuf = c.fragBuf[:MaxMessageSize]
	}
}

func (c *Conn) takeFrag() []byte {
	b := make([]byte, len(c.fragBuf))
	copy(b, c.fragBuf)
	c.fragBuf = c.fragBuf[:0]
	return b
}

// WriteMessage 写入一条消息（客户端自动加掩码）。
func (c *Conn) WriteMessage(opcode int, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrame(opcode, payload)
}

func (c *Conn) writeFrame(opcode int, payload []byte) error {
	if c.closed && opcode != OpClose {
		return ErrClosed
	}
	b0 := byte(0x80 | opcode) // 单帧发送，fin=1
	l := len(payload)
	var head []byte
	switch {
	case l < 126:
		head = []byte{b0, byte(l)}
	case l <= 0xFFFF:
		head = []byte{b0, 126, 0, 0}
		binary.BigEndian.PutUint16(head[2:], uint16(l))
	default:
		head = []byte{b0, 127, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint64(head[2:], uint64(l))
	}
	if _, err := c.bw.Write(head); err != nil {
		return err
	}
	if c.isClient && l > 0 {
		var key [4]byte
		if _, err := io.ReadFull(rand.Reader, key[:]); err != nil {
			return err
		}
		if _, err := c.bw.Write(key[:]); err != nil {
			return err
		}
		m := make([]byte, l)
		copy(m, payload)
		mask(m, key)
		if _, err := c.bw.Write(m); err != nil {
			return err
		}
	} else if l > 0 {
		if _, err := c.bw.Write(payload); err != nil {
			return err
		}
	}
	return c.bw.Flush()
}

// WriteText / WriteBinary 便捷方法。
func (c *Conn) WriteText(s string) error { return c.WriteMessage(OpText, []byte(s)) }
func (c *Conn) WriteBinary(b []byte) error {
	return c.WriteMessage(OpBinary, b)
}

// Ping 发送 ping 帧。
func (c *Conn) Ping(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrame(OpPing, payload)
}

// SetDeadline 透传到底层连接，用于控制心跳超时。
func (c *Conn) SetDeadline(t time.Time) error { return c.conn.SetDeadline(t) }

// LocalAddr / RemoteAddr
func (c *Conn) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *Conn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

// Close 发送 close 帧并关闭底层连接。
func (c *Conn) Close() error {
	c.writeMu.Lock()
	if !c.closed {
		c.closed = true
		_ = c.writeFrame(OpClose, []byte{0x03, 0xE8}) // 1000 normal closure
	}
	c.writeMu.Unlock()
	return c.conn.Close()
}
