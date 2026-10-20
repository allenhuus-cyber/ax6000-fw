// Package ws 是 RFC6455 WebSocket 服务端实现（零第三方依赖）。
// 与固件侧实现同源，这里仅保留服务端需要的部分。
package ws

import (
	"bufio"
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
	ErrHandshake = errors.New("ws: handshake failed")
	ErrClosed    = errors.New("ws: connection closed")
	ErrBadFrame  = errors.New("ws: malformed frame")
	ErrTooLarge  = errors.New("ws: message too large")
)

// MaxMessageSize 单条消息上限。
const MaxMessageSize = 8 << 20

// Conn 是一条 WebSocket 连接。
type Conn struct {
	conn net.Conn
	br   *bufio.Reader
	bw   *bufio.Writer

	writeMu sync.Mutex
	readMu  sync.Mutex

	fragOpcode int
	fragBuf    []byte
	closed     bool
}

// Upgrader 服务端握手配置。
type Upgrader struct {
	ReadBuffer   int
	WriteBuffer  int
	CheckOrigin  func(r *http.Request) bool
	Subprotocols []string
}

func (u *Upgrader) size(v int) int {
	if v <= 0 {
		return 4096
	}
	return v
}

// Upgrade 把 HTTP 连接升级为 WebSocket。
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
		return nil, errors.New("ws: hijack unsupported")
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	if brw != nil {
		_ = brw.Flush()
	}
	accept := computeAccept(key)
	var sb strings.Builder
	sb.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	sb.WriteString("Upgrade: websocket\r\n")
	sb.WriteString("Connection: Upgrade\r\n")
	sb.WriteString("Sec-WebSocket-Accept: " + accept + "\r\n")
	sb.WriteString("\r\n")
	if _, err := conn.Write([]byte(sb.String())); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &Conn{
		conn: conn,
		br:   bufio.NewReaderSize(conn, u.size(u.ReadBuffer)),
		bw:   bufio.NewWriterSize(conn, u.size(u.WriteBuffer)),
	}, nil
}

func computeAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(GUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func headerContains(v, token string) bool {
	for _, p := range strings.Split(v, ",") {
		if strings.EqualFold(strings.TrimSpace(p), token) {
			return true
		}
	}
	return false
}

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
	if !masked {
		return frame{}, ErrBadFrame // 客户端必须掩码
	}
	var key [4]byte
	if _, err := io.ReadFull(c.br, key[:]); err != nil {
		return frame{}, err
	}
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return frame{}, err
		}
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	return frame{opcode: opcode, payload: payload, fin: fin}, nil
}

// ReadMessage 读取一条完整消息，自动处理分片与 ping/pong。
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
			c.fragBuf = append(c.fragBuf, f.payload...)
			if f.fin {
				msg := make([]byte, len(c.fragBuf))
				copy(msg, c.fragBuf)
				op := c.fragOpcode
				c.fragBuf = c.fragBuf[:0]
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
			_ = c.writeFrame(OpPong, f.payload)
			continue
		case OpPong:
			continue
		case OpClose:
			c.closed = true
			return 0, nil, ErrClosed
		default:
			return 0, nil, ErrBadFrame
		}
	}
}

// WriteMessage 写入一条消息。
func (c *Conn) WriteMessage(opcode int, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrame(opcode, payload)
}

// WriteText 写入文本消息。
func (c *Conn) WriteText(s string) error { return c.WriteMessage(OpText, []byte(s)) }

func (c *Conn) writeFrame(opcode int, payload []byte) error {
	if c.closed && opcode != OpClose {
		return ErrClosed
	}
	l := len(payload)
	var head []byte
	switch {
	case l < 126:
		head = []byte{byte(0x80 | opcode), byte(l)}
	case l <= 0xFFFF:
		head = []byte{byte(0x80 | opcode), 126, 0, 0}
		binary.BigEndian.PutUint16(head[2:], uint16(l))
	default:
		head = []byte{byte(0x80 | opcode), 127, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint64(head[2:], uint64(l))
	}
	if _, err := c.bw.Write(head); err != nil {
		return err
	}
	if l > 0 {
		if _, err := c.bw.Write(payload); err != nil {
			return err
		}
	}
	return c.bw.Flush()
}

// Ping 发送 ping 帧。
func (c *Conn) Ping(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrame(OpPing, payload)
}

// RemoteAddr 返回对端地址。
func (c *Conn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

// SetDeadline 透传超时设置。
func (c *Conn) SetDeadline(t time.Time) error { return c.conn.SetDeadline(t) }

// Close 关闭连接。
func (c *Conn) Close() error {
	c.writeMu.Lock()
	if !c.closed {
		c.closed = true
		_ = c.writeFrame(OpClose, []byte{0x03, 0xE8})
	}
	c.writeMu.Unlock()
	return c.conn.Close()
}
