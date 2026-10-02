import SwiftUI

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
            err = error.localizedDescription
        }
    }
}
