import Foundation

// MARK: - 与固件 / 云端共用的数据契约（JSON 字段为 snake_case）

struct RouterStatus: Codable, Equatable {
    let deviceId: String
    let name: String
    let firmware: String
    let uptime: Int64
    let cpuUsage: Int
    let memTotal: Int64
    let memFree: Int64
    let lanAddr: String
    let wan: WANStatus
    let hostTotal: Int
    let hostOnline: Int
}

struct WANStatus: Codable, Equatable {
    let mode: String          // pppoe / dhcp / static / relay
    let up: Bool
    let ip: String?
    let gateway: String?
    let dns: [String]?
    let mac: String?
    let device: String?
    let uptime: Int64?
    let rxBytes: Int64?
    let txBytes: Int64?
    let error: String?
}

struct WANConfig: Codable, Equatable {
    var mode: String
    var username: String?
    var password: String?
    var ip: String?
    var netmask: String?
    var gateway: String?
    var dns: [String]?
    var mtu: Int?
    var macClone: String?
    var relaySSID: String?
    var relayPassword: String?
    var relayBand: String?
    var serviceName: String?

    enum CodingKeys: String, CodingKey {
        case mode, username, password, ip, netmask, gateway, dns, mtu
        case macClone = "mac_clone"
        case relaySSID = "relay_ssid"
        case relayPassword = "relay_password"
        case relayBand = "relay_band"
        case serviceName = "service_name"
    }
}

struct LANConfig: Codable, Equatable {
    var ip: String
    var netmask: String
    var dhcpEnabled: Bool

    enum CodingKeys: String, CodingKey {
        case ip, netmask
        case dhcpEnabled = "dhcp_enabled"
    }
}

struct Host: Codable, Identifiable, Equatable {
    var id: String { mac }
    let mac: String
    let ip: String?
    let name: String?
    let alias: String?
    let vendor: String?
    let iface: String?
    let type: String?
    let online: Bool
    let firstSeen: Int64
    let lastSeen: Int64
    let rxBytes: Int64?
    let txBytes: Int64?
    let blocked: Bool
    let rateLimitKbps: Int
    let blockedApps: [String]

    enum CodingKeys: String, CodingKey {
        case mac, ip, name, alias, vendor, iface, type, online
        case firstSeen = "first_seen"
        case lastSeen = "last_seen"
        case rxBytes = "rx_bytes"
        case txBytes = "tx_bytes"
        case blocked
        case rateLimitKbps = "rate_limit_kbps"
        case blockedApps = "blocked_apps"
    }

    /// 展示名：备注 > 主机名 > 厂商 > MAC
    var displayName: String {
        if let a = alias, !a.isEmpty { return a }
        if let n = name, !n.isEmpty { return n }
        if let v = vendor, !v.isEmpty { return v + " 设备" }
        return mac
    }

    var isWireless: Bool { type == "wireless" }
}

struct AppInfo: Codable, Identifiable, Equatable {
    var id: String
    var name: String
    var category: String
    var engines: [String]

    var categoryName: String {
        switch category {
        case "short_video": return "短视频"
        case "video": return "视频"
        case "social": return "社交"
        case "game": return "游戏"
        case "shopping": return "购物"
        case "music": return "音乐"
        case "live": return "直播"
        case "reading": return "阅读"
        case "tool": return "工具"
        default: return "其他"
        }
    }
}

struct LoginResponse: Codable, Equatable {
    let token: String
    let expiresIn: Int64
    let deviceId: String
    let name: String

    enum CodingKeys: String, CodingKey {
        case token
        case expiresIn = "expires_in"
        case deviceId = "device_id"
        case name
    }
}

struct RouterEvent: Codable {
    let type: String
    let name: String
    let ts: Int64
    let data: HostEventData?
}

struct HostEventData: Codable {
    let host: Host
}

struct PairingInfo: Codable {
    let deviceId: String
    let secret: String
    let name: String
    let lanAddr: String
    let apiPort: String
    let cloud: Bool

    enum CodingKeys: String, CodingKey {
        case deviceId = "device_id"
        case secret
        case name
        case lanAddr = "lan_addr"
        case apiPort = "api_port"
        case cloud
    }
}

// MARK: - 连接配置（存 Keychain）

struct RouterIdentity: Codable, Equatable {
    let deviceId: String
    let secret: String
    let name: String
    var lanHost: String    // 局域网地址，如 192.168.1.1
    var lanPort: Int
    var cloudBaseURL: String?
}
