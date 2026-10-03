import SwiftUI

/**
 * 连接状态:后端地址 + 设备令牌(Keychain 存储)+ 当前账户 + 账户缓存。
 * 令牌经配对获得;手机丢了在网页端吊销 —— 与 web 同语义。
 * Keychain 而非 UserDefaults:令牌是凭据,不该进 iCloud 备份的 plist。
 */
enum Keychain {
    static func set(_ value: String, _ key: String) {
        let data = Data(value.utf8)
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrAccount as String: key,
        ]
        SecItemDelete(query as CFDictionary)
        var attrs = query
        attrs[kSecValueData as String] = data
        attrs[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        SecItemAdd(attrs as CFDictionary, nil)
    }

    static func get(_ key: String) -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrAccount as String: key,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
              let data = item as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    static func delete(_ key: String) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrAccount as String: key,
        ]
        SecItemDelete(query as CFDictionary)
    }
}

/// 全局连接(注入环境)。accounts 在启动后拉一次缓存,顶栏徽章与切换器共用。
@MainActor
final class Connection: ObservableObject {
    @Published var serverUrl: String = ""
    @Published var token: String = ""
    @Published var accountId: String = ""
    @Published var accounts: [[String: Any]] = []

    var isPaired: Bool { !serverUrl.isEmpty && !token.isEmpty }

    /// 当前生效账户(选中 > 默认 > 第一个;与 web AccountSwitcher 同回退序)
    var activeAccount: [String: Any]? {
        accounts.first { ($0["id"] as? String) == accountId }
            ?? accounts.first { ($0["isDefault"] as? Bool) == true }
            ?? accounts.first
    }

    var client: ApiClient {
        // __key__ 前缀 = 手动密钥(与 RN 版同约定)
        var deviceToken: String?
        var apiKey: String?
        if token.hasPrefix("__key__") { apiKey = String(token.dropFirst(7)) }
        else { deviceToken = token }
        return ApiClient(baseUrl: serverUrl, deviceToken: deviceToken, apiKey: apiKey, accountId: accountId)
    }

    private let K_SERVER = "ovh_server_url"
    private let K_TOKEN = "ovh_device_token"
    private let K_ACCOUNT = "ovh_active_account"

    init() {
        serverUrl = Keychain.get(K_SERVER) ?? ""
        token = Keychain.get(K_TOKEN) ?? ""
        accountId = Keychain.get(K_ACCOUNT) ?? ""
    }

    /// 配对成功后持久化
    func save(server: String, token: String) {
        serverUrl = server
        self.token = token
        Keychain.set(server, K_SERVER)
        Keychain.set(token, K_TOKEN)
    }

    /// 切换全局账户(与 web 同语义:所有请求自动带 ?account=)
    func setAccount(_ id: String) {
        accountId = id
        if id.isEmpty { Keychain.delete(K_ACCOUNT) } else { Keychain.set(id, K_ACCOUNT) }
    }

    /// 拉账户列表(启动 / 设置页保存后调用)
    func loadAccounts() async {
        guard let r = try? await client.getDict("/accounts") else { return }
        accounts = (r["accounts"] as? [[String: Any]]) ?? []
    }

    /// 断开(吊销在网页端做,这里只清本地)
    func forget() {
        serverUrl = ""; token = ""; accountId = ""; accounts = []
        Keychain.delete(K_SERVER); Keychain.delete(K_TOKEN); Keychain.delete(K_ACCOUNT)
    }
}
