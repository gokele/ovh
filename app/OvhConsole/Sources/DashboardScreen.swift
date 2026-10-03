import SwiftUI

/**
 * 总览(仪表盘):KPI、活跃队列、系统状态、系统资源三圆环。
 * 对齐 web 首页;每块独立失败态(某块挂了不拖垮整页)。
 */
struct DashboardScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var nav: AppNav
    @EnvironmentObject var toast: Toast
    var t: Tokens { theme.t }

    @State private var stats: [String: Any]?
    @State private var statsErr: String?
    @State private var queue: [[String: Any]] = []
    @State private var metrics: [String: Any]?
    @State private var version: [String: Any]?
    @State private var refreshAt = Date.distantPast
    @State private var showAccountPicker = false

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 12) {
                    accountBanner
                    kpiRow
                    activeQueueCard
                    systemCard
                    if metrics != nil { resourceCard }
                }
                .padding(16)
            }
            .background(t.color(t.bg))
            .navigationTitle("总览")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarColorScheme(theme.dark ? .dark : .light, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    AccountButton { showAccountPicker = true }
                }
            }
            .refreshable { await load(force: true) }
            .onChange(of: conn.accountId) { _ in Task { await load(force: true) } }
            .sheet(isPresented: $showAccountPicker) {
                AccountPickerSheet()
                    .environmentObject(theme).environmentObject(conn).environmentObject(toast)
            }
        }
        .task { await load() }
    }

    // MARK: 账户横幅(当前账户 + 后端版本)

    private var accountBanner: some View {
        HStack(spacing: 8) {
            if let acc = conn.activeAccount {
                Dot(color: (acc["valid"] as? Bool ?? false) ? t.success : t.danger)
                Text(acc["name"] as? String ?? "").font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                if zoneBadgeVisible(name: acc["name"] as? String ?? "", zone: acc["zone"] as? String ?? "") {
                    Chip(text: acc["zone"] as? String ?? "")
                }
                if let ver = version?["version"] as? String {
                    Chip(text: "v\(ver)", color: t.info, mono: true)
                }
            } else {
                Image(systemName: "person.crop.circle.badge.exclamationmark").font(.system(size: 13)).foregroundColor(t.color(t.warning))
                Text("没有 OVH 账户 —— 去网页端设置添加").font(.system(size: 12)).foregroundColor(t.color(t.muted))
            }
            Spacer()
        }
        .padding(.horizontal, 4)
    }

    // MARK: KPI

    private var kpiRow: some View {
        HStack(spacing: 10) {
            Button { nav.tab = .snipe; nav.snipeSegment = 1 } label: {
                StatTile(icon: "bolt.fill", label: "活跃队列", value: stats?["activeQueues"].intText ?? "—", tint: t.warning)
            }.buttonStyle(.plain)
            Button { nav.tab = .snipe; nav.snipeSegment = 0 } label: {
                StatTile(icon: "server.rack", label: "目录机型", value: stats?["totalServers"].intText ?? "—",
                         tint: (stats?["availableServers"] as? Int ?? 0) > 0 ? t.success : nil)
            }.buttonStyle(.plain)
            Button { nav.tab = .snipe; nav.snipeSegment = 2 } label: {
                StatTile(icon: "checkmark.seal.fill", label: "下单成功", value: stats?["purchaseSuccess"].intText ?? "—", tint: t.success)
            }.buttonStyle(.plain)
        }
    }

    // MARK: 活跃队列

    private var activeQueueCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 10) {
                HStack {
                    SectionTitle(text: "活跃队列")
                    Spacer()
                    Text("\(activeItems.count) 条").font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                }
                if statsErr != nil && queue.isEmpty {
                    LoadFailed(message: statsErr ?? "") { Task { await load(force: true) } }
                } else if activeItems.isEmpty {
                    EmptyHint(icon: "tray", text: "没有运行中的抢购任务")
                } else {
                    ForEach(Array(activeItems.prefix(4).enumerated()), id: \.offset) { _, item in
                        HStack(spacing: 8) {
                            Dot(color: statusColor(item["status"] as? String ?? ""))
                            Text(item["planCode"] as? String ?? "")
                                .font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                            Chip(text: (item["datacenter"] as? String ?? "").uppercased())
                            Spacer()
                            Text("第 \((item["failureCount"] as? Int ?? 0) + 1) 轮")
                                .font(.system(size: 10)).foregroundColor(t.color(t.muted))
                        }
                    }
                    if activeItems.count > 4 {
                        Text("还有 \(activeItems.count - 4) 条…")
                            .font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }
        }
        .onTapGesture { nav.tab = .snipe; nav.snipeSegment = 1 }
    }

    private var activeItems: [[String: Any]] {
        queue.filter {
            ["running", "pending", "paused"].contains(($0["status"] as? String ?? "").lowercased())
        }
    }

    private func statusColor(_ s: String) -> String {
        switch s.lowercased() {
        case "running": return t.success
        case "paused": return t.warning
        case "failed": return t.danger
        default: return t.muted
        }
    }

    // MARK: 系统状态

    private var systemCard: some View {
        Card {
            VStack(spacing: 9) {
                SectionTitle(text: "系统状态")
                sysRow(icon: "arrow.triangle.2.circlepath", name: "OVH API 连接", ok: true)
                sysRow(icon: "bolt.badge.clock", name: "自动抢购引擎", ok: stats?["queueProcessorRunning"] as? Bool ?? false)
                sysRow(icon: "dot.radiowaves.left.and.right", name: "补货监控", ok: stats?["monitorRunning"] as? Bool ?? false)
                if let host = metrics?["host"] as? [String: Any],
                   let up = host["uptimeSec"] as? Double, up > 0 {
                    KV(k: "后端已运行", v: uptimeText(up))
                }
            }
        }
    }

    private func sysRow(icon: String, name: String, ok: Bool) -> some View {
        HStack(spacing: 9) {
            Image(systemName: icon).font(.system(size: 13)).foregroundColor(t.color(ok ? t.success : t.faint))
            Text(name).font(.system(size: 12.5)).foregroundColor(t.color(t.fg))
            Spacer()
            Text(ok ? "正常" : "停止").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(ok ? t.success : t.faint))
        }
    }

    // MARK: 资源三圆环

    private var resourceCard: some View {
        Card {
            VStack(spacing: 12) {
                SectionTitle(text: "后端资源")
                HStack(spacing: 6) {
                    Ring(label: "CPU", sub: "\(cpuCores) 核", pct: cpuPct)
                    Ring(label: "内存", sub: fmtBytes(memUsed) + " / " + fmtBytes(memTotal), pct: memPct)
                    Ring(label: "磁盘", sub: fmtBytes(diskUsed) + " / " + fmtBytes(diskTotal), pct: diskPct)
                }
                if let host = metrics?["host"] as? [String: Any] {
                    KV(k: "主机", v: "\(host["hostname"] as? String ?? "") · \(host["platform"] as? String ?? "")")
                }
            }
        }
    }

    private var cpuPct: Double { (metrics?["cpu"] as? [String: Any])?["percent"] as? Double ?? 0 }
    private var cpuCores: Int { (metrics?["cpu"] as? [String: Any])?["cores"] as? Int ?? 0 }
    private var memPct: Double { (metrics?["memory"] as? [String: Any])?["percent"] as? Double ?? 0 }
    private var memUsed: Double { (metrics?["memory"] as? [String: Any])?["usedBytes"] as? Double ?? 0 }
    private var memTotal: Double { (metrics?["memory"] as? [String: Any])?["totalBytes"] as? Double ?? 0 }
    private var diskPct: Double { (metrics?["disk"] as? [String: Any])?["percent"] as? Double ?? 0 }
    private var diskUsed: Double { (metrics?["disk"] as? [String: Any])?["usedBytes"] as? Double ?? 0 }
    private var diskTotal: Double { (metrics?["disk"] as? [String: Any])?["totalBytes"] as? Double ?? 0 }

    private func uptimeText(_ sec: Double) -> String {
        let d = Int(sec) / 86400, h = (Int(sec) % 86400) / 3600, m = (Int(sec) % 3600) / 60
        return d > 0 ? "\(d) 天 \(h) 小时" : (h > 0 ? "\(h) 小时 \(m) 分" : "\(m) 分钟")
    }

    // MARK: 加载

    private func load(force: Bool = false) async {
        if force || Date().timeIntervalSince(refreshAt) > 15 { refreshAt = Date() }
        do {
            async let s = conn.client.getDict("/stats")
            async let q = conn.client.getArray("/queue")
            async let m = conn.client.getDict("/system/metrics")
            async let v = conn.client.getDict("/version")
            let (sr, qr, mr, vr) = try await (s, q, m, v)
            stats = sr; queue = qr; metrics = mr; version = vr
            statsErr = nil
        } catch {
            statsErr = error.localizedDescription
        }
        if conn.accounts.isEmpty { await conn.loadAccounts() }
    }
}

/// 字典取 Int 的 KPI 文本("—" 兜底)
private extension Optional where Wrapped == Any {
    var intText: String? {
        flatMap { v in
            if let i = v as? Int { return "\(i)" }
            if let d = v as? Double { return Int(d).description }
            if let s = v as? String { return s }
            return nil
        }
    }
}
