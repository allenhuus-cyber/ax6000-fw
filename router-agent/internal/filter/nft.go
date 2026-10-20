package filter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ax6000/ragent/internal/logx"
	"github.com/ax6000/ragent/internal/router"
)

// NFT 维护一张自包含的 nftables 表（inet ragent），不依赖 fw4 的具体规则集，
// 因而不会与固件自带防火墙互相干扰。
//
// 用途：
//  1. 被拉黑终端的转发流量直接 drop；
//  2. 配置了 App 限制的终端，其 DNS(53/UDP) 重定向到内置过滤代理，实现按域名拦截。
type NFT struct {
	Table      string
	ScriptPath string
	DNSPort    int
}

// NewNFT 创建 nftables 管理器。
func NewNFT(scriptPath string, dnsPort int) *NFT {
	if scriptPath == "" {
		scriptPath = "/etc/ragent/nft.conf"
	}
	if dnsPort == 0 {
		dnsPort = 5353
	}
	return &NFT{Table: "ragent", ScriptPath: scriptPath, DNSPort: dnsPort}
}

// Available 判断系统是否存在 nft 命令（AX6000 的 OpenWrt 23+/24+ 默认 nftables）。
func (n *NFT) Available() bool {
	_, err := router.Run("nft", "--version")
	return err == nil
}

// State 是需要落到 nftables 的集合内容。
type State struct {
	BlockedMACs []string // 禁止上网
	FilterMACs  []string // 需要 DNS 过滤
}

// Apply 重建 inet ragent 表。
func (n *NFT) Apply(st State) error {
	if !n.Available() {
		return fmt.Errorf("nft 命令不可用，无法下发过滤规则")
	}
	script := n.render(st)
	if err := os.MkdirAll(filepath.Dir(n.ScriptPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(n.ScriptPath, []byte(script), 0o644); err != nil {
		return err
	}
	// 已存在则先删除，保证规则与配置严格一致
	if _, err := router.Run("nft", "list", "table", "inet", n.Table); err == nil {
		if _, err := router.Run("nft", "delete", "table", "inet", n.Table); err != nil {
			logx.Warnf("nft", "delete table: %v", err)
		}
	}
	if out, err := router.Run("nft", "-f", n.ScriptPath); err != nil {
		return fmt.Errorf("加载 nft 规则失败: %v (%s)", err, out)
	}
	return nil
}

// Clear 删除整张表（代理退出或关闭过滤时调用）。
func (n *NFT) Clear() error {
	if !n.Available() {
		return nil
	}
	if _, err := router.Run("nft", "list", "table", "inet", n.Table); err != nil {
		return nil
	}
	_, err := router.Run("nft", "delete", "table", "inet", n.Table)
	return err
}

func (n *NFT) render(st State) string {
	var sb strings.Builder
	sb.WriteString("# 自动生成，请勿手工修改：由 ragentd 维护\n")
	sb.WriteString("table inet " + n.Table + " {\n")
	sb.WriteString("\tset blocked_macs {\n\t\ttype ether_addr\n")
	if len(st.BlockedMACs) > 0 {
		sb.WriteString("\t\telements = { " + strings.Join(st.BlockedMACs, ", ") + " }\n")
	}
	sb.WriteString("\t}\n")
	sb.WriteString("\tset filter_macs {\n\t\ttype ether_addr\n")
	if len(st.FilterMACs) > 0 {
		sb.WriteString("\t\telements = { " + strings.Join(st.FilterMACs, ", ") + " }\n")
	}
	sb.WriteString("\t}\n\n")

	sb.WriteString("\tchain prerouting {\n")
	sb.WriteString("\t\ttype nat hook prerouting priority -100; policy accept;\n")
	if len(st.FilterMACs) > 0 {
		sb.WriteString(fmt.Sprintf("\t\tether saddr @filter_macs udp dport 53 redirect to :%d\n", n.DNSPort))
		sb.WriteString(fmt.Sprintf("\t\tether saddr @filter_macs tcp dport 53 redirect to :%d\n", n.DNSPort))
	}
	sb.WriteString("\t}\n\n")

	sb.WriteString("\tchain forward {\n")
	sb.WriteString("\t\ttype filter hook forward priority 0; policy accept;\n")
	if len(st.BlockedMACs) > 0 {
		sb.WriteString("\t\tether saddr @blocked_macs drop\n")
	}
	sb.WriteString("\t}\n")
	sb.WriteString("}\n")
	return sb.String()
}
