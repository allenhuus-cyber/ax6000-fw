import Foundation
import Security

/// 极简 Keychain 封装，用于保存路由器凭证（secret 属于敏感信息，不放 UserDefaults）。
enum KeychainStore {
    private static let service = "com.ax6000.routermanager"

    static func save(_ data: Data, for account: String) {
        delete(account: account)
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecValueData as String: data
        ]
        SecItemAdd(query as CFDictionary, nil)
    }

    static func save<T: Encodable>(_ value: T, for account: String) {
        if let data = try? JSONEncoder().encode(value) {
            save(data, for: account)
        }
    }

    static func load(account: String) -> Data? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne
        ]
        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        guard status == errSecSuccess else { return nil }
        return result as? Data
    }

    static func load<T: Decodable>(_ type: T.Type, account: String) -> T? {
        guard let data = load(account: account) else { return nil }
        return try? JSONDecoder().decode(type, from: data)
    }

    static func delete(account: String) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account
        ]
        SecItemDelete(query as CFDictionary)
    }
}
