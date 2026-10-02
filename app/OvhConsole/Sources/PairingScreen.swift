import SwiftUI
import UniformTypeIdentifiers

/**
 * 配对首屏(SwiftUI):地址 + 8 位码 → Keychain。
 */
struct PairingScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @State private var url = ""
    @State private var code = ""
    @State private var busy = false
    @State private var err = ""

    var t: Tokens { theme.t }

    var body: some View {
        ScrollView {
            VStack(spacing: 14) {
                RoundedRectangle(cornerRadius: 18)
                    .stroke(t.color(t.fg), lineWidth: 1.6)
                    .frame(width: 74, height: 74)
                    .overlay(Image(systemName: "server.rack").font(.system(size: 32)).foregroundColor(t.color(t.fg)))
                    .padding(.top, 60)

                Text("连接你的控制台").font(.system(size: 17, weight: .bold)).foregroundColor(t.color(t.fg))

                Text("在电脑端打开网页控制台 → 设置 →「App 配对」,\n把地址和配对码填到下面。\n2 分钟内有效 · 每台设备独立令牌,可在网页端单独吊销")
                    .font(.system(size: 12)).foregroundColor(t.color(t.muted))
                    .multilineTextAlignment(.center)
                    .lineSpacing(4)

                field("控制台地址,如 http://192.168.1.10:19998", text: $url, keyboard: .URL)
                field("配对码(8 位)", text: $code, keyboard: .asciiCapable)

                if !err.isEmpty {
                    Text(err).font(.system(size: 12)).foregroundColor(t.color(t.danger))
                }

                Button { Task { await pair() } } label: {
                    HStack(spacing: 8) {
                        if busy { ProgressView().tint(.white) }
                        Text(busy ? "配对中…" : "配对").font(.system(size: 15, weight: .semibold)).foregroundColor(.white)
                    }
                    .frame(maxWidth: .infinity, minHeight: 47)
                    .background(RoundedRectangle(cornerRadius: 14).fill(Color(red: 0.07, green: 0.07, blue: 0.07)))
                }
                .buttonStyle(.plain)
                .disabled(busy)
            }
            .padding(.horizontal, 28)
        }
        .background(t.color(t.bg))
        .onOpenURL { url in
            // ovhconsole://pair?host=http://x:19997&code=ABCDEFGH
            guard url.host == "pair" else { return }
            let comps = URLComponents(url: url, resolvingAgainstBaseURL: false)
            let items = comps?.queryItems ?? []
            if let h = items.first(where: { $0.name == "host" })?.value { self.url = h }
            if let c = items.first(where: { $0.name == "code" })?.value { code = c }
        }
    }

    private func field(_ ph: String, text: Binding<String>, keyboard: UIKeyboardType) -> some View {
        TextField(ph, text: text)
            .font(.system(size: 14))
            .keyboardType(keyboard)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .foregroundColor(t.color(t.fg))
            .padding(.horizontal, 13)
            .frame(height: 46)
            .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 12).stroke(t.color(t.border), lineWidth: 1)))
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
            conn.save(server: clean, token: r.token)
        } catch {
            err = explainPairError(error, address: clean)
        }
    }
}


/// 配对失败的错误翻译:把底层 URLError 变成能照做的排查指引
private func explainPairError(_ error: Error, address: String) -> String {
    let ns = error as NSError
    if ns.domain == NSURLErrorDomain {
        switch ns.code {
        case NSURLErrorTimedOut:
            return "连接超时:\(address) 没有响应。检查地址和端口(默认 19998),确认手机能访问到这台服务器"
        case NSURLErrorCannotFindHost, NSURLErrorCannotConnectToHost:
            return "连不上 \(address):地址不对或服务没在跑。注意手机要能访问到服务器(同一网络,或公网可达)"
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
