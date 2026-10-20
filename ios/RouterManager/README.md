# iOS App（RouterManager）

SwiftUI 实现的路由器管理 App，支持局域网直连与云端远程管理。

## 工程创建

方式一（推荐，需 `brew install xcodegen`）：

```bash
xcodegen generate
open RouterManager.xcodeproj
```

方式二（手动）：

1. Xcode → New Project → iOS → App，语言 Swift，界面 SwiftUI，命名 `RouterManager`；
2. 删除模板生成的 `ContentView.swift`；
3. 把 `RouterManager/` 目录下的 `App`、`Models`、`Services`、`Views` 拖入工程；
4. 指定 `Info.plist` 为 `RouterManager/Info.plist`，entitlements 为 `RouterManager.entitlements`；
5. 把 `project.yml` 中的 `DEVELOPMENT_TEAM` / Bundle ID 换成你自己的。

## Capabilities

- **Push Notifications**（远程提醒）
- **Local Network**（局域网发现路由器，Info.plist 已声明 `NSLocalNetworkUsageDescription`）
- **Camera**（扫码配对，已声明 `NSCameraUsageDescription`）

## 调试建议

- 真机才能测推送与本地网络；模拟器可跑 UI 但无法注册 APNs；
- 调试阶段用局域网直连：确保手机与路由器在同一 WiFi，且 `192.168.1.1:8080` 可达；
- 若始终显示"云端中继"，检查路由器 `ragent.cloud.url` 与云端 `wss` 地址、证书是否有效。
