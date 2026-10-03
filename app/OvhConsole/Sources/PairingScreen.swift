import SwiftUI
import os

/**
 * 配对首屏(新设计):地址 + 8 位码 → 设备令牌进 Keychain。
 * 深链 ovhconsole://pair?host=..&code=..&auto=1 支持(从设置页二维码扫码进入)。
 */
struct PairingScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    var deepLink: URL?
    var onPaired: () -> Void

    @State private var url = ""
    @State private var code = ""
    @State private var busy = false
    @State private var err = ""

    var t: Tokens { theme.t }

    var body: some View {
        ScrollView {
            VStack(spacing: 16) {
                // 品牌区
                VStack(spacing: 12) {
                    ZStack {
                        RoundedRectangle(cornerRadius: 20)
                            .fill(t.color(t.accent).opacity(0.12))
                            .frame(width: 84, height: 84)
                        Image(systemName: "server.rack")
                            .font(.system(size: 34, weight: .medium))
                            .foregroundColor(t.color(t.accent))
                    }
                    .padding(.top, 56)
                    VStack(spacing: 5) {
                        Text("OVH 控制台").font(.system(size: 22, weight: .bold)).foregroundColor(t.color(t.fg))
                        Text("连接你的自建后端").font(.system(size: 12)).foregroundColor(t.color(t.muted))
                    }
                }

                Card {
                    VStack(alignment: .leading, spacing: 14) {
                        Text("配对").font(.system(size: 14, weight: .bold)).foregroundColor(t.color(t.fg))

                        VStack(alignment: .leading, spacing: 6) {
                            Text("控制台地址").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.muted))
                            SheetField(placeholder: "http://192.168.1.10:19998", text: $url, mono: true, keyboard: .URL)
                        }
                        VStack(alignment: .leading, spacing: 6) {
                            Text("配对码(8 位)").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.muted))
                            SheetField(placeholder: "在网页端 设置 → App 配对 生成", text: $code, mono: true)
                        }

                        if !err.isEmpty {
                            HStack(alignment: .top, spacing: 7) {
                                Image(systemName: "exclamationmark.octagon.fill").font(.system(size: 12)).foregroundColor(t.color(t.danger))
                                Text(err).font(.system(size: 11)).foregroundColor(t.color(t.danger))
                            }
                            .padding(9)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.danger).opacity(0.08)))
                        }

                        ActBtn(kind: .primary, icon: "link", label: busy ? "配对中…" : "开始配对", busy: busy) {
                            Task { await pair() }
                        }
                    }
                }

                VStack(spacing: 4) {
                    Text("2 分钟内有效 · 每台设备独立令牌")
                    Text("手机丢了在网页端「设置 → App 配对」吊销")
                }
                .font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                .multilineTextAlignment(.center)
            }
            .padding(.horizontal, 22)
        }
        .background(t.color(t.bg))
        .onChange(of: deepLink) { nl in
            if let nl { consume(nl) }
        }
        .onAppear {
            if let dl = deepLink { consume(dl) }
        }
    }

    private func consume(_ url: URL) {
        guard url.scheme == "ovhconsole", url.host == "pair" else { return }
        let comps = URLComponents(url: url, resolvingAgainstBaseURL: false)
        let items = comps?.queryItems ?? []
        if let h = items.first(where: { $0.name == "host" })?.value { self.url = h }
        if let c = items.first(where: { $0.name == "code" })?.value { code = c }
        if let a = items.first(where: { $0.name == "auto" })?.value, a == "1" {
            Task {
                try? await Task.sleep(nanoseconds: 250_000_000)
                await pair()
            }
        }
    }

    private func pair() async {
        err = ""
        let clean = url.trimmingCharacters(in: .whitespacesAndNewlines)
        guard clean.hasPrefix("http://") || clean.hasPrefix("https://") else {
            err = "地址要以 http:// 或 https:// 开头"
            return
        }
        let c = code.trimmingCharacters(in: .whitespaces).uppercased()
        guard c.count == 8 else { err = "配对码是 8 位(网页端设置 → App 配对 生成)"; return }
        busy = true
        defer { busy = false }
        do {
            let r = try await ApiClient.pair(serverUrl: clean, code: c, deviceName: UIDevice.current.name)
            // conn 是 @MainActor;从 async 上下文调它必须 await(否则静默失败/挂起)
            await conn.save(server: clean, token: r.token)
            onPaired()
        } catch {
            PairLog.log("配对失败: \(error)")
            err = explainPairError(error, address: clean)
        }
    }
}

/// 配对失败的错误翻译:底层错误变排查指引
private func explainPairError(_ error: Error, address: String) -> String {
    let ns = error as NSError
    if ns.domain == NSURLErrorDomain {
        switch ns.code {
        case NSURLErrorTimedOut:
            return "连接超时:\(address) 没有响应。检查地址和端口,确认手机能访问到这台服务器"
        case NSURLErrorCannotFindHost, NSURLErrorCannotConnectToHost:
            return "连不上 \(address):地址不对或服务没在跑。手机要能访问到服务器(同一网络或公网可达)"
        case -1022:
            return "iOS 安全策略拦了 http 连接(已知配置,请重装本构建)"
        case NSURLErrorNotConnectedToInternet:
            return "手机没有网络连接"
        default:
            break
        }
    }
    // ApiClient 已经把后端 4xx 的中文 error 解出来了,直接显示
    return error.localizedDescription
}

/// 轻量日志(系统 Console 可见)
private struct PairLog {
    static let shared = os.Logger(subsystem: "com.gokele.ovhconsole", category: "pairing")
    static func log(_ msg: String) {
        shared.info("\(msg, privacy: .public)")
    }
}
