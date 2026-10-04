import SwiftUI

/**
 * 独服弹窗 · 维护/高级篇:撤单 / 续费策略 / 合同期 / 硬件更换 / 变更联系人 /
 * Burst / 防火墙 / Backup FTP / 通用数据浏览 / DDoS 缓解
 */

// MARK: - 14 天撤单

struct RetractionSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let sn: String
    var t: Tokens { theme.t }

    @State private var info: [String: Any]?
    @State private var reasons: [[String: Any]] = []
    @State private var pickedReason: String?
    @State private var comment = ""
    @State private var loading = true
    @State private var err: String?
    @State private var busy = false
    @State private var confirming = false

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "arrow.uturn.left.circle", tint: t.info, title: "14 天无理由撤单")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else if let msg = ineligibleMsg {
                        EmptyHint(icon: "clock.slash", text: msg)
                    } else {
                        if let deadline = info?["retractionDate"] as? String ?? info?["deadline"] as? String, !deadline.isEmpty {
                            SheetNote(text: "可撤单截止:\(fmtDate(deadline))。撤单后服务器将被回收并退款。", tint: t.warning)
                        } else {
                            SheetNote(text: "撤单后服务器将被回收并退款,不可恢复。", tint: t.danger)
                        }

                        Text("选择撤单原因").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        VStack(spacing: 6) {
                            ForEach(reasons.indices, id: \.self) { i in
                                let r = reasons[i]
                                let code = r["value"] as? String ?? (r["code"] as? String ?? "")
                                let on = pickedReason == code
                                Button { pickedReason = code } label: {
                                    HStack {
                                        Text(r["label"] as? String ?? code)
                                            .font(.system(size: 12)).foregroundColor(t.color(t.fg))
                                        Spacer()
                                        if on { Image(systemName: "checkmark.circle.fill").font(.system(size: 14)).foregroundColor(t.color(t.accent)) }
                                    }
                                    .padding(10)
                                    .background(RoundedRectangle(cornerRadius: 11).fill(t.color(on ? t.accent : t.surface).opacity(0.08)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(on ? t.accent : t.border), lineWidth: 1)))
                                }.buttonStyle(.plain)
                            }
                        }

                        Text("备注(可选)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetField(placeholder: "补充说明", text: $comment)

                        ActBtn(kind: .danger, icon: "arrow.uturn.left", label: "申请撤单") { confirming = true }
                            .disabled(pickedReason == nil)
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.large])
        .task { await load() }
        .sheet(isPresented: $confirming) {
            ConfirmSheet(title: "确认撤单", message: "服务器 \(sn) 将被 OVH 回收,合同终止并退款。此操作不可恢复。", confirmText: "确认撤单") {
                await submit()
            }
        }
    }

    @State private var ineligibleMsg: String? = nil

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/retraction")
            info = r
            reasons = (r["reasons"] as? [[String: Any]]) ?? []
            pickedReason = reasons.first?["value"] as? String
            // 不可撤单的机器后端给 eligible:false + 原因 —— 显示原因并收起表单
            if (r["eligible"] as? Bool) == false {
                ineligibleMsg = (r["message"] as? String) ?? "该服务器不在可撤单期内(交付满 14 天后不可无理由撤回)"
            }
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func submit() async {
        guard let reason = pickedReason else { return }
        busy = true
        defer { busy = false }
        let body: [String: Any] = ["reason": reason, "comment": comment, "confirm": true]
        do {
            _ = try await conn.client.post("/server-control/\(sn)/retraction", body: body)
            toast.show("撤单申请已提交")
            dismiss()
        } catch { toast.show(error.localizedDescription, error: true) }
    }
}

// MARK: - 续费策略(独服 / VPS 共用)

struct RenewalSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let sn: String
    let isVps: Bool
    let info: [String: Any]
    var t: Tokens { theme.t }

    private var base: String { isVps ? "/vps-control" : "/server-control" }

    @State private var mode = 0   // 0 自动 / 1 手动 / 2 到期终止
    @State private var period: Int = 1
    @State private var busy = false
    @State private var possible: [Int] = []

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "arrow.triangle.2.circlepath", tint: t.info, title: "续费策略")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    let forced = info["renewalForced"] as? Bool ?? false

                    if forced {
                        SheetNote(text: "OVH 强制该服务自动续费,策略不可修改。", tint: t.warning)
                    }

                    VStack(spacing: 8) {
                        modeRow(0, icon: "arrow.triangle.2.circlepath", title: "自动续费", desc: "到期自动扣款续期")
                            .disabled(forced)
                        modeRow(1, icon: "hand.raised", title: "手动续费", desc: "到期前自己付款", disabled: forced)
                        modeRow(2, icon: "scissors", title: "到期终止", desc: "到期后删除服务", danger: true, disabled: forced)
                    }

                    if mode == 0 {
                        Text("续费周期").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        FlowLayout(spacing: 7) {
                            ForEach(possible.isEmpty ? [1, 3, 6, 12] : possible, id: \.self) { p in
                                Button { period = p } label: {
                                    Text("\(p) 月").font(.system(size: 11.5)).foregroundColor(t.color(period == p ? t.fg : t.muted))
                                        .padding(.horizontal, 12).padding(.vertical, 7)
                                        .background(RoundedRectangle(cornerRadius: 9).fill(period == p ? t.color(t.accent).opacity(0.2) : t.color(t.surfaceMuted)))
                                }.buttonStyle(.plain)
                            }
                        }
                    }

                    ActBtn(kind: mode == 2 ? .danger : .primary, icon: nil, label: info.isEmpty ? "读取服务信息中…" : (busy ? "保存中…" : "保存策略")) {
                        await submit()
                    }
                    .disabled(forced || busy)
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .onAppear {
            let delete = info["renewalDeleteAtExpiration"] as? Bool ?? false
            let automatic = info["renewalType"] as? Bool ?? true
            mode = delete ? 2 : (automatic ? 0 : 1)
            period = info["renewalPeriod"] as? Int ?? 1
            if let p = info["possibleRenewPeriod"] as? [Any] {
                possible = p.compactMap { ($0 as? Int) ?? Int("\($0)") }
            }
        }
    }

    private func modeRow(_ m: Int, icon: String, title: String, desc: String, danger: Bool = false, disabled: Bool = false) -> some View {
        let on = mode == m
        return Button { mode = m } label: {
            HStack(spacing: 10) {
                Image(systemName: icon).font(.system(size: 14)).foregroundColor(t.color(danger && on ? t.danger : on ? t.accent : t.muted))
                VStack(alignment: .leading, spacing: 1) {
                    Text(title).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                    Text(desc).font(.system(size: 10)).foregroundColor(t.color(t.muted))
                }
                Spacer()
                Image(systemName: on ? "checkmark.circle.fill" : "circle").font(.system(size: 15)).foregroundColor(t.color(on ? (danger ? t.danger : t.accent) : t.faint))
            }
            .padding(11)
            .background(RoundedRectangle(cornerRadius: 12).fill(t.color(on ? (danger ? t.danger : t.accent) : t.surface).opacity(0.07)).overlay(RoundedRectangle(cornerRadius: 12).stroke(t.color(on ? (danger ? t.danger : t.accent) : t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
        .disabled(disabled)
    }

    private func submit() async {
        // 无变化禁保存(S-064)
        let initialMode: Int = {
            if (info["terminationScheduled"] as? Bool ?? false) || (info["renewalDeleteAtExpiration"] as? Bool ?? false) { return 2 }
            if info["renewalType"] as? Bool == true { return 0 }
            return 1
        }()
        let initialPeriod = info["renewalPeriod"] as? Int ?? 1
        if mode == initialMode && (mode != 0 || period == initialPeriod) {
            return
        }
        busy = true
        defer { busy = false }
        do {
            // 契约:自动/手动走 renewal{mode,period};到期终止走 termination-policy
            // (renewal handler 会 400 拒绝 delete);从终止切回必须先撤销终止标记
            if mode == 2 {
                _ = try await conn.client.put("\(base)/\(sn)/termination-policy", body: ["policy": "terminateAtExpirationDate"])
                toast.show("已设为到期终止")
            } else {
                let wasTerminating = (info["terminationScheduled"] as? Bool ?? false) || (info["renewalDeleteAtExpiration"] as? Bool ?? false)
                if wasTerminating {
                    _ = try await conn.client.put("\(base)/\(sn)/termination-policy", body: ["policy": "empty"])
                }
                let modeStr = mode == 0 ? "auto" : "manual"
                _ = try await conn.client.put("\(base)/\(sn)/serviceinfo/renewal",
                                              body: ["mode": modeStr, "period": mode == 0 ? period : 0])
                toast.show(mode == 0 ? "已设为自动续费(\(period) 月)" : "已设为手动续费")
            }
            dismiss()
        } catch { toast.show(error.localizedDescription, error: true) }
    }
}

// MARK: - 合同期(Engagement,独服 / VPS 共用)

struct EngagementSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    let sn: String
    let isVps: Bool
    var t: Tokens { theme.t }

    private var base: String { isVps ? "/vps-control" : "/server-control" }

    @State private var current: [String: Any]?
    @State private var available: [[String: Any]] = []
    @State private var pending: [String: Any]?
    @State private var loading = true
    @State private var err: String?
    @State private var busy = false
    @State private var confirming: String?

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "doc.plaintext", tint: t.info, title: "合同期(承诺期)")
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else {
                        if let cur = current {
                            Card {
                                VStack(spacing: 8) {
                                    SectionTitle(text: "当前承诺期")
                                    KV(k: "模式", v: (cur["pricingMode"] as? String) ?? (cur["mode"] as? String ?? "—"))
                                    KV(k: "开始", v: fmtDate(cur["from"] as? String ?? cur["startDate"] as? String))
                                    KV(k: "结束", v: fmtDate(cur["to"] as? String ?? cur["endDate"] as? String))
                                }
                            }
                        } else {
                            SheetNote(text: "当前按月付费,没有承诺期。订阅承诺期通常更便宜。", tint: t.muted)
                        }

                        if let p = pending {
                            Card(border: t.warning) {
                                VStack(spacing: 8) {
                                    SectionTitle(text: "订单已创建,等待支付")
                                    Text("OVH 已为此变更创建订单,付款前合同期不会生效,服务继续按原月付。30 天未付订单将自动取消。")
                                        .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                                    KV(k: "目标模式", v: modeName(p["pricingMode"] as? String ?? ""))
                                    KV(k: "提交时间", v: fmtDate(p["from"] as? String ?? p["date"] as? String))
                                    if let oid = p["orderId"] as? String, !oid.isEmpty {
                                        KV(k: "订单号", v: oid, mono: true)
                                    }
                                    HStack(spacing: 10) {
                                        if let url = p["url"] as? String ?? p["orderUrl"] as? String, let u = URL(string: url) {
                                            Link(destination: u) {
                                                HStack(spacing: 4) {
                                                    Image(systemName: "safari").font(.system(size: 11))
                                                    Text("前往 OVH 支付").font(.system(size: 12, weight: .semibold))
                                                }.foregroundColor(t.color(t.info))
                                            }
                                        }
                                        ActBtn(kind: .danger, icon: "xmark", label: "撤销请求") {
                                            await cancelPending()
                                        }
                                    }
                                }
                            }
                        }

                        if !available.isEmpty {
                            Text("可订阅承诺期").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                            ForEach(available.indices, id: \.self) { i in
                                let a = available[i]
                                let mode = a["pricingMode"] as? String ?? "—"
                                let price = a["price"] as? [String: Any]
                                let val = price?["value"] as? Double ?? (a["monthlyPrice"] as? Double ?? 0)
                                let cur2 = price?["currencyCode"] as? String ?? (a["currency"] as? String ?? "")
                                let duration = numToDoubleAny(a["duration"]).map(Int.init) ?? 12
                                let perMonth = duration > 0 ? val / Double(duration) : val
                                Button { confirming = mode } label: {
                                    HStack {
                                        VStack(alignment: .leading, spacing: 1) {
                                            HStack(spacing: 5) {
                                                Text(modeName(mode)).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                                                Chip(text: String(describing: a["pricingType"] ?? "periodic").contains("upfront") ? "一次性预付" : "周期付费")
                                            }
                                            Text(a["description"] as? String ?? "").font(.system(size: 10)).foregroundColor(t.color(t.muted)).lineLimit(1)
                                        }
                                        Spacer()
                                        VStack(alignment: .trailing, spacing: 1) {
                                            if val > 0 { Text(String(format: "%.2f %@", val, cur2)).font(.system(size: 11, design: .rounded)).foregroundColor(t.color(t.muted)) }
                                            if perMonth > 0 { Text(String(format: "%.2f %@/月", perMonth, cur2)).font(.system(size: 11.5, design: .rounded)).foregroundColor(t.color(t.accent)) }
                                        }
                                        Image(systemName: "chevron.right").font(.system(size: 10)).foregroundColor(t.color(t.faint))
                                    }
                                    .padding(11)
                                    .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 12).stroke(t.color(t.border), lineWidth: 1)))
                                }
                                .buttonStyle(.plain)
                                .disabled(busy || pending != nil)
                            }
                            if pending != nil {
                                Text("有变更请求处理中,需先撤销才能订阅新承诺期")
                                    .font(.system(size: 10)).foregroundColor(t.color(t.faint))
                            }
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.large])
        .task { await load() }
        .sheet(item: Binding(
            get: { confirming.map { ModeWrap(mode: $0) } },
            set: { confirming = $0?.mode }
        )) { w in
            ConfirmSheet(title: "确认订阅承诺期?", message: "切换到 \(modeName(w.mode))。OVH 创建一笔未付订单;付款前合同期不会激活,服务继续按原月付。一旦付款合同期锁死,中途解约按未消耗月数计违约金。", confirmText: "创建订单", danger: false) {
                await createRequest(w.mode)
            }
        }
    }

    private struct ModeWrap: Identifiable {
        let mode: String
        var id: String { mode }
    }

    private func modeName(_ m: String) -> String {
        switch m {
        case "12months": return "12 个月承诺"
        case "24months": return "24 个月承诺"
        case "36months": return "36 个月承诺"
        case "1month": return "按月"
        default: return m
        }
    }

    private func load() async {
        do {
            async let c = conn.client.getDict("\(base)/\(sn)/engagement")
            async let a = conn.client.getDict("\(base)/\(sn)/engagement/available")
            async let p = conn.client.getDict("\(base)/\(sn)/engagement/request")
            let (cr, ar, pr) = try await (c, a, p)
            current = cr["engagement"] as? [String: Any]
            available = (ar["pricings"] as? [[String: Any]]) ?? (ar["available"] as? [[String: Any]]) ?? []
            pending = pr["request"] as? [String: Any]
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func createRequest(_ mode: String) async {
        busy = true
        defer { busy = false }
        do {
            _ = try await conn.client.post("\(base)/\(sn)/engagement/request", body: ["pricingMode": mode])
            toast.show("变更订单已创建,请到 OVH 完成支付")
            await load()
        } catch { toast.show(error.localizedDescription, error: true) }
    }

    private func cancelPending() async {
        busy = true
        defer { busy = false }
        let (ok, msg) = await conn.client.actionDelete("\(base)/\(sn)/engagement/request")
        toast.show(ok ? "已撤销" : (msg.isEmpty ? "撤销失败" : msg), error: !ok)
        if ok { await load() }
    }
}

// MARK: - 硬件更换工单

struct HardwareReplaceSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let sn: String
    var t: Tokens { theme.t }

    @State private var component = "disk"
    @State private var serials = ""
    @State private var slots = ""
    @State private var comment = ""
    @State private var inverse = false
    @State private var busy = false
    @State private var hwConfirm = false

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "wrench.and.screwdriver", tint: t.warning, title: "硬件更换工单")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    SheetNote(text: "提交后 OVH 机房上门更换部件。更换硬盘必须提供盘序列号(在系统日志/BIOS 里查)。", tint: t.warning)

                    Picker("部件", selection: $component) {
                        Text("硬盘").tag("hardDiskDrive")
                        Text("内存").tag("memory")
                        Text("散热").tag("cooling")
                    }
                    .pickerStyle(.segmented)

                    if component == "hardDiskDrive" {
                        Text("硬盘序列号列表").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetField(placeholder: "序列号 [槽位],逗号分隔,如 WS0A123 [d0]", text: $serials, mono: true)
                    }
                    if component == "memory" {
                        Text("内存槽位(可选)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetField(placeholder: "如 0, 1(留空=全部)", text: $slots, mono: true)
                    }

                    Text("英文备注").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    SheetField(placeholder: "Additional info for datacenter", text: $comment)

                    Toggle(isOn: $inverse) {
                        VStack(alignment: .leading, spacing: 1) {
                            Text("故障盘已经读不出序列号 —— 改为列出所有健康盘").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                            Text("OVH 更换其余的盘(OVH 硬盘更换指南规定的做法)。列漏一块健康盘它也会被换掉,务必列全。")
                                .font(.system(size: 10)).foregroundColor(t.color(t.warning))
                        }
                    }.tint(t.color(t.accent))
                    SheetNote(text: "OVH 按 disk_serial 定位硬盘,列表不能为空(空等于申请更换整机所有硬盘,后端会拒绝)。序列号在系统里用 smartctl -i /dev/sdX(NVMe 用 nvme list)查看。官方指南建议把故障盘和健康盘的序列号都写进备注,避免机房技师换错盘;工单提交后可在 OVH 帮助中心按工单号跟进。", tint: t.muted)

                    ActBtn(kind: .primary, icon: "paperplane", label: busy ? "提交中…" : "提交工单") {
                        hwConfirm = true
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.large])
        .sheet(isPresented: $hwConfirm) {
            ConfirmSheet(title: "提交硬件更换工单", message: "机房将物理更换部件(\(componentName)),换盘有数据丢失风险。", confirmText: "确认提交", danger: false) {
                await submit()
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
    }

    private var componentName: String {
        ["hardDiskDrive": "硬盘", "memory": "内存", "cooling": "散热"][component] ?? component
    }

    private func submit() async {
        busy = true
        defer { busy = false }
        // 必填校验(S-051)
        if component == "memory" || component == "cooling" {
            let d = comment.trimmingCharacters(in: .whitespaces)
            guard !d.isEmpty else {
                toast.show("此类型需要填写故障详情", error: true)
                return
            }
        }
        if component == "hardDiskDrive" {
            let s0 = serials.trimmingCharacters(in: .whitespaces)
            guard !s0.isEmpty else {
                toast.show(inverse ? "请填写所有健康盘的序列号(未列出的盘都会被更换)" : "请填写至少一块故障盘的序列号", error: true)
                return
            }
        }
        var body: [String: Any] = ["componentType": component]
        // handler 的 parseReplaceDisks 要对象数组 [{disk_serial, slot_id}],
        // 输入形如 "序列号 [槽位]" 逗号分隔
        let s = serials.trimmingCharacters(in: .whitespaces)
        if component == "hardDiskDrive" && !s.isEmpty {
            body["disks"] = s.components(separatedBy: ",").map { raw -> [String: Any] in
                let item = raw.trimmingCharacters(in: .whitespaces)
                if let br = item.range(of: #"\[([^\]]*)\]"#, options: .regularExpression) {
                    let slot = item[br].trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
                    let serial = item[..<br.lowerBound].trimmingCharacters(in: .whitespaces)
                    return ["disk_serial": serial, "slot_id": slot.isEmpty ? 0 : (Int(slot) ?? 0)]
                }
                return ["disk_serial": item, "slot_id": 0]
            }
        }
        let sl = slots.trimmingCharacters(in: .whitespaces)
        if !sl.isEmpty { body["slots"] = sl.components(separatedBy: ",").map { $0.trimmingCharacters(in: .whitespaces) } }
        let c = comment.trimmingCharacters(in: .whitespaces)
        if !c.isEmpty { body["comment"] = c }
        if inverse { body["inverse"] = true }
        do {
            let r = try await conn.client.post("/server-control/\(sn)/hardware/replace", body: body)
            let tn = r["ticketNumber"] as? Int ?? (r["ticketId"] as? Int ?? 0)
            toast.show(tn > 0 ? "工单已提交,工单号 #\(tn)(可在 OVH 帮助中心跟进)" : (r["message"] as? String ?? "工单已提交"))
            dismiss()
        } catch { toast.show("提交失败:\(error.localizedDescription)", error: true) }
    }
}

// MARK: - 变更联系人

struct ChangeContactSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let sn: String
    let isVps: Bool
    var t: Tokens { theme.t }

    @State private var admin = ""
    @State private var tech = ""
    @State private var billing = ""
    @State private var requests: [[String: Any]] = []
    @State private var tokenInput: [String: String] = [:]
    @State private var busy = false

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "person.2", tint: t.info, title: "变更联系人")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    SheetNote(text: "填 OVH NIC 账号(如 xx1111-ovh)或邮箱。变更需要新联系人邮件确认。美区账户不支持。", tint: t.info)

                    Text("管理员(admin)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    SheetField(placeholder: "NIC 或邮箱", text: $admin)
                    Text("技术(tech)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    SheetField(placeholder: "NIC 或邮箱", text: $tech)
                    Text("计费(billing)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    SheetField(placeholder: "NIC 或邮箱", text: $billing)

                    ActBtn(kind: .primary, icon: "paperplane", label: busy ? "提交中…" : "提交变更") {
                        await submit()
                    }

                    if !requests.isEmpty {
                        Divider().overlay(t.color(t.border)).padding(.vertical, 4)
                        Text("待确认请求").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        ForEach(requests.indices, id: \.self) { i in
                            reqRow(requests[i])
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.large])
        .task { await loadReqs() }
    }

    private func reqRow(_ r: [String: Any]) -> some View {
        let id = String(describing: r["id"] ?? r["requestId"] ?? "")
        return VStack(alignment: .leading, spacing: 7) {
            KV(k: "任务", v: String(describing: r["taskId"] ?? r["task"] ?? id))
            KV(k: "状态", v: (r["state"] as? String ?? "—"))
            HStack(spacing: 8) {
                Button { Task { await resend(id) } } label: {
                    Text("重发邮件").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.info))
                        .padding(.horizontal, 10).padding(.vertical, 6)
                        .background(Capsule().stroke(t.color(t.info), lineWidth: 1))
                }.buttonStyle(.plain)
                Spacer()
            }
            HStack(spacing: 8) {
                TextField("邮件里的 token", text: Binding(
                    get: { tokenInput[id] ?? "" },
                    set: { tokenInput[id] = $0 }
                ))
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundColor(t.color(t.fg))
                    .padding(.horizontal, 9).frame(height: 34)
                    .background(RoundedRectangle(cornerRadius: 9).fill(t.color(t.surfaceMuted)))
                Button { Task { await respond(id, accept: true) } } label: {
                    Text("接受").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.accent))
                        .padding(.horizontal, 10).padding(.vertical, 7)
                        .background(Capsule().fill(t.color(t.accent).opacity(0.15)))
                }.buttonStyle(.plain)
                Button { Task { await respond(id, accept: false) } } label: {
                    Text("拒绝").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.danger))
                        .padding(.horizontal, 10).padding(.vertical, 7)
                        .background(Capsule().fill(t.color(t.danger).opacity(0.12)))
                }.buttonStyle(.plain)
            }
        }
        .padding(11)
        .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 12).stroke(t.color(t.border), lineWidth: 1)))
    }

    private func loadReqs() async {
        if let r = try? await conn.client.getDict("/ovh/contact-change-requests") {
            requests = (r["data"] as? [[String: Any]]) ?? (r["requests"] as? [[String: Any]]) ?? []
        }
    }

    private func submit() async {
        var body: [String: Any] = [:]
        for (k, v) in [("contactAdmin", admin), ("contactTech", tech), ("contactBilling", billing)] {
            let s = v.trimmingCharacters(in: .whitespaces)
            if !s.isEmpty { body[k] = s }
        }
        guard !body.isEmpty else { return toast.show("至少填一个联系人", error: true) }
        busy = true
        defer { busy = false }
        let base = isVps ? "/vps-control" : "/server-control"
        do {
            _ = try await conn.client.post("\(base)/\(sn)/change-contact", body: body)
            toast.show("变更请求已提交,等待对方邮件确认")
            dismiss()
        } catch { toast.show(error.localizedDescription, error: true) }
    }

    private func resend(_ id: String) async {
        let (ok, msg) = await conn.client.actionPostData("/ovh/contact-change-requests/\(id)/resend-email", bodyData: nil)
        toast.show(ok ? "已重发" : (msg.isEmpty ? "失败" : msg), error: !ok)
    }

    private func respond(_ id: String, accept: Bool) async {
        let tok = (tokenInput[id] ?? "").trimmingCharacters(in: .whitespaces)
        guard !tok.isEmpty else { return toast.show("先填邮件里的 token", error: true) }
        let body = try? JSONSerialization.data(withJSONObject: ["token": tok])
        let (ok, msg) = await conn.client.actionPostData("/ovh/contact-change-requests/\(id)/\(accept ? "accept" : "refuse")", bodyData: body)
        toast.show(ok ? (accept ? "已接受" : "已拒绝") : (msg.isEmpty ? "失败" : msg), error: !ok)
        if ok { await loadReqs() }
    }
}

// MARK: - 通用开关 sheet(Burst / 防火墙)

struct ToggleSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let title: String
    let icon: String
    let getPath: String
    let putPath: String
    var t: Tokens { theme.t }

    @State private var enabled: Bool?
    @State private var detail: [String: Any]?
    @State private var loading = true
    @State private var err: String?
    @State private var busy = false
    @State private var notAvailable = false
    /// Burst 的 PUT 语义是 status=active/inactive(不是布尔 enabled)
    var usesStatusBody: Bool { getPath.contains("burst") }

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: icon, tint: t.info, title: title)
            ScrollView {
                VStack(spacing: 13) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else if notAvailable {
                        // KS 等入门机型不支持 Burst/防火墙,后端返回 notAvailable
                        EmptyHint(icon: "minus.circle", text: "该机型不支持此功能")
                    } else {
                        if let on = enabled {
                            HStack {
                                Text(on ? "已启用" : "已停用").font(.system(size: 14, weight: .bold)).foregroundColor(t.color(on ? t.success : t.faint))
                                Spacer()
                                Toggle("", isOn: Binding(
                                    get: { on },
                                    set: { nv in Task { await toggle(nv) } }
                                )).labelsHidden().tint(t.color(t.accent)).disabled(busy)
                            }
                            .padding(14)
                            .background(RoundedRectangle(cornerRadius: 14).fill(t.color(t.surface)))
                        }
                        if let d = detail {
                            Card {
                                VStack(spacing: 8) {
                                    SectionTitle(text: "详情")
                                    ForEach(d.keys.filter { !["success"].contains($0) }.sorted(), id: \.self) { k in
                                        KV(k: k, v: "\(d[k] ?? "—")")
                                    }
                                }
                            }
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .task { await load() }
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict(getPath)
            notAvailable = (r["notAvailable"] as? Bool ?? false)
            // handler 把状态包在 burst / firewall 对象里
            notAvailable = (r["notAvailable"] as? Bool ?? false)
            let inner = (r["burst"] as? [String: Any]) ?? (r["firewall"] as? [String: Any]) ?? r
            if let st = inner["status"] as? String {
                enabled = (st == "active" || st == "enabled" || st == "enabledForVrack")
            } else {
                enabled = inner["enabled"] as? Bool ?? inner["activated"] as? Bool
            }
            detail = inner
            err = nil
        } catch {
            // 后端对不支持的机型直接 404(KS 系 Burst/防火墙)—— 这不是错误,是"没有此功能"
            if let ae = error as? ApiClient.ApiError, ae.status == 404 {
                notAvailable = true
            } else {
                err = error.localizedDescription
            }
        }
        loading = false
    }

    private func toggle(_ on: Bool) async {
        busy = true
        defer { busy = false }
        let payload: [String: Any] = usesStatusBody ? ["status": on ? "active" : "inactive"] : ["enabled": on]
        let body = try? JSONSerialization.data(withJSONObject: payload)
        let (ok, msg) = await conn.client.actionPutData(putPath, bodyData: body)
        if ok {
            enabled = on
            toast.show(on ? "已启用" : "已停用")
            await load()
        } else {
            toast.show(msg.isEmpty ? "设置失败" : msg, error: true)
        }
    }
}

// MARK: - Backup FTP

struct BackupFtpSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @AppStorage("ovh_mask_ip") private var mask = false
    let sn: String
    var t: Tokens { theme.t }

    @State private var info: [String: Any]?
    @State private var accesses: [[String: Any]] = []
    @State private var blocks: [[String: Any]] = []
    @State private var newBlock = ""
    @State private var ftp = true
    @State private var nfs = false
    @State private var cifs = false
    @State private var loading = true
    @State private var err: String?
    @State private var busy = false
    @State private var disableConfirm = false

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "externaldrive.badge.icloud", tint: t.info, title: "Backup FTP")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else if notActivated || info == nil {
                        SheetNote(text: "该服务器的备份存储未激活。激活后可把备份传到独立 FTP 空间。", tint: t.muted)
                        ActBtn(kind: .primary, icon: "checkmark.circle", label: busy ? "激活中…" : "激活备份存储") {
                            await activate()
                        }
                    } else if let i = info {
                        if (i["activated"] as? Bool ?? (i["status"] as? String == "active")) == false {
                            SheetNote(text: "该服务器的备份存储未激活。激活后可把备份传到独立 FTP 空间。", tint: t.muted)
                            ActBtn(kind: .primary, icon: "checkmark.circle", label: busy ? "激活中…" : "激活备份存储") {
                                await activate()
                            }
                        } else {
                            Card {
                                VStack(spacing: 8) {
                                    SectionTitle(text: "状态")
                                    KV(k: "服务器", v: i["ftpUrl"] as? String ?? (i["server"] as? String ?? "—"), mono: true)
                                    KV(k: "配额", v: fmtBytes(numToDoubleAny(i["quota"]) ?? 0))
                                    KV(k: "已用", v: fmtBytes(numToDoubleAny(i["used"]) ?? 0))
                                    KV(k: "状态", v: i["status"] as? String ?? "—")
                                }
                            }

                            ActBtn(kind: .ghost, icon: "key.horizontal", label: "重置密码") { await resetPwd() }

                            VStack(alignment: .leading, spacing: 8) {
                                SectionTitle(text: "访问控制(允许连接的 IP 段)")
                                ForEach(accesses.indices, id: \.self) { x in
                                    let a = accesses[x]
                                    HStack(spacing: 6) {
                                        Text(mask ? maskIP(a["ipBlock"] as? String ?? "—") : (a["ipBlock"] as? String ?? "—"))
                                            .font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                                        if (a["ftp"] as? Bool) ?? true { Chip(text: "FTP") }
                                        if (a["nfs"] as? Bool) == true { Chip(text: "NFS") }
                                        if (a["cifs"] as? Bool) == true { Chip(text: "CIFS") }
                                        if (a["isApplied"] as? Bool) == false { Chip(text: "生效中", color: t.warning) }
                                        Spacer()
                                        Button { accessDel = a["ipBlock"] as? String ?? "" } label: {
                                            Image(systemName: "trash").font(.system(size: 11)).foregroundColor(t.color(t.danger))
                                        }.buttonStyle(.plain)
                                    }
                                    .padding(9)
                                    .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.surfaceMuted)))
                                }
                                if !blocks.isEmpty {
                                    Text("可授权网段(点选)").font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                                    FlowLayout(spacing: 6) {
                                        ForEach(blocks.indices, id: \.self) { b in
                                            let ip = blocks[b]["ipBlock"] as? String ?? ""
                                            Button { newBlock = ip } label: {
                                                Text(ip).font(.system(size: 10, design: .monospaced))
                                                    .foregroundColor(t.color(newBlock == ip ? t.accent : t.muted))
                                                    .padding(.horizontal, 8).padding(.vertical, 4)
                                                    .background(RoundedRectangle(cornerRadius: 8).fill(t.color(t.surfaceMuted)))
                                            }.buttonStyle(.plain)
                                        }
                                    }
                                }
                                SheetField(placeholder: "IP 段,如 1.2.3.4/32", text: $newBlock, mono: true)
                                HStack(spacing: 10) {
                                    Toggle("FTP", isOn: $ftp).tint(t.color(t.accent))
                                    Toggle("NFS", isOn: $nfs).tint(t.color(t.accent))
                                    Toggle("CIFS", isOn: $cifs).tint(t.color(t.accent))
                                }
                                .font(.system(size: 11)).foregroundColor(t.color(t.fg))
                                ActBtn(kind: .ghost, icon: "plus.circle", label: "添加授权") {
                                    await addAccess()
                                }
                            }

                            Divider().overlay(t.color(t.border))
                            ActBtn(kind: .danger, icon: "trash", label: "关闭备份服务(删除所有备份)") {
                                disableConfirm = true
                            }
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.large])
        .task { await load() }
        .sheet(item: Binding(
            get: { accessDel.map { AccessDel(ip: $0) } },
            set: { accessDel = $0?.ip }
        )) { w in
            ConfirmSheet(title: "删除备份 FTP 授权?", message: "删除对 \(w.ip) 的备份FTP授权?", confirmText: "确认删除") {
                let (ok2, msg) = await conn.client.actionDelete("/server-control/\(sn)/backup-ftp/access?ipBlock=\(urlEncode(w.ip))")
                toast.show(ok2 ? "已提交删除授权" : (msg.isEmpty ? "删除授权失败" : msg), error: !ok2)
                await load()
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(isPresented: $disableConfirm) {
            ConfirmSheet(title: "关闭备份服务", message: "所有备份将被删除,不可恢复。", confirmText: "确认关闭") {
                let (ok, msg) = await conn.client.actionDelete("/server-control/\(sn)/backup-ftp")
                toast.show(ok ? "已关闭" : (msg.isEmpty ? "失败" : msg), error: !ok)
                await load()
            }
        }
    }

    @State private var notActivated = false
    @State private var accessDel: String? = nil

    private func load() async {
        do {
            async let i = conn.client.getDict("/server-control/\(sn)/backup-ftp")
            async let a = conn.client.getDict("/server-control/\(sn)/backup-ftp/access")
            let (ir, ar) = try await (i, a)
            info = (ir["backupFtp"] as? [String: Any]) ?? ir
            accesses = (ar["accessList"] as? [[String: Any]]) ?? (ar["access"] as? [[String: Any]]) ?? []
            let inner = (ir["backupFtp"] as? [String: Any]) ?? ir
            if inner["activated"] as? Bool ?? (inner["status"] as? String == "active") {
                if let br = try? await conn.client.getDict("/server-control/\(sn)/backup-ftp/authorizable-blocks") {
                    // handler 返回字符串数组;统一成 [{ipBlock:...}] 供点选复用
                    if let arr = br["blocks"] as? [String] {
                        blocks = arr.map { ["ipBlock": $0] }
                    } else {
                        blocks = (br["blocks"] as? [[String: Any]]) ?? []
                    }
                }
            }
            err = nil
        } catch {
            // 后端对未激活的备份 FTP 返回 404 + notActivated —— 这正是"未激活"分支
            if let ae = error as? ApiClient.ApiError, ae.status == 404 {
                notActivated = true
            } else {
                err = error.localizedDescription
            }
        }
        loading = false
    }

    private func activate() async {
        busy = true
        defer { busy = false }
        do {
            _ = try await conn.client.post("/server-control/\(sn)/backup-ftp")
            toast.show("已激活")
            await load()
        } catch { toast.show(error.localizedDescription, error: true) }
    }

    private func resetPwd() async {
        let (ok, msg) = await conn.client.actionPostData("/server-control/\(sn)/backup-ftp/password", bodyData: nil)
        toast.show(ok ? "新密码已发到邮箱" : (msg.isEmpty ? "失败" : msg), error: !ok)
    }

    private func addAccess() async {
        let ip = newBlock.trimmingCharacters(in: .whitespaces)
        guard !ip.isEmpty else { return toast.show("先填 IP 段", error: true) }
        // handler 契约:ftp/nfs/cifs 是三个布尔
        let body = try? JSONSerialization.data(withJSONObject: [
            "ipBlock": ip, "ftp": ftp, "nfs": nfs, "cifs": cifs,
        ])
        let (ok, msg) = await conn.client.actionPostData("/server-control/\(sn)/backup-ftp/access", bodyData: body)
        toast.show(ok ? "已添加" : (msg.isEmpty ? "失败" : msg), error: !ok)
        if ok { await load() }
    }

    private func removeAccess(_ idx: Int) async {
        guard let ip = accesses[idx]["ipBlock"] as? String else { return }
        // CIDR 含 "/",不能拼路径段;handler 走 ?ipBlock= 查询参数
        let (ok, msg) = await conn.client.actionDelete("/server-control/\(sn)/backup-ftp/access?ipBlock=\(urlEncode(ip))")
        toast.show(ok ? "已删除" : (msg.isEmpty ? "失败" : msg), error: !ok)
        if ok { await load() }
    }
}

// MARK: - 通用数据浏览(二级DNS / 虚拟MAC / vRack / 可订购 / 选项 / IP规格 / 网络规格)

struct JsonSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let title: String
    let icon: String
    let path: String
    var t: Tokens { theme.t }

    @State private var data: [String: Any]?
    @State private var err: String?
    @State private var loading = true

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: icon, tint: t.muted, title: title)
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else if let d = data {
                        jsonView(d, depth: 0)
                    } else {
                        EmptyHint(icon: icon, text: "无数据")
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .task { await load() }
        .refreshable { await load() }
    }

    private func jsonView(_ obj: Any, depth: Int) -> AnyView {
        return AnyView(Group {
        switch obj {
        case let dict as [String: Any]:
            let keys = dict.keys.filter { $0 != "success" }.sorted()
            if keys.isEmpty {
                Text("空").font(.system(size: 11)).foregroundColor(t.color(t.faint))
            }
            VStack(spacing: 6) {
                ForEach(keys, id: \.self) { k in
                    let v = dict[k]!
                    if isScalar(v) {
                        KV(k: k, v: scalarText(v), mono: true)
                    } else {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(k).font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.muted))
                                .padding(.top, 4)
                            jsonView(v, depth: depth + 1)
                                .padding(.leading, 8)
                                .overlay(Rectangle().frame(width: 2).foregroundColor(t.color(t.border)).opacity(0.5), alignment: .leading)
                        }
                    }
                }
            }
        case let arr as [[String: Any]]:
            if arr.isEmpty {
                Text("(空列表)").font(.system(size: 11)).foregroundColor(t.color(t.faint))
            }
            ForEach(arr.indices, id: \.self) { i in
                jsonView(arr[i], depth: depth + 1)
                    .padding(9)
                    .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.surfaceMuted)))
            }
        case let arr as [Any]:
            Text(arr.map(scalarText).joined(separator: "、")).font(.system(size: 11.5)).foregroundColor(t.color(t.fg))
        default:
            Text(scalarText(obj)).font(.system(size: 11.5)).foregroundColor(t.color(t.fg))
        }
        })
    }
    private func isScalar(_ v: Any) -> Bool {
        !(v is [String: Any]) && !(v is [Any])
    }

    private func scalarText(_ v: Any) -> String {
        if let b = v as? Bool { return b ? "是" : "否" }
        return "\(v)"
    }

    private func load() async {
        do {
            data = try await conn.client.getDict(path)
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }
}

// MARK: - DDoS 永久缓解(独服 / VPS 共用)

struct MitigationSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    let sn: String
    let isVps: Bool
    var t: Tokens { theme.t }

    private var base: String { isVps ? "/vps-control" : "/server-control" }

    @State private var blocks: [[String: Any]] = []
    @State private var loading = true
    @State private var err: String?
    @State private var busy: Bool = false

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "shield.lefthalf.filled", tint: t.info, title: "DDoS 永久缓解")
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else {
                        SheetNote(text: "OVH 自带「自动缓解」会在检测到攻击时自动启用,无需配置。下面是手动启用「永久缓解」的开关:开启后该 IP 全程过 Anti-DDoS 设备(延迟略增,持续防护)。", tint: t.info)
                        SheetNote(text: "仅支持 IPv4。IPv6 走 OVH 网络层默认免疫,无需手动配置。", tint: t.warning)
                        if blocks.isEmpty {
                            EmptyHint(icon: "shield", text: "没有可配置的 IP 块")
                        }
                        ForEach(blocks.indices, id: \.self) { i in
                            blockCard(blocks[i])
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.large])
        .task { await load() }
    }

    private func blockCard(_ b: [String: Any]) -> some View {
        let block = b["ipBlock"] as? String ?? "—"
        let mitigations = b["mitigations"] as? [[String: Any]] ?? []
        let permanentOn = mitigations.contains { ($0["permanent"] as? Bool ?? false) || (($0["type"] as? String) == "permanent") }
        let isV6 = block.contains(":")
        return Card(border: permanentOn ? t.success : t.border) {
            VStack(alignment: .leading, spacing: 9) {
                HStack {
                    Text(block).font(.system(size: 12, design: .monospaced)).foregroundColor(t.color(t.fg))
                    Spacer()
                    Chip(text: permanentOn ? "永久缓解中" : "自动", color: permanentOn ? t.success : t.muted)
                }
                if let note = b["note"] as? String, !note.isEmpty {
                    Text(note).font(.system(size: 10)).foregroundColor(t.color(t.muted))
                }
                if isV6 {
                    Text("IPv6 不适用 anti-DDoS Mitigation(OVH 网络层免疫)").font(.system(size: 10)).foregroundColor(t.color(t.faint))
                } else if mitigations.isEmpty {
                    HStack {
                        Text("无永久缓解,自动缓解备用中").font(.system(size: 11)).foregroundColor(t.color(t.muted))
                        Spacer()
                        Button { Task { await toggle(block: block, on: true) } } label: {
                            Text(busy ? "应用中…" : "启用永久缓解").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.accent))
                                .padding(.horizontal, 10).padding(.vertical, 6)
                                .background(Capsule().stroke(t.color(t.accent), lineWidth: 1))
                        }.buttonStyle(.plain)
                    }
                } else {
                    // 有 mitigation 行:状态 chip + 关闭按钮三态(S-063)
                    ForEach(mitigations.indices, id: \.self) { mi in
                        let mrow = mitigations[mi]
                        let ip = mrow["ip"] as? String ?? ""
                        let state = mrow["state"] as? String ?? "ok"
                        let pending2 = state == "creationPending" || state == "removalPending"
                        let sc = state == "ok" ? t.success : t.warning
                        VStack(spacing: 7) {
                            HStack {
                                Text(ip).font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                                Spacer()
                                Chip(text: state == "ok" ? "已生效" : (state == "creationPending" ? "应用中" : "移除中"), color: sc)
                                Chip(text: (mrow["type"] as? String) == "permanent" ? "永久" : "自动")
                            }
                            ActBtn(kind: .danger, icon: "xmark.shield",
                                   label: busy ? "处理中…" : (state == "removalPending" ? "移除中…" : "关闭永久"),
                                   busy: busy || pending2) {
                                await toggle(block: block, on: false)
                            }
                        }
                        .padding(10)
                        .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted).opacity(0.5)))
                    }
                }
            }
        }
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("\(base)/\(sn)/mitigation")
            blocks = (r["ips"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func toggle(block: String, on: Bool) async {
        busy = true
        defer { busy = false }
        // :ip 要单个 IPv4(取 CIDR 前段),?block= 带完整网段(与 web AdvancedTab 一致)
        let ipOnly = block.components(separatedBy: "/").first ?? block
        let path = "\(base)/\(sn)/mitigation/\(urlEncode(ipOnly))?block=\(urlEncode(block))"
        let (ok, msg) = on ? await conn.client.actionPostData(path, bodyData: nil)
                           : await conn.client.actionDelete(path)
        if ok {
            toast.show(on ? "已启用永久 DDoS 缓解" : "已关闭永久 DDoS 缓解")
        } else {
            // 错误翻译(S-063 原文)
            if msg.contains("state need to be ok") {
                toast.show("当前 mitigation 状态不允许关闭(可能正在被自动启用或攻击中)。等状态变 ok 再试", error: true)
            } else if msg.contains("is not valid for type ipv4") {
                toast.show("OVH anti-DDoS 只支持 IPv4。IPv6 默认有网络层防护,无需手动配置", error: true)
            } else {
                toast.show(msg.isEmpty ? "操作失败" : msg, error: true)
            }
        }
        if ok { await load() }
    }
}


/// 网络规格(S-049):带宽四档+路由表+信息块,替代原始 JSON
struct NetworkSpecsSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @AppStorage("ovh_mask_ip") private var mask = false
    let sn: String
    var t: Tokens { theme.t }

    @State private var data: [String: Any]?
    @State private var err: String?
    @State private var loading = true

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "network", tint: t.info, title: "网络规格")
            ScrollView {
                VStack(alignment: .leading, spacing: 11) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: "网络规格读取失败:\(e)") { Task { await load() } }
                    } else if let d = data {
                        let net = d["network"] as? [String: Any] ?? d
                        if net.isEmpty {
                            EmptyHint(icon: "network", text: "无网络规格数据")
                        } else {
                            // 带宽四档
                            let bw = net["bandwidth"] as? [String: Any] ?? [:]
                            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 8) {
                                bwTile("出向 (OVH → 互联网)", bw["out"])
                                bwTile("入向 (互联网 → OVH)", bw["in"])
                                bwTile("内部 (OVH → OVH)", bw["internal"])
                                bwTile("端口速率", bw["port"])
                            }
                            if let ty = net["bandwidthType"] as? String ?? net["type"] as? String, !ty.isEmpty {
                                KV(k: "带宽类型", v: ty)
                            }
                            // 其余键值
                            ForEach(net.keys.filter { !["bandwidth","bandwidthType","type","ipv4","ipv6"].contains($0) }.sorted(), id: \.self) { k in
                                if isScalar2(net[k]!) {
                                    KV(k: k, v: "\(net[k]!)", mono: k.lowercased().contains("ip") || k.lowercased().contains("gateway"))
                                }
                            }
                            // IPv4/v6 路由
                            ForEach(["ipv4", "ipv6"], id: \.self) { ver in
                                if let routes = net[ver] as? [[String: Any]], !routes.isEmpty {
                                    VStack(alignment: .leading, spacing: 5) {
                                        SectionTitle(text: ver.uppercased() + " 路由")
                                        ForEach(routes.indices, id: \.self) { ri in
                                            let r2 = routes[ri]
                                            KV(k: (r2["ip"] as? String).map { mask ? maskIP($0) : $0 } ?? "—",
                                               v: "\(r2["gateway"] as? String ?? "—") / \(r2["block"] as? String ?? r2["cidr"] as? String ?? "—")", mono: true)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .task { await load() }
    }

    private func bwTile(_ label: String, _ v: Any?) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(label).font(.system(size: 10)).foregroundColor(t.color(t.muted))
            Text(bwText(v)).font(.system(size: 13, weight: .bold, design: .rounded)).foregroundColor(t.color(t.fg))
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)))
    }

    private func bwText(_ v: Any?) -> String {
        if let n = numToDoubleAny(v) { return fmtMbps(n) }
        if let s = v as? String { return s }
        if let d = v as? [String: Any] {
            let val = numToDoubleAny(d["value"]) ?? 0
            return fmtMbps(val) + (d["unit"] as? String ?? "")
        }
        return "—"
    }

    private func isScalar2(_ v: Any) -> Bool { !(v is [String: Any]) && !(v is [Any]) }

    private func load() async {
        do {
            data = try await conn.client.getDict("/server-control/\(sn)/network-specs")
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }
}

/// 附加选项(S-061):名称翻译+state 色
struct OptionsSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let sn: String
    var t: Tokens { theme.t }

    static let names: [String: String] = [
        "BANDWIDTH": "带宽", "TRAFFIC": "流量", "BACKUP_STORAGE": "备份存储", "HARD_RAID": "硬件 RAID",
        "SLA": "SLA", "SYSTEM_STORAGE": "系统存储", "MEMORY": "内存", "CPU": "CPU", "PRIVATE_BANDWIDTH": "私有带宽",
    ]

    @State private var items: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "shippingbox", tint: t.muted, title: "附加选项")
            ScrollView {
                VStack(spacing: 8) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: "附加选项读取失败:\(e)") { Task { await load() } }
                    } else if items.isEmpty {
                        EmptyHint(icon: "shippingbox", text: "无附加选项")
                    } else {
                        ForEach(items.indices, id: \.self) { i in
                            let it = items[i]
                            let code = it["option"] as? String ?? it["code"] as? String ?? "—"
                            let state = it["state"] as? String ?? ""
                            let color = state == "subscribed" ? t.success : (["releasing","todelete"].contains(state) ? t.warning : t.muted)
                            HStack {
                                Text(Self.names[code.uppercased()] ?? code).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                                Spacer()
                                if !state.isEmpty { Chip(text: state, color: color) }
                            }
                            .padding(11)
                            .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surfaceMuted).opacity(0.5)))
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .task { await load() }
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/options")
            items = (r["options"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }
}


struct AccessDel: Identifiable {
    let ip: String
    var id: String { ip }
}
