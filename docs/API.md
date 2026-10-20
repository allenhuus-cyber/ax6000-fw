# 控制接口文档 v1

基础路径：`http://<路由器IP>:8080/api/v1`（局域网）或云端 `https://<cloud>/api/v1/rpc`（转发）。
除 `POST /auth/login` 与 `POST /pair` 外，全部需要 `Authorization: Bearer <token>`。

云端转发时，请求体为：

```json
{ "device_id": "ax6-xxxxxxxx", "method": "GET", "path": "/api/v1/hosts", "body": { } }
```

## 认证与配对

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/auth/login` | body `{device_id, secret, client}` → `{token, expires_in, device_id, name}` |
| POST | `/pair` | body `{username, password}`（路由器管理账号）→ `{device_id, secret, name, lan_addr, api_port}` |

## 状态

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/status` | 路由器状态：uptime、CPU、内存、LAN 地址、WAN 状态、终端数量 |

## 上网方式

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/network/wan` | 当前上网方式与参数 |
| PUT | `/network/wan` | 设置上网方式，见下 |
| GET | `/network/lan` | LAN 地址与 DHCP 配置 |
| PUT | `/network/lan` | 修改 LAN 配置 |
| GET | `/network/scan?radio=radio1` | 扫描周边热点（无线中继用） |

`PUT /network/wan` 请求体：

```json
{
  "mode": "pppoe",              // pppoe | dhcp | static | relay
  "username": "宽带账号",
  "password": "宽带密码",
  "ip": "", "netmask": "", "gateway": "", "dns": ["223.5.5.5"],  // static
  "relay_ssid": "", "relay_password": "", "relay_band": "5g",    // relay
  "mtu": 1492,
  "mac_clone": "",
  "service_name": ""
}
```

## 终端管理

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/hosts` | 全部终端（含离线） |
| GET | `/hosts/{mac}` | 单个终端 |
| POST | `/hosts/{mac}` | 修改 `{alias, blocked, rate_limit_kbps, notify}` |
| POST | `/hosts/{mac}/apps` | 设置禁止的 App：`{apps:["douyin","wangzhe"]}` |

## 应用管控

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/apps` | 可管控 App 清单与引擎可用性 `{apps:[...], engines:{oaf,nft,tc,dns}}` |

## 推送

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/notify/register` | 注册 APNs token：`{apns_token, device_id, app_id, sandbox, notify_new_host, notify_offline}` |

## 事件流

`GET /api/v1/ws?token=<jwt>`（局域网）或 `GET wss://<cloud>/app?device_id=...`（云端）

```json
{"type":"event","name":"host.online","ts":1750000000,
 "data":{"host":{"mac":"aabbccddeeff","ip":"192.168.1.23","online":true,"alias":"小米13"}}}
```

事件名：`host.new`（新设备）、`host.online`、`host.offline`、`wan.change`、`filter.apply`。
