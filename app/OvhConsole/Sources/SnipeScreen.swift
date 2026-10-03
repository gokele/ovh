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

// MARK: - 目录数据源:优先直连 OVH 公开目录,失败回落自建后端

/// 子公司 → OVH 站点主机(与后端 ovh.SubsidiaryRegion/APIBaseURLForRegion 同表)
func catalogHost(_ sub: String) -> String {
    let s = sub.uppercased()
    let caSet: Set<String> = ["ASIA", "AU", "CA", "IN", "QC", "SG", "WE", "WS"]
    if s == "US" { return "api.us.ovhcloud.com" }
    if caSet.contains(s) { return "ca.api.ovh.com" }
    return "eu.api.ovh.com"
}

/// OVH 直连失败(被墙/超时)后的冷却:10 分钟内不再白等,直接走后端
private var directCatalogFailedAt: Date? = nil

/// 抢购页的机型配置/价格:直连 OVH 公开 eco 目录(与后端拉的是同一 URL,
/// 响应结构完全一致,解析零改动)。公开接口不带凭据,不占账户配额;
/// 直连失败(典型:国内网络被墙)自动回落自建后端 /catalog。
func fetchCatalogSmart(conn: Connection, sub: String) async -> [String: Any]? {
    let s = sub.uppercased()
    if let failedAt = directCatalogFailedAt, Date().timeIntervalSince(failedAt) < 600 {
        // 冷却期内:直接后端
        let path = s.isEmpty ? "/catalog" : "/catalog?subsidiary=\(urlEncode(s))"
        return try? await conn.client.getDict(path, timeoutSec: 45)
    }
    if !s.isEmpty {
        let url = "https://\(catalogHost(s))/v1/order/catalog/public/eco?ovhSubsidiary=\(urlEncode(s))"
        // 直连在独立线程跑 URLSession(阻塞式)
        let direct = await Task.detached(priority: .userInitiated) {
            ApiClient.httpsJSON(url, timeoutSec: 15)
        }.value
        if let d = direct, !(d["plans"] as? [[String: Any]] ?? []).isEmpty {
            return d
        }
        directCatalogFailedAt = Date()
    }
    let path = s.isEmpty ? "/catalog" : "/catalog?subsidiary=\(urlEncode(s))"
    return try? await conn.client.getDict(path, timeoutSec: 45)
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
    @State private var cacheAgeMin: Int? = nil
    @State private var cacheExpired = false
    @State private var err: String?
    @State private var forcing = false

    /// App 端目录缓存:与 web 的 React Query 同思路 —— 5 分钟内切回来直接用旧数据,
    /// 后台静默刷新(否则每次进抢购页都等 OVH 目录+价格,体感就是"加载好久")。
    private static let cacheTTL: TimeInterval = 300
    static var memCache: (at: Date, plans: [[String: Any]], avail: [String: [String: String]], prices: [String: Double], addons: [String: Double], currency: String, age: Int?, expired: Bool)? = nil
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

                HStack(spacing: 8) {
                    if let age = cacheAgeMin {
                        Chip(text: cacheExpired ? "目录缓存已过期(\(age)分前)" : "缓存 \(age) 分钟前",
                             color: cacheExpired ? t.warning : nil)
                    }
                    Spacer()
                    Button {
                        guard !forcing else { return }
                        forcing = true
                        Task {
                            // 对齐 web 的强刷三件套:先清后端内存缓存,逼它去 OVH 拿新数据;
                            // 只清 App 缓存的话,拉回来的还是后端那份旧缓存,看起来"点了没用"
                            let body = try? JSONSerialization.data(withJSONObject: ["type": "memory"])
                            _ = await conn.client.actionPostData("/cache/clear", bodyData: body)
                            Self.memCache = nil
                            await load(force: true)
                            forcing = false
                            toast.show(cacheExpired ? "已刷新(后端仍在用过期缓存)" : "目录已刷新")
                        }
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: "arrow.triangle.2.circlepath")
                                .font(.system(size: 10.5, weight: .semibold))
                                .rotationEffect(.degrees(forcing ? 360 : 0))
                                .animation(forcing ? .linear(duration: 0.8).repeatForever(autoreverses: false) : .default, value: forcing)
                            Text(forcing ? "刷新中…" : "强刷目录").font(.system(size: 11.5, weight: .semibold))
                        }.foregroundColor(t.color(t.info))
                        .padding(.horizontal, 10).padding(.vertical, 6)
                        .background(Capsule().stroke(t.color(t.info), lineWidth: 1))
                    }.buttonStyle(.plain).disabled(forcing)
                }

                if let e = err {
                    Card { LoadFailed(message: e) { Task { await load(force: true) } } }
                } else if loading && plans.isEmpty {
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
            .padding(.bottom, 60)   // 给底部 Tab 栏让位,避免最后一张卡的操作按钮被挡
        }
        .background(t.color(t.bg))
        .refreshable { await load(force: true) }
        .task { await load() }
        .onChange(of: conn.accountId) { _ in Self.memCache = nil; loading = true; Task { await load(force: true) } }
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
                        }.foregroundColor(.white)
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

    private func load(force: Bool = false) async {
        // 命中缓存且未过期:立即上屏,后台再刷新
        if !force, let c = Self.memCache, Date().timeIntervalSince(c.at) < Self.cacheTTL, !plans.isEmpty {
            return
        }
        if let c = Self.memCache, !c.plans.isEmpty, plans.isEmpty {
            applyCache(c)   // 先显示旧数据,不等网络
        }
        err = nil
        do {
            // servers(后端内存缓存,快)与 catalog(直连 OVH / 失败回落后端)并行拉
            let sub = sub2
            async let catalogTask: [String: Any]? = fetchCatalogSmart(conn: conn, sub: sub)
            let sr = try await conn.client.getDict("/servers")
            let cr = try await catalogTask
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
            var addonMap: [String: Double] = [:]
            if let cr = cr, let cplans = cr["plans"] as? [[String: Any]] {
                for p in cplans {
                    if let code = p["planCode"] as? String {
                        priceMap[code] = monthlyPriceOf(p["pricings"] as? [[String: Any]])
                    }
                }
                for a in (cr["addons"] as? [[String: Any]]) ?? [] {
                    if let c = a["planCode"] as? String {
                        addonMap[c] = monthlyPriceOf(a["pricings"] as? [[String: Any]])
                    }
                }
                priceCurrency = (cr["locale"] as? [String: Any])?["currencyCode"] as? String ?? ""
            }
            // 缓存龄(后端 /servers 自带 cacheInfo)
            if let ci = sr["cacheInfo"] as? [String: Any] {
                cacheAgeMin = numToDoubleAny(ci["cacheAgeMinutes"]).map(Int.init)
                cacheExpired = (ci["usingExpiredCache"] as? Bool ?? false) || (ci["cached"] as? Bool == false)
            }
            Self.memCache = (Date(), plans, availability, priceMap, addonMap, priceCurrency, cacheAgeMin, cacheExpired)
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func applyCache(_ c: (at: Date, plans: [[String: Any]], avail: [String: [String: String]], prices: [String: Double], addons: [String: Double], currency: String, age: Int?, expired: Bool)) {
        plans = c.plans; availability = c.avail; priceMap = c.prices
        priceCurrency = c.currency; cacheAgeMin = c.age; cacheExpired = c.expired
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

    /// 配置分组(对齐 web:按 option.family 分类,中文组名,排除许可证等非硬件项)
    @State private var groups: [(key: String, items: [(value: String, label: String, price: Double, isDefault: Bool)])] = []
    @State private var pickedByGroup: [String: String] = [:]
    @State private var addonPrices: [String: Double] = [:]
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
                        // 价格 hero:大数字 + 构成分段着色 + 渐变强调
                        VStack(alignment: .leading, spacing: 6) {
                            HStack(alignment: .firstTextBaseline, spacing: 5) {
                                Text(String(format: "%.2f", totalPrice))
                                    .font(.system(size: 32, weight: .heavy, design: .rounded))
                                    .foregroundColor(t.color(t.fg))
                                Text(currency.isEmpty ? "/月" : "\(currency) /月")
                                    .font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.muted))
                                Spacer()
                                if !pickedDCs.isEmpty {
                                    Chip(text: "\(pickedDCs.count * qty) 个任务", color: t.warning)
                                }
                            }
                            HStack(spacing: 12) {
                                Text("基础 ").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                                Text(String(format: "%.2f", basePrice)).font(.system(size: 10.5, weight: .semibold, design: .rounded)).foregroundColor(t.color(t.fg))
                                Text("· 选配 ").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                                Text(String(format: "+%.2f", totalPrice - basePrice)).font(.system(size: 10.5, weight: .semibold, design: .rounded)).foregroundColor(t.color(t.accent))
                                Spacer()
                                Text("下单账户:\(conn.activeAccount?["name"] as? String ?? "—")")
                                    .font(.system(size: 10)).foregroundColor(t.color(t.faint))
                            }
                        }
                        .padding(14)
                        .background(RoundedRectangle(cornerRadius: 15).fill(t.color(t.accent).opacity(0.08)))
                        .overlay(RoundedRectangle(cornerRadius: 15).stroke(t.color(t.accent).opacity(0.22), lineWidth: 1))

                        // 配置组:每组一张圆角容器卡,选中 chip 绿描边 + 对勾
                        if groups.isEmpty {
                            SheetNote(text: "目录里没有该机型的选配项,按默认配置下单。", tint: t.muted)
                        } else {
                            ForEach(groups.indices, id: \.self) { gi in
                                let g = groups[gi]
                                VStack(alignment: .leading, spacing: 9) {
                                    HStack(spacing: 5) {
                                        Image(systemName: groupIcon(g.key)).font(.system(size: 10, weight: .semibold)).foregroundColor(t.color(t.accent))
                                        Text(g.key).font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.muted))
                                    }
                                    FlowLayout(spacing: 7) {
                                        ForEach(g.items, id: \.value) { item in
                                            optionChip(item)
                                        }
                                    }
                                }
                                .padding(11)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.surfaceMuted).opacity(0.55)))
                            }
                        }

                        // 机房:双行 chip(代码 + 中文交付短语),选中绿实心
                        VStack(alignment: .leading, spacing: 9) {
                            HStack(spacing: 6) {
                                Image(systemName: "map").font(.system(size: 10, weight: .semibold)).foregroundColor(t.color(t.accent))
                                Text("数据中心").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.muted))
                                Spacer()
                                if dcs.contains(where: { isOrderable((availability[planCode]?[$0]) ?? "") }) {
                                    miniAction("选有货", color: t.accent) { pickAllAvailable() }
                                }
                                if !pickedDCs.isEmpty {
                                    miniAction("清空", color: t.faint) { pickedDCs.removeAll() }
                                }
                            }
                            FlowLayout(spacing: 7) {
                                ForEach(dcs, id: \.self) { dc in
                                    dcChip(dc)
                                }
                            }
                        }
                        .padding(11)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.surfaceMuted).opacity(0.55)))

                        // 数量与间隔:自绘加减器(原生 Stepper 与深色风格割裂)
                        HStack(spacing: 10) {
                            stepCard("每机房数量", value: "\(qty)", onMinus: { if qty > 1 { qty -= 1 } }, onPlus: { if qty < 10 { qty += 1 } })
                            stepCard("重试间隔", value: "\(interval) 秒", onMinus: { if interval > 1 { interval -= 1 } }, onPlus: { if interval < 3600 { interval += 1 } })
                        }

                        Toggle(isOn: $autoPay) {
                            VStack(alignment: .leading, spacing: 1) {
                                Text("抢到后自动付款").font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                                Text("自动付款有真实扣款风险,默认关闭").font(.system(size: 10)).foregroundColor(t.color(t.muted))
                            }
                        }.tint(t.color(t.accent))

                        Text("将创建 \(pickedDCs.count * qty) 个任务").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.warning))

                        HStack(spacing: 10) {
                            Button {
                                Task { await addMonitorWithConfig() }
                            } label: {
                                VStack(spacing: 3) {
                                    Image(systemName: "eye").font(.system(size: 15, weight: .semibold))
                                    Text("加入监控").font(.system(size: 10.5, weight: .semibold))
                                }
                                .foregroundColor(t.color(t.muted))
                                .frame(width: 86, height: 58)
                                .background(RoundedRectangle(cornerRadius: 14).stroke(t.color(t.border), lineWidth: 1))
                            }.buttonStyle(.plain)
                            ActBtn(kind: .primary, icon: "bolt.fill", label: busy ? "创建中…" : "创建抢购任务(\(pickedDCs.count * qty))", busy: busy) {
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

    private func miniAction(_ label: String, color: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label).font(.system(size: 10.5, weight: .semibold)).foregroundColor(t.color(color))
                .padding(.horizontal, 9).padding(.vertical, 5)
                .background(Capsule().stroke(t.color(color).opacity(0.5), lineWidth: 1))
        }.buttonStyle(.plain)
    }

    /// OVH 可用性枚举 → 中文短语
    static func availText(_ raw: String) -> String {
        let r = raw.lowercased()
        if r == "comingsoon" { return "即将开卖" }
        if r == "unavailable" || r == "unknown" || r.isEmpty { return "无货" }
        if let m = r.range(of: #"^(\d+)h(-high|-low)?$"#, options: .regularExpression) {
            let hours = String(r[m]).prefix(while: \Character.isNumber)
            let h = Int(hours) ?? 0
            let suffix = r.contains("-high") ? "充足" : (r.contains("-low") ? "紧张" : "")
            if h < 24 { return h == 0 ? "现货" : "\(h)小时 交付 \(suffix)".trimmingCharacters(in: .whitespaces) }
            if h < 168 { return "\(h / 24)天 交付 \(suffix)".trimmingCharacters(in: .whitespaces) }
            return "\(h / 24)天 \(suffix)".trimmingCharacters(in: .whitespaces)
        }
        return raw
    }

    private func dcChip(_ dc: String) -> some View {
        let status = availability[planCode]?[dc] ?? ""
        let inStock = isOrderable(status)
        let on = pickedDCs.contains(dc)
        return Button {
            withAnimation(.easeOut(duration: 0.15)) { toggleDC(dc) }
        } label: {
            VStack(spacing: 1.5) {
                HStack(spacing: 4) {
                    if inStock {
                        Circle().fill(on ? Color.white : t.color(t.success)).frame(width: 5, height: 5)
                    }
                    Text(dc.uppercased())
                        .font(.system(size: 11, weight: .bold, design: .rounded))
                        .foregroundColor(on ? .white : (inStock ? t.color(t.fg) : t.color(t.faint)))
                }
                Text(Self.availText(status))
                    .font(.system(size: 8.5, weight: .medium))
                    .foregroundColor(on ? Color.white.opacity(0.85) : t.color(inStock ? t.success : t.faint))
            }
            .padding(.horizontal, 10).padding(.vertical, 6)
            .frame(minWidth: 64)
            .background(RoundedRectangle(cornerRadius: 10).fill(on ? t.color(t.accent) : (inStock ? t.color(t.success).opacity(0.09) : t.color(t.bg).opacity(0.45))))
            .overlay(RoundedRectangle(cornerRadius: 10).stroke(on ? t.color(t.accent) : (inStock ? t.color(t.success).opacity(0.25) : t.color(t.border).opacity(0.4)), lineWidth: 1))
        }
        .buttonStyle(.plain)
    }

    /// 自绘加减器(替代原生 Stepper)
    private func stepCard(_ title: String, value: String, onMinus: @escaping () -> Void, onPlus: @escaping () -> Void) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(title).font(.system(size: 10.5, weight: .semibold)).foregroundColor(t.color(t.muted))
            HStack(spacing: 0) {
                stepBtn("minus", action: onMinus)
                Spacer(minLength: 4)
                Text(value).font(.system(size: 13, weight: .bold, design: .rounded)).foregroundColor(t.color(t.fg))
                    .frame(minWidth: 40)
                Spacer(minLength: 4)
                stepBtn("plus", action: onPlus)
            }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surfaceMuted).opacity(0.55)))
    }

    private func stepBtn(_ icon: String, action: @escaping () -> Void) -> some View {
        Button {
            withAnimation(.easeOut(duration: 0.12)) { action() }
        } label: {
            Image(systemName: icon).font(.system(size: 11, weight: .semibold))
                .foregroundColor(t.color(t.fg))
                .frame(width: 34, height: 34)
                .background(Circle().fill(t.color(t.bg).opacity(0.5)))
        }.buttonStyle(.plain)
    }

    private var totalPrice: Double {
        basePrice + pickedByGroup.values.reduce(0) { $0 + (addonPrices[$1] ?? 0) }
    }
    private var selected: [String] { Array(pickedByGroup.values) }

    private func optionChip(_ item: (value: String, label: String, price: Double, isDefault: Bool)) -> some View {
        let on = pickedByGroup.values.contains(item.value)
        return Button {
            if let g = groups.first(where: { grp in grp.items.contains(where: { $0.value == item.value }) }) {
                withAnimation(.easeOut(duration: 0.15)) { pickedByGroup[g.key] = item.value }
            }
        } label: {
            HStack(spacing: 5) {
                if on {
                    Image(systemName: "checkmark").font(.system(size: 8.5, weight: .bold)).foregroundColor(t.color(t.accent))
                }
                Text(item.label)
                    .font(.system(size: 11, weight: on ? .semibold : .regular))
                    .foregroundColor(t.color(on ? t.fg : t.muted))
                    .lineLimit(1)
                if item.price > 0 {
                    Text(String(format: "+%.0f", item.price)).font(.system(size: 9.5, design: .rounded)).foregroundColor(t.color(on ? t.accent : t.faint))
                }
                if item.isDefault && !on {
                    Text("默认").font(.system(size: 8, weight: .bold))
                        .foregroundColor(t.color(t.faint))
                        .padding(.horizontal, 4).padding(.vertical, 1.5)
                        .background(RoundedRectangle(cornerRadius: 4).fill(t.color(t.fg).opacity(0.08)))
                }
            }
            .padding(.horizontal, 10).padding(.vertical, 7)
            .background(RoundedRectangle(cornerRadius: 9).fill(on ? t.color(t.accent).opacity(0.13) : t.color(t.bg).opacity(0.45)))
            .overlay(RoundedRectangle(cornerRadius: 9).stroke(on ? t.color(t.accent).opacity(0.55) : t.color(t.border).opacity(0.4), lineWidth: 1))
        }
        .buttonStyle(.plain)
    }

    /// 配置组图标
    private func groupIcon(_ key: String) -> String {
        switch key {
        case "CPU / 处理器": return "cpu"
        case "内存": return "memorychip"
        case "系统盘", "存储 / 数据盘": return "internaldrive"
        case "带宽 / 网络": return "speedometer"
        case "vRack 内网": return "network"
        default: return "shippingbox"
        }
    }

    /// option.family → 中文组名;非硬件(许可证/系统/面板)返回 nil 排除(对齐 web isHardwareOption)
    static func optionGroup(of option: [String: Any]) -> String? {
        let family = ((option["family"] as? String) ?? "").lowercased()
        let value = ((option["value"] as? String) ?? (option["label"] as? String) ?? "").lowercased()
        let label = (option["label"] as? String) ?? ""
        // 排除许可证/OS/面板类
        let exclude = ["windows-server", "sql-server", "cpanel-license", "plesk-", "-license-", "control-panel", "panel", "security", "antivirus", "firewall"]
        if value.hasPrefix("os-") || exclude.contains(where: { value.contains($0) }) || label.lowercased().contains("license") { return nil }
        if family.contains("system-storage") { return "系统盘" }
        if family.contains("memory") || family.contains("ram") { return "内存" }
        if family.contains("storage") { return "存储 / 数据盘" }
        if family.contains("bandwidth") || family.contains("traffic") { return "带宽 / 网络" }
        if family.contains("vrack") { return "vRack 内网" }
        if value.contains("ram-") { return "内存" }
        if value.contains("cpu") || value.contains("processor") { return "CPU / 处理器" }
        if value.contains("vrack") { return "vRack 内网" }
        return "其他"
    }

    /// 选项 code → 人类可读短名(去机型尾缀与冗余前缀)
    static func prettyOption(_ raw: String) -> String {
        var s = raw
        if let r = s.range(of: #"-\d{2}[a-z]{2,6}-v\d+$"#, options: .regularExpression) { s = String(s[..<r.lowerBound]) }
        if let r = s.range(of: #"\d{3,}[a-z]+-v\d+$"#, options: .regularExpression) { s = String(s[..<r.lowerBound]) }
        for (from, to) in [("ram-", ""), ("on-die-ecc-", "ECC "), ("softraid-", "软RAID "), ("hybridsoftraid-", "混合RAID "), ("bandwidth-", ""), ("vrack-bandwidth-", "vRack ")] {
            if s.hasPrefix(from) { s = to + String(s.dropFirst(from.count)); break }
        }
        return s.isEmpty ? raw : s
    }

    private func toggleDC(_ dc: String) {
        if pickedDCs.contains(dc) { pickedDCs.remove(dc) } else { pickedDCs.insert(dc) }
    }

    private func pickAllAvailable() {
        pickedDCs = Set(dcs.filter { isOrderable((availability[planCode]?[$0]) ?? "") })
    }

    private func load() async {
        defer { loading = false }
        // 机房(可用性 map)
        dcs = (availability[planCode] ?? [:]).keys.sorted()

        // 目录数据:优先复用目录页的缓存(12MB 的目录不该每开一次下单就重拉一遍)
        var r: [String: Any]? = nil
        if let c = CatalogPane.memCache, !c.prices.isEmpty {
            // 缓存:prices = 各机型基础价;addons = 各选配增量价(ServerPlan 无 pricings 字段,
            // 基础价必须取缓存价表而不是从 plan 里算 —— 之前就是这里取错来源显示 0.00)
            basePrice = c.prices[planCode] ?? 0
            addonPrices = c.addons
            currency = c.currency
            if let p = (c.plans as [[String: Any]]).first(where: { ($0["planCode"] as? String) == planCode }) {
                buildGroups(from: p)
            }
            return
        }
        var sub = ""
        if let acc = conn.activeAccount, let z = acc["zone"] as? String, !z.isEmpty { sub = z }
        guard let resp = await fetchCatalogSmart(conn: conn, sub: sub) else { return }
        r = resp
        currency = (resp["locale"] as? [String: Any])?["currencyCode"] as? String ?? ""
        let cplans = (resp["plans"] as? [[String: Any]]) ?? []
        let caddons = (resp["addons"] as? [[String: Any]]) ?? []
        for a in caddons {
            if let c = a["planCode"] as? String {
                addonPrices[c] = monthlyPrice(a["pricings"] as? [[String: Any]])
            }
        }
        if let p = cplans.first(where: { ($0["planCode"] as? String) == planCode }) {
            basePrice = monthlyPrice(p["pricings"] as? [[String: Any]])
            buildGroups(from: p)
        }
    }

    /// defaultOptions + availableOptions 合并 → family 中文分组 → 默认项预选
    private func buildGroups(from p: [String: Any]) {
        if let pr = p["pricings"] as? [[String: Any]] {
            basePrice = max(basePrice, monthlyPrice(pr))   // catalog 原始 plan 才有 pricings
        }
        let defaults = (p["defaultOptions"] as? [[String: Any]]) ?? []
        let defaultValues = Set(defaults.compactMap { $0["value"] as? String })
        var all = defaults
        for o in (p["availableOptions"] as? [[String: Any]]) ?? [] {
            if let v = o["value"] as? String, !defaultValues.contains(v) { all.append(o) }
        }
        var byGroup: [String: [(value: String, label: String, price: Double, isDefault: Bool)]] = [:]
        var order: [String] = []
        for o in all {
            guard let key = Self.optionGroup(of: o),
                  let v = o["value"] as? String else { continue }
            let item = (value: v, label: Self.prettyOption(v), price: addonPrices[v] ?? 0, isDefault: defaultValues.contains(v))
            if byGroup[key] == nil { order.append(key) }
            if !(byGroup[key]?.contains(where: { $0.value == v }) ?? false) {
                byGroup[key, default: []].append(item)
            }
            if defaultValues.contains(v) { pickedByGroup[key] = v }   // 默认预选
        }
        let groupOrder = ["CPU / 处理器", "内存", "系统盘", "存储 / 数据盘", "带宽 / 网络", "vRack 内网", "其他"]
        groups = byGroup.keys.sorted { a, b in
            (groupOrder.firstIndex(of: a) ?? 99) < (groupOrder.firstIndex(of: b) ?? 99)
        }.map { key in (key: key, items: byGroup[key] ?? []) }
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
    @State private var selected: Set<String> = []
    @State private var selecting = false

    var body: some View {
        ScrollView {
            VStack(spacing: 10) {
                HStack {
                    Text("\(items.count) 个任务").font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                    Spacer()
                    if !items.isEmpty {
                        if selecting {
                            Button { selected = selected.count == items.count ? [] : Set(items.compactMap { $0["id"] as? String }) } label: {
                                Text(selected.count == items.count ? "取消全选" : "全选").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.info))
                            }.buttonStyle(.plain)
                        }
                        Button { selecting.toggle(); selected.removeAll() } label: {
                            Text(selecting ? "完成" : "批量").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(selecting ? t.accent : t.muted))
                        }.buttonStyle(.plain)
                        if !selecting {
                            Button { clearConfirm = true } label: {
                                Text("清空队列").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.danger))
                            }.buttonStyle(.plain)
                        }
                    }
                }
                if selecting && !selected.isEmpty {
                    HStack(spacing: 8) {
                        qBtn(icon: "pause.fill", label: "暂停 \(selected.count)", color: t.muted) {
                            await batch("paused")
                        }
                        qBtn(icon: "play.fill", label: "恢复 \(selected.count)", color: t.accent) {
                            await batch("running")
                        }
                        qBtn(icon: "trash", label: "删除 \(selected.count)", color: t.danger) {
                            await batchDelete()
                        }
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
        return Card(border: selected.contains(id) ? t.accent : color) {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    if selecting {
                        Button {
                            if selected.contains(id) { selected.remove(id) } else { selected.insert(id) }
                        } label: {
                            Image(systemName: selected.contains(id) ? "checkmark.circle.fill" : "circle")
                                .font(.system(size: 17)).foregroundColor(t.color(selected.contains(id) ? t.accent : t.faint))
                        }.buttonStyle(.plain)
                    }
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

    private func batch(_ status: String) async {
        for id in selected {
            let body = try? JSONSerialization.data(withJSONObject: ["status": status])
            _ = await conn.client.actionPutData("/queue/\(id)/status", bodyData: body)
        }
        selected.removeAll()
        await load()
    }

    private func batchDelete() async {
        for id in selected {
            _ = await conn.client.actionDelete("/queue/\(id)")
        }
        selected.removeAll()
        await load()
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
