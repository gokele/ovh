import SwiftUI

/**
 * 重装系统 sheet(SwiftUI):OS 模板选择 → 智能分区说明 → 输机器名 → 确认 → Face ID → POST /install。
 */
struct ReinstallSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let serviceName: String
    let serverName: String
    @Environment(\.dismiss) private var dismiss

    @State private var templates: [[String: Any]] = []
    @State private var picked: String?
    @State private var confirmName = ""
    @State private var err: String?
    @State private var loading = true
    @State private var busy = false

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                HStack(spacing: 7) {
                    Image(systemName: "opticaldiscdrive.fill").font(.system(size: 15)).foregroundColor(t.color(t.danger))
                    Text("重装系统").font(.system(size: 15, weight: .bold)).foregroundColor(t.color(t.fg))
                }
                Spacer()
                Button { dismiss() } label: { Image(systemName: "xmark").font(.system(size: 15)).foregroundColor(t.color(t.muted)) }
            }
            .padding(16)

            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    // 不可逆警告
                    HStack(alignment: .top, spacing: 8) {
                        Image(systemName: "exclamationmark.triangle.fill").font(.system(size: 13)).foregroundColor(t.color(t.danger))
                        Text("清空系统盘所有数据,不可逆。默认使用智能分区方案;自定义分区请到网页端。")
                            .font(.system(size: 11)).foregroundColor(t.color(t.fg))
                    }
                    .padding(10)
                    .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.danger).opacity(0.05)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.danger).opacity(0.27), lineWidth: 1)))

                    Text("选择系统").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))

                    if let e = err {
                        Text(e).font(.system(size: 12)).foregroundColor(t.color(t.danger))
                    } else if loading {
                        ProgressView().padding(10)
                    } else {
                        VStack(spacing: 6) {
                            ForEach(templates.prefix(30).indices, id: \.self) { i in
                                tplRow(templates[i])
                            }
                        }
                    }

                    Text("输入机器名确认").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    TextField(serviceName, text: $confirmName)
                        .font(.system(size: 13, design: .monospaced))
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .foregroundColor(t.color(t.fg))
                        .padding(.horizontal, 13)
                        .frame(height: 44)
                        .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))

                    Button { Task { await submit() } } label: {
                        Text(busy ? "提交中…" : "面容确认并重装")
                            .font(.system(size: 15, weight: .semibold)).foregroundColor(.white)
                            .frame(maxWidth: .infinity, minHeight: 47)
                            .background(RoundedRectangle(cornerRadius: 14).fill(t.color(t.danger)))
                    }
                    .buttonStyle(.plain)
                    .disabled(busy)
                }
                .padding(14)
            }
        }
        .background(t.color(t.surface))
        .task { await load() }
    }

    private func tplRow(_ tpl: [String: Any]) -> some View {
        let name = tpl["templateName"] as? String ?? ""
        let on = picked == name
        return Button { picked = name } label: {
            HStack(spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(tpl["distribution"] as? String ?? name).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                    Text("\(name) · \(tpl["bitFormat"] ?? 64)-bit")
                        .font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.muted))
                }
                Spacer()
                if on {
                    Circle().fill(t.color(t.fg)).frame(width: 16, height: 16)
                        .overlay(Circle().fill(t.color(t.surface)).frame(width: 6, height: 6))
                }
            }
            .padding(11)
            .background(RoundedRectangle(cornerRadius: 12).fill(on ? t.color(t.surfaceMuted) : t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 12).stroke(t.color(on ? t.fg : t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(serviceName)/templates")
            templates = (r["templates"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func submit() async {
        guard let tpl = picked else { return alert("先选系统", "从上面的列表选一个模板") }
        guard confirmName.trimmingCharacters(in: .whitespaces) == serviceName else {
            return alert("机器名不匹配", "请输入完整的机器名:\(serviceName)")
        }
        busy = true
        defer { busy = false }
        guard await Biometric.require("重装系统") else { return }
        do {
            _ = try await conn.client.post("/server-control/\(serviceName)/install", body: ["templateName": tpl])
            dismiss()
            alert("重装任务已提交", "通常 5-10 分钟完成")
        } catch {
            alert("失败", error.localizedDescription)
        }
    }

    private func alert(_ title: String, _ msg: String) {
        let a = UIAlertController(title: title, message: msg, preferredStyle: .alert)
        a.addAction(UIAlertAction(title: "好", style: .default))
        UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.keyWindow?.rootViewController }.first?.present(a, animated: true)
    }
}

/**
 * VPS 重装 sheet:镜像选择 → SSH key 可选 → 输名 → Face ID → POST /vps-control/:sn/reinstall。
 */
struct VpsReinstallSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let vpsName: String
    @Environment(\.dismiss) private var dismiss

    @State private var templates: [[String: Any]] = []
    @State private var pickedId: String?
    @State private var sshKey = ""
    @State private var noMail = false
    @State private var confirmName = ""
    @State private var loading = true

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                HStack(spacing: 7) {
                    Image(systemName: "opticaldiscdrive.fill").font(.system(size: 15)).foregroundColor(t.color(t.danger))
                    Text("重装 VPS").font(.system(size: 15, weight: .bold)).foregroundColor(t.color(t.fg))
                }
                Spacer()
                Button { dismiss() } label: { Image(systemName: "xmark").font(.system(size: 15)).foregroundColor(t.color(t.muted)) }
            }
            .padding(16)

            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    Text("选择镜像").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    if loading {
                        ProgressView().padding(10)
                    } else {
                        VStack(spacing: 6) {
                            ForEach(templates.indices, id: \.self) { i in
                                imgRow(templates[i])
                            }
                        }
                    }

                    inputField("SSH key 名称(可选)", text: $sshKey)
                    Toggle(isOn: $noMail) {
                        Text("不发送密码邮件(配 SSH key 用)").font(.system(size: 12.5)).foregroundColor(t.color(t.fg))
                    }
                    .tint(t.color(t.success))

                    Text("输入名称确认").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    inputFieldMonospace(vpsName, text: $confirmName)

                    Button { Task { await submit() } } label: {
                        Text("面容确认并重装").font(.system(size: 15, weight: .semibold)).foregroundColor(.white)
                            .frame(maxWidth: .infinity, minHeight: 47)
                            .background(RoundedRectangle(cornerRadius: 14).fill(t.color(t.danger)))
                    }
                    .buttonStyle(.plain)
                }
                .padding(14)
            }
        }
        .background(t.color(t.surface))
        .task { await load() }
    }

    private func imgRow(_ tpl: [String: Any]) -> some View {
        let id = String(describing: tpl["id"] ?? "")
        let on = pickedId == id
        return Button { pickedId = id } label: {
            HStack {
                Text(tpl["name"] as? String ?? id).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                Spacer()
                if on {
                    Circle().fill(t.color(t.fg)).frame(width: 16, height: 16)
                        .overlay(Circle().fill(t.color(t.surface)).frame(width: 6, height: 6))
                }
            }
            .padding(11)
            .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 12).stroke(t.color(on ? t.fg : t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }

    private func load() async {
        if let r = try? await conn.client.getDict("/vps-control/\(vpsName)/templates") {
            templates = (r["templates"] as? [[String: Any]]) ?? []
        }
        loading = false
    }

    private func submit() async {
        guard let id = pickedId else { return }
        guard confirmName.trimmingCharacters(in: .whitespaces) == vpsName else { return }
        guard await Biometric.require("重装 VPS") else { return }
        var body: [String: Any] = ["templateId": id, "doNotSendPassword": noMail]
        let k = sshKey.trimmingCharacters(in: .whitespaces)
        if !k.isEmpty { body["sshKey"] = [k] }
        _ = try? await conn.client.post("/vps-control/\(vpsName)/reinstall", body: body)
        dismiss()
    }

    private func inputField(_ ph: String, text: Binding<String>) -> some View {
        TextField(ph, text: text)
            .font(.system(size: 13)).textInputAutocapitalization(.never).autocorrectionDisabled()
            .foregroundColor(t.color(t.fg)).padding(.horizontal, 13).frame(height: 44)
            .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))
    }
    private func inputFieldMonospace(_ ph: String, text: Binding<String>) -> some View {
        TextField(ph, text: text)
            .font(.system(size: 13, design: .monospaced)).textInputAutocapitalization(.never).autocorrectionDisabled()
            .foregroundColor(t.color(t.fg)).padding(.horizontal, 13).frame(height: 44)
            .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))
    }
}
