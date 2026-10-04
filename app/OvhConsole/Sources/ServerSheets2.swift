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
    @Environment(\.openURL) private var openURL
    let sn: String
    let isVps: Bool
    var t: Tokens { theme.t }

    private var base: String { isVps ? "/vps-control" : "/server-control" }

    // 后端透传 OVH 原始结构(web hooks EngagementInfo/EngagementRequest 同形):
    // current = {currentPeriod:{startDate,endDate}, endRule:{strategy,possibleStrategies}}
    // request = {pricing:{description,…}, requestDate, order:{orderId,url}}
    @State private var current: [String: Any]?
    @State private var currentErr: String? = nil
    @State private var available: [[String: Any]] = []
    @State private var pending: [String: Any]?
    @State private var pendingErr: String? = nil
    @State private var availErr: String? = nil
    @State private var loading = true
    @State private var busy = false
    @State private var confirming: String?
    @State private var confirmStrategy: String? = nil

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "doc.plaintext", tint: t.info, title: "合同期管理")
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if loading {
                        ProgressView().padding(30)
                    } else {
                        // 当前合同期(读失败 ≠ 没签合同期)
                        Card {
                            VStack(alignment: .leading, spacing: 8) {
                                SectionTitle(text: "当前合同期")
                                if let ce = currentErr {
                                    Text("合同期信息读取失败:\(ce)。当前是否处于承诺期未知,请勿据此判断续费方式。")
                                        .font(.system(size: 11)).foregroundColor(t.color(t.danger))
                                    Button("重试") { Task { await loadCurrent() } }
                                        .font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.accent))
                                } else if let cur = current {
                                    if let period = cur["currentPeriod"] as? [String: Any] {
                                        KV(k: "周期", v: "\(fmtDate(period["startDate"] as? String)) — \(fmtDate(period["endDate"] as? String))")
                                    }
                                    if let rule = cur["endRule"] as? [String: Any] {
                                        let strategy = rule["strategy"] as? String ?? ""
                                        Text("到期策略:").font(.system(size: 11)).foregroundColor(t.color(t.muted))
                                        FlowLayout(spacing: 6) {
                                            Chip(text: endStrategyName(strategy), color: t.info)
                                            ForEach(((rule["possibleStrategies"] as? [String]) ?? []).filter { $0 != strategy }, id: \.self) { s in
                                                Button {
                                                    if s == "CANCEL_SERVICE" { confirmStrategy = s }
                                                    else { Task { await updateEndRule(s) } }
                                                } label: {
                                                    Text("改为「\(endStrategyName(s))」").font(.system(size: 10.5, weight: .semibold))
                                                        .foregroundColor(t.color(t.fg))
                                                        .padding(.horizontal, 8).padding(.vertical, 4)
                                                        .background(Capsule().stroke(t.color(t.border), lineWidth: 1))
                                                }.buttonStyle(.plain).disabled(busy)
                                            }
                                        }
                                    }
                                } else {
                                    Text("该服务未签合同期,按标准月付方式续费。可在下方订阅承诺期享受折扣。")
                                        .font(.system(size: 11)).foregroundColor(t.color(t.muted))
                                }
                            }
                        }

                        // 进行中的变更请求(读失败 ≠ 没有请求)
                        if let p = pending {
                            Card(border: t.warning) {
                                VStack(alignment: .leading, spacing: 8) {
                                    SectionTitle(text: "订单已创建,等待支付")
                                    Text("OVH 已为此变更创建订单,付款前合同期不会生效,服务继续按原月付。30 天未付订单将自动取消。")
                                        .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                                    if let pricing = p["pricing"] as? [String: Any],
                                       let desc = pricing["description"] as? String, !desc.isEmpty {
                                        KV(k: "目标", v: desc)
                                    }
                                    if let rd = p["requestDate"] as? String, !rd.isEmpty {
                                        KV(k: "提交时间", v: fmtDate(rd))
                                    }
                                    if let order = p["order"] as? [String: Any],
                                       let oid = numToDoubleAny(order["orderId"]).map(Int.init) {
                                        KV(k: "订单号", v: "#\(oid)", mono: true)
                                    }
                                    HStack(spacing: 10) {
                                        if let order = p["order"] as? [String: Any],
                                           let url = order["url"] as? String, !url.isEmpty, let u = URL(string: url) {
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
                        } else if let pe = pendingErr {
                            SheetNote(text: "进行中的变更请求读取失败,暂时无法订阅 —— 无法确认是否已有未付订单,重复提交会多出一笔订单(\(pe))。", tint: t.warning)
                        }

                        // 可订阅列表
                        Text("可订阅的承诺期").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        Text("长周期承诺通常有折扣(部分入门款无折扣)。月均价就是月度等效成本。")
                            .font(.system(size: 10)).foregroundColor(t.color(t.faint))
                        if let ae = availErr {
                            LoadFailed(message: "可订阅承诺期读取失败:\(ae)") { Task { await load() } }
                        } else if available.isEmpty {
                            EmptyHint(icon: "calendar", text: "暂无可订阅的承诺期")
                        } else {
                            ForEach(available.indices, id: \.self) { i in
                                pricingRow(available[i])
                            }
                            if pending != nil {
                                Text("有变更请求处理中,需先撤销才能订阅新承诺期")
                                    .font(.system(size: 10)).foregroundColor(t.color(t.faint))
                            } else if pendingErr != nil {
                                Text("进行中的变更请求读取失败,暂时无法订阅(见上方提示)")
                                    .font(.system(size: 10)).foregroundColor(t.color(t.warning))
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
            ConfirmSheet(title: "确认订阅承诺期?", message: "1. OVH 创建一笔未付订单(一次性预付=承诺期总价;周期付费=首期价)\n2. 付款前合同期不会激活,服务继续按原月付收费\n3. 若已设自动扣款 + 余额充足 → 几分钟内自动扣款激活\n4. 否则需手动去 OVH manager 支付,30 天没付订单自动取消\n5. 一旦付款 → 合同期锁死,中途解约按未消耗月数计违约金", confirmText: "创建订单", danger: false) {
                await createRequest(w.mode)
            }
        }
        .sheet(item: Binding(
            get: { confirmStrategy.map { ModeWrap(mode: $0) } },
            set: { confirmStrategy = $0?.mode }
        )) { _ in
            ConfirmSheet(title: "确认改为「到期自动销毁服务」?", message: "承诺期结束时,OVH 会直接销毁这台服务器,数据不保留、IP 不保留。设置后若要反悔,需要在承诺期结束前改回其它策略。", confirmText: "确认销毁", danger: true) {
                await updateEndRule("CANCEL_SERVICE", confirm: true)
            }
        }
    }

    private struct ModeWrap: Identifiable {
        let mode: String
        var id: String { mode }
    }

    // MARK: 可订阅行(价格口径与 web PricingRow 一致:
    // price 的区间是 pricing.duration(续费区间),periodic 行的 price 是每期价,
    // 总价 = 每期价 × 期数;upfront 直接用 OVH 的 price.text)

    private func pricingRow(_ a: [String: Any]) -> some View {
        let pricing = a["price"] as? [String: Any] ?? [:]
        let priceVal = numToDoubleAny(pricing["value"]) ?? 0
        let currency = pricing["currencyCode"] as? String ?? ""
        let engCfg = a["engagementConfiguration"] as? [String: Any]
        let months = parseISOMonths(engCfg?["duration"] as? String) ?? 0
        let intervalMonths = parseISOMonths(a["duration"] as? String) ?? 0
        let perMonth = intervalMonths > 0 ? priceVal / Double(intervalMonths) : 0
        let engType = engCfg?["type"] as? String
        let isUpfront: Bool
        if let ty = engType, ty == "upfront" || ty == "periodic" { isUpfront = ty == "upfront" }
        else { isUpfront = (a["pricingMode"] as? String ?? "").lowercased().contains("upfront") }
        let totalValue = intervalMonths > 0 && months > 0 ? (priceVal / Double(intervalMonths)) * Double(months) : priceVal
        let totalText: String
        if isUpfront, let txt = pricing["text"] as? String, !txt.isEmpty { totalText = txt }
        else if totalValue > 0 { totalText = currency.isEmpty ? String(format: "%.2f", totalValue) : String(format: "%.2f %@", totalValue, currency) }
        else { totalText = "—" }
        let perMonthText = perMonth > 0 ? (currency.isEmpty ? String(format: "%.2f/月", perMonth) : String(format: "%.2f %@/月", perMonth, currency)) : ""
        let subscribeDisabled = busy || pending != nil || pendingErr != nil
        return Button { confirming = a["pricingMode"] as? String } label: {
            HStack {
                VStack(alignment: .leading, spacing: 1) {
                    HStack(spacing: 5) {
                        Text(humanTitle(a, months: months, isUpfront: isUpfront)).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                        Chip(text: isUpfront ? "一次性预付" : "周期付费")
                    }
                    if let endAction = engCfg?["defaultEndAction"] as? String, !endAction.isEmpty {
                        Text("到期:\(endStrategyName(endAction))").font(.system(size: 10)).foregroundColor(t.color(t.muted))
                    }
                }
                Spacer()
                VStack(alignment: .trailing, spacing: 1) {
                    Text(totalText).font(.system(size: 12, weight: .semibold, design: .rounded)).foregroundColor(t.color(t.fg))
                    if !perMonthText.isEmpty {
                        Text(perMonthText).font(.system(size: 10, design: .rounded)).foregroundColor(t.color(t.muted))
                    }
                    Text("订阅").font(.system(size: 11, weight: .semibold)).foregroundColor(subscribeDisabled ? t.color(t.faint) : t.color(t.accent))
                }
            }
            .padding(11)
            .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 12).stroke(t.color(t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
        .disabled(subscribeDisabled)
    }

    /// ISO 8601 duration("P12M"/"P1Y")→ 月数;web parseDurationMonths 同款,解析不出返回 0(web 语义:0=不显示月均价)
    private func parseISOMonths(_ raw: String?) -> Int? {
        guard let s = raw, s.hasPrefix("P") else { return nil }
        var months = 0
        if let ym = s.range(of: #"([0-9]+)Y"#, options: .regularExpression),
           let y = Int(s[ym].dropLast()) { months += y * 12 }
        if let mm = s.range(of: #"([0-9]+)M"#, options: .regularExpression),
           let m = Int(s[mm].dropLast()) { months += m }
        return months
    }

    /// "rental for 12 months" → "1 年预付/周期"(web humanizeDescription 同款)
    private func humanTitle(_ a: [String: Any], months: Int, isUpfront: Bool) -> String {
        if months > 0 {
            let human = months % 12 == 0 ? "\(months / 12) 年" : "\(months) 个月"
            return isUpfront ? "\(human)预付" : "\(human)周期"
        }
        return (a["description"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? "—"
    }

    /// OVH 到期策略枚举 → 中文(web translateEndStrategy 同款)
    private func endStrategyName(_ s: String) -> String {
        switch s {
        case "REACTIVATE_ENGAGEMENT": return "到期自动再签同样合同期"
        case "STOP_ENGAGEMENT_FALLBACK_DEFAULT_PRICE": return "到期转月付(回到标准价)"
        case "STOP_ENGAGEMENT_KEEP_PRICE": return "到期转月付(保持当前价,无合同期)"
        case "CANCEL_SERVICE": return "到期自动销毁服务"
        default: return s.isEmpty ? "—" : s
        }
    }

    // MARK: 请求(三个分区独立失败,互不拖垮)

    private func load() async {
        await loadCurrent()
        pending = nil
        pendingErr = nil
        do {
            let pr = try await conn.client.getDict("\(base)/\(sn)/engagement/request")
            pending = pr["request"] as? [String: Any]
        } catch { pendingErr = error.localizedDescription }
        availErr = nil
        do {
            let ar = try await conn.client.getDict("\(base)/\(sn)/engagement/available")
            available = (ar["pricings"] as? [[String: Any]]) ?? []
        } catch {
            available = []
            availErr = error.localizedDescription
        }
        loading = false
    }

    private func loadCurrent() async {
        currentErr = nil
        do {
            let cr = try await conn.client.getDict("\(base)/\(sn)/engagement")
            current = cr["engagement"] as? [String: Any]
        } catch {
            current = nil
            currentErr = error.localizedDescription
        }
    }

    private func createRequest(_ mode: String) async {
        busy = true
        defer { busy = false }
        do {
            let resp = try await conn.client.post("\(base)/\(sn)/engagement/request", body: ["pricingMode": mode])
            if let url = ((resp["request"] as? [String: Any])?["order"] as? [String: Any])?["url"] as? String,
               !url.isEmpty, let u = URL(string: url) {
                toast.show("订单已创建,正在打开 OVH 支付页面…")
                openURL(u)
            } else {
                toast.show("变更请求已提交,请前往 OVH manager 完成支付")
            }
            await load()
        } catch { toast.show(error.localizedDescription, error: true) }
    }

    /// 改到期策略;CANCEL_SERVICE 不可逆,后端要求 confirm:true(调用前必须过二次确认)
    private func updateEndRule(_ strategy: String, confirm: Bool = false) async {
        busy = true
        defer { busy = false }
        var body: [String: Any] = ["strategy": strategy]
        if confirm { body["confirm"] = true }
        let bodyData = try? JSONSerialization.data(withJSONObject: body)
        let (ok, msg) = await conn.client.actionPutData("\(base)/\(sn)/engagement/end-rule", bodyData: bodyData)
        toast.show(ok ? "到期策略已更新" : (msg.isEmpty ? "更新失败" : msg), error: !ok)
        if ok { await loadCurrent() }
        confirmStrategy = nil
    }

    private func cancelPending() async {
        busy = true
        defer { busy = false }
        let (ok, msg) = await conn.client.actionDelete("\(base)/\(sn)/engagement/request")
        toast.show(ok ? "已撤销变更请求" : (msg.isEmpty ? "撤销失败" : msg), error: !ok)
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
    @State private var details = ""
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
                        Text(inverse ? "健康盘序列号(必填,每行一块;未列出的盘都会被更换)" : "故障盘序列号(必填,每行一块)")
                            .font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetField(placeholder: "S3Z2NB0K123456 2   ← 序列号后可跟槽位号", text: $serials, mono: true)
                    }
                    if component == "memory" {
                        Text("故障内存槽位(可选,逗号或换行分隔)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetField(placeholder: "DIMM_A1, DIMM_B2", text: $slots, mono: true)
                    }
                    if component == "memory" || component == "cooling" {
                        Text("故障详情(\(component == "memory" ? "内存" : "散热")必填,建议英文)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetField(placeholder: component == "memory" ? "e.g., Memory module failure, slot 1" : "e.g., Fan noise, overheating issue", text: $details)
                    }

                    Text("英文备注").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    SheetField(placeholder: "Additional info for datacenter", text: $comment)

                    if component == "hardDiskDrive" {
                        Toggle(isOn: $inverse) {
                            VStack(alignment: .leading, spacing: 1) {
                                Text("故障盘已经读不出序列号 —— 改为列出所有健康盘").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                                Text("OVH 更换其余的盘(OVH 硬盘更换指南规定的做法)。列漏一块健康盘它也会被换掉,务必列全。")
                                    .font(.system(size: 10)).foregroundColor(t.color(t.warning))
                            }
                        }.tint(t.color(t.accent))
                        SheetNote(text: "OVH 按 disk_serial 定位硬盘,列表不能为空(空等于申请更换整机所有硬盘,后端会拒绝)。序列号在系统里用 smartctl -i /dev/sdX(NVMe 用 nvme list)查看。官方指南建议把故障盘和健康盘的序列号都写进备注,避免机房技师换错盘;工单提交后可在 OVH 帮助中心按工单号跟进。", tint: t.muted)
                    }

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
        // 必填校验(S-051):内存/散热的"故障详情"是独立字段 details,不是备注
        let d = details.trimmingCharacters(in: .whitespaces)
        if component == "memory" || component == "cooling" {
            guard !d.isEmpty else {
                toast.show("此类型需要填写故障详情", error: true)
                return
            }
        }
        let s = serials.trimmingCharacters(in: .whitespacesAndNewlines)
        if component == "hardDiskDrive" {
            guard !s.isEmpty else {
                toast.show(inverse ? "请填写所有健康盘的序列号(未列出的盘都会被更换)" : "请填写至少一块故障盘的序列号", error: true)
                return
            }
        }
        var body: [String: Any] = ["componentType": component]
        // 每行/每项 "序列号 槽位":槽位段是数字才带 slot_id,拿不到就整个省略 ——
        // 伪造槽位号(比如兜底发 0)会让机房按错误槽位定位盘(web parseDisks 同款)
        if component == "hardDiskDrive" && !s.isEmpty {
            body["disks"] = s.components(separatedBy: CharacterSet(charactersIn: ",\n")).map { raw -> [String: Any] in
                let item = raw.trimmingCharacters(in: .whitespaces)
                let parts = item.split(separator: " ", omittingEmptySubsequences: true).map(String.init)
                guard let serial = parts.first, !serial.isEmpty else { return [:] }
                if parts.count > 1, let slot = Int(parts[1]) {
                    return ["disk_serial": serial, "slot_id": slot]
                }
                return ["disk_serial": serial]
            }.filter { !$0.isEmpty }
        }
        if component == "memory" {
            let sl = slots.trimmingCharacters(in: .whitespacesAndNewlines)
            if !sl.isEmpty {
                body["slots"] = sl.components(separatedBy: CharacterSet(charactersIn: ",\n"))
                    .map { $0.trimmingCharacters(in: .whitespaces) }
                    .filter { !$0.isEmpty }
            }
        }
        if !d.isEmpty { body["details"] = d }
        let c = comment.trimmingCharacters(in: .whitespaces)
        if !c.isEmpty { body["comment"] = c }
        if component == "hardDiskDrive" && inverse { body["inverse"] = true }
        do {
            let r = try await conn.client.post("/server-control/\(sn)/hardware/replace", body: body)
            let tn = numToDoubleAny(r["ticketNumber"]).map(Int.init) ?? 0
            var msg = tn > 0 ? "工单已提交,工单号 #\(tn)(可在 OVH 帮助中心跟进)" : (r["message"] as? String ?? "工单已提交")
            if let notice = r["notice"] as? String, !notice.isEmpty {
                msg += " · \(notice)"   // OVH additionalNotice(如 datacenter 特殊安排)必须让用户看到
            }
            toast.show(msg)
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
    @State private var accessErr: String? = nil
    @State private var blocks: [[String: Any]] = []
    @State private var newBlock = ""
    @State private var nfs = false
    @State private var cifs = false
    @State private var loading = true
    @State private var err: String?
    @State private var busy = false
    @State private var disableConfirm = false
    // 三种"不可用"形态(与 web 分支同口径):未激活是 404;切错账户是 200+success:false+unknownService;US 区本地拦截
    @State private var notActivated = false
    @State private var notAvailTitle: String? = nil
    @State private var notAvailMsg: String? = nil
    @State private var accessDel: String? = nil

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "externaldrive.badge.icloud", tint: t.info, title: "Backup FTP")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else if let title = notAvailTitle {
                        VStack(spacing: 7) {
                            Text(title).font(.system(size: 12.5, weight: .bold)).foregroundColor(t.color(t.fg))
                            Text(notAvailMsg ?? "").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(13)
                        .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surfaceMuted)))
                    } else if notActivated {
                        SheetNote(text: "尚未激活 Backup FTP 服务。激活后可获得用于离线备份的 FTP / NFS / CIFS 存储。", tint: t.muted)
                        ActBtn(kind: .primary, icon: "checkmark.circle", label: busy ? "激活中…" : "激活 Backup FTP") {
                            await activate()
                        }
                    } else if let i = info {
                        // OVH 模型字段:ftpBackupName / quota{value,unit} / usage{value,unit} / type(无 activated/status)
                        Card {
                            VStack(spacing: 8) {
                                SectionTitle(text: "状态")
                                KV(k: "服务器", v: i["ftpBackupName"] as? String ?? "—", mono: true)
                                KV(k: "配额", v: sizeText(i["quota"]))
                                KV(k: "已用", v: sizeText(i["usage"]))
                                if let ty = i["type"] as? String, !ty.isEmpty {
                                    KV(k: "类型", v: ty)
                                }
                            }
                        }

                        ActBtn(kind: .ghost, icon: "key.horizontal", label: "重置密码") { await resetPwd() }

                        VStack(alignment: .leading, spacing: 8) {
                            SectionTitle(text: "访问控制(允许连接的 IP 段)")
                            // access 拉失败不拖垮主信息,但必须说明"列表为空是没查到,不是没配过"
                            if let ae = accessErr {
                                Text("访问控制列表获取失败:\(ae)。下面列表可能不完整,为空不代表没配过。")
                                    .font(.system(size: 10)).foregroundColor(t.color(t.danger))
                            }
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

    /// OVH 字段格式:{value, unit} 对象(web quotaText 同款)
    private func sizeText(_ v: Any?) -> String {
        guard let d = v as? [String: Any], let val = numToDoubleAny(d["value"]) else {
            if let s = v as? String { return s }
            return "—"
        }
        return "\(Int(val)) \(d["unit"] as? String ?? "")".trimmingCharacters(in: .whitespaces)
    }

    private func load() async {
        // US 区官方 schema 没有 backupFTP 系列路径,本地直接拦下,省一次注定失败的请求
        let endpoint = conn.activeAccount?["endpoint"] as? String ?? ""
        if endpoint.contains("ovh-us") {
            notAvailTitle = "美区账户不提供备份FTP"
            notAvailMsg = "OVHcloud US 没有 dedicated/server/backupFTP 系列接口,请在 OVHcloud US 控制台使用其它备份方案。"
            loading = false
            return
        }
        do {
            let ir = try await conn.client.getDict("/server-control/\(sn)/backup-ftp")
            if (ir["success"] as? Bool) == false {
                // 200 + success:false = 后端拦下的"不可用"(切错账户 unknownService 等),给激活按钮只会必败
                if ir["unknownService"] as? Bool == true {
                    notAvailTitle = "服务器不存在或不属于当前账户"
                    var m = ir["error"] as? String ?? ""
                    if let reason = ir["reason"] as? String, !reason.isEmpty {
                        m += m.isEmpty ? "OVH 原文:" + reason : "(OVH 原文:" + reason + ")"
                    }
                    notAvailMsg = m
                } else {
                    notAvailTitle = "此服务器无 Backup FTP"
                    notAvailMsg = ir["error"] as? String
                }
            } else {
                // 200 有 backupFtp = 已激活(OVH 模型没有 activated/status 字段,响应本身就是激活的凭证)
                info = (ir["backupFtp"] as? [String: Any]) ?? ir
                accessErr = nil
                if let ar = try? await conn.client.getDict("/server-control/\(sn)/backup-ftp/access") {
                    accesses = (ar["accessList"] as? [[String: Any]]) ?? (ar["access"] as? [[String: Any]]) ?? []
                } else {
                    accesses = []
                    accessErr = "访问控制列表获取失败"
                }
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
            // 后端对未激活的备份 FTP 返回 404 —— 这正是"未激活"分支,不是读失败
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
            toast.show("激活请求已发送")
            await load()
        } catch { toast.show(error.localizedDescription, error: true) }
    }

    private func resetPwd() async {
        let (ok, msg) = await conn.client.actionPostData("/server-control/\(sn)/backup-ftp/password", bodyData: nil)
        toast.show(ok ? "新密码已发到邮箱" : (msg.isEmpty ? "失败" : msg), error: !ok)
    }

    private func addAccess() async {
        let ip = newBlock.trimmingCharacters(in: .whitespaces)
        guard !ip.isEmpty else { return toast.show("请填写要授权的 IP 段(CIDR,如 1.2.3.4/32)", error: true) }
        // handler 契约:ftp/nfs/cifs 是三个布尔;web 恒发 ftp:true(FTP 始终授权,勾选只加 NFS/CIFS)
        let body = try? JSONSerialization.data(withJSONObject: [
            "ipBlock": ip, "ftp": true, "nfs": nfs, "cifs": cifs,
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
    /// 有行处于 creationPending/removalPending 时每 5s 自动刷新(web 同款),状态自己变成「已生效」
    @State private var pollGen = 0

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
        .task(id: pollGen) {
            guard pollGen > 0 else { return }
            try? await Task.sleep(nanoseconds: 5_000_000_000)
            if !Task.isCancelled { await load() }
        }
    }

    private func blockCard(_ b: [String: Any]) -> some View {
        let block = b["ipBlock"] as? String ?? "—"
        let mitigations = b["mitigations"] as? [[String: Any]] ?? []
        let permanentOn = mitigations.contains { $0["permanent"] as? Bool ?? false }
        let isV6 = block.contains(":")
        return Card(border: permanentOn ? t.success : t.border) {
            VStack(alignment: .leading, spacing: 9) {
                HStack {
                    Text(block).font(.system(size: 12, design: .monospaced)).foregroundColor(t.color(t.fg))
                    Spacer()
                    Chip(text: permanentOn ? "永久缓解中" : "自动", color: permanentOn ? t.success : t.muted)
                }
                if let be = b["error"] as? String, !be.isEmpty {
                    Text("该 IP 块的缓解列表读取失败:\(be)").font(.system(size: 10)).foregroundColor(t.color(t.danger))
                }
                if isV6 {
                    Text("IPv6 不适用 anti-DDoS Mitigation(OVH 网络层免疫)").font(.system(size: 10)).foregroundColor(t.color(t.faint))
                } else if mitigations.isEmpty {
                    HStack {
                        Text("无永久缓解,自动缓解备用中").font(.system(size: 11)).foregroundColor(t.color(t.muted))
                        Spacer()
                        Button { Task { await toggle(ip: block, block: block, on: true) } } label: {
                            Text(busy ? "应用中…" : "启用永久缓解").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.accent))
                                .padding(.horizontal, 10).padding(.vertical, 6)
                                .background(Capsule().stroke(t.color(t.accent), lineWidth: 1))
                        }.buttonStyle(.plain)
                    }
                } else {
                    // 行字段与 web/后端一致:ipOnMitigation / state / auto / permanent / error(详情拉不到的占位行)
                    ForEach(mitigations.indices, id: \.self) { mi in
                        let mrow = mitigations[mi]
                        let ip = mrow["ipOnMitigation"] as? String ?? ""
                        let state = mrow["state"] as? String ?? ""
                        let rowErr = mrow["error"] as? String
                        let isOk = state == "ok"
                        let isCreating = state == "creationPending"
                        let isRemoving = state == "removalPending"
                        let pending2 = isCreating || isRemoving
                        VStack(spacing: 7) {
                            HStack {
                                Text(ip.isEmpty ? "—" : ip).font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                                Spacer()
                                if let re = rowErr, !re.isEmpty {
                                    // 详情没拉到的占位行:标出来,不然显示成一个没有状态的空行
                                    Chip(text: "获取失败", color: t.danger)
                                } else {
                                    Chip(text: isOk ? "已生效" : (isCreating ? "应用中" : (isRemoving ? "移除中" : (state.isEmpty ? "未知" : state))),
                                         color: isOk ? t.success : (pending2 ? t.warning : t.muted))
                                    if mrow["auto"] as? Bool ?? false {
                                        Text("自动").font(.system(size: 10)).foregroundColor(t.color(t.muted))
                                    }
                                    if mrow["permanent"] as? Bool ?? false {
                                        Text("永久").font(.system(size: 10, weight: .semibold)).foregroundColor(t.color(t.success))
                                    }
                                }
                            }
                            ActBtn(kind: .danger, icon: "xmark.shield",
                                   label: isCreating ? "应用中…" : (isRemoving ? "移除中…" : "关闭永久"),
                                   busy: busy || pending2) {
                                await toggle(ip: ip.isEmpty ? block : ip, block: block, on: false)
                            }
                            .disabled(!isOk || rowErr != nil)
                            if isCreating {
                                Text("正在启用中,通常 30 秒-2 分钟,等状态变已生效再点关闭")
                                    .font(.system(size: 9.5)).foregroundColor(t.color(t.faint))
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
        // 有过渡态就自轮询(web:creationPending/removalPending 每 5s 刷)
        let hasPending = blocks.flatMap { $0["mitigations"] as? [[String: Any]] ?? [] }
            .contains { ($0["state"] as? String ?? "") == "creationPending" || ($0["state"] as? String ?? "") == "removalPending" }
        if hasPending { pollGen += 1 }
    }

    private func toggle(ip: String, block: String, on: Bool) async {
        busy = true
        defer { busy = false }
        // :ip 要单个 IPv4(启用取网段前段;关闭用该行的 ipOnMitigation),?block= 带完整网段(与 web AdvancedTab 一致)
        let ipOnly = on ? block.components(separatedBy: "/").first ?? block : ip
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
                            let bw = net["bandwidth"] as? [String: Any] ?? [:]
                            // 带宽四档(OVH schema BandwidthDetails:OvhToInternet/InternetToOvh/OvhToOvh)
                            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 8) {
                                bwTile("出向 (OVH → 互联网)", bw["OvhToInternet"])
                                bwTile("入向 (互联网 → OVH)", bw["InternetToOvh"])
                                bwTile("内部 (OVH → OVH)", bw["OvhToOvh"])
                                bwTile("端口速率", net["connection"])
                            }
                            if let ty = bw["type"] as? String, !ty.isEmpty {
                                Text("带宽类型:" + ty).font(.system(size: 10.5, design: .monospaced)).foregroundColor(t.color(t.muted))
                            }

                            // 路由:routing.ipv4 / routing.ipv6 各是单个对象 {ip, gateway, network}
                            if let routing = net["routing"] as? [String: Any] {
                                routeCard("IPv4 路由", routing["ipv4"] as? [String: Any])
                                routeCard("IPv6 路由", routing["ipv6"] as? [String: Any])
                            }

                            // 交换机 / vMAC / vRack / 流量配额 / OLA
                            if let sw = net["switching"] as? [String: Any],
                               let name = sw["name"] as? String, !name.isEmpty {
                                Card {
                                    VStack(alignment: .leading, spacing: 5) {
                                        SectionTitle(text: "交换机")
                                        Text(name).font(.system(size: 12, design: .monospaced)).foregroundColor(t.color(t.fg)).textSelection(.enabled)
                                    }
                                }
                            }
                            if let vmac = net["vmac"] as? [String: Any] {
                                Card {
                                    VStack(alignment: .leading, spacing: 5) {
                                        SectionTitle(text: "虚拟 MAC (vMAC)")
                                        HStack(spacing: 8) {
                                            Chip(text: (vmac["supported"] as? Bool ?? false) ? "支持" : "不支持",
                                                 color: (vmac["supported"] as? Bool ?? false) ? t.success : t.muted)
                                            if let q = numToDoubleAny(vmac["quota"]).map(Int.init) {
                                                Text("配额:\(q)").font(.system(size: 11)).foregroundColor(t.color(t.muted))
                                            }
                                        }
                                    }
                                }
                            }
                            if let vrack = net["vrack"] as? [String: Any],
                               numToDoubleAny(vrack["bandwidth"]) != nil || (vrack["type"] as? String) != nil {
                                Card {
                                    VStack(alignment: .leading, spacing: 5) {
                                        SectionTitle(text: "vRack 私有网络")
                                        if let ty = vrack["type"] as? String {
                                            KV(k: "类型", v: ty, mono: true)
                                        }
                                        if numToDoubleAny(vrack["bandwidth"]) != nil {
                                            KV(k: "带宽", v: bwText(vrack["bandwidth"]))
                                        }
                                    }
                                }
                            }
                            if let traffic = net["traffic"] as? [String: Any] {
                                Card {
                                    VStack(alignment: .leading, spacing: 5) {
                                        SectionTitle(text: "流量配额")
                                        KV(k: "入向配额", v: traffic["inputQuotaSize"].map(quotaText) ?? "无限")
                                        KV(k: "出向配额", v: traffic["outputQuotaSize"].map(quotaText) ?? "无限")
                                        KV(k: "限速状态", v: (traffic["isThrottled"] as? Bool ?? false) ? "已限速" : "正常")
                                        if let rd = traffic["resetQuotaDate"] as? String, !rd.isEmpty {
                                            KV(k: "重置日期", v: fmtDate(rd))
                                        }
                                    }
                                }
                            }
                            if let ola = net["ola"] as? [String: Any] {
                                Card {
                                    VStack(alignment: .leading, spacing: 5) {
                                        SectionTitle(text: "OLA (OVH Link Aggregation)")
                                        Chip(text: (ola["available"] as? Bool ?? false) ? "可用" : "不可用",
                                             color: (ola["available"] as? Bool ?? false) ? t.success : t.muted)
                                        if let modes = ola["supportedModes"] as? [String], !modes.isEmpty {
                                            Text("支持模式:\(modes.joined(separator: ", "))")
                                                .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
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

    /// 路由卡:{ip, gateway, network};路由里有 CIDR/IPv6,打码用宽松规则(保留首段)
    private func routeCard(_ title: String, _ r: [String: Any]?) -> some View {
        Group {
            if let r {
                Card {
                    VStack(alignment: .leading, spacing: 5) {
                        SectionTitle(text: title)
                        KV(k: "IP 地址", v: maskAny(r["ip"]), mono: true)
                        KV(k: "网关", v: maskAny(r["gateway"]), mono: true)
                        KV(k: "网段", v: maskAny(r["network"]), mono: true)
                    }
                }
            }
        }
    }

    private func maskAny(_ v: Any?) -> String {
        guard let s = v as? String, !s.isEmpty else { return "—" }
        guard mask else { return s }
        if let dot = s.firstIndex(of: "."), dot > s.startIndex {
            return String(s[..<dot]) + ".***"
        }
        if let colon = s.firstIndex(of: ":"), colon > s.startIndex {
            return String(s[..<colon]) + ":****"
        }
        return s
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

    /// OVH 字段格式:{value, unit} | number | null(web fmtBandwidth 同款)
    private func bwText(_ v: Any?) -> String {
        if let d = v as? [String: Any] {
            guard let val = numToDoubleAny(d["value"]) else { return "—" }
            let unit = (d["unit"] as? String ?? "").trimmingCharacters(in: .whitespaces)
            return "\(trimNum(val)) \(unit)".trimmingCharacters(in: .whitespaces)
        }
        if let n = numToDoubleAny(v) {
            if n >= 1_000_000_000 { return String(format: "%.1f Gbps", n / 1_000_000_000) }
            if n >= 1_000_000 { return String(format: "%.0f Mbps", n / 1_000_000) }
            return trimNum(n)
        }
        if let s = v as? String { return s }
        return "—"
    }

    private func trimNum(_ n: Double) -> String {
        n.truncatingRemainder(dividingBy: 1) == 0 ? String(Int(n)) : String(format: "%.1f", n)
    }

    /// 字节数格式化(最小 GB,1024 进位;接受裸数字或 {value,unit},web fmtBytes 同款)——流量配额用这个,别用带宽的 bps 进位
    private func quotaText(_ v: Any?) -> String {
        var bytes: Double?
        if let d = v as? [String: Any], let n = numToDoubleAny(d["value"]) {
            let mult: Double
            switch (d["unit"] as? String ?? "B").uppercased() {
            case "PB": mult = 1125899906842624.0
            case "TB": mult = 1099511627776.0
            case "GB": mult = 1073741824.0
            case "MB": mult = 1048576.0
            case "KB": mult = 1024.0
            default: mult = 1.0
            }
            bytes = n * mult
        } else if let n = numToDoubleAny(v) {
            bytes = n
        }
        guard let b = bytes else {
            if let s = v as? String { return s }
            return "—"
        }
        if b == 0 { return "0 GB" }
        let gb = 1073741824.0, tb = 1099511627776.0, pb = 1125899906842624.0
        if b >= pb { return "\(fmtQ(b / pb)) PB" }
        if b >= tb { return "\(fmtQ(b / tb)) TB" }
        return "\(fmtQ(b / gb)) GB"
    }

    private func fmtQ(_ x: Double) -> String {
        x.truncatingRemainder(dividingBy: 1) == 0 ? String(Int(x)) : String(format: "%.2f", x)
    }

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
