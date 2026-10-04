import Foundation
import UIKit

extension Notification.Name {
    static let tokenInvalidated = Notification.Name("tokenInvalidated")
}

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
        var full = "http://\(s)"   // posixHTTP 不支持 TLS,默认 http(与配对页校验一致)
        if s.hasPrefix("http://") || s.hasPrefix("https://") { full = s }
        full += path.hasPrefix("/api") ? path : "/api" + path
        var comp = URLComponents(string: full)
        var items = comp?.queryItems ?? []
        if !accountId.isEmpty { items.append(URLQueryItem(name: "account", value: accountId)) }
        comp?.queryItems = items.isEmpty ? nil : items
        return comp?.url
    }


    /// POSIX socket HTTP(完全绕过 ATS/URLSession/CFNetwork)
    /// ATS 只检查 CFNetwork 层;直接用 BSD socket 没有 ATS。
    /// HTTP chunked 解码:"size(十六进制)\r\n <size 字节> \r\n" 循环,读到 size=0。
    /// Go 后端对大响应(MRTG 31KB+)自动走分块编码,不剥长度行 JSON 开头就是坏的。
    static func decodeChunked(_ input: Data) -> Data {
        var out = Data()
        var idx = input.startIndex
        let crlf = Data("\r\n".utf8)
        while idx < input.endIndex {
            guard let lineEnd = input.range(of: crlf, in: idx..<input.endIndex) else { break }
            let sizeStr = String(decoding: input[idx..<lineEnd.lowerBound], as: UTF8.self)
                .trimmingCharacters(in: .whitespaces)
                .components(separatedBy: ";").first ?? ""
            guard let size = Int(sizeStr, radix: 16), size > 0 else { break }
            let chunkStart = lineEnd.upperBound
            guard let chunkEnd = input.index(chunkStart, offsetBy: size, limitedBy: input.endIndex) else { break }
            out.append(input[chunkStart..<chunkEnd])
            idx = chunkEnd
            if idx < input.endIndex, let cap = input.index(idx, offsetBy: 2, limitedBy: input.endIndex),
               let tail = input.range(of: crlf, in: idx..<cap) {
                idx = tail.upperBound
            } else {
                break
            }
        }
        return out
    }

    /// 仅支持明文 http(自建后端);域名走 getaddrinfo,读写有超时,循环写完整请求。
    static func posixHTTP(_ method: String, url: String, body: Data? = nil, extraHeaders: String = "", timeoutSec: Int = 15) throws -> (Int, Data) {
        guard let u = URL(string: url), let host = u.host else {
            throw ApiError(status: 0, message: "地址不合法")
        }
        // 无显式端口时按 scheme 给默认值(https 明文 socket 打不通,直接讲清楚)
        guard u.scheme?.lowercased() != "https" else {
            throw ApiError(status: 0, message: "暂不支持 https 后端:请在配对地址使用 http")
        }
        let port = u.port ?? 80

        // DNS:域名与 IPv4 都走 getaddrinfo(inet_addr 只认点分 IP 且失败不报错)
        var addr = sockaddr_in()
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = UInt16(port).bigEndian
        var resolved = false
        if inet_pton(AF_INET, host, &addr.sin_addr) == 1 {
            resolved = true
        } else {
            var hints = addrinfo(), res: UnsafeMutablePointer<addrinfo>? = nil
            hints.ai_family = AF_INET
            hints.ai_socktype = SOCK_STREAM
            guard getaddrinfo(host, "\(port)", &hints, &res) == 0, let first = res else {
                throw ApiError(status: 0, message: "解析不了地址:\(host)")
            }
            defer { freeaddrinfo(res) }
            if first.pointee.ai_family == AF_INET, let sa = first.pointee.ai_addr {
                let saIn = sa.withMemoryRebound(to: sockaddr_in.self, capacity: 1) { $0.pointee }
                addr.sin_addr = saIn.sin_addr
                resolved = true
            }
        }
        guard resolved else { throw ApiError(status: 0, message: "地址不含 IPv4 记录:\(host)") }

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

        // select 等 connect 完成。fd_set 在 Darwin 上是 int32 元组,
        // 按 fd/32 选字、fd%32 选位设置(手写 1<<fd 在 fd≥32 时溢出崩溃)
        guard Int(fd) < 1024 else { throw ApiError(status: 0, message: "系统句柄耗尽(fd \(fd))") }
        var writeSet = fd_set()
        withUnsafeMutableBytes(of: &writeSet) { raw in
            let ints = raw.bindMemory(to: Int32.self)
            ints[Int(fd) / 32] |= Int32(1 << (Int(fd) % 32))
        }
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
        
        // 恢复阻塞模式 + 读写超时(服务器不关连接时 recv 不能永久挂死)
        _ = fcntl(fd, F_SETFL, flags)
        var tv = timeval(tv_sec: timeoutSec, tv_usec: 0)
        setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
        setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))

        // 发请求(头与 body 分开、循环写完;不再经 String 往返 —— 非 UTF-8 字节
        // 会被替换导致 Content-Length 与实发不一致)
        var path = u.path.isEmpty ? "/" : u.path
        if let q = u.query { path += "?" + q }
        var head = "\(method) \(path) HTTP/1.1\r\nHost: \(host):\(port)\r\nConnection: close\r\nUser-Agent: OvhConsole-iOS\r\n" + extraHeaders
        if body != nil {
            head += "Content-Type: application/json\r\nContent-Length: \(body!.count)\r\n"
        }
        head += "\r\n"
        var out = Data(head.utf8)
        if let b = body { out.append(b) }
        out.withUnsafeBytes { (raw: UnsafeRawBufferPointer) in
            var offset = 0
            while offset < raw.count {
                let n = send(fd, raw.baseAddress! + offset, raw.count - offset, 0)
                if n <= 0 { break }
                offset += n
            }
        }

        // 读响应(按 Content-Length 优先收齐 —— 判断要用"已收 body 字节数",含头会提前 ~200B 截断)
        var response = Data()
        var buf = [UInt8](repeating: 0, count: 32768)
        var contentLength = -1
        var bodyOffset = -1
        while true {
            let n = recv(fd, &buf, buf.count, 0)
            if n <= 0 { break }
            response.append(Data(buf[0..<n]))
            if contentLength < 0,
               let headerEnd = response.range(of: Data("\r\n\r\n".utf8)) {
                bodyOffset = response.distance(from: response.startIndex, to: headerEnd.upperBound)
                let headerStr = String(decoding: response[0..<headerEnd.lowerBound], as: UTF8.self).lowercased()
                if let cl = headerStr.components(separatedBy: "\r\n").first(where: { $0.hasPrefix("content-length:") })?
                    .dropFirst("content-length:".count).trimmingCharacters(in: .whitespaces),
                   let v = Int(cl) {
                    contentLength = v
                }
            }
            if contentLength >= 0, bodyOffset >= 0, response.count - bodyOffset >= contentLength { break }
            if response.count > 33_554_432 { break } // 32MB 上限(ASIA 目录实测 12.4MB)
        }
        
        // 解析 HTTP 状态码和 body
        guard let headerEnd = response.range(of: Data("\r\n\r\n".utf8)) else {
            throw ApiError(status: 0, message: "响应格式异常")
        }
        let headerStr = String(decoding: response[0..<headerEnd.lowerBound], as: UTF8.self)
        var bodyData = response[headerEnd.upperBound...]

        // Transfer-Encoding: chunked —— 大响应(如 MRTG 31KB)Go 后端会走分块编码,
        // 每块前有十六进制长度行("7b5b\r\n"),不剥掉的话 JSON 开头就坏了(实测踩坑)
        if headerStr.lowercased().contains("transfer-encoding: chunked") {
            bodyData = Self.decodeChunked(bodyData)
        }
        
        let statusLine = headerStr.components(separatedBy: "\r\n").first ?? ""
        let parts = statusLine.components(separatedBy: " ")
        let statusCode = Int(parts.count > 1 ? parts[1] : "0") ?? 0
        
        return (statusCode, Data(bodyData))
    }

    /// URLSession 直连 https 公开接口(OVH 公开目录)。
    /// https 不在 ATS 拦截范围,走系统栈反而能吃到连接复用与 CDN 优势。
    private static let httpsSession: URLSession = {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.timeoutIntervalForRequest = 20
        cfg.timeoutIntervalForResource = 60   // 12MB 大目录给足资源时间
        return URLSession(configuration: cfg)
    }()

    static func httpsJSON(_ url: String, timeoutSec: Double = 15) -> [String: Any]? {
        guard let u = URL(string: url) else { return nil }
        let sem = DispatchSemaphore(value: 0)
        var out: [String: Any]? = nil
        let task = Self.httpsSession.dataTask(with: u) { data, resp, _ in
            defer { sem.signal() }
            guard let http = resp as? HTTPURLResponse, http.statusCode == 200,
                  let d = data,
                  let obj = try? JSONSerialization.jsonObject(with: d) as? [String: Any] else { return }
            out = obj
        }
        task.resume()
        sem.wait()
        return out
    }

    private func request(_ method: String, _ path: String, bodyData: Data? = nil, timeoutSec: Int = 15) async throws -> Data {
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
        // posixHTTP 是同步阻塞调用,绝不能跑在 Swift 协作线程池里:
        // 概览页一次并发 6 个请求就会把池(≈CPU 核数)占满,MRTG 之类慢请求
        // 直接饿死,整个 UI 冻住(web 正常、App 看不到数据的根因)。
        // detached 任务跑在独立线程,不占协作池。
        let (status, data) = try await Task.detached(priority: .userInitiated) {
            try Self.posixHTTP(method, url: u.absoluteString, body: bodyData, extraHeaders: headers, timeoutSec: timeoutSec)
        }.value
        guard (200..<300).contains(status) else {
            if let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                for key in ["message", "error", "msg"] {
                    if let s = obj[key] as? String, !s.isEmpty {
                        throw ApiError(status: status, message: s)
                    }
                }
            }
            if status == 401 {
                // G-034:令牌失效 —— 广播回配对(web 是固定 toast+重弹登录;App 对应回配对)
                await MainActor.run {
                    NotificationCenter.default.post(name: .tokenInvalidated, object: nil)
                }
                throw ApiError(status: status, message: "登录状态已失效,请重新输入配对码")
            }
            throw ApiError(status: status, message: "请求失败(HTTP \(status))")
        }
        return data
    }

    /// GET → 泛型解码
    func get<T: Decodable>(_ path: String, as type: T.Type) async throws -> T {
        let data = try await request("GET", path)
        return try JSONDecoder().decode(T.self, from: data)
    }

    /// GET → 原始字典(后端大量接口字段动态,先以字典落地,逐步固化模型)
    func getDict(_ path: String, timeoutSec: Int = 15) async throws -> [String: Any] {
        let data = try await request("GET", path, timeoutSec: timeoutSec)
        if let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
            return obj
        }
        MrtgLog.log("getDict 解析失败 path=\(path) bytes=\(data.count) head=\(String(decoding: data.prefix(200), as: UTF8.self))")
        return [:]
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
    func getArray(_ path: String, timeoutSec: Int = 15) async throws -> [[String: Any]] {
        let data = try await request("GET", path, timeoutSec: timeoutSec)
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
