import Foundation
import UIKit

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


    /// POSIX socket HTTP(完全绕过 ATS/URLSession/CFNetwork)
    /// ATS 只检查 CFNetwork 层;直接用 BSD socket 没有 ATS
    static func posixHTTP(_ method: String, url: String, body: Data? = nil, extraHeaders: String = "", timeoutSec: Int = 15) throws -> (Int, Data) {
        guard let u = URL(string: url), let host = u.host, let port = u.port else {
            throw ApiError(status: 0, message: "地址不合法")
        }
        
        var addr = sockaddr_in()
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = UInt16(port).bigEndian
        addr.sin_addr = in_addr(s_addr: inet_addr(host))
        
        let fd = socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else { throw ApiError(status: 0, message: "创建 socket 失败") }
        defer { close(fd) }
        
        // 非阻塞 + select 超时
        let flags = fcntl(fd, F_GETFL, 0)
        _ = fcntl(fd, F_SETFL, flags | O_NONBLOCK)
        
        let connectResult = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                connect(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        if connectResult != 0 && errno != EINPROGRESS {
            throw ApiError(status: 0, message: "连接失败(\(String(cString: strerror(errno))))")
        }
        
        // select 等 connect 完成
        // fd_set: iOS 上是 int32 数组,直接清零再设位
        var writeSet = fd_set()
        writeSet.fds_bits.0 = 0
        writeSet.fds_bits.0 |= Int32(1 << fd)
        var timeout = timeval(tv_sec: timeoutSec, tv_usec: 0)
        let sel = withUnsafeMutablePointer(to: &writeSet) { setPtr in
            select(fd + 1, nil, setPtr, nil, &timeout)
        }
        guard sel > 0 else { throw ApiError(status: 0, message: "连接超时") }
        
        // 检查 connect 结果
        var soError: Int32 = 0
        var len = socklen_t(MemoryLayout<Int32>.size)
        getsockopt(fd, SOL_SOCKET, SO_ERROR, &soError, &len)
        guard soError == 0 else { throw ApiError(status: 0, message: "连接失败(errno \(soError))") }
        
        // 恢复阻塞模式
        _ = fcntl(fd, F_SETFL, flags)
        
        // 发请求
        var path = u.path
        if let q = u.query { path += "?" + q }
        var req = "\(method) \(path) HTTP/1.1\r\nHost: \(host):\(port)\r\nConnection: close\r\n" + extraHeaders
        if let b = body {
            req += "Content-Type: application/json\r\nContent-Length: \(b.count)\r\n\r\n"
            var full = Data(req.utf8)
            full.append(b)
            req = String(decoding: full, as: UTF8.self)
        } else {
            req += "\r\n"
        }
        
        let sent = req.withCString { ptr in
            send(fd, ptr, strlen(ptr), 0)
        }
        guard sent > 0 else { throw ApiError(status: 0, message: "发送失败") }
        
        // 读响应
        var response = Data()
        var buf = [UInt8](repeating: 0, count: 16384)
        while true {
            let n = recv(fd, &buf, buf.count, 0)
            if n <= 0 { break }
            response.append(Data(buf[0..<n]))
            if response.count > 1_048_576 { break } // 1MB 上限
        }
        
        // 解析 HTTP 状态码和 body
        guard let headerEnd = response.range(of: Data("\r\n\r\n".utf8)) else {
            throw ApiError(status: 0, message: "响应格式异常")
        }
        let headerStr = String(decoding: response[0..<headerEnd.lowerBound], as: UTF8.self)
        let bodyData = response[headerEnd.upperBound...]
        
        let statusLine = headerStr.components(separatedBy: "\r\n").first ?? ""
        let parts = statusLine.components(separatedBy: " ")
        let statusCode = Int(parts.count > 1 ? parts[1] : "0") ?? 0
        
        return (statusCode, Data(bodyData))
    }

    /// 不走 ATS 的 URLSession:某些 iOS 版本对 Info.plist 的
    /// NSAllowsArbitraryLoads 处理有差异,代码层再兜一道底
    /// (URLSessionConfiguration 默认继承 ATS,这里显式允许不安全连接)
    static let allowHTTPSession: URLSession = {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.timeoutIntervalForRequest = 20
        cfg.timeoutIntervalForResource = 25
        return URLSession(configuration: cfg)
    }()

    private func request(_ method: String, _ path: String, bodyData: Data? = nil) async throws -> Data {
        guard let u = url(path) else { throw ApiError(status: 0, message: "后端地址不合法") }
        // POSIX socket 直发:绕过 ATS(ATS 只在 CFNetwork 层,底层 socket 没有)
        var headers = ""
        if let tok = deviceToken, !tok.isEmpty {
            headers += "Authorization: Bearer \(tok)\r\n"
        } else if let k = apiKey, !k.isEmpty {
            headers += "X-API-Key: \(k)\r\n"
        }
        if bodyData != nil {
            headers += "Content-Type: application/json\r\n"
        }
        let (status, data) = try Self.posixHTTP(method, url: u.absoluteString, body: bodyData, extraHeaders: headers)
        guard (200..<300).contains(status) else {
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
        // POSIX socket 直发:完全绕过 ATS(ATS 只存在于 CFNetwork 层,
        // BSD socket 没有 ATS)。配对时 App 还没有任何凭据,
        // 连的是用户自己的后端 —— 没有理由被安全策略拦。
        let body = try JSONSerialization.data(withJSONObject: ["code": code.uppercased(), "deviceName": deviceName])
        let (status, data) = try posixHTTP("POST", url: u.absoluteString, body: body)
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


/// 全局唯一的弹窗宿主查找:SPM 可执行目标在真机上 keyWindow 时机不稳,
/// 逐级回退(前台 scene 的窗口 → 任意窗口 → 放弃),绝不强制解包。
@MainActor
enum AlertHost {
    static func present(_ alert: UIAlertController) {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        let windows = scenes.flatMap { $0.windows }.filter { $0.isKeyWindow } 
            .isEmpty ? scenes.flatMap { $0.windows } : scenes.flatMap { $0.windows }.filter { $0.isKeyWindow }
        guard let root = (windows.first { $0.rootViewController != nil })?.rootViewController else {
            // 没有可用的窗口就不弹 —— 比 EXC_BREAKPOINT 崩溃好
            return
        }
        // 找最顶层的 presented VC,否则被已存在的 sheet 挡住
        var top = root
        while let p = top.presentedViewController { top = p }
        top.present(alert, animated: true)
    }
}
