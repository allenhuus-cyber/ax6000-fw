// Package logx 提供轻量分级日志（写入 stderr，由 procd 收集到 logread）。
package logx

import (
	"fmt"
	"log"
	"os"
	"sync"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var (
	mu     sync.RWMutex
	level  = LevelInfo
	logger = log.New(os.Stderr, "", 0)
)

func SetLevel(l Level) {
	mu.Lock()
	level = l
	mu.Unlock()
}

func SetLevelString(s string) {
	switch s {
	case "debug":
		SetLevel(LevelDebug)
	case "warn":
		SetLevel(LevelWarn)
	case "error":
		SetLevel(LevelError)
	default:
		SetLevel(LevelInfo)
	}
}

func logf(l Level, tag, format string, args ...interface{}) {
	mu.RLock()
	cur := level
	mu.RUnlock()
	if l < cur {
		return
	}
	var name string
	switch l {
	case LevelDebug:
		name = "DEBUG"
	case LevelInfo:
		name = "INFO "
	case LevelWarn:
		name = "WARN "
	case LevelError:
		name = "ERROR"
	}
	msg := fmt.Sprintf(format, args...)
	logger.Printf("%s %s %s", name, tag, msg)
}

func Debugf(tag, format string, args ...interface{}) { logf(LevelDebug, tag, format, args...) }
func Infof(tag, format string, args ...interface{})  { logf(LevelInfo, tag, format, args...) }
func Warnf(tag, format string, args ...interface{})  { logf(LevelWarn, tag, format, args...) }
func Errorf(tag, format string, args ...interface{}) { logf(LevelError, tag, format, args...) }
