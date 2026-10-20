package filter

import (
	"encoding/xml"
	"os"
	"strconv"
	"strings"

	"github.com/ax6000/ragent/internal/apps"
	"github.com/ax6000/ragent/internal/logx"
	"github.com/ax6000/ragent/internal/router"
)

// OAF 是 OpenAppFilter（DPI 应用识别）引擎的适配器。
//
// 组成：
//   - 内核模块 appfilter：在 netfilter 上做应用识别与丢包；
//   - 用户态 oafd：读取 /etc/config/appfilter 的规则并通过 netlink 下发内核。
//
// 不同版本的 OAF 在 UCI 键名上存在差异，这里统一抽象到 OAFOptionKeys，
// 默认值按上游常见写法，可通过 /etc/config/ragent 覆盖。
type OAF struct {
	Keys    OAFOptionKeys
	nameToID map[string]int // 特征库中的应用名 -> appid
	idToName map[int]string
}

// OAFOptionKeys OAF 规则段的 UCI 键名。
type OAFOptionKeys struct {
	Section string // 段类型，默认 "rule"
	MAC     string // 默认 "mac"
	Apps    string // 默认 "app_list"
	Action  string // 默认 "action"
	Enable  string // 默认 "enable"
	DenyVal string // 默认 "deny"
}

// DefaultOAFKeys 上游 OpenAppFilter 的常见键名。
func DefaultOAFKeys() OAFOptionKeys {
	return OAFOptionKeys{
		Section: "rule",
		MAC:     "mac",
		Apps:    "app_list",
		Action:  "action",
		Enable:  "enable",
		DenyVal: "deny",
	}
}

// 特征库候选路径（不同版本位置不同）。
var oafFeaturePaths = []string{
	"/etc/appfilter/appfilter.xml",
	"/etc/appfilter/feature.xml",
	"/usr/share/appfilter/appfilter.xml",
	"/etc/config/appfilter_feature.xml",
}

// OAFAvailable 判断本机是否安装了 OpenAppFilter。
func OAFAvailable() bool {
	for _, p := range []string{"/etc/init.d/appfilter", "/usr/sbin/oafd", "/etc/config/appfilter"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// NewOAF 加载 OAF 引擎并解析特征库，失败返回 nil。
func NewOAF() *OAF {
	if !OAFAvailable() {
		return nil
	}
	o := &OAF{
		Keys:     DefaultOAFKeys(),
		nameToID: map[string]int{},
		idToName: map[int]string{},
	}
	o.loadFeature()
	logx.Infof("oaf", "loaded %d apps from feature library", len(o.nameToID))
	return o
}

// oafFeatureXML 对应 <appfilter><app><appid>..</appid><name>..</name></app>...</appfilter>
type oafFeatureXML struct {
	XMLName xml.Name `xml:"appfilter"`
	Apps    []struct {
		AppID int    `xml:"appid"`
		Name  string `xml:"name"`
	} `xml:"app"`
}

func (o *OAF) loadFeature() {
	for _, p := range oafFeaturePaths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f oafFeatureXML
		if err := xml.Unmarshal(b, &f); err != nil {
			logx.Warnf("oaf", "parse %s: %v", p, err)
			continue
		}
		for _, a := range f.Apps {
			if a.Name == "" {
				continue
			}
			o.nameToID[strings.ToLower(a.Name)] = a.AppID
			o.idToName[a.AppID] = a.Name
		}
		return
	}
	logx.Warnf("oaf", "feature library not found, falling back to builtin catalog")
}

// ResolveIDs 把我们内部的 app id 列表翻译成 OAF 的 appid 列表。
// 先按中文名/别名精确匹配特征库，未命中时尝试包含匹配。
func (o *OAF) ResolveIDs(ids []string) []int {
	var out []int
	for _, id := range ids {
		def, ok := apps.ByID(id)
		if !ok {
			continue
		}
		found := false
		for _, name := range def.OAFNames {
			if appid, ok := o.nameToID[strings.ToLower(name)]; ok {
				out = append(out, appid)
				found = true
				break
			}
		}
		if found {
			continue
		}
		// 包含匹配兜底：特征库名称形如 "抖音短视频"
		for name, appid := range o.nameToID {
			for _, alias := range def.OAFNames {
				if strings.Contains(name, strings.ToLower(alias)) {
					out = append(out, appid)
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	return out
}

// Catalog 返回本引擎可识别的 App（与内置目录取交集，附带 OAF appid）。
func (o *OAF) Catalog() map[string]int {
	out := map[string]int{}
	for _, def := range apps.Catalog {
		ids := o.ResolveIDs([]string{def.ID})
		if len(ids) > 0 {
			out[def.ID] = ids[0]
		}
	}
	return out
}

// Apply 写入 per-MAC 规则并重启 oafd 生效。
func (o *OAF) Apply(mac string, appIDs []string) error {
	// 1) 删除该 MAC 已有的规则段
	for _, sec := range o.ruleSections() {
		if strings.EqualFold(router.UciGet("appfilter."+sec+"."+o.Keys.MAC), mac) {
			_ = router.UciDelete("appfilter." + sec)
		}
	}
	if len(appIDs) == 0 {
		_ = router.UciCommit("appfilter")
		return o.reload()
	}

	// 2) 新增规则段
	sec, err := router.UciAdd("appfilter", o.Keys.Section)
	if err != nil {
		return err
	}
	sec = strings.TrimSpace(sec)
	sets := map[string]string{
		o.Keys.MAC:    mac,
		o.Keys.Apps:   joinInts(o.ResolveIDs(appIDs)),
		o.Keys.Action: o.Keys.DenyVal,
		o.Keys.Enable: "1",
	}
	for k, v := range sets {
		if err := router.UciSet("appfilter."+sec+"."+k, v); err != nil {
			return err
		}
	}
	if err := router.UciCommit("appfilter"); err != nil {
		return err
	}
	return o.reload()
}

func (o *OAF) reload() error {
	if _, err := os.Stat("/etc/init.d/appfilter"); err == nil {
		return router.RestartService("appfilter")
	}
	return nil
}

// ruleSections 列出 /etc/config/appfilter 中所有规则段（形如 appfilter.@rule[0]）。
func (o *OAF) ruleSections() []string {
	m, err := router.UciShow("appfilter")
	if err != nil {
		return nil
	}
	var out []string
	for k, v := range m {
		if v != o.Keys.Section {
			continue
		}
		parts := strings.Split(k, ".")
		if len(parts) == 2 {
			out = append(out, parts[1])
		}
	}
	return out
}

func joinInts(in []int) string {
	var sb strings.Builder
	for i, v := range in {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.Itoa(v))
	}
	return sb.String()
}
