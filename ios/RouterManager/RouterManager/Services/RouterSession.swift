import Foundation
import UserNotifications
import SwiftUI

/// App 的核心状态机：管理凭证、连接模式、数据刷新与事件处理。
@MainActor
final class RouterSession: ObservableObject {
    private static let identityKey = "router.identity"
    private static let pushTokenKey = "router.pushToken"

    @Published var identity: RouterIdentity?
    @Published var status: RouterStatus?
    @Published var hosts: [Host] = []
    @Published var apps: [AppInfo] = []
    @Published var wanConfig: WANConfig?
    @Published var lanConfig: LANConfig?
    @Published var mode: ConnectionMode = .lan
    @Published var connected = false
    @Published var isLoading = false
    @Published var errorMessage: String?
    @Published var notifyEnabled = true

    private var client: APIClient?
    private var ws: WebSocketClient?
    private var refreshTimer: Timer?

    // MARK: - 生命周期

    func bootstrap() {
        guard let id = KeychainStore.load(RouterIdentity.self, account: Self.identityKey) else { return }
        identity = id
        client = APIClient(identity: id, mode: .lan)
        setupWebSocket()
        Task { await connectAndLoad() }
        startAutoRefresh()
        registerPushIfNeeded()
    }

    private func setupWebSocket() {
        guard let client else { return }
        let ws = WebSocketClient { [weak self] in self?.client?.wsURL }
        ws.onStateChange = { [weak self] up in
            Task { @MainActor in self?.connected = up }
        }
        ws.onEvent = { [weak self] event in
            Task { @MainActor in self?.handleEvent(event) }
        }
        self.ws = ws
    }

    /// 连接（自动选路）并拉取全部数据
    func connectAndLoad() async {
        guard let client else { return }
        isLoading = true
        defer { isLoading = false }
        do {
            mode = await client.refreshMode()
            _ = try await client.login()
            ws?.start()
            await refresh()
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func refresh() async {
        guard let client else { return }
        do {
            async let st = client.status()
            async let hs = client.hosts()
            async let ap = client.apps()
            status = try await st
            hosts = try await hs
            apps = try await ap
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func startAutoRefresh() {
        refreshTimer?.invalidate()
        refreshTimer = Timer.scheduledTimer(withTimeInterval: 30, repeats: true) { [weak self] _ in
            Task { @MainActor in
                await self?.refresh()
            }
        }
    }

    // MARK: - 配对 / 解绑

    func savePairing(_ info: PairingInfo, cloudURL: String? = nil) {
        let port = Int(info.apiPort.trimmingCharacters(in: CharacterSet(charactersIn: ": "))) ?? 8080
        let id = RouterIdentity(
            deviceId: info.deviceId,
            secret: info.secret,
            name: info.name,
            lanHost: info.lanAddr.isEmpty ? "192.168.1.1" : info.lanAddr,
            lanPort: port,
            cloudBaseURL: cloudURL
        )
        KeychainStore.save(id, for: Self.identityKey)
        identity = id
        client = APIClient(identity: id, mode: .lan)
        setupWebSocket()
        Task { await connectAndLoad() }
        startAutoRefresh()
        registerPushIfNeeded()
    }

    func unbind() {
        ws?.stop()
        refreshTimer?.invalidate()
        KeychainStore.delete(account: Self.identityKey)
        identity = nil
        client = nil
        status = nil
        hosts = []
    }

    // MARK: - 操作

    func loadNetworkConfig() async {
        guard let client else { return }
        do {
            wanConfig = try await client.wanConfig()
            lanConfig = try await client.lanConfig()
        } catch { errorMessage = error.localizedDescription }
    }

    func applyWAN(_ cfg: WANConfig) async {
        guard let client else { return }
        do {
            try await client.applyWAN(cfg)
            wanConfig = cfg
            try await Task.sleep(nanoseconds: 3_000_000_000)
            await refresh()
        } catch { errorMessage = error.localizedDescription }
    }

    func applyLAN(_ cfg: LANConfig) async {
        guard let client else { return }
        do {
            try await client.applyLAN(cfg)
            lanConfig = cfg
        } catch { errorMessage = error.localizedDescription }
    }

    func setBlocked(_ host: Host, blocked: Bool) async {
        guard let client else { return }
        do {
            _ = try await client.updateHost(mac: host.mac, blocked: blocked)
            await refresh()
        } catch { errorMessage = error.localizedDescription }
    }

    func setRateLimit(_ host: Host, kbps: Int) async {
        guard let client else { return }
        do {
            _ = try await client.updateHost(mac: host.mac, rateLimitKbps: kbps)
            await refresh()
        } catch { errorMessage = error.localizedDescription }
    }

    func rename(_ host: Host, alias: String) async {
        guard let client else { return }
        do {
            _ = try await client.updateHost(mac: host.mac, alias: alias)
            await refresh()
        } catch { errorMessage = error.localizedDescription }
    }

    func setBlockedApps(_ host: Host, apps: [String]) async {
        guard let client else { return }
        do {
            try await client.setBlockedApps(mac: host.mac, apps: apps)
            await refresh()
        } catch { errorMessage = error.localizedDescription }
    }

    /// 扫描周边可用于中继的无线热点
    func scanRelay() async throws -> [APIClient.RelayNetwork] {
        guard let client else { throw APIError.offline("未连接") }
        return try await client.scanRelay()
    }

    // MARK: - 事件与提醒

    private func handleEvent(_ event: RouterEvent) {
        switch event.name {
        case "host.new", "host.online", "host.offline":
            Task { await refresh() }
            if notifyEnabled, let data = event.data {
                notify(host: data.host, event: event.name)
            }
        case "wan.change", "filter.apply":
            Task { await refresh() }
        default:
            break
        }
    }

    private func notify(host: Host, event: String) {
        let content = UNMutableNotificationContent()
        switch event {
        case "host.new":
            content.title = "新设备接入"
            content.body = "\(host.displayName) 首次连接到你的网络"
        case "host.online":
            content.title = "设备上线"
            content.body = "\(host.displayName) 已连接到网络"
        default:
            content.title = "设备离线"
            content.body = "\(host.displayName) 已断开连接"
        }
        content.sound = .default
        content.userInfo = ["mac": host.mac, "event": event]
        let req = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
        UNUserNotificationCenter.current().add(req)
    }

    // MARK: - 推送注册

    func registerPushIfNeeded() {
        guard notifyEnabled else { return }
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { granted, _ in
            guard granted else { return }
            DispatchQueue.main.async { UIApplication.shared.registerForRemoteNotifications() }
        }
    }

    /// AppDelegate 拿到 APNs token 后调用
    func didReceiveAPNsToken(_ data: Data) {
        let token = data.map { String(format: "%02.2hhx", $0) }.joined()
        let old = KeychainStore.load(String.self, account: Self.pushTokenKey)
        guard old != token else { return }
        KeychainStore.save(token, for: Self.pushTokenKey)
        Task {
            do {
                #if DEBUG
                try await client?.registerPush(apnsToken: token, sandbox: true)
                #else
                try await client?.registerPush(apnsToken: token, sandbox: false)
                #endif
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }
}
