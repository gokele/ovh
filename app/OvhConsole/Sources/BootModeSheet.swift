import SwiftUI

/**
 * 启动模式切换(SwiftUI sheet):列出 boot 项,当前项打标;
 * 选择后三选一(取消/只切换/切换并立即重启)—— 官方流程切换必须重启生效。
 */
struct BootModeSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let serviceName: String
    @Environment(\.dismiss) private var dismiss

    @State private var modes: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("启动模式").font(.system(size: 15, weight: .bold)).foregroundColor(t.color(t.fg))
                Spacer()
                Button { dismiss() } label: { Image(systemName: "xmark").font(.system(size: 15)).foregroundColor(t.color(t.muted)) }
            }
            .padding(16)

            ScrollView {
                VStack(spacing: 8) {
                    if let e = err {
                        Text(e).font(.system(size: 12)).foregroundColor(t.color(t.danger)).padding(16)
                    } else if loading {
                        ProgressView().padding(20)
                    } else {
                        ForEach(modes.indices, id: \.self) { i in
                            bootRow(modes[i])
                        }
                    }
                }
                .padding(.horizontal, 12)
            }
            Text("切换后需重启生效;救援建议用「一键救援」(带邮箱)")
                .font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(16)
        }
        .background(t.color(t.surface))
        .task { await load() }
    }

    private func bootRow(_ m: [String: Any]) -> some View {
        let active = m["active"] as? Bool ?? false
        let type = m["bootType"] as? String ?? ""
        return Button { pick(m) } label: {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        Text(type).font(.system(size: 12.5, weight: .semibold, design: .monospaced)).foregroundColor(t.color(t.fg))
                        if active {
                            Text("当前").font(.system(size: 9, weight: .bold)).foregroundColor(t.color(t.success))
                                .padding(.horizontal, 6).padding(.vertical, 1.5)
                                .background(Capsule().fill(t.color(t.success).opacity(0.12)))
                        }
                    }
                    Text(m["description"] as? String ?? m["kernel"] as? String ?? "—")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.muted)).lineLimit(1)
                }
                Spacer()
                if active { Image(systemName: "checkmark").font(.system(size: 13)).foregroundColor(t.color(t.success)) }
            }
            .padding(12)
            .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 13).stroke(t.color(active ? t.fg : t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }

    private func pick(_ m: [String: Any]) {
        guard !(m["active"] as? Bool ?? false) else { return }
        let alert = UIAlertController(
            title: "切换到 \(m["bootType"] as? String ?? "")?",
            message: (m["description"] as? String ?? "") + "\n\n切换启动模式需要重启才生效。",
            preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: "取消", style: .cancel))
        alert.addAction(UIAlertAction(title: "只切换", style: .default) { _ in
            Task { await apply(m, reboot: false) }
        })
        alert.addAction(UIAlertAction(title: "切换并重启", style: .destructive) { _ in
            Task { await apply(m, reboot: true) }
        })
        root?.present(alert, animated: true)
    }

    private func apply(_ m: [String: Any], reboot: Bool) async {
        do {
            _ = try await conn.client.put("/server-control/\(serviceName)/boot-mode", body: ["bootId": m["id"] ?? 0])
            if reboot {
                _ = try await conn.client.post("/server-control/\(serviceName)/reboot", body: [:])
            }
            await load()
        } catch {
            root?.present(failAlert(error.localizedDescription), animated: true)
        }
    }

    private func load() async {
        loading = true
        do {
            let r = try await conn.client.getDict("/server-control/\(serviceName)/boot-mode")
            modes = (r["bootModes"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private var root: UIViewController? {
        UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.keyWindow?.rootViewController }.first
    }
    private func failAlert(_ msg: String) -> UIAlertController {
        let a = UIAlertController(title: "失败", message: msg, preferredStyle: .alert)
        a.addAction(UIAlertAction(title: "好", style: .default))
        return a
    }
}
