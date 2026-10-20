import Foundation

/// 把 Encodable 模型转成 JSON 字典，便于统一的请求体拼装。
extension Encodable {
    func asDictionary() -> [String: Any]? {
        guard let data = try? JSONEncoder().encode(self) else { return nil }
        return try? JSONSerialization.jsonObject(with: data, options: []) as? [String: Any]
    }
}

enum APIError: LocalizedError {
    case badURL
    case http(code: Int, message: String)
    case offline(String)
    case decoding

    var errorDescription: String? {
        switch self {
        case .badURL: return "地址无效"
        case .http(let code, let message): return "请求失败(\(code)): \(message)"
        case .offline(let msg): return msg
        case .decoding: return "数据解析失败"
        }
    }
}

enum ConnectionMode: String {
    case lan    // 同一 WiFi 内直连路由器
    case cloud  // 经云端中继
}

/// 统一的 API 客户端：局域网直连与云端中继对上层透明。
final class APIClient {
    let identity: RouterIdentity
    private(set) var mode: ConnectionMode
    private(set) var token: String?

    private let session: URLSession = {
        let cfg = URLSessionConfiguration.default
        cfg.timeoutIntervalForRequest = 15
        return URLSession(configuration: cfg)
    }()

    private let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        return d
    }()

    init(identity: RouterIdentity, mode: ConnectionMode = .lan) {
        self.identity = identity
        self.mode = mode
    }

    // MARK: - 地址

    var lanBaseURL: URL? {
        URL(string: "http://\(identity.lanHost):\(identity.lanPort)")
    }

    var cloudBaseURL: URL? {
        guard let s = identity.cloudBaseURL, !s.isEmpty else { return nil }
        return URL(string: s)
    }

    var wsURL: URL? {
        let tok = token ?? ""
        switch mode {
        case .lan:
            return URL(string: "ws://\(identity.lanHost):\(identity.lanPort)/api/v1/ws?token=\(tok)")
        case .cloud:
            guard let base = cloudBaseURL,
                  var comp = URLComponents(url: base, resolvingAgainstBaseURL: false) else { return nil }
            comp.scheme = comp.scheme == "https" ? "wss" : "ws"
            comp.path = "/app"
            comp.queryItems = [
                URLQueryItem(name: "device_id", value: identity.deviceId),
                URLQueryItem(name: "token", value: tok)
            ]
            return comp.url
        }
    }

    // MARK: - 连接模式探测

    /// 优先尝试局域网直连，失败则切到云端。
    @discardableResult
    func refreshMode() async -> ConnectionMode {
        if await probeLAN() {
            mode = .lan
        } else if cloudBaseURL != nil {
            mode = .cloud
        }
        return mode
    }

    private func probeLAN() async -> Bool {
        guard let base = lanBaseURL else { return false }
        var req = URLRequest(url: base.appendingPathComponent("/api/v1/status"))
        req.timeoutInterval = 3
        if let token { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        do {
            let (_, resp) = try await session.data(for: req)
            guard let http = resp as? HTTPURLResponse else { return false }
            return (200..<500).contains(http.statusCode) // 401 也说明路由器可达
        } catch {
            return false
        }
    }

    // MARK: - 认证

    func login() async throws -> LoginResponse {
        let body = ["device_id": identity.deviceId, "secret": identity.secret, "client": "ios"]
        let res: LoginResponse = try await send("POST", "/api/v1/auth/login", body: body, retryOn401: false)
        token = res.token
        return res
    }

    /// 用路由器管理账号换取配对信息（首次配对）
    static func pair(host: String, port: Int, username: String, password: String) async throws -> PairingInfo {
        guard let url = URL(string: "http://\(host):\(port)/api/v1/pair") else { throw APIError.badURL }
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try? JSONSerialization.data(withJSONObject: ["username": username, "password": password])
        let (data, resp) = try await URLSession.shared.data(for: req)
        guard let http = resp as? HTTPURLResponse else { throw APIError.offline("无响应") }
        guard (200..<300).contains(http.statusCode) else {
            let msg = String(data: data, encoding: .utf8) ?? ""
            throw APIError.http(code: http.statusCode, message: msg)
        }
        let d = JSONDecoder(); d.keyDecodingStrategy = .convertFromSnakeCase
        return try d.decode(PairingInfo.self, from: data)
    }

    // MARK: - 业务接口

    func status() async throws -> RouterStatus { try await get("/api/v1/status") }

    func wanConfig() async throws -> WANConfig { try await get("/api/v1/network/wan") }

    func applyWAN(_ cfg: WANConfig) async throws {
        try await sendVoid("PUT", "/api/v1/network/wan", body: cfg.asDictionary())
    }

    func lanConfig() async throws -> LANConfig { try await get("/api/v1/network/lan") }

    func applyLAN(_ cfg: LANConfig) async throws {
        try await sendVoid("PUT", "/api/v1/network/lan", body: cfg.asDictionary())
    }

    func hosts() async throws -> [Host] {
        struct Res: Decodable { let hosts: [Host] }
        let res: Res = try await get("/api/v1/hosts")
        return res.hosts
    }

    func apps() async throws -> [AppInfo] {
        struct Res: Decodable { let apps: [AppInfo] }
        let res: Res = try await get("/api/v1/apps")
        return res.apps
    }

    func updateHost(mac: String, alias: String? = nil, blocked: Bool? = nil,
                    rateLimitKbps: Int? = nil, notify: Bool? = nil) async throws -> Host {
        var body: [String: Any] = [:]
        if let alias { body["alias"] = alias }
        if let blocked { body["blocked"] = blocked }
        if let rateLimitKbps { body["rate_limit_kbps"] = rateLimitKbps }
        if let notify { body["notify"] = notify }
        return try await send("POST", "/api/v1/hosts/\(mac)", body: body)
    }

    func setBlockedApps(mac: String, apps: [String]) async throws {
        try await sendVoid("POST", "/api/v1/hosts/\(mac)/apps", body: ["apps": apps])
    }

    func scanRelay() async throws -> [RelayNetwork] {
        struct Res: Decodable { let results: [RelayNetwork] }
        let res: Res = try await get("/api/v1/network/scan")
        return res.results
    }

    struct RelayNetwork: Decodable, Identifiable {
        var id: String { bssid ?? ssid }
        let ssid: String
        let bssid: String?
        let channel: Int?
        let signal: Int?
        let encryption: String?
        let band: String?
    }

    /// 注册 APNs 推送令牌（局域网经设备转交云端，或直接上报云端）
    func registerPush(apnsToken: String, sandbox: Bool) async throws {
        let body: [String: Any] = [
            "apns_token": apnsToken,
            "device_id": identity.deviceId,
            "app_id": Bundle.main.bundleIdentifier ?? "com.ax6000.routermanager",
            "sandbox": sandbox,
            "notify_new_host": true,
            "notify_offline": true
        ]
        switch mode {
        case .lan:
            try await sendVoid("POST", "/api/v1/notify/register", body: body)
        case .cloud:
            try await sendVoid("POST", "/api/v1/notify/register", body: body, forceCloudAPI: true)
        }
    }

    // MARK: - 底层

    private func get<T: Decodable>(_ path: String) async throws -> T {
        try await send("GET", path)
    }

    private func send<T: Decodable>(_ method: String, _ path: String,
                                    body: Any? = nil, retryOn401: Bool = true,
                                    forceCloudAPI: Bool = false) async throws -> T {
        let data = try await rawSend(method, path, body: body, retryOn401: retryOn401, forceCloudAPI: forceCloudAPI)
        do {
            return try decoder.decode(T.self, from: data)
        } catch {
            throw APIError.decoding
        }
    }

    private func sendVoid(_ method: String, _ path: String, body: Any? = nil,
                          forceCloudAPI: Bool = false) async throws {
        _ = try await rawSend(method, path, body: body, forceCloudAPI: forceCloudAPI)
    }

    private func rawSend(_ method: String, _ path: String, body: Any? = nil,
                         retryOn401: Bool = true, forceCloudAPI: Bool = false) async throws -> Data {
        if token == nil && path != "/api/v1/auth/login" {
            _ = try? await login()
        }
        do {
            return try await perform(method, path, body: body, forceCloudAPI: forceCloudAPI)
        } catch APIError.http(let code, _) where code == 401 && retryOn401 {
            _ = try await login()
            return try await perform(method, path, body: body, forceCloudAPI: forceCloudAPI)
        }
    }

    private func perform(_ method: String, _ path: String, body: Any?,
                         forceCloudAPI: Bool) async throws -> Data {
        let req: URLRequest
        switch mode {
        case .cloud where !forceCloudAPI:
            // 经云端把请求转发给设备
            var payload: [String: Any] = ["device_id": identity.deviceId, "method": method, "path": path]
            if let body { payload["body"] = body }
            req = try buildCloudRequest(path: "/api/v1/rpc", body: payload)
        default:
            guard let base = (mode == .lan ? lanBaseURL : cloudBaseURL) else { throw APIError.badURL }
            var r = URLRequest(url: base.appendingPathComponent(path))
            r.httpMethod = method
            r.setValue("application/json", forHTTPHeaderField: "Content-Type")
            if let token { r.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
            if let body { r.httpBody = try? JSONSerialization.data(withJSONObject: body) }
            req = r
        }
        let (data, resp) = try await session.data(for: req)
        guard let http = resp as? HTTPURLResponse else { throw APIError.offline("无响应") }
        guard (200..<300).contains(http.statusCode) else {
            let msg = (try? JSONDecoder().decode([String: String].self, from: data))?["error"]
                ?? String(data: data, encoding: .utf8) ?? ""
            throw APIError.http(code: http.statusCode, message: msg)
        }
        return data
    }

    private func buildCloudRequest(path: String, body: [String: Any]) throws -> URLRequest {
        guard let base = cloudBaseURL else { throw APIError.offline("未配置云端地址") }
        var r = URLRequest(url: base.appendingPathComponent(path))
        r.httpMethod = "POST"
        r.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { r.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        r.httpBody = try? JSONSerialization.data(withJSONObject: body)
        return r
    }
}
