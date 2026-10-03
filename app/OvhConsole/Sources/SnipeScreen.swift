import SwiftUI

/**
 * 抢购页:目录(机型+库存+价格)/ 队列(任务管理)/ 历史(订单与付款)三段。
 * 对齐 web servers + queue + history 三页的手机版。
 */
struct SnipeScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @EnvironmentObject var nav: AppNav
    var t: Tokens { theme.t }

    @State private var showAccountPicker = false

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                Picker("", selection: $nav.snipeSegment) {
                    Text("目录").tag(0)
                    Text("队列").tag(1)
                    Text("历史").tag(2)
                }
                .pickerStyle(.segmented)
                .padding(.horizontal, 16).padding(.vertical, 8)

                switch nav.snipeSegment {
                case 1: QueuePane()
                case 2: HistoryPane()
                default: CatalogPane()
                }
            }
            .background(t.color(t.bg))
            .navigationTitle("抢购")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarColorScheme(theme.dark ? .dark : .light, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    AccountButton { showAccountPicker = true }
                }
            }
            .sheet(isPresented: $showAccountPicker) {
                AccountPickerSheet()
                    .environmentObject(theme).environmentObject(conn).environmentObject(toast)
            }
        }
    }
}

// MARK: - 目录段

struct CatalogPane: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    var t: Tokens { theme.t }

    @State private var plans: [[String: Any]] = []
    @State private var availability: [String: [String: String]] = [:]
    @State private var priceMap: [String: Double] = [:]
    @State private var priceCurrency = ""
    @State private var err: String?
    @State private var loading = true
    @State private var search = ""
    @State private var onlyAvailable = false
    @State private var orderPlan: [String: Any]?

    var body: some View {
        ScrollView {
            VStack(spacing: 10) {
                HStack(spacing: 8) {
                    Image(systemName: "magnifyingglass").font(.system(size: 12)).foregroundColor(t.color(t.faint))
                    TextField("搜 planCode / CPU / 内存", text: $search)
                        .font(.system(size: 12.5)).textInputAutocapitalization(.never).autocorrectionDisabled()
                        .foregroundColor(t.color(t.fg))
                    if !search.isEmpty {
                        Button { search = "" } label: {
                            Image(systemName: "xmark.circle.fill").font(.system(size: 12)).foregroundColor(t.color(t.faint))
                        }.buttonStyle(.plain)
                    }
                    Toggle(isOn: $onlyAvailable) {
                        Text("仅有货").font(.system(size: 11)).foregroundColor(t.color(t.muted))
                    }.toggleStyle(.button).tint(t.color(t.accent))
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surface)))

                if let e = err {
                    Card { LoadFailed(message: e) { Task { await load() } } }
                } else if loading {
                    ProgressView().padding(.top, 50)
                } else if filtered.isEmpty {
                    Card { EmptyHint(icon: "shippingbox", text: "目录为空 —— 检查账户或刷新缓存") }
                } else {
                    Text("\(filtered.count) 款机型")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                        .frame(maxWidth: .infinity, alignment: .leading)
                    ForEach(filtered.indices, id: \.self) { i in
                        catalogCard(filtered[i])
                    }
                }
            }
            .padding(16)
        }
        .background(t.color(t.bg))
        .refreshable { await load() }
        .task { await load() }
        .onChange(of: conn.accountId) { _ in loading = true; Task { await load() } }
        .sheet(item: Binding(
            get: { orderPlan.map { PlanWrap(plan: $0) } },
            set: { orderPlan = $0?.plan }
        )) { w in
            SnipeOrderSheet(plan: w.plan, availability: availability)
                .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
    }

    private struct PlanWrap: Identifiable {
        let plan: [String: Any]
        var id: String { plan["planCode"] as? String ?? UUID().uuidString }
    }

    private var filtered: [[String: Any]] {
        var list = plans
        if onlyAvailable { list = list.filter { hasStock($0) } }
        let q = search.trimmingCharacters(in: .whitespaces).lowercased()
        if !q.isEmpty {
            list = list.filter {
                let s = (($0["planCode"] as? String ?? "") + ($0["name"] as? String ?? "") +
                         ($0["cpu"] as? String ?? "") + ($0["memory"] as? String ?? "") +
                         ($0["description"] as? String ?? "")).lowercased()
                return s.contains(q)
            }
        }
        return list.sorted { hasStock($0) && !hasStock($1) }
    }

    private func hasStock(_ p: [String: Any]) -> Bool {
        let code = p["planCode"] as? String ?? ""
        return (availability[code] ?? [:]).values.contains { isOrderable($0) }
    }

    private func catalogCard(_ p: [String: Any]) -> some View {
        let code = p["planCode"] as? String ?? ""
        let stock = hasStock(p)
        let dcs = availability[code] ?? [:]
        let availCount = dcs.values.filter { isOrderable($0) }.count
        return Card(border: stock ? t.success : nil) {
            VStack(spacing: 10) {
                HStack(alignment: .top, spacing: 8) {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 6) {
                            Text(code).font(.system(size: 13, weight: .bold, design: .monospaced)).foregroundColor(t.color(t.fg))
                            if stock { Chip(text: "有货", color: t.success) }
                        }
                        Text(specLine(p)).font(.system(size: 10.5)).foregroundColor(t.color(t.muted)).lineLimit(1)
                    }
                    Spacer()
                    if let price = priceMap[code], price > 0 {
                        Text(String(format: "%.2f", price) + (priceCurrency.isEmpty ? "" : " ") + priceCurrency)
                            .font(.system(size: 13, weight: .bold, design: .rounded)).foregroundColor(t.color(t.fg))
                        Text("/月").font(.system(size: 9)).foregroundColor(t.color(t.faint))
                    }
                }
                if !dcs.isEmpty {
                    FlowLayout(spacing: 5) {
                        ForEach(dcs.keys.sorted(), id: \.self) { dc in
                            let ok = isOrderable(dcs[dc] ?? "")
                            Text("\(dc.uppercased()) \(dcs[dc] ?? "")")
                                .font(.system(size: 9, design: .monospaced))
                                .foregroundColor(t.color(ok ? t.success : t.faint))
                                .padding(.horizontal, 7).padding(.vertical, 3)
                                .background(RoundedRectangle(cornerRadius: 7).fill(ok ? t.color(t.success).opacity(0.1) : t.color(t.surfaceMuted)))
                        }
                    }
                }
                HStack(spacing: 8) {
                    Text(availCount > 0 ? "\(availCount) 个机房可下单" : "全部无货")
                        .font(.system(size: 10)).foregroundColor(t.color(t.faint))
                    Spacer()
                    // 一键加监控:盯该机型全部机房
                    Button {
                        Task { await addMonitor(code) }
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: "eye").font(.system(size: 10))
                            Text("加监控").font(.system(size: 11, weight: .semibold))
                        }.foregroundColor(t.color(t.info))
                        .padding(.horizontal, 10).padding(.vertical, 6)
                        .background(Capsule().stroke(t.color(t.info), lineWidth: 1))
                    }.buttonStyle(.plain)
                    Button { orderPlan = p } label: {
                        HStack(spacing: 4) {
                            Image(systemName: "bolt.fill").font(.system(size: 10))
                            Text("抢购").font(.system(size: 11, weight: .bold))
                        }.foregroundColor(theme.t.dark ? .white : t.color(t.accent))
                        .padding(.horizontal, 12).padding(.vertical, 6)
                        .background(Capsule().fill(t.color(t.accent)))
                    }.buttonStyle(.plain)
                }
            }
        }
    }

    private func specLine(_ p: [String: Any]) -> String {
        var parts: [String] = []
        for key in ["cpu", "processor"] { if let v = p[key] as? String, !v.isEmpty { parts.append(v) } }
        for key in ["memory", "ram"] { if let v = p[key] as? String, !v.isEmpty { parts.append(v) } }
        for key in ["storage", "disk"] { if let v = p[key] as? String, !v.isEmpty { parts.append(v) } }
        for key in ["bandwidth"] { if let v = p[key] as? String, !v.isEmpty { parts.append(v) } }
        return parts.isEmpty ? (p["description"] as? String ?? "") : parts.joined(separator: " · ")
    }

    private func addMonitor(_ code: String) async {
        do {
            _ = try await conn.client.post("/monitor/subscriptions", body: ["planCode": code])
            toast.show("已加入雷达监控(全部机房)")
        } catch { toast.show(error.localizedDescription, error: true) }
    }

    private func load() async {
        err = nil
        do {
            let sr = try await conn.client.getDict("/servers")
            plans = (sr["servers"] as? [[String: Any]]) ?? []
            // 可用性内嵌在每个 plan 的 datacenters:[{datacenter, availability}]
            var avail: [String: [String: String]] = [:]
            for p in plans {
                guard let code = p["planCode"] as? String else { continue }
                var dcMap: [String: String] = [:]
                for d in (p["datacenters"] as? [[String: Any]]) ?? [] {
                    if let dc = d["datacenter"] as? String, let st = d["availability"] as? String {
                        dcMap[dc] = st
                    }
                }
                avail[code] = dcMap
            }
            availability = avail
            // 目录价(catalog 缓存 2 小时,后端秒回)
            if sub2.isEmpty { return }
            if let cr = try? await conn.client.getDict("/catalog?subsidiary=\(urlEncode(sub2))"),
               let cplans = cr["plans"] as? [[String: Any]] {
                for p in cplans {
                    if let code = p["planCode"] as? String {
                        priceMap[code] = monthlyPriceOf(p["pricings"] as? [[String: Any]])
                    }
                }
                priceCurrency = (cr["locale"] as? [String: Any])?["currencyCode"] as? String ?? ""
            }
        } catch { err = error.localizedDescription }
        loading = false
    }

    private var sub2: String {
        if let acc = conn.activeAccount, let z = acc["zone"] as? String, !z.isEmpty { return z }
        return ""
    }

    private func monthlyPriceOf(_ pricings: [[String: Any]]?) -> Double {
        guard let ps = pricings else { return 0 }
        for p in ps {
            let caps = p["capacities"] as? [String] ?? []
            if caps.contains("installation") { continue }
            if (p["intervalUnit"] as? String) == "month", (p["interval"] as? Int) == 1, (p["mode"] as? String) == "default" {
                return numToDoubleAny(p["price"]).map { $0 / 1e8 } ?? 0
            }
        }
        return 0
    }
}

// MARK: - 抢购下单 sheet(配置 + 机房 + 数量)

struct SnipeOrderSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let plan: [String: Any]
    let availability: [String: [String: String]]
    var t: Tokens { theme.t }

    @State private var families: [(key: String, addons: [String])] = []
    @State private var addonPrices: [String: Double] = [:]
    @State private var selected: [String] = []           // 每组选中的 addon(去前缀比较)
    @State private var dcs: [String] = []
    @State private var pickedDCs: Set<String> = []
    @State private var qty = 1
    @State private var interval = 5
    @State private var autoPay = false
    @State private var currency = ""
    @State private var basePrice: Double = 0
    @State private var loading = true
    @State private var busy = false

    private var planCode: String { plan["planCode"] as? String ?? "" }

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "bolt.fill", tint: t.accent, title: "抢购 \(planCode)")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    if loading {
                        ProgressView().padding(30)
                    } else {
                        // 价格 hero
                        VStack(spacing: 3) {
                            Text(String(format: "%.2f", totalPrice) + (currency.isEmpty ? " 无币种" : " \(currency)"))
                                .font(.system(size: 24, weight: .bold, design: .rounded)).foregroundColor(t.color(t.fg))
                            Text("月费 = 基础 \(String(format: "%.2f", basePrice)) + 选配 \(String(format: "%.2f", totalPrice - basePrice))")
                                .font(.system(size: 10)).foregroundColor(t.color(t.muted))
                        }
                        .frame(maxWidth: .infinity).padding(12)
                        .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.accent).opacity(0.07)))

                        // 配置组
                        if families.isEmpty {
                            SheetNote(text: "目录里没有该机型的选配项,按默认配置下单。", tint: t.muted)
                        } else {
                            ForEach(families.indices, id: \.self) { i in
                                let fam = families[i]
                                VStack(alignment: .leading, spacing: 6) {
                                    Text(groupLabel(fam.key)).font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.muted))
                                    FlowLayout(spacing: 6) {
                                        ForEach(fam.addons, id: \.self) { code in
                                            addonChip(fam.key, code)
                                        }
                                    }
                                }
                            }
                        }

                        // 机房
                        VStack(alignment: .leading, spacing: 6) {
                            HStack {
                                Text("数据中心").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.muted))
                                Spacer()
                                if dcs.contains(where: { isOrderable((availability[planCode]?[$0]) ?? "") }) {
                                    Button { pickAllAvailable() } label: {
                                        Text("选有货").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.accent))
                                    }.buttonStyle(.plain)
                                }
                                if !pickedDCs.isEmpty {
                                    Button { pickedDCs.removeAll() } label: {
                                        Text("清空").font(.system(size: 11)).foregroundColor(t.color(t.faint))
                                    }.buttonStyle(.plain)
                                }
                            }
                            FlowLayout(spacing: 6) {
                                ForEach(dcs, id: \.self) { dc in
                                let status = availability[planCode]?[dc] ?? ""
                                    let ok = isOrderable(status)
                                    let on = pickedDCs.contains(dc)
                                    Button { toggleDC(dc) } label: {
                                        Text("\(dc.uppercased()) \(status)")
                                            .font(.system(size: 10, design: .monospaced))
                                            .foregroundColor(t.color(on ? t.fg : (ok ? t.success : t.faint)))
                                            .padding(.horizontal, 8).padding(.vertical, 5)
                                            .background(RoundedRectangle(cornerRadius: 8).fill(on ? t.color(t.accent) : (ok ? t.color(t.success).opacity(0.1) : t.color(t.surfaceMuted))))
                                    }.buttonStyle(.plain)
                                }
                            }
                        }

                        // 数量与间隔
                        HStack(spacing: 14) {
                            VStack(alignment: .leading, spacing: 4) {
                                Text("每机房数量:\(qty)").font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
                                Stepper("", value: $qty, in: 1...10).labelsHidden()
                            }
                            VStack(alignment: .leading, spacing: 4) {
                                Text("重试间隔:\(interval) 秒").font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
                                Stepper("", value: $interval, in: 1...120, step: 1).labelsHidden()
                            }
                        }

                        Toggle(isOn: $autoPay) {
                            VStack(alignment: .leading, spacing: 1) {
                                Text("抢到后自动付款").font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                                Text("自动付款有真实扣款风险,默认关闭").font(.system(size: 10)).foregroundColor(t.color(t.muted))
                            }
                        }.tint(t.color(t.accent))

                        Text("将创建 \(pickedDCs.count * qty) 个任务").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.warning))

                        HStack(spacing: 10) {
                            ActBtn(kind: .ghost, icon: "eye", label: "加入监控") {
                                Task { await addMonitorWithConfig() }
                            }
                            ActBtn(kind: .primary, icon: "bolt.fill", label: busy ? "创建中…" : "创建抢购任务", busy: busy) {
                                await submit()
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
    }

    private var totalPrice: Double {
        basePrice + selected.reduce(0) { $0 + (addonPrices[$1] ?? 0) }
    }

    private func groupLabel(_ key: String) -> String {
        // family key 形如 "250sk-内存" 或 fqn 维度;去掉 planCode 前缀做标签
        var label = key
        for prefix in [planCode + "-", planCode] where label.hasPrefix(prefix) {
            label = String(label.dropFirst(prefix.count))
        }
        return label.isEmpty ? key : label
    }

    private func addonChip(_ famKey: String, _ code: String) -> some View {
        let on = isSelected(famKey, code)
        let price = addonPrices[code] ?? 0
        return Button { toggleAddon(famKey, code) } label: {
            VStack(spacing: 2) {
                Text(shortAddon(code, famKey: famKey))
                    .font(.system(size: 10.5)).foregroundColor(t.color(on ? t.fg : t.muted))
                if price > 0 {
                    Text(String(format: "+%.2f", price)).font(.system(size: 9, design: .rounded)).foregroundColor(t.color(on ? t.accent : t.faint))
                }
            }
            .padding(.horizontal, 9).padding(.vertical, 6)
            .background(RoundedRectangle(cornerRadius: 9).fill(on ? t.color(t.accent).opacity(0.18) : t.color(t.surfaceMuted)))
        }
        .buttonStyle(.plain)
    }

    private func shortAddon(_ code: String, famKey: String) -> String {
        // 去掉组前缀,剩人类可读部分
        var s = code
        if let dash = s.range(of: famKey + "-", options: .caseInsensitive) {
            s = String(s[dash.upperBound...])
        } else if s.hasSuffix(famKey) {
            s = String(s.dropLast(famKey.count))
        }
        return s.isEmpty ? code : s
    }

    private func isSelected(_ famKey: String, _ code: String) -> Bool {
        // 每组单选:selected 存完整 addon code
        selected.contains(code)
    }

    private func toggleAddon(_ famKey: String, _ code: String) {
        // 同组其他项取消
        let fam = families.first { $0.key == famKey }
        selected.removeAll { fam?.addons.contains($0) ?? false }
        if !selected.contains(code) { selected.append(code) }
    }

    private func toggleDC(_ dc: String) {
        if pickedDCs.contains(dc) { pickedDCs.remove(dc) } else { pickedDCs.insert(dc) }
    }

    private func pickAllAvailable() {
        pickedDCs = Set(dcs.filter { isOrderable((availability[planCode]?[$0]) ?? "") })
    }

    private func load() async {
        defer { loading = false }
        // catalog 只认 ?subsidiary=(不认 ?account=),要跟当前账户的结算区走
        var sub = ""
        if let acc = conn.activeAccount, let z = acc["zone"] as? String, !z.isEmpty { sub = z }
        let path = sub.isEmpty ? "/catalog" : "/catalog?subsidiary=\(urlEncode(sub))"
        guard let r = try? await conn.client.getDict(path) else { return }
        currency = (r["locale"] as? [String: Any])?["currencyCode"] as? String ?? ""
        let plans = (r["plans"] as? [[String: Any]]) ?? []
        let addons = (r["addons"] as? [[String: Any]]) ?? []
        var addonByCode: [String: [String: Any]] = [:]
        for a in addons { if let c = a["planCode"] as? String { addonByCode[c] = a } }
        // addon 月价
        for (c, a) in addonByCode {
            addonPrices[c] = monthlyPrice(a["pricings"] as? [[String: Any]])
        }
        // 机房(可用性 map)
        dcs = (availability[planCode] ?? [:]).keys.sorted()
        // 该 plan 的 families
        if let p = plans.first(where: { ($0["planCode"] as? String) == planCode }) {
            basePrice = monthlyPrice(p["pricings"] as? [[String: Any]])
            // 目录形状:addonFamilies:[{name, addons:[...]}](不是 families 字典)
            if let fams = p["addonFamilies"] as? [[String: Any]] {
                families = fams.compactMap { f in
                    let name = f["name"] as? String ?? ""
                    let list = (f["addons"] as? [Any])?.compactMap { $0 as? String } ?? []
                    return (name.isEmpty || list.isEmpty) ? nil : (key: name, addons: list)
                }
            }
        }
    }

    private func monthlyPrice(_ pricings: [[String: Any]]?) -> Double {
        guard let ps = pricings else { return 0 }
        for p in ps {
            let caps = p["capacities"] as? [String] ?? []
            if caps.contains("installation") { continue }
            if (p["intervalUnit"] as? String) == "month", (p["interval"] as? Int) == 1, (p["mode"] as? String) == "default" {
                return (p["price"] as? Double ?? 0) / 1e8
            }
        }
        return 0
    }

    private func submit() async {
        guard !pickedDCs.isEmpty else { return toast.show("至少选一个机房", error: true) }
        // 后端按 body.account_id 决定下单账户(?account= 只影响查询)
        let accountId = conn.accountId.isEmpty ? (conn.activeAccount?["id"] as? String ?? "") : conn.accountId
        guard !accountId.isEmpty else { return toast.show("没有可用的 OVH 账户", error: true) }
        let total = pickedDCs.count * qty
        guard total <= 30 else { return toast.show("一次最多创建 30 个任务(当前 \(total))", error: true) }
        busy = true
        defer { busy = false }
        var okCount = 0, failMsg = ""
        for dc in pickedDCs.sorted() {
            for _ in 0..<qty {
                var body: [String: Any] = [
                    "account_id": accountId,
                    "planCode": planCode,
                    "datacenter": dc,
                    "retryInterval": interval,
                ]
                if !selected.isEmpty { body["options"] = selected }
                if autoPay { body["autoPay"] = true }
                do {
                    _ = try await conn.client.post("/queue", body: body)
                    okCount += 1
                } catch {
                    failMsg = error.localizedDescription
                }
            }
        }
        toast.show(okCount > 0 ? "已创建 \(okCount) 个任务" : (failMsg.isEmpty ? "创建失败" : failMsg), error: okCount == 0)
        if okCount > 0 { dismiss() }
    }

    private func addMonitorWithConfig() async {
        var body: [String: Any] = ["planCode": planCode]
        let dcs = pickedDCs.sorted()
        if !dcs.isEmpty { body["datacenters"] = dcs }
        if !selected.isEmpty { body["options"] = selected }
        do {
            _ = try await conn.client.post("/monitor/subscriptions", body: body)
            toast.show("已加入雷达(含当前选配)")
            dismiss()
        } catch { toast.show(error.localizedDescription, error: true) }
    }
}

// MARK: - 队列段

struct QueuePane: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    var t: Tokens { theme.t }

    @State private var items: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true
    @State private var editItem: [String: Any]?
    @State private var clearConfirm = false

    var body: some View {
        ScrollView {
            VStack(spacing: 10) {
                HStack {
                    Text("\(items.count) 个任务").font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                    Spacer()
                    if !items.isEmpty {
                        Button { clearConfirm = true } label: {
                            Text("清空队列").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.danger))
                        }.buttonStyle(.plain)
                    }
                }
                if let e = err {
                    Card { LoadFailed(message: e) { Task { await load() } } }
                } else if loading {
                    ProgressView().padding(.top, 50)
                } else if items.isEmpty {
                    Card { EmptyHint(icon: "tray", text: "队列为空 —— 去目录页创建抢购任务") }
                } else {
                    ForEach(items.indices, id: \.self) { i in queueCard(items[i]) }
                }
            }
            .padding(16)
        }
        .background(t.color(t.bg))
        .refreshable { await load() }
        .task { await load() }
        .onChange(of: conn.accountId) { _ in Task { await load() } }
        .sheet(item: Binding(
            get: { editItem.map { QueueItemWrap(item: $0) } },
            set: { editItem = $0?.item }
        )) { w in
            QueueIntervalSheet(item: w.item) {
                Task { await load() }
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(isPresented: $clearConfirm) {
            ConfirmSheet(title: "清空队列", message: "删除全部抢购任务。进行中的订单可能仍然成交,不会自动取消。", confirmText: "确认清空") {
                let (ok, msg) = await conn.client.actionDelete("/queue/clear")
                toast.show(ok ? "已清空" : (msg.isEmpty ? "失败" : msg), error: !ok)
                await load()
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
    }

    private struct QueueItemWrap: Identifiable {
        let item: [String: Any]
        var id: String { item["id"] as? String ?? UUID().uuidString }
    }

    private func queueCard(_ item: [String: Any]) -> some View {
        let status = (item["status"] as? String ?? "").lowercased()
        let color = status == "running" ? t.success : status == "paused" ? t.warning : status == "failed" ? t.danger : t.muted
        let id = item["id"] as? String ?? ""
        return Card(border: color) {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    HStack(spacing: 8) {
                        Dot(color: color)
                        Text(item["planCode"] as? String ?? "").font(.system(size: 12.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                    }
                    Spacer()
                    Text(["running": "运行中", "paused": "已暂停", "failed": "失败", "pending": "等待中", "completed": "已完成"][status] ?? status)
                        .font(.system(size: 10.5, weight: .semibold)).foregroundColor(t.color(color))
                }
                HStack(spacing: 6) {
                    Chip(text: (item["datacenter"] as? String ?? "").uppercased())
                    if let opts = item["options"] as? [String], !opts.isEmpty {
                        Chip(text: "\(opts.count) 项选配", color: t.info)
                    }
                    if item["autoPay"] as? Bool == true { Chip(text: "自动付款", color: t.danger) }
                    Spacer()
                }
                Button { editItem = item } label: {
                    Text("已重试 \(item["failureCount"] as? Int ?? 0) 次 · 间隔 \(item["retryInterval"] as? Int ?? 0)s(点此改)")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                }.buttonStyle(.plain)
                HStack(spacing: 8) {
                    if status == "running" || status == "paused" {
                        qBtn(icon: status == "paused" ? "play.fill" : "pause.fill", label: status == "paused" ? "恢复" : "暂停", color: t.muted) {
                            let body = try? JSONSerialization.data(withJSONObject: ["status": status == "paused" ? "running" : "paused"])
                            _ = await conn.client.actionPutData("/queue/\(id)/status", bodyData: body)
                            await load()
                        }
                    }
                    qBtn(icon: "trash", label: "删除", color: t.danger) {
                        _ = await conn.client.actionDelete("/queue/\(id)")
                        await load()
                    }
                }
            }
        }
    }

    private func qBtn(icon: String, label: String, color: String, action: @escaping () async -> Void) -> some View {
        Button { Task { await action() } } label: {
            HStack(spacing: 4) {
                Image(systemName: icon).font(.system(size: 10.5))
                Text(label).font(.system(size: 11))
            }
            .foregroundColor(t.color(color))
            .padding(.horizontal, 11).padding(.vertical, 6)
            .background(Capsule().stroke(t.color(color), lineWidth: 1))
        }
        .buttonStyle(.plain)
    }

    private func load() async {
        err = nil
        do { items = try await conn.client.getArray("/queue") }
        catch { err = error.localizedDescription }
        loading = false
    }
}

struct QueueIntervalSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let item: [String: Any]
    let onDone: () async -> Void
    var t: Tokens { theme.t }

    @State private var interval = 5
    @State private var busy = false

    var body: some View {
        VStack(spacing: 14) {
            SheetHeader(icon: "clock.arrow.circlepath", tint: t.info, title: "修改重试间隔")
            VStack(alignment: .leading, spacing: 12) {
                KV(k: "任务", v: item["planCode"] as? String ?? "—", mono: true)
                KV(k: "机房", v: (item["datacenter"] as? String ?? "").uppercased())
                VStack(alignment: .leading, spacing: 6) {
                    Text("间隔:\(interval) 秒").font(.system(size: 13, weight: .semibold)).foregroundColor(t.color(t.fg))
                    Slider(value: Binding(get: { Double(interval) }, set: { interval = Int($0) }), in: 1...3600, step: 1)
                        .tint(t.color(t.accent))
                }
                ActBtn(kind: .primary, icon: "checkmark", label: busy ? "保存中…" : "保存", busy: busy) {
                    await save()
                }
            }.padding(.horizontal, 16)
            Spacer()
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium])
        .onAppear { interval = item["retryInterval"] as? Int ?? 5 }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        let id = item["id"] as? String ?? ""
        let body = try? JSONSerialization.data(withJSONObject: ["retryInterval": interval])
        let (ok, msg) = await conn.client.actionPutData("/queue/\(id)/interval", bodyData: body)
        toast.show(ok ? "已保存" : (msg.isEmpty ? "失败" : msg), error: !ok)
        dismiss()
        if ok { await onDone() }
    }
}

// MARK: - 历史段

struct HistoryPane: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @AppStorage("ovh_mask_ip") private var mask = false
    var t: Tokens { theme.t }

    @State private var items: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true
    @State private var search = ""
    @State private var filter = 0
    @State private var busy = false
    @State private var clearConfirm = false

    var body: some View {
        ScrollView {
            VStack(spacing: 10) {
                HStack(spacing: 8) {
                    Image(systemName: "magnifyingglass").font(.system(size: 12)).foregroundColor(t.color(t.faint))
                    TextField("搜型号 / 机房 / 订单号", text: $search)
                        .font(.system(size: 12.5)).textInputAutocapitalization(.never).autocorrectionDisabled()
                        .foregroundColor(t.color(t.fg))
                }
                .padding(.horizontal, 12).padding(.vertical, 9)
                .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surface)))

                Picker("", selection: $filter) {
                    Text("全部").tag(0)
                    Text("成功").tag(1)
                    Text("失败").tag(2)
                }
                .pickerStyle(.segmented)

                HStack {
                    if busy { ProgressView().scaleEffect(0.7) }
                    Text("\(filtered.count) 条").font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                    Spacer()
                    Button { Task { await refreshStatus() } } label: {
                        HStack(spacing: 4) {
                            Image(systemName: "arrow.triangle.2.circlepath").font(.system(size: 10.5))
                            Text("刷新支付状态").font(.system(size: 11.5, weight: .semibold))
                        }.foregroundColor(t.color(t.info))
                    }.buttonStyle(.plain)
                    if !items.isEmpty {
                        Button { clearConfirm = true } label: {
                            Text("清空").font(.system(size: 11.5)).foregroundColor(t.color(t.danger))
                        }.buttonStyle(.plain)
                    }
                }

                if let e = err {
                    Card { LoadFailed(message: e) { Task { await load() } } }
                } else if loading {
                    ProgressView().padding(.top, 50)
                } else if filtered.isEmpty {
                    Card { EmptyHint(icon: "clock.arrow.circlepath", text: "没有历史订单") }
                } else {
                    ForEach(filtered.indices, id: \.self) { i in historyCard(filtered[i]) }
                }
            }
            .padding(16)
        }
        .background(t.color(t.bg))
        .refreshable { await load() }
        .task { await load() }
        .onChange(of: conn.accountId) { _ in Task { await load() } }
        .sheet(isPresented: $clearConfirm) {
            ConfirmSheet(title: "清空历史", message: "删除全部下单记录(不影响 OVH 订单本身)。", confirmText: "确认清空") {
                let (ok, msg) = await conn.client.actionDelete("/purchase-history")
                toast.show(ok ? "已清空" : (msg.isEmpty ? "失败" : msg), error: !ok)
                await load()
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
    }

    private var filtered: [[String: Any]] {
        var list = items
        if filter == 1 { list = list.filter { ($0["status"] as? String) == "success" } }
        if filter == 2 { list = list.filter { ($0["status"] as? String) == "failed" } }
        let q = search.trimmingCharacters(in: .whitespaces).lowercased()
        if !q.isEmpty {
            list = list.filter {
                (($0["planCode"] as? String ?? "") + ($0["datacenter"] as? String ?? "") +
                 ($0["orderId"] as? String ?? "")).lowercased().contains(q)
            }
        }
        return list
    }

    private func historyCard(_ it: [String: Any]) -> some View {
        // 后端 PurchaseHistoryEntry:status/orderStatus/expirationTime/errorMessage/
        // purchaseTime/totalMs/price{withoutTax,currencyCode}/retractionTime
        let success = (it["status"] as? String) != "failed"
        let payStatus = it["orderStatus"] as? String ?? ""
        let pay = paymentChip(payStatus, expiresAt: it["expirationTime"] as? String)
        let priceObj = it["price"] as? [String: Any]
        let price = numToDoubleAny(priceObj?["withoutTax"]) ?? 0
        let currency = priceObj?["currencyCode"] as? String ?? ""
        let totalMs = numToDoubleAny(it["totalMs"]) ?? 0
        return Card(border: success ? nil : t.danger) {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    HStack(spacing: 7) {
                        Image(systemName: success ? "checkmark.seal.fill" : "xmark.seal.fill")
                            .font(.system(size: 12)).foregroundColor(t.color(success ? t.success : t.danger))
                        Text(it["planCode"] as? String ?? "").font(.system(size: 12.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                    }
                    Spacer()
                    if totalMs > 0 {
                        Chip(text: totalMs >= 1000 ? String(format: "%.1fs", totalMs / 1000) : "\(Int(totalMs))ms")
                    }
                }
                HStack(spacing: 6) {
                    Chip(text: (it["datacenter"] as? String ?? "").uppercased())
                    if price > 0 {
                        Chip(text: String(format: "%.2f %@", price, currency), color: t.accent)
                    }
                    if let retr = it["retractionTime"] as? String, !retr.isEmpty {
                        Chip(text: "可撤单至 \(fmtDate(retr))", color: t.info)
                    }
                    Spacer()
                }
                if !pay.text.isEmpty {
                    HStack(spacing: 6) {
                        Image(systemName: pay.icon).font(.system(size: 10)).foregroundColor(t.color(pay.color))
                        Text(pay.text).font(.system(size: 10.5, weight: .semibold)).foregroundColor(t.color(pay.color))
                    }
                }
                if let e = it["errorMessage"] as? String, !e.isEmpty {
                    Text(e).font(.system(size: 10.5)).foregroundColor(t.color(t.danger)).lineLimit(3)
                }
                HStack {
                    Text(fmtDate(it["purchaseTime"] as? String)).font(.system(size: 10)).foregroundColor(t.color(t.faint))
                    Spacer()
                    if let url = it["orderUrl"] as? String, let u = URL(string: url),
                       success, (it["orderStatus"] as? String ?? "notPaid") == "notPaid" {
                        Link(destination: u) {
                            HStack(spacing: 3) {
                                Image(systemName: "safari").font(.system(size: 10))
                                Text("去付款").font(.system(size: 10.5, weight: .semibold))
                            }.foregroundColor(t.color(t.info))
                        }
                    }
                }
            }
        }
    }

    private func paymentChip(_ status: String, expiresAt: String?) -> (text: String, color: String, icon: String) {
        switch status.lowercased() {
        case "delivering": return ("交付中", t.info, "shippingbox.fill")
        case "delivered": return ("已交付", t.success, "checkmark.seal.fill")
        case "checking": return ("核验中", t.warning, "clock.fill")
        case "cancelling": return ("取消中", t.warning, "arrow.uturn.left")
        case "cancelled", "canceled": return ("已取消", t.muted, "xmark.circle")
        case "documentsrequested": return ("需补材料", t.danger, "doc.badge.ellipsis")
        default:
            if let exp = expiresAt, !exp.isEmpty {
                let f = DateFormatter()
                f.dateFormat = "yyyy-MM-dd'T'HH:mm:ssZZZZZ"
                if let d = f.date(from: exp.hasSuffix("Z") ? String(exp.dropLast()) + "+0000" : exp) {
                    let left = d.timeIntervalSince(Date())
                    if left < 0 { return ("付款已过期", t.muted, "clock.slash") }
                    let h = Int(left) / 3600
                    let text = h < 24 ? "付款倒计时 \(h) 小时" : "付款剩 \(h / 24) 天"
                    return (text, h < 24 ? t.danger : t.warning, "hourglass")
                }
            }
            return ("待付款", t.warning, "creditcard")
        }
    }

    private func refreshStatus() async {
        busy = true
        let (ok, msg) = await conn.client.actionPostData("/purchase-history/refresh-status", bodyData: nil)
        busy = false
        toast.show(ok ? "已向 OVH 查询" : (msg.isEmpty ? "查询失败" : msg), error: !ok)
        await load()
    }

    private func load() async {
        err = nil
        do { items = try await conn.client.getArray("/purchase-history") }
        catch { err = error.localizedDescription }
        loading = false
    }
}
