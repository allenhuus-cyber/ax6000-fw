// Package store 提供单文件 JSON 持久化（终端备注、限速、App 黑名单、设备身份等）。
// 路由器上不适合引入数据库，文件足够；写入采用「临时文件 + rename」保证原子性。
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// File 是一个带互斥锁的 JSON 文件存储。
type File struct {
	path string
	mu   sync.RWMutex
	data map[string]interface{}
}

// ErrNotFound 键不存在。
var ErrNotFound = errors.New("key not found")

// Open 打开（或创建）JSON 文件存储。
func Open(path string) (*File, error) {
	f := &File{path: path, data: map[string]interface{}{}}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return f, nil
		}
		return nil, err
	}
	if len(b) > 0 {
		_ = json.Unmarshal(b, &f.data)
	}
	if f.data == nil {
		f.data = map[string]interface{}{}
	}
	return f, nil
}

// Get 读取键并反序列化到 v。
func (f *File) Get(key string, v interface{}) error {
	f.mu.RLock()
	raw, ok := f.data[key]
	f.mu.RUnlock()
	if !ok {
		return ErrNotFound
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Set 写入任意可序列化值并落盘。
func (f *File) Set(key string, v interface{}) error {
	f.mu.Lock()
	f.data[key] = v
	snapshot := make(map[string]interface{}, len(f.data))
	for k, val := range f.data {
		snapshot[k] = val
	}
	f.mu.Unlock()
	return f.flush(snapshot)
}

// Delete 删除键。
func (f *File) Delete(key string) error {
	f.mu.Lock()
	delete(f.data, key)
	snapshot := make(map[string]interface{}, len(f.data))
	for k, val := range f.data {
		snapshot[k] = val
	}
	f.mu.Unlock()
	return f.flush(snapshot)
}

// Keys 返回所有键。
func (f *File) Keys() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]string, 0, len(f.data))
	for k := range f.data {
		out = append(out, k)
	}
	return out
}

func (f *File) flush(snapshot map[string]interface{}) error {
	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(f.path)
	tmp := filepath.Join(dir, ".tmp-"+filepath.Base(f.path))
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, f.path); err != nil {
		return err
	}
	_ = os.Chtimes(f.path, time.Now(), time.Now())
	return nil
}
