// Package router 封装与 OpenWrt 系统层的交互：ubus、uci、无线、防火墙、系统状态采集。
// 所有对系统的改动都收敛在这里，便于在无路由器环境下用 mock 做单元测试。
package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ExecTimeout 单条系统命令的默认超时。
const ExecTimeout = 8 * time.Second

// Run 执行命令并返回标准输出（已 TrimSpace）。
func Run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ExecTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		out := strings.TrimSpace(stdout.String())
		errText := strings.TrimSpace(stderr.String())
		if errText != "" {
			return out, fmt.Errorf("%s %v: %v: %s", name, args, err, errText)
		}
		return out, fmt.Errorf("%s %v: %v", name, args, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// RunJSON 执行命令并把 stdout 解析为 JSON。
func RunJSON(v interface{}, name string, args ...string) error {
	out, err := Run(name, args...)
	if err != nil {
		return err
	}
	if out == "" {
		return errors.New("empty json output")
	}
	return json.Unmarshal([]byte(out), v)
}

// ---- ubus ----

// UbusCall 调用 ubus 方法，params 为 nil 时传空对象。
func UbusCall(obj, method string, params interface{}) (map[string]interface{}, error) {
	payload := "{}"
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		payload = string(b)
	}
	out, err := Run("ubus", "-t", "5", "call", obj, method, payload)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return map[string]interface{}{}, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return nil, fmt.Errorf("ubus %s.%s: bad json: %v", obj, method, err)
	}
	return m, nil
}

// UbusList 返回指定 ubus 对象暴露的方法名列表（ubus -v list <obj>）。
func UbusList(obj string) ([]string, error) {
	out, err := Run("ubus", "-v", "list", obj)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// 形如：`get_clients': { } 或 "get_clients": { }
		line = strings.Trim(line, "`")
		if i := strings.Index(line, ":"); i > 0 {
			name := strings.Trim(line[:i], `"' `)
			if name != "" && !strings.HasPrefix(name, obj) {
				names = append(names, name)
			}
		}
	}
	return names, nil
}

// ---- uci ----

// UciGet 读取 UCI 值；不存在时返回空字符串。
func UciGet(key string) string {
	out, err := Run("uci", "get", key)
	if err != nil {
		return ""
	}
	return strings.Trim(out, `"'`)
}

// UciSet 设置 UCI 值。
func UciSet(key, value string) error {
	_, err := Run("uci", "set", key+"="+value)
	return err
}

// UciDelete 删除 UCI 配置段/选项。
func UciDelete(key string) error {
	_, err := Run("uci", "delete", key)
	return err
}

// UciCommit 提交配置（默认提交指定文件）。
func UciCommit(config string) error {
	if config == "" {
		_, err := Run("uci", "commit")
		return err
	}
	_, err := Run("uci", "commit", config)
	return err
}

// UciAdd 添加一个匿名/命名段，返回段名。
func UciAdd(config, sectionType string) (string, error) {
	return Run("uci", "add", config, sectionType)
}

// UciSetNamed 创建命名段并设置类型。
func UciSetNamed(config, name, sectionType string) error {
	return UciSet(config+"."+name, sectionType)
}

// UciShow 导出某段配置为 map（key 为 option 名）。
func UciShow(config string) (map[string]string, error) {
	out, err := Run("uci", "-q", "show", config)
	if err != nil {
		return nil, err
	}
	res := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		key := line[:eq]
		val := strings.Trim(line[eq+1:], `"'`)
		res[key] = val
	}
	return res, nil
}

// ---- 服务控制 ----

// ReloadNetwork 应用网络配置（不重启整机）。
func ReloadNetwork() error {
	_, err := Run("service", "network", "reload")
	if err != nil {
		// 部分固件没有 service 包装，回退到 /etc/init.d
		_, err = Run("/etc/init.d/network", "reload")
	}
	return err
}

// RestartService 重启指定 init.d 服务。
func RestartService(name string) error {
	if _, err := Run("service", name, "restart"); err != nil {
		_, err = Run("/etc/init.d/"+name, "restart")
		return err
	}
	return nil
}

// ReloadService 重载指定 init.d 服务。
func ReloadService(name string) error {
	if _, err := Run("service", name, "reload"); err != nil {
		_, err = Run("/etc/init.d/"+name, "reload")
		return err
	}
	return nil
}
