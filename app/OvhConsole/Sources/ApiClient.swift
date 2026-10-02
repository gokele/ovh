import Foundation

/**
 * API 客户端(Swift 版,对齐 packages/core 的 api-client 契约):
 * - 基址 + 设备令牌(Authorization: Bearer)或访问密钥(X-API-Key)
 * - ?account= 自动追加(与 web/RN 同语义:三区目录互不相通)
 * - 错误解包成中文人话(后端 body.error/message)
 */
struct ApiClient {
    var baseUrl: String
    var deviceToken: String?
    var apiKey: String?
    var accountId: String

    struct ApiError: LocalizedError {
        let status: Int
        let message: String
        var errorDescription: String { message }
    }

    /// 组 URL:/api 前缀 + account 参数
    private func url(_ path: String) -> URL? {
        var s = baseUrl.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        var full = "https://\(s)"
        if s.hasPrefix("http://") || s.hasPrefix("https://") { full = s }
        full += path.hasPrefix("/api") ? path : "/api" + path
        var comp = URLComponents(string: full)
        var items = comp?.queryItems ?? []
        if !accountId.isEmpty { items.append(URLQueryItem(name: "account", value: accountId)) }
        comp?.queryItems = items.isEmpty ? nil : items
        return comp?.url
    }

    private func request(_ method: String, _ path: String, bodyData: Data? = nil) async throws -> Data {
        guard let u = url(path) else { throw ApiError(status: 0, message: "后端地址不合法") }
        var req = URLRequest(url: u, timeoutInterval: 20)
        req.httpMethod = method
        if let tok = deviceToken, !tok.isEmpty {
            req.setValue("Bearer \(tok)", forHTTPHeaderField: "Authorization")
        } else if let k = apiKey, !k.isEmpty {
            req.setValue(k, forHTTPHeaderField: "X-API-Key")
        }
        if let bodyData {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = bodyData
        }
        let (data, resp) = try await URLSession.shared.data(for: req)
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            // 后端错误体:{error} 或 {message},取人话
            if let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                for key in ["message", "error", "msg"] {
                    if let s = obj[key] as? String, !s.isEmpty {
                        throw ApiError(status: status, message: s)
                    }
                }
            }
            throw ApiError(status: status, message: status == 401 ? "鉴权失败:令牌或密钥无效" : "请求失败(HTTP \(status))")
        }
        return data
    }

    /// GET → 泛型解码
    func get<T: Decodable>(_ path: String, as type: T.Type) async throws -> T {
        let data = try await request("GET", path)
        return try JSONDecoder().decode(T.self, from: data)
    }

    /// GET → 原始字典(后端大量接口字段动态,先以字典落地,逐步固化模型)
    func getDict(_ path: String) async throws -> [String: Any] {
        let data = try await request("GET", path)
        return (try? JSONSerialization.jsonObject(with: data) as? [String: Any]) ?? [:]
    }

    /// DELETE 动作辅助
    func actionDelete(_ path: String) async -> (Bool, String) {
        do {
            let data = try await request("DELETE", path)
            let r = (try? JSONSerialization.jsonObject(with: data) as? [String: Any]) ?? [:]
            return (true, r["message"] as? String ?? "")
        } catch { return (false, error.localizedDescription) }
    }

    /// 动作辅助(供 View 调):接收已序列化的 Data(Sendable),View 侧不跨域传 [String:Any]
    func actionPostData(_ path: String, bodyData: Data?) async -> (Bool, String) {
        do {
            let data = try await request("POST", path, bodyData: bodyData)
            let r = (try? JSONSerialization.jsonObject(with: data) as? [String: Any]) ?? [:]
            return (true, r["message"] as? String ?? "")
        } catch { return (false, error.localizedDescription) }
    }
    func actionPutData(_ path: String, bodyData: Data?) async -> (Bool, String) {
        do {
            let data = try await request("PUT", path, bodyData: bodyData)
            let r = (try? JSONSerialization.jsonObject(with: data) as? [String: Any]) ?? [:]
            return (true, r["message"] as? String ?? "")
        } catch { return (false, error.localizedDescription) }
    }

    /// GET → 数组
    func getArray(_ path: String) async throws -> [[String: Any]] {
        let data = try await request("GET", path)
        return (try? JSONSerialization.jsonObject(with: data) as? [[String: Any]]) ?? []
    }

    /// Swift 6 严格并发下 [String:Any] 不可 Sendable —— 调用侧先序列化成 Data 再跨界
    @discardableResult
    func post(_ path: String, body: [String: Any]? = [:]) async throws -> [String: Any] {
        let bodyData = body.flatMap { try? JSONSerialization.data(withJSONObject: $0) }
        let data = try await request("POST", path, bodyData: bodyData)
        return (try? JSONSerialization.jsonObject(with: data) as? [String: Any]) ?? [:]
    }

    @discardableResult
    func put(_ path: String, body: [String: Any]? = [:]) async throws -> [String: Any] {
        let bodyData = body.flatMap { try? JSONSerialization.data(withJSONObject: $0) }
        let data = try await request("PUT", path, bodyData: bodyData)
        return (try? JSONSerialization.jsonObject(with: data) as? [String: Any]) ?? [:]
    }

    @discardableResult
    func delete(_ path: String) async throws -> [String: Any] {
        let data = try await request("DELETE", path)
        return (try? JSONSerialization.jsonObject(with: data) as? [String: Any]) ?? [:]
    }
}

// MARK: - 配对(对齐后端 /api/app/pair 契约)

struct PairResult: Decodable {
    let success: Bool
    let token: String
    let deviceId: Int
    let serverVersion: String?
}

extension ApiClient {
    /// 凭一次性码换设备令牌(白名单端点,不需要已有凭据)
    static func pair(serverUrl: String, code: String, deviceName: String) async throws -> PairResult {
        var s = serverUrl.trimmingCharacters(in: .whitespacesAndNewlines)
        while s.hasSuffix("/") { s.removeLast() }
        guard let u = URL(string: s + "/api/app/pair") else {
            throw ApiError(status: 0, message: "地址不合法")
        }
        var req = URLRequest(url: u, timeoutInterval: 20)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONSerialization.data(withJSONObject: ["code": code.uppercased(), "deviceName": deviceName])
        let (data, resp) = try await URLSession.shared.data(for: req)
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            if let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
               let msg = (obj["error"] as? String) ?? (obj["message"] as? String) {
                throw ApiError(status: status, message: msg)
            }
            throw ApiError(status: status, message: "配对失败(HTTP \(status))")
        }
        return try JSONDecoder().decode(PairResult.self, from: data)
    }
}
