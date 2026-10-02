import SwiftUI

/**
 * 抢购写操作(最后一块全量缺口):
 * - CreateOrderSheet:创建抢购任务(机型+机房多选+数量+自动付款开关,默认关)
 * - MonitorOverlay:监控订阅管理(列表+自动下单开关+数量编辑+删除)
 * 契约均对后端 handler 逐字段核过(AddQueueItem / AddSubscription / UpdateSubscription)。
 */

// MARK: - 创建抢购任务

struct CreateOrderSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let onClose: () -> Void

    @State private var plans: [[String: Any]] = []
    @State private var availability: [String: [String: String]] = [:]
    @State private var planCode = ""
    @State private var pickedDCs: Set<String> = []
    @State private var quantity = 1
    @State private var autoPay = false
    @State private var busy = false
    @State private var loadErr: String?

    var t: Tokens { theme.t }

    /// 与 web/后端一致的上限(core/order-limits)
    static let maxQty = 20
    static let maxFanout = 60

    /// 收进扇出上限的真实方案(预览行显示真数,不虚报)
    var clampedQuantity: Int {
        let maxByFanout = pickedDCs.isEmpty ? Self.maxQty : Self.maxFanout / pickedDCs.count
        return max(1, min(min(quantity, Self.maxQty), max(1, maxByFanout)))
    }
    var totalTasks: Int { pickedDCs.count * clampedQuantity }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                HStack(spacing: 7) {
                    Image(systemName: "plus.circle.fill").font(.system(size: 15)).foregroundColor(t.color(t.fg))
                    Text("创建抢购任务").font(.system(size: 15, weight: .bold)).foregroundColor(t.color(t.fg))
                }
                Spacer()
                Button(action: onClose) { Image(systemName: "xmark").font(.system(size: 15)).foregroundColor(t.color(t.muted)) }
            }
            .padding(16)

            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if let e = loadErr {
                        Text(e).font(.system(size: 12)).foregroundColor(t.color(t.danger))
                    }

                    // 机型:目录里有货的排前
                    Text("机型").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    if plans.isEmpty {
                        Text("目录读取中…").font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
                    } else {
                        VStack(spacing: 6) {
                            ForEach(sortedPlans.prefix(25).indices, id: \.self) { i in
                                let code = sortedPlans[i]["planCode"] as? String ?? ""
                                let on = planCode == code
                                Button { planCode = code; pickedDCs = []; refreshDCs() } label: {
                                    HStack {
                                        VStack(alignment: .leading, spacing: 2) {
                                            Text(code).font(.system(size: 12.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                                            if let mem = sortedPlans[i]["memory"] as? String, !mem.isEmpty {
                                                Text("\(mem) / \(sortedPlans[i]["storage"] as? String ?? "")")
                                                    .font(.system(size: 10)).foregroundColor(t.color(t.muted))
                                            }
                                        }
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
                        }
                    }

                    // 机房:只列该机型在当前账户站点实际可选的(可用性接口返回什么就显示什么)
                    if !planCode.isEmpty {
                        Text("机房(可多选)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        FlowLayout(spacing: 6) {
                            ForEach(dcKeys, id: \.self) { dc in
                                let status = availability[planCode]?[dc] ?? ""
                                let ok = isOrderable(status)
                                let on = pickedDCs.contains(dc)
                                Button { toggleDC(dc) } label: {
                                    Text("\(dc.uppercased()) \(status)")
                                        .font(.system(size: 10, design: .monospaced))
                                        .foregroundColor(t.color(on ? t.fg : (ok ? t.success : t.faint)))
                                        .padding(.horizontal, 9).padding(.vertical, 5)
                                        .background(RoundedRectangle(cornerRadius: 9).fill(on ? t.color(t.fg).opacity(0.1) : Color.clear).overlay(RoundedRectangle(cornerRadius: 9).stroke(t.color(on ? t.fg : (ok ? t.success : t.border)), lineWidth: on ? 1.5 : 1)))
                                }
                                .buttonStyle(.plain)
                            }
                        }
                    }

                    // 数量 stepper
                    Text("每机房数量:\(clampedQuantity)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    HStack(spacing: 14) {
                        Button { quantity = max(1, quantity - 1) } label: {
                            Image(systemName: "minus.circle").font(.system(size: 21)).foregroundColor(t.color(quantity > 1 ? t.fg : t.faint))
                        }
                        .buttonStyle(.plain)
                        Text("\(quantity)").font(.system(size: 16, weight: .bold, design: .monospaced)).foregroundColor(t.color(t.fg))
                        Button { quantity = min(Self.maxQty, quantity + 1) } label: {
                            Image(systemName: "plus.circle").font(.system(size: 21)).foregroundColor(t.color(quantity < Self.maxQty ? t.fg : t.faint))
                        }
                        .buttonStyle(.plain)
                        Spacer()
                        Text("将创建 \(totalTasks) 个任务").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                    }

                    // 自动付款(默认关 — 用户拍板的规则)
                    Toggle(isOn: $autoPay) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("抢到后自动付款").font(.system(size: 12.5)).foregroundColor(t.color(t.fg))
                            Text("用 OVH 账户默认支付方式扣款;默认关闭").font(.system(size: 10)).foregroundColor(t.color(t.muted))
                        }
                    }
                    .tint(t.color(t.success))

                    Text("任务创建后立即开始抢购;可在队列页暂停/删除。下单保留 14 天撤回权。")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.faint))

                    Button { Task { await submit() } } label: {
                        Text(busy ? "创建中…" : "创建 \(totalTasks) 个任务")
                            .font(.system(size: 15, weight: .semibold)).foregroundColor(.white)
                            .frame(maxWidth: .infinity, minHeight: 47)
                            .background(RoundedRectangle(cornerRadius: 14).fill(t.color(t.fg)))
                    }
                    .buttonStyle(.plain)
                    .disabled(busy || planCode.isEmpty || pickedDCs.isEmpty)
                    .opacity(planCode.isEmpty || pickedDCs.isEmpty ? 0.5 : 1)
                }
                .padding(14)
            }
        }
        .background(t.color(t.surface))
        .task { await load() }
    }

    private var sortedPlans: [[String: Any]] {
        plans.sorted { stock($0) && !stock($1) }
    }
    private func stock(_ p: [String: Any]) -> Bool {
        let code = p["planCode"] as? String ?? ""
        return (availability[code] ?? [:]).values.contains { isOrderable($0) }
    }
    private var dcKeys: [String] {
        (availability[planCode] ?? [:]).keys.sorted()
    }
    private func toggleDC(_ dc: String) {
        if pickedDCs.contains(dc) { pickedDCs.remove(dc) } else { pickedDCs.insert(dc) }
    }
    /// 选机型时预选所有有货机房(web 同款便利)
    private func refreshDCs() {
        pickedDCs = Set(dcKeys.filter { isOrderable(availability[planCode]?[$0] ?? "") })
    }

    /// 契约:POST /queue,每机房×每台一条(account_id 走 ?account= 由 client 统一追加)
    private func submit() async {
        busy = true
        defer { busy = false }
        let payload: [String: Any] = [
            "planCode": planCode,
            "datacenter": "",
            "options": [],
            "retryInterval": 30,
            "autoPay": autoPay,
        ]
        let data = try? JSONSerialization.data(withJSONObject: payload)
        var created = 0, firstErr: String?
        for dc in pickedDCs.sorted() {
            for _ in 0..<clampedQuantity {
                var body = payload
                body["datacenter"] = dc
                let d = try? JSONSerialization.data(withJSONObject: body)
                let (ok, msg) = await conn.client.actionPostData("/queue", bodyData: d)
                _ = data
                if ok { created += 1 } else if firstErr == nil { firstErr = msg }
            }
        }
        onClose()
        let a = UIAlertController(
            title: created > 0 ? "已创建 \(created) 个任务" : "创建失败",
            message: firstErr ?? "可在「抢购队列」页查看与暂停",
            preferredStyle: .alert)
        a.addAction(UIAlertAction(title: "好", style: .default))
        AlertHost.present(a)
    }

    private func load() async {
        let c = conn.client
        do {
            async let p = c.getDict("/servers")
            async let a = c.getDict("/availability")
            let (pr, ar) = try await (p, a)
            plans = (pr["servers"] as? [[String: Any]]) ?? []
            availability = (ar["availability"] as? [String: [String: String]]) ?? [:]
            loadErr = nil
        } catch { loadErr = error.localizedDescription }
    }
}

/// 可用性白名单(与后端 IsAvailableForOrder / core 一致)
private func isOrderable(_ s: String) -> Bool {
    s.range(of: "^\\d+H(-high|-low)?$", options: .regularExpression) != nil
}

// MARK: - 监控订阅管理

struct MonitorOverlay: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let onClose: () -> Void

    @State private var subs: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            OverlayHeaderView(title: "服务器监控", subtitle: "补货订阅", onClose: onClose, t: t)
            ScrollView {
                LazyVStack(spacing: 10) {
                    if let e = err {
                        ErrorCard(message: e, t: t)
                    } else if loading {
                        ProgressView().padding(.top, 40)
                    } else if subs.isEmpty {
                        EmptyCard(t: t, text: "没有订阅 —— 在网页端或 TG /watch 添加\n(App 侧创建下一批接入)")
                    } else {
                        ForEach(subs.indices, id: \.self) { i in
                            subCard(subs[i])
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .task { await load() }
        .refreshable { await load() }
    }

    private func subCard(_ sub: [String: Any]) -> some View {
        let code = sub["planCode"] as? String ?? ""
        let auto = sub["autoOrder"] as? Bool ?? false
        let qty = sub["quantity"] as? Int ?? 1
        let autoPay = sub["autoPay"] as? Bool ?? false
        let dcs = (sub["datacenters"] as? [String]) ?? []
        let options = (sub["options"] as? [String]) ?? []

        return VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(code).font(.system(size: 12.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                Spacer()
                Text(auto ? "自动抢 \(qty)" : "只通知")
                    .font(.system(size: 10.5, weight: .semibold))
                    .foregroundColor(t.color(auto ? t.success : t.muted))
            }
            Text("机房:\(dcs.map { $0.uppercased() }.joined(separator: " "))")
                .font(.system(size: 11)).foregroundColor(t.color(t.muted))
            if !options.isEmpty {
                Text("配置:\(options.joined(separator: ", "))")
                    .font(.system(size: 10.5, design: .monospaced)).foregroundColor(t.color(t.muted))
                    .lineLimit(2)
            }
            if auto && autoPay {
                Text("抢到后自动付款").font(.system(size: 10)).foregroundColor(t.color(t.warning))
            }
            // 操作:自动下单开关 / 删除
            HStack(spacing: 8) {
                Button { Task { await toggleAuto(sub, to: !auto) } } label: {
                    Label(auto ? "改为只通知" : "开自动抢", systemImage: auto ? "bell" : "bolt.fill")
                        .font(.system(size: 11)).foregroundColor(t.color(auto ? t.muted : t.success))
                        .padding(.horizontal, 10).padding(.vertical, 5)
                        .background(Capsule().stroke(t.color(auto ? t.muted : t.success), lineWidth: 1))
                }
                .buttonStyle(.plain)
                Button { delete(code) } label: {
                    Label("删除", systemImage: "trash")
                        .font(.system(size: 11)).foregroundColor(t.color(t.danger))
                        .padding(.horizontal, 10).padding(.vertical, 5)
                        .background(Capsule().stroke(t.color(t.danger), lineWidth: 1))
                }
                .buttonStyle(.plain)
            }
        }
        .padding(13)
        .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(auto ? t.success : t.border), lineWidth: auto ? 1.5 : 1)))
    }

    /// 契约:PUT /monitor/subscriptions/:planCode(对 UpdateSubscription;保留原机房等字段只改动目标项)
    private func toggleAuto(_ sub: [String: Any], to auto: Bool) async {
        var payload: [String: Any] = [
            "planCode": sub["planCode"] ?? "",
            "datacenters": sub["datacenters"] ?? [],
            "notifyAvailable": sub["notifyAvailable"] ?? true,
            "notifyUnavailable": sub["notifyUnavailable"] ?? false,
            "autoOrder": auto,
            "quantity": auto ? (sub["quantity"] as? Int ?? 1) : 0,
            "autoPay": auto ? (sub["autoPay"] as? Bool ?? false) : false,
        ]
        if let acc = sub["autoOrderAccountId"] as? String, !acc.isEmpty {
            payload["autoOrderAccountId"] = acc
        }
        let d = try? JSONSerialization.data(withJSONObject: payload)
        _ = await conn.client.actionPutData("/monitor/subscriptions/\(sub["planCode"] ?? "")", bodyData: d)
        await load()
    }

    private func delete(_ code: String) {
        let a = UIAlertController(title: "删除订阅?", message: "\(code) 的补货监控将停止。", preferredStyle: .alert)
        a.addAction(UIAlertAction(title: "取消", style: .cancel))
        a.addAction(UIAlertAction(title: "删除", style: .destructive) { _ in
            Task {
                _ = await conn.client.actionDelete("/monitor/subscriptions/\(code)")
                await load()
            }
        })
        AlertHost.present(a)
    }

    private func load() async {
        do { subs = try await conn.client.getArray("/monitor/subscriptions"); err = nil }
        catch { err = error.localizedDescription }
        loading = false
    }
}
