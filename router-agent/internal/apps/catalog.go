// Package apps 内置主流 App 识别清单。
//
// 每个 App 提供两套识别依据：
//  1. OAFNames —— 用于匹配 DPI 引擎(OpenAppFilter)特征库里的应用名；
//  2. Domains  —— 用于兜底引擎做域名拦截（可精确枚举的 App）。
//
// 以游戏为代表的 App 域名难以枚举，主要依赖 DPI；短视频/电商/视频类两者都可用。
package apps

import "strings"

// Category 应用分类。
type Category string

const (
	CatShortVideo Category = "short_video"
	CatVideo      Category = "video"
	CatSocial     Category = "social"
	CatGame       Category = "game"
	CatShopping   Category = "shopping"
	CatMusic      Category = "music"
	CatLive       Category = "live"
	CatReading    Category = "reading"
	CatTool       Category = "tool"
	CatOther      Category = "other"
)

// Def 一个可管控的 App 定义。
type Def struct {
	ID       string   // 稳定标识，跨端使用
	Name     string   // 中文名
	Category Category
	OAFNames []string // DPI 特征库中的名称（中文/英文别名）
	Domains  []string // 兜底域名（支持 *.example.com 通配）
}

// Catalog 全部内置 App。
var Catalog = []Def{
	{ID: "douyin", Name: "抖音", Category: CatShortVideo, OAFNames: []string{"抖音", "douyin", "aweme"},
		Domains: []string{"*.douyin.com", "*.douyinvod.com", "*.amemv.com", "*.iesdouyin.com", "*.snssdk.com", "*.byteimg.com"}},
	{ID: "kuaishou", Name: "快手", Category: CatShortVideo, OAFNames: []string{"快手", "kuaishou", "gifshow"},
		Domains: []string{"*.kuaishou.com", "*.gifshow.com", "*.ksyungslb.com", "*.kuaishouv.com"}},
	{ID: "wechat", Name: "微信", Category: CatSocial, OAFNames: []string{"微信", "wechat", "weixin"},
		Domains: []string{"*.weixin.qq.com", "*.wechat.com", "*.wxapp.qq.com", "*.qpic.cn"}},
	{ID: "qq", Name: "QQ", Category: CatSocial, OAFNames: []string{"QQ", "qq"},
		Domains: []string{"*.qzone.qq.com", "*.qqmail.com"}},
	{ID: "weibo", Name: "微博", Category: CatSocial, OAFNames: []string{"微博", "weibo", "sina"},
		Domains: []string{"*.weibo.com", "*.weibo.cn", "*.sinaimg.cn"}},
	{ID: "xiaohongshu", Name: "小红书", Category: CatSocial, OAFNames: []string{"小红书", "xiaohongshu"},
		Domains: []string{"*.xiaohongshu.com", "*.xhscdn.com"}},
	{ID: "zhihu", Name: "知乎", Category: CatReading, OAFNames: []string{"知乎", "zhihu"},
		Domains: []string{"*.zhihu.com", "*.zhimg.com"}},
	{ID: "bilibili", Name: "哔哩哔哩", Category: CatVideo, OAFNames: []string{"哔哩哔哩", "bilibili", "bili"},
		Domains: []string{"*.bilibili.com", "*.hdslb.com", "*.bilivideo.com", "*.biliapi.net"}},
	{ID: "iqiyi", Name: "爱奇艺", Category: CatVideo, OAFNames: []string{"爱奇艺", "iqiyi", "qiyi"},
		Domains: []string{"*.iqiyi.com", "*.qiyi.com", "*.iqiyipic.com"}},
	{ID: "youku", Name: "优酷", Category: CatVideo, OAFNames: []string{"优酷", "youku"},
		Domains: []string{"*.youku.com", "*.ykimg.com", "*.youku.com.cn"}},
	{ID: "tencentvideo", Name: "腾讯视频", Category: CatVideo, OAFNames: []string{"腾讯视频", "qqlive", "tencentvideo"},
		Domains: []string{"*.video.qq.com", "*.qqlive.qq.com"}},
	{ID: "qqmusic", Name: "QQ音乐", Category: CatMusic, OAFNames: []string{"QQ音乐", "qqmusic"},
		Domains: []string{"*.y.qq.com", "*.music.qq.com"}},
	{ID: "netease_music", Name: "网易云音乐", Category: CatMusic, OAFNames: []string{"网易云音乐", "netease", "163music"},
		Domains: []string{"*.music.163.com", "*.126.net"}},
	{ID: "taobao", Name: "淘宝", Category: CatShopping, OAFNames: []string{"淘宝", "taobao", "tmall"},
		Domains: []string{"*.taobao.com", "*.tmall.com", "*.taobaocdn.com"}},
	{ID: "jd", Name: "京东", Category: CatShopping, OAFNames: []string{"京东", "jingdong", "jd"},
		Domains: []string{"*.jd.com", "*.360buyimg.com"}},
	{ID: "pinduoduo", Name: "拼多多", Category: CatShopping, OAFNames: []string{"拼多多", "pinduoduo"},
		Domains: []string{"*.pinduoduo.com", "*.yangkeduo.com"}},
	{ID: "meituan", Name: "美团", Category: CatShopping, OAFNames: []string{"美团", "meituan", "dianping"},
		Domains: []string{"*.meituan.com", "*.dianping.com"}},
	{ID: "alipay", Name: "支付宝", Category: CatTool, OAFNames: []string{"支付宝", "alipay"},
		Domains: []string{"*.alipay.com", "*.alipayobjects.com"}},
	{ID: "wangzhe", Name: "王者荣耀", Category: CatGame, OAFNames: []string{"王者荣耀", "honor of kings", "sgame"},
		Domains: []string{"*.game.qq.com"}},
	{ID: "heping", Name: "和平精英", Category: CatGame, OAFNames: []string{"和平精英", "pubg", "game for peace"},
		Domains: nil},
	{ID: "yuanshen", Name: "原神", Category: CatGame, OAFNames: []string{"原神", "genshin", "mihoyo", "hoyoverse"},
		Domains: []string{"*.mihoyo.com", "*.hoyoverse.com", "*.yuanshen.com"}},
	{ID: "steam", Name: "Steam", Category: CatGame, OAFNames: []string{"Steam", "steam"},
		Domains: []string{"*.steampowered.com", "*.steamcommunity.com"}},
	{ID: "douyu", Name: "斗鱼", Category: CatLive, OAFNames: []string{"斗鱼", "douyu"},
		Domains: []string{"*.douyu.com", "*.douyucdn.cn"}},
	{ID: "huya", Name: "虎牙", Category: CatLive, OAFNames: []string{"虎牙", "huya"},
		Domains: []string{"*.huya.com", "*.huyacdn.com"}},
	{ID: "baidu", Name: "百度", Category: CatTool, OAFNames: []string{"百度", "baidu"},
		Domains: []string{"*.baidu.com", "*.bdstatic.com"}},
	{ID: "youtube", Name: "YouTube", Category: CatVideo, OAFNames: []string{"youtube", "YouTube"},
		Domains: []string{"*.youtube.com", "*.googlevideo.com", "*.youtu.be"}},
	{ID: "netflix", Name: "Netflix", Category: CatVideo, OAFNames: []string{"netflix", "Netflix"},
		Domains: []string{"*.netflix.com", "*.nflxvideo.net"}},
	{ID: "tiktok", Name: "TikTok", Category: CatShortVideo, OAFNames: []string{"tiktok", "TikTok"},
		Domains: []string{"*.tiktok.com", "*.tiktokv.com"}},
	{ID: "twitter", Name: "X(Twitter)", Category: CatSocial, OAFNames: []string{"twitter", "Twitter"},
		Domains: []string{"*.twitter.com", "*.twimg.com"}},
	{ID: "facebook", Name: "Facebook", Category: CatSocial, OAFNames: []string{"facebook", "Facebook"},
		Domains: []string{"*.facebook.com", "*.fbcdn.net"}},
	{ID: "instagram", Name: "Instagram", Category: CatSocial, OAFNames: []string{"instagram", "Instagram"},
		Domains: []string{"*.instagram.com", "*.cdninstagram.com"}},
}

// ByID 按 ID 查找。
func ByID(id string) (Def, bool) {
	for _, d := range Catalog {
		if d.ID == id {
			return d, true
		}
	}
	return Def{}, false
}

// Lookup 支持按 ID 或中文名查找（App 端可能传来中文名）。
func Lookup(key string) (Def, bool) {
	for _, d := range Catalog {
		if d.ID == key || d.Name == key {
			return d, true
		}
	}
	return Def{}, false
}

// DomainsOf 返回一组 App 的全部兜底域名（已去掉通配前缀）。
func DomainsOf(ids []string) []string {
	var out []string
	for _, id := range ids {
		d, ok := ByID(id)
		if !ok {
			continue
		}
		for _, dom := range d.Domains {
			out = append(out, strings.TrimPrefix(dom, "*."))
		}
	}
	return out
}

// MatchDomain 判断域名是否属于给定 App 集合。
// name 应为已完成解析的主机名（小写、无结尾点）。
func MatchDomain(name string, appIDs []string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	for _, id := range appIDs {
		d, ok := ByID(id)
		if !ok {
			continue
		}
		for _, dom := range d.Domains {
			dom = strings.ToLower(dom)
			if strings.HasPrefix(dom, "*.") {
				base := dom[2:]
				if name == base || strings.HasSuffix(name, "."+base) {
					return true
				}
				continue
			}
			if name == dom {
				return true
			}
		}
	}
	return false
}
