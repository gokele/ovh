import SwiftUI

/**
 * 维护类写操作 sheets:
 * - RetractionSheet:撤单提交(理由七选一,对齐后端 RetractionReasonEnum;不可逆+双确认)
 * - HardwareReplaceSheet:硬件更换(硬盘:序列号逐行 / 内存:症状描述 / 散热)
 *   契约均对后端 handler 逐字段核过(componentType/comment/disks/inverse/details/slots)。
 */

// MARK: - 撤单提交

struct RetractionSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let serviceName: String
    @Environment(\.dismiss) private var dismiss

    /// 后端 RetractionReasonEnum(七选一;对齐 web RetractionDialog)
    private static let reasons: [(code: String, label: String)] = [
        ("competitor", "找到了更便宜的供应商"),
        ("difficulty", "使用上有困难"),
        ("expensive", "价格太贵"),
        ("other", "其他原因"),
        ("performance", "性能不满足"),
        ("reliability", "稳定性/可靠性问题"),
        ("unused", "不再需要"),
    ]

    @State private var reason = ""
    @State private var comment = ""
    @State private var busy = false

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                HStack(spacing: 7) {
                    Image(systemName: "shield.slash.fill").font(.system(size: 15)).foregroundColor(t.color(t.danger))
                    Text("撤回订单").font(.system(size: 15, weight: .bold)).foregroundColor(t.color(t.fg))
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
                        Text("撤单 = 退款 + 服务器注销,数据将被清除,不可逆。")
                            .font(.system(size: 11)).foregroundColor(t.color(t.fg))
                    }
                    .padding(10)
                    .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.danger).opacity(0.05)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.danger).opacity(0.27), lineWidth: 1)))

                    Text("选择理由(必填)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    VStack(spacing: 6) {
                        ForEach(Self.reasons, id: \.code) { r in
                            Button { reason = r.code } label: {
                                HStack {
                                    Text(r.label).font(.system(size: 12.5)).foregroundColor(t.color(t.fg))
                                    Spacer()
                                    if reason == r.code {
                                        Circle().fill(t.color(t.fg)).frame(width: 16, height: 16)
                                            .overlay(Circle().fill(t.color(t.surface)).frame(width: 6, height: 6))
                                    }
                                }
                                .padding(11)
                                .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 12).stroke(t.color(reason == r.code ? t.fg : t.border), lineWidth: 1)))
                            }
                            .buttonStyle(.plain)
                        }
                    }

                    TextField("补充说明(可选)", text: $comment, axis: .vertical)
                        .font(.system(size: 13))
                        .lineLimit(3...5)
                        .foregroundColor(t.color(t.fg))
                        .padding(.horizontal, 13).padding(.vertical, 9)
                        .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))

                    Button { Task { await submit() } } label: {
                        Text(busy ? "提交中…" : "面容确认并撤单")
                            .font(.system(size: 15, weight: .semibold)).foregroundColor(.white)
                            .frame(maxWidth: .infinity, minHeight: 47)
                            .background(RoundedRectangle(cornerRadius: 14).fill(t.color(t.danger)))
                    }
                    .buttonStyle(.plain)
                    .disabled(busy || reason.isEmpty)
                    .opacity(reason.isEmpty ? 0.5 : 1)
                }
                .padding(14)
            }
        }
        .background(t.color(t.surface))
    }

    /// 契约:POST /retraction {reason, comment, confirm:true}(confirm 是后端强制的显式确认)
    private func submit() async {
        guard await Biometric.require("撤回订单") else { return }
        busy = true
        defer { busy = false }
        let payload: [String: Any] = ["reason": reason, "comment": comment, "confirm": true]
        let data = try? JSONSerialization.data(withJSONObject: payload)
        let (ok, msg) = await conn.client.actionPostData("/server-control/\(serviceName)/retraction", bodyData: data)
        dismiss()
        _ = msg
        if ok {
            showAlert("撤单申请已提交", "退款按 OVH 规则处理;服务器将在处理完成后注销")
        } else {
            showAlert("失败", msg)
        }
    }

    private func showAlert(_ title: String, _ msg: String) {
        let a = UIAlertController(title: title, message: msg, preferredStyle: .alert)
        a.addAction(UIAlertAction(title: "好", style: .default))
        AlertHost.present(a)
    }
}

// MARK: - 硬件更换

struct HardwareReplaceSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let serviceName: String
    @Environment(\.dismiss) private var dismiss

    private enum Kind: String, CaseIterable {
        case hardDiskDrive = "hardDiskDrive"
        case memory = "memory"
        case cooling = "cooling"
        var label: String {
            switch self {
            case .hardDiskDrive: return "硬盘"
            case .memory: return "内存"
            case .cooling: return "散热"
            }
        }
    }

    @State private var kind: Kind = .hardDiskDrive
    @State private var serials = ""     // 硬盘:序列号,每行一个
    @State private var inverse = false  // 硬盘:故障盘读不出序列号时,改列健康盘
    @State private var details = ""     // 内存/散热:症状描述
    @State private var busy = false

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                HStack(spacing: 7) {
                    Image(systemName: "wrench.and.screwdriver").font(.system(size: 15)).foregroundColor(t.color(t.fg))
                    Text("硬件更换(工单)").font(.system(size: 15, weight: .bold)).foregroundColor(t.color(t.fg))
                }
                Spacer()
                Button { dismiss() } label: { Image(systemName: "xmark").font(.system(size: 15)).foregroundColor(t.color(t.muted)) }
            }
            .padding(16)

            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    // 类型选择
                    Picker("类型", selection: $kind) {
                        ForEach(Kind.allCases, id: \.self) { k in
                            Text(k.label).tag(k)
                        }
                    }
                    .pickerStyle(.segmented)

                    if kind == .hardDiskDrive {
                        Text("故障盘序列号(每行一个,必填)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        TextEditor(text: $serials)
                            .font(.system(size: 12.5, design: .monospaced))
                            .frame(minHeight: 88)
                            .scrollContentBackground(.hidden)
                            .padding(8)
                            .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))

                        Toggle(isOn: $inverse) {
                            VStack(alignment: .leading, spacing: 2) {
                                Text("故障盘读不出序列号(反选模式)").font(.system(size: 12.5)).foregroundColor(t.color(t.fg))
                                Text("上面的框改为填【健康盘】的序列号,OVH 会更换所有未列出的盘")
                                    .font(.system(size: 10)).foregroundColor(t.color(t.warning))
                            }
                        }
                        .tint(t.color(t.warning))
                    } else {
                        Text("故障描述(必填)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        TextField(kind == .memory ? "例:2 号槽内存报错,机器频繁重启" : "例:风扇异响,温度偏高", text: $details, axis: .vertical)
                            .font(.system(size: 13))
                            .lineLimit(3...5)
                            .foregroundColor(t.color(t.fg))
                            .padding(.horizontal, 13).padding(.vertical, 9)
                            .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))
                    }

                    Text("提交后 OVH 创建工单,单号在网页端维护页查看;换盘会涉及数据,建议先备份。")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.faint))

                    Button { Task { await submit() } } label: {
                        Text(busy ? "提交中…" : "面容确认并提交工单")
                            .font(.system(size: 15, weight: .semibold)).foregroundColor(.white)
                            .frame(maxWidth: .infinity, minHeight: 47)
                            .background(RoundedRectangle(cornerRadius: 14).fill(t.color(t.danger)))
                    }
                    .buttonStyle(.plain)
                    .disabled(busy || !valid)
                    .opacity(!valid ? 0.5 : 1)
                }
                .padding(14)
            }
        }
        .background(t.color(t.surface))
    }

    private var valid: Bool {
        if kind == .hardDiskDrive {
            return !parsedSerials.isEmpty
        }
        return !details.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private var parsedSerials: [String] {
        serials
            .components(separatedBy: .newlines)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
    }

    /// 契约(对后端 HardwareReplace 核过):
    /// 硬盘 {componentType:"hardDiskDrive", comment, disks:[串行号], inverse}
    /// 内存 {componentType:"memory", comment, details, slots?}(slots 选填,App 不收集)
    /// 散热 {componentType:"cooling", comment, details}
    private func submit() async {
        guard await Biometric.require("硬件更换") else { return }
        busy = true
        defer { busy = false }

        var payload: [String: Any] = ["componentType": kind.rawValue]
        switch kind {
        case .hardDiskDrive:
            payload["disks"] = parsedSerials.map { ["disk_serial": $0] }
            payload["inverse"] = inverse
            payload["comment"] = inverse
                ? "列出的为健康盘,更换未列出的故障盘"
                : "Request hard disk drive replacement - faulty disk detected"
        case .memory:
            payload["details"] = details
            payload["comment"] = "Request memory module replacement - hardware failure detected"
        case .cooling:
            payload["details"] = details
            payload["comment"] = "Request cooling system replacement - fan failure or overheating"
        }

        let data = try? JSONSerialization.data(withJSONObject: payload)
        let (ok, msg) = await conn.client.actionPostData("/server-control/\(serviceName)/hardware/replace", bodyData: data)
        dismiss()
        if ok {
            showAlert("工单已提交", "单号请到网页端维护页查看")
        } else {
            showAlert("失败", msg)
        }
    }

    private func showAlert(_ title: String, _ msg: String) {
        let a = UIAlertController(title: title, message: msg, preferredStyle: .alert)
        a.addAction(UIAlertAction(title: "好", style: .default))
        AlertHost.present(a)
    }
}
