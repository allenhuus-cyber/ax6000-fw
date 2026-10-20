# AX6000 定制固件工程（红米 AX6000 / MT7986A）

从零搭建的一套**可自主控制的路由固件 + iOS App**，围绕三个目标：

| 需求 | 实现 |
|---|---|
| ① 多种上网方式 | PPPoE 拨号 / DHCP / 静态 IP / 无线中继(WISP)，UCI + netifd 生效，App 端一键切换 |
| ② 终端管理 | 实时发现（hostapd + DHCP + ARP + FDB）、上下线事件、拉黑、限速、**主流 App 访问限制（DPI + 域名双引擎）** |
| ③ iOS App 管理 | SwiftUI App：局域网直连 + 云端中继双通道，终端上线经 APNs 推送提醒 |

> 说明：固件侧的"从 0"是指**控制平面从零自研**（协议、API、策略引擎、App），
> 而不是从裸机重写内核与无线驱动——MT7986 的无线/交换芯片需要大量闭源组件，
> 业界通用做法是在 OpenWrt/ImmortalWrt 之上构建。

## 架构

```text
┌──────────────┐   局域网 ws/http     ┌────────────────────────────┐
│  iOS App     │◄───────────────────►│  路由器 ragentd (Go,arm64)  │
│  (SwiftUI)   │                     │  ├ 上网方式 (ubus/uci)      │
└──────┬───────┘                     │  ├ 终端管理 (hosts)         │
       │ 远程 wss                     │  └ 过滤引擎 (oaf/nft/dns)  │
       │                             └─────────────┬──────────────┘
┌──────▼───────┐   设备长连接(ws)                 │ 上报事件
│  cloudsrv    │◄─────────────────────────────────┘
│  (Go)        │──► APNs ──► iPhone 推送
└──────────────┘
```

- 设备**主动外连**云端，家庭宽带无需公网 IP、无需端口映射；
- 云端只是管道：App 的 JWT 由**设备**签发/校验，云端无法伪造；
- 断网在家时 App 自动走局域网直连，外出自动切云端。

## 目录

```text
router-agent/     路由器端控制代理（Go，零第三方依赖）
  cmd/ragentd/            入口
  internal/router/        ubus / uci / 无线 / 系统状态
  internal/hosts/         终端发现、上下线事件、策略
  internal/filter/        DPI(OpenAppFilter) + nftables + DNS 代理 + tc 限速
  internal/apps/          主流 App 识别清单（30+）
  internal/api/           REST + WebSocket + JWT
  internal/cloudlink/     到云端的长连接
cloud/            云端中继服务（Go，零第三方依赖）
  internal/apns/          APNs HTTP/2 + ES256（标准库实现）
ios/RouterManager/       iOS App（SwiftUI）
build/            Docker 构建环境与一键脚本
firmware/         OpenWrt 包定义、init 脚本、UCI 配置
docs/             刷机与接口文档
```

## 快速开始

### 1. 编译（需要 Docker，或任意 Linux + Go 1.22）

```bash
docker build -t ax6000-builder -f build/Dockerfile .
docker run --rm -v $PWD:/work ax6000-builder agent      # dist/ragentd（分钟级）
docker run --rm -v $PWD:/work ax6000-builder cloud      # dist/cloudsrv
docker run --rm -v $PWD:/work ax6000-builder firmware   # 完整固件（小时级）
```

不用 Docker 时：

```bash
cd router-agent && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o ../dist/ragentd ./cmd/ragentd
cd ../cloud      && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../dist/cloudsrv ./cmd/cloudsrv
```

**零第三方依赖**：WebSocket、JWT、APNs 全部用标准库实现，`go build ./...` 离线可直接通过。

### 2. 刷机

见 [docs/FLASH.md](docs/FLASH.md)。

### 3. 部署云端

```bash
cd cloud/deploy
cp config.json.example config.json   # 填 APNs 的 team_id/key_id/topic 与 p8 路径
mkdir -p data apns && cp ~/AuthKey_XXXX.p8 apns/
docker compose up -d
```

路由器侧开启：

```sh
uci set ragent.cloud.enabled='1'
uci set ragent.cloud.url='wss://你的域名/device'
uci set ragent.cloud.token='设备令牌'
uci commit ragent && /etc/init.d/ragentd restart
```

### 4. iOS App

```bash
cd ios/RouterManager
xcodegen generate          # 或手动新建 iOS App 工程并拖入 RouterManager/ 目录
open RouterManager.xcodeproj
```

Xcode 中打开 **Push Notifications**、**Local Network**、**Camera(扫码)** 权限，
真机运行后输入路由器管理账号完成配对（或扫描 `ragent-pair` 生成的二维码）。

## 功能实现要点

### 上网方式
- PPPoE：`network.wan.proto=pppoe` + 账号/服务名/MTU；
- 静态：IP/掩码/网关/DNS；
- 无线中继：自动新建 `mode=sta` 的无线接口 + `wwan` 网络接口，并把 `wwan` 移入防火墙 wan 区（路由模式）；
- 修改后 `service network reload`，4 秒后向 App 推送 `wan.change`。

### 终端管理
- 数据源：`hostapd` 的 ubus 客户端表 + DHCP 租约（dnsmasq/odhcpd）+ `/proc/net/arp` + 桥接 FDB；
- 在线判定：无线以关联状态为准（90s 未出现判定离线），有线以 ARP 为准（300s）；
- 事件：`host.new` / `host.online` / `host.offline` → 本地 WebSocket 广播 + 云端 → APNs。

### App 访问限制
1. **DPI 主引擎**：OpenAppFilter（内核 `appfilter` 模块 + `oafd`），按 MAC 下发应用规则，可识别王者荣耀、和平精英等无法靠域名枚举的应用；
2. **域名兜底引擎**：内置 DNS 代理（`internal/filter/dnsproxy.go`）+ nftables 把受控终端的 53 端口重定向过来，命中黑名单返回 NXDOMAIN；
3. 两个引擎对同一终端同时生效；`GET /api/v1/apps` 会返回每个 App 当前可用哪些引擎。

### 限速 / 拉黑
- 拉黑：nftables 自包含表 `inet ragent`，`ether saddr @blocked_macs drop`（不干扰固件自带防火墙）；
- 限速：`tc htb` + `cls_basic` 按源 MAC 分类（需要 `kmod-sched-htb`、`kmod-cls-basic`）。

## 安全设计

- 配对凭证（secret）32 字节随机，仅保存在 iOS Keychain；
- 全部接口使用设备签发的 HS256 JWT（12 小时有效）；
- 云端不持有设备密钥，转发时透传 App 的 JWT 由设备侧校验；
- 首次配对需路由器管理账号密码（经 ubus rpcd session 校验）；
- 关闭远程时（`ragent.cloud.enabled=0`）所有控制仅局域网可达。

## 已知限制

- DoH/DoT（443/853）与 IP 直连无法被域名引擎拦截，只能由 DPI 覆盖；
- 限速依赖 `cls_basic`/`em_ether` 内核模块，缺失时 `GET /api/v1/apps` 的 `engines.tc` 为 false；
- OpenAppFilter 需与内核版本匹配编译（构建脚本已集成该 feed）；
- 推送需要 Apple 开发者账号（.p8 密钥）+ 云端服务器。

## 许可证

自研代码可自由使用；集成的第三方项目（OpenWrt/ImmortalWrt、OpenAppFilter）遵循各自许可证。
