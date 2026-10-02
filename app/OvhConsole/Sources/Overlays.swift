import SwiftUI

/**
 * 菜单三个次要页(覆盖层,「完成」返回机器列表):
 * 雷达(补货可用性)/ 队列(任务)/ 设置与账户。
 * 服务器控制是主体,这些是"点开查看"的入口 —— 按用户定的信息架构。
 */

// MARK: - 雷达

struct RadarOverlay: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let onClose: () -> Void

    @State private var plans: [[String: Any]] = []
    @State private var availability: [String: [String: String]] = [:]
    @State private var err: String?
    @State private var loading = true

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            HStack(alignment: .bottom) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("雷达").font(.system(size: 24, weight: .bold)).foregroundColor(t.color(t.fg))
                    Text("机型 × 机房可用性").font(.system(size: 11)).foregroundColor(t.color(t.muted))
                }
                Spacer()
            }
            .padding(.horizontal, 16).padding(.vertical, 10)
            ScrollView {
                LazyVStack(spacing: 10) {
                    if let e = err {
                        ErrorCard(message: e, t: t)
                    } else if loading {
                        ProgressView().padding(.top, 40)
                    } else if plans.isEmpty {
                        EmptyCard(t: t, text: "目录为空 —— 检查账户与后端缓存")
                    } else {
                        // 有货的排前面(和 RN 版同排序策略)
                        ForEach(sortedPlans.indices, id: \.self) { i in
                            radarCard(sortedPlans[i])
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

    /// 该机型任一机房是否可下单(白名单:\d+H,对齐后端 IsAvailableForOrder)
    private func hasStock(_ plan: [String: Any]) -> Bool {
        let code = plan["planCode"] as? String ?? ""
        let dcs = availability[code] ?? [:]
        return dcs.values.contains { isOrderable($0) }
    }

    private var sortedPlans: [[String: Any]] {
        plans.sorted { hasStock($0) && !hasStock($1) }
    }

    private func radarCard(_ plan: [String: Any]) -> some View {
        let code = plan["planCode"] as? String ?? ""
        let dcs = availability[code] ?? [:]
        let stock = hasStock(plan)
        // 排序稳定的机房键(可用性 map 的键排序)
        let dcKeys = dcs.keys.sorted()
        return VStack(spacing: 9) {
            HStack {
                HStack(spacing: 6) {
                    if stock {
                        Image(systemName: "star.fill").font(.system(size: 11)).foregroundColor(t.color(t.warning))
                    }
                    Text(code).font(.system(size: 12.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                }
                Spacer()
                Text(stock ? "有货" : "无货")
                    .font(.system(size: 10.5, weight: .semibold))
                    .foregroundColor(t.color(stock ? t.success : t.faint))
            }
            if !dcKeys.isEmpty {
                // 机房格:原始可用性枚举(1H-low 这类),红绿只认白名单
                FlowLayout(spacing: 6) {
                    ForEach(dcKeys, id: \.self) { dc in
                        let status = dcs[dc] ?? ""
                        let ok = isOrderable(status)
                        Text("\(dc.uppercased()) \(status)")
                            .font(.system(size: 10, design: .monospaced))
                            .foregroundColor(t.color(ok ? t.success : t.faint))
                            .padding(.horizontal, 8).padding(.vertical, 4)
                            .background(RoundedRectangle(cornerRadius: 8).fill(ok ? t.color(t.success).opacity(0.08) : Color.clear).overlay(RoundedRectangle(cornerRadius: 8).stroke(t.color(ok ? t.success : t.border), lineWidth: 1)))
                    }
                }
            }
            if let mem = plan["memory"] as? String, let storage = plan["storage"] as? String, !mem.isEmpty {
                Text("\(mem) / \(storage)").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(13)
        .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(t.border), lineWidth: 1)))
    }

    private func load() async {
        err = nil
        do {
            let c = conn.client
            async let p = c.getDict("/servers")
            async let a = c.getDict("/availability")
            let (pr, ar) = try await (p, a)
            plans = (pr["servers"] as? [[String: Any]]) ?? []
            // availability: { planCode: { dc: status } }
            if let avail = ar["availability"] as? [String: [String: String]] {
                availability = avail
            }
        } catch { err = error.localizedDescription }
        loading = false
    }
}

/// 可用性白名单(对齐 core/availability 与后端 IsAvailableForOrder):
/// 只有 \d+H(-high|-low) 是"多久能交付"的承诺;comingSoon/unknown 都下不了单
private func isOrderable(_ s: String) -> Bool {
    let pattern = "^\\d+H(-high|-low)?$"
    return s.range(of: pattern, options: .regularExpression) != nil
}

// MARK: - 队列

struct QueueOverlay: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let onClose: () -> Void

    @State private var items: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true
    @State private var showCreate = false

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            HStack(alignment: .bottom) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("队列").font(.system(size: 24, weight: .bold)).foregroundColor(t.color(t.fg))
                    Text("任务状态与耗时").font(.system(size: 11)).foregroundColor(t.color(t.muted))
                }
                Spacer()
                Button { showCreate = true } label: {
                    HStack(spacing: 5) {
                        Image(systemName: "plus.circle.fill").font(.system(size: 13))
                        Text("新建").font(.system(size: 13, weight: .semibold))
                    }
                    .foregroundColor(t.color(t.fg))
                    .padding(.horizontal, 12).padding(.vertical, 7)
                    .background(Capsule().stroke(t.color(t.border), lineWidth: 1))
                }
                .buttonStyle(.plain)
                Button(action: onClose) {
                    Text("完成").font(.system(size: 13)).foregroundColor(t.color(t.muted))
                }
            }
            .padding(.horizontal, 16).padding(.vertical, 10)
            ScrollView {
                LazyVStack(spacing: 10) {
                    if let e = err {
                        ErrorCard(message: e, t: t)
                    } else if loading {
                        ProgressView().padding(.top, 40)
                    } else if items.isEmpty {
                        EmptyCard(t: t, text: "队列为空 —— 去网页端或 TG 下单")
                    } else {
                        ForEach(items.indices, id: \.self) { i in
                            queueCard(items[i])
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .task { await load() }
        .refreshable { await load() }
        .sheet(isPresented: $showCreate) {
            CreateOrderSheet(onClose: {
                showCreate = false
                Task { await load() }
            })
        }
    }

    private func queueCard(_ item: [String: Any]) -> some View {
        let status = (item["status"] as? String ?? "").lowercased()
        let color = status == "running" ? t.success : status == "paused" ? t.warning : status == "failed" ? t.danger : t.muted
        let timings = item["timings"] as? [String: Double] ?? [:]
        let totalMs = timings.values.reduce(0, +)
        let timingsText = timings
            .sorted { $0.key < $1.key }
            .map { "\(stageCn($0.key)) \(msText($0.value))" }
            .joined(separator: " / ")

        return VStack(alignment: .leading, spacing: 7) {
            HStack {
                HStack(spacing: 8) {
                    Circle().fill(t.color(color)).frame(width: 7, height: 7)
                    Text(item["planCode"] as? String ?? "").font(.system(size: 12.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                }
                Spacer()
                Text(statusLabel(status)).font(.system(size: 10.5, weight: .semibold)).foregroundColor(t.color(color))
            }
            Text("\((item["datacenter"] as? String ?? "").uppercased()) · 重试 \(item["failureCount"] as? Int ?? 0)/\(item["maxRetries"] as? Int ?? 20) · 间隔 \(item["retryInterval"] as? Int ?? 0)s")
                .font(.system(size: 11)).foregroundColor(t.color(t.muted))
            if !timings.isEmpty {
                Text("上轮 \(msText(totalMs))(\(timingsText))")
                    .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
            }
            if let le = item["lastError"] as? String, !le.isEmpty {
                Text(le).font(.system(size: 11)).foregroundColor(t.color(t.danger))
            }
            // 轻操作:暂停/恢复 + 删除
            HStack(spacing: 8) {
                if status == "running" || status == "paused" {
                    QueueBtn(icon: status == "paused" ? "play.fill" : "pause.fill", label: status == "paused" ? "恢复" : "暂停", color: t.muted, t: t) {
                        await toggle(item, to: status == "paused" ? "running" : "paused")
                    }
                    QueueBtn(icon: "trash", label: "删除", color: t.danger, t: t) {
                        await remove(item)
                    }
                }
            }
        }
        .padding(13)
        .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(color), lineWidth: 1)))
    }

    /// 暂停/恢复:后端契约 PUT /queue/:id/status + {status}(与 RN 版核过)
    private func toggle(_ item: [String: Any], to status: String) async {
        let id = item["id"] as? String ?? ""
        _ = await conn.client.actionPutData("/queue/\(id)/status", bodyData: try? JSONSerialization.data(withJSONObject: ["status": status]))
        await load()
    }

    private func remove(_ item: [String: Any]) async {
        let id = item["id"] as? String ?? ""
        _ = await conn.client.actionDelete("/queue/\(id)")
        await load()
    }

    private func load() async {
        err = nil
        do { items = try await conn.client.getArray("/queue") }
        catch { err = error.localizedDescription }
        loading = false
    }
}

private func statusLabel(_ s: String) -> String {
    ["running": "运行中", "paused": "已暂停", "failed": "失败", "success": "成功", "pending": "等待"][s] ?? s
}
private func stageCn(_ k: String) -> String {
    ["availability": "查库存", "price": "验价", "cart": "建车", "checkout": "下单"][k] ?? k
}
private func msText(_ v: Double) -> String {
    v >= 1000 ? String(format: "%.1fs", v / 1000) : "\(Int(v))ms"
}

struct QueueBtn: View {
    let icon: String
    let label: String
    let color: String
    let t: Tokens
    let action: () async -> Void
    var body: some View {
        Button { Task { await action() } } label: {
            HStack(spacing: 4) {
                Image(systemName: icon).font(.system(size: 11))
                Text(label).font(.system(size: 11))
            }
            .foregroundColor(t.color(color))
            .padding(.horizontal, 10).padding(.vertical, 5)
            .background(Capsule().stroke(t.color(color), lineWidth: 1))
        }
        .buttonStyle(.plain)
    }
}

// MARK: - 设置与账户

struct ProfileOverlay: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let onClose: () -> Void
    let onDisconnected: () -> Void

    @State private var accounts: [[String: Any]] = []
    @State private var err: String?

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            OverlayHeaderView(title: "设置与账户", subtitle: "配对 / 账户 / 外观", onClose: onClose, t: t)
            ScrollView {
                VStack(spacing: 10) {
                    if let e = err {
                        ErrorCard(message: e, t: t)
                    }
                    // 账户卡:当前选中加粗描边
                    ForEach(accounts.indices, id: \.self) { i in
                        accountCard(accounts[i])
                    }
                    // 外观
                    HStack {
                        Label("深色外观", systemImage: "moon")
                            .font(.system(size: 12.5)).foregroundColor(t.color(t.fg))
                        Spacer()
                        Toggle("", isOn: $theme.dark).labelsHidden().tint(t.color(t.success))
                    }
                    .padding(13)
                    .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surfaceMuted)))

                    Text("后端:\(conn.serverUrl)\n切换账户与 web 同语义:所有请求自动带 ?account=。\n设备令牌可在网页端「设置 → App 配对」单独吊销。")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                        .frame(maxWidth: .infinity, alignment: .leading)

                    // 断开
                    Button { conn.forget(); onDisconnected() } label: {
                        HStack(spacing: 7) {
                            Image(systemName: "rectangle.portrait.and.arrow.right").font(.system(size: 14))
                            Text("断开连接(清除本机令牌)").font(.system(size: 13, weight: .semibold))
                        }
                        .foregroundColor(t.color(t.danger))
                        .frame(maxWidth: .infinity, minHeight: 46)
                        .background(RoundedRectangle(cornerRadius: 14).stroke(t.color(t.danger), lineWidth: 1))
                    }
                    .buttonStyle(.plain)
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .task { await load() }
    }

    private func accountCard(_ a: [String: Any]) -> some View {
        let id = a["id"] as? String ?? ""
        let isActive = id == conn.accountId || (conn.accountId.isEmpty && (a["isDefault"] as? Bool == true))
        let valid = a["valid"] as? Bool ?? false
        return Button { conn.setAccount(id) } label: {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    HStack(spacing: 8) {
                        Circle().fill(t.color(valid ? t.success : t.danger)).frame(width: 7, height: 7)
                        Text(a["name"] as? String ?? "").font(.system(size: 13, weight: .semibold)).foregroundColor(t.color(t.fg))
                        // 区域徽章
                        Text(a["zone"] as? String ?? "")
                            .font(.system(size: 9, weight: .bold))
                            .foregroundColor(t.color(t.fg))
                            .padding(.horizontal, 6).padding(.vertical, 2)
                            .background(RoundedRectangle(cornerRadius: 6).fill(t.color(t.surfaceMuted)))
                    }
                    Spacer()
                    if isActive {
                        Text("当前").font(.system(size: 10, weight: .semibold)).foregroundColor(t.color(t.fg))
                    } else if a["isDefault"] as? Bool == true {
                        Text("默认").font(.system(size: 10)).foregroundColor(t.color(t.muted))
                    }
                }
                Text(a["endpoint"] as? String ?? "").font(.system(size: 11, design: .monospaced)).foregroundColor(t.color(t.muted))
            }
            .padding(13)
            .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(isActive ? t.fg : t.border), lineWidth: isActive ? 1.5 : 1)))
        }
        .buttonStyle(.plain)
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/accounts")
            accounts = (r["accounts"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
    }
}

// MARK: - 共享件

/// 覆盖页头:标题 + 完成按钮
struct OverlayHeaderView: View {
    let title: String
    let subtitle: String
    let onClose: () -> Void
    let t: Tokens
    var body: some View {
        HStack(alignment: .bottom) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.system(size: 24, weight: .bold)).foregroundColor(t.color(t.fg))
                Text(subtitle).font(.system(size: 11)).foregroundColor(t.color(t.muted))
            }
            Spacer()
            Button(action: onClose) {
                Text("完成").font(.system(size: 13)).foregroundColor(t.color(t.muted))
            }
        }
        .padding(.horizontal, 16).padding(.vertical, 10)
    }
}

/// 简易流式布局(机房格这种不定宽 chip 需要)
struct FlowLayout: Layout {
    var spacing: CGFloat = 8
    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? 320
        var x: CGFloat = 0, y: CGFloat = 0, rowH: CGFloat = 0
        for sub in subviews {
            let size = sub.sizeThatFits(.unspecified)
            if x + size.width > width, x > 0 { x = 0; y += rowH + spacing; rowH = 0 }
            x += size.width + spacing
            rowH = max(rowH, size.height)
        }
        return CGSize(width: width, height: y + rowH)
    }
    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, rowH: CGFloat = 0
        for sub in subviews {
            let size = sub.sizeThatFits(.unspecified)
            if x + size.width > bounds.maxX, x > bounds.minX {
                x = bounds.minX; y += rowH + spacing; rowH = 0
            }
            sub.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
            x += size.width + spacing
            rowH = max(rowH, size.height)
        }
    }
}
