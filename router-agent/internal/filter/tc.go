package filter

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/ax6000/ragent/internal/logx"
	"github.com/ax6000/ragent/internal/router"
)

// TC 通过 htb 队列实现按 MAC 的下行限速（单位 kbps）。
//
// 依赖内核模块：kmod-sched-core、kmod-sched-htb、kmod-cls-basic（含 em_ether）。
// 若固件未包含这些模块，Apply 会返回明确错误，其余功能不受影响。
type TC struct {
	Dev   string // 出口设备，默认 br-lan（下行=LAN 侧出方向）
	start int    // classid 起始编号

	mu       sync.Mutex
	classIDs map[string]int
}

// NewTC 创建限速管理器，dev 为空时默认 br-lan。
func NewTC(dev string) *TC {
	if dev == "" {
		dev = "br-lan"
	}
	return &TC{Dev: dev, start: 10, classIDs: map[string]int{}}
}

// Restore 从持久化的 map 恢复 classid 分配，避免重建后规则错乱。
func (t *TC) Restore(saved map[string]int) {
	if saved == nil {
		return
	}
	t.mu.Lock()
	t.classIDs = saved
	t.mu.Unlock()
}

// Snapshot 导出当前分配，供持久化。
func (t *TC) Snapshot() map[string]int {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]int, len(t.classIDs))
	for k, v := range t.classIDs {
		out[k] = v
	}
	return out
}

// Available 检测 tc 命令是否存在。
func (t *TC) Available() bool {
	_, err := router.Run("tc", "qdisc", "show", "dev", t.Dev)
	return err == nil
}

func (t *TC) classID(mac string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if id, ok := t.classIDs[mac]; ok {
		return id
	}
	id := t.start + len(t.classIDs)
	t.classIDs[mac] = id
	return id
}

// Apply 设置某终端限速（kbps <= 0 表示解除限速）。
func (t *TC) Apply(mac string, kbps int) error {
	if !t.Available() {
		return fmt.Errorf("tc 不可用或设备 %s 不存在，限速功能需要 kmod-sched-htb", t.Dev)
	}
	if err := t.ensureRoot(); err != nil {
		return err
	}
	id := t.classID(mac)
	macPretty := router.PrettyMAC(mac)
	if kbps <= 0 {
		_, _ = router.Run("tc", "filter", "del", "dev", t.Dev, "parent", "1:", "prio", "1", "handle", "::"+strconv.Itoa(id), "basic")
		_, _ = router.Run("tc", "class", "del", "dev", t.Dev, "classid", "1:"+strconv.Itoa(id))
		return nil
	}
	rate := strconv.Itoa(kbps) + "kbit"
	if _, err := router.Run("tc", "class", "add", "dev", t.Dev, "parent", "1:",
		"classid", "1:"+strconv.Itoa(id), "htb", "rate", rate, "ceil", rate); err != nil {
		return fmt.Errorf("创建限速队列失败: %v", err)
	}
	// 按源 MAC 匹配：受控终端下行流量从 br-lan 发出，源 MAC 是被限速设备
	if _, err := router.Run("tc", "filter", "add", "dev", t.Dev, "parent", "1:",
		"protocol", "all", "prio", "1", "handle", "::"+strconv.Itoa(id), "basic",
		"match", "ether(src "+macPretty+")", "flowid", "1:"+strconv.Itoa(id)); err != nil {
		return fmt.Errorf("绑定终端失败(需要 kmod-cls-basic 与 em_ether): %v", err)
	}
	return nil
}

// ensureRoot 建立 htb 根队列（幂等）。
func (t *TC) ensureRoot() error {
	show, err := router.Run("tc", "qdisc", "show", "dev", t.Dev)
	if err != nil {
		return err
	}
	if strings.Contains(show, "htb") {
		return nil
	}
	if _, err := router.Run("tc", "qdisc", "add", "dev", t.Dev, "root", "handle", "1:", "htb", "default", "20"); err != nil {
		return fmt.Errorf("创建根队列失败: %v", err)
	}
	if _, err := router.Run("tc", "class", "add", "dev", t.Dev, "parent", "1:",
		"classid", "1:20", "htb", "rate", "1000mbit", "ceil", "1000mbit"); err != nil {
		logx.Warnf("tc", "default class: %v", err)
	}
	return nil
}

// Clear 移除根队列（慎用，会一并清除所有限速）。
func (t *TC) Clear() error {
	if !t.Available() {
		return nil
	}
	_, err := router.Run("tc", "qdisc", "del", "dev", t.Dev, "root")
	return err
}
