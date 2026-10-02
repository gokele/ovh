import SwiftUI

/**
 * 抢购历史(含订单支付状态)与运行日志 —— 菜单里的两个查看页。
 * 历史:成功单显示付款倒计时(expirationTime 是订单未付款何时作废,不是撤回期)
 *      + OVH 订单状态;失败单显示原因与各阶段耗时。
 * 日志:只读流,自动刷新,隐私模式下后端已打码。
 */

// MARK: - 历史

struct HistoryOverlay: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let onClose: () -> Void

    @State private var items: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            OverlayHeaderView(title: "抢购历史", subtitle: "订单状态与付款倒计时", onClose: onClose, t: t)
            ScrollView {
                LazyVStack(spacing: 10) {
                    if let e = err {
                        ErrorCard(message: e, t: t)
                    } else if loading {
                        ProgressView().padding(.top, 40)
                    } else if items.isEmpty {
                        EmptyCard(t: t, text: "还没有抢购记录")
                    } else {
                        ForEach(items.indices, id: \.self) { i in
                            historyCard(items[i])
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

    private func historyCard(_ item: [String: Any]) -> some View {
        let success = (item["status"] as? String) == "success"
        let orderStatus = item["orderStatus"] as? String ?? ""
        let unpaid = success && (orderStatus.isEmpty || orderStatus == "notPaid")

        return VStack(alignment: .leading, spacing: 7) {
            HStack {
                HStack(spacing: 8) {
                    Circle().fill(t.color(success ? t.success : t.danger)).frame(width: 7, height: 7)
                    Text(item["planCode"] as? String ?? "").font(.system(size: 12.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                }
                Spacer()
                // 状态标签:成功+未付款 = 最要紧的提醒
                if unpaid {
                    pill("成功·待付款", t.warning)
                } else if success {
                    pill(orderStatusLabel(orderStatus), t.success)
                } else {
                    pill("失败", t.danger)
                }
            }
            Text("\((item["datacenter"] as? String ?? "").uppercased()) · \(item["purchaseTime"] as? String ?? "")")
                .font(.system(size: 10.5, design: .monospaced)).foregroundColor(t.color(t.muted))

            // 付款倒计时:expirationTime 是「未付款何时作废」,与撤回期是两回事
            if unpaid, let exp = item["expirationTime"] as? String, let left = timeLeft(exp) {
                Text("付款剩余 \(left) — 逾期未付订单自动作废")
                    .font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.warning))
            }
            // 撤回期(有则显示,与付款窗口分开)
            if let rt = item["retractionTime"] as? String, !rt.isEmpty, let left = timeLeft(rt) {
                Text("可撤单剩 \(left)(从下单日起算)")
                    .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
            }
            // 失败原因 + 各阶段耗时(输了之后唯一有用的信息:慢在哪一步)
            if let em = item["errorMessage"] as? String, !em.isEmpty {
                Text(em).font(.system(size: 11)).foregroundColor(t.color(t.danger))
            }
            if let timing = item["timing"] as? [[String: Any]], let total = item["totalMs"] as? Double, total > 0 {
                let phases = timing.compactMap { ph -> String? in
                    guard let name = ph["phase"] as? String, let ms = ph["ms"] as? Double else { return nil }
                    return "\(phaseCn(name)) \(msText(ms))"
                }.joined(separator: " / ")
                if !phases.isEmpty {
                    Text("总 \(msText(total))(\(phases))").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                }
            }
            // 订单号 + 打开付款页(orderUrl 是控制面板深链,不是带凭证的 checkout URL)
            if success, let oid = item["orderId"] as? String, !oid.isEmpty {
                HStack(spacing: 8) {
                    Text("订单 #\(oid)").font(.system(size: 10.5, design: .monospaced)).foregroundColor(t.color(t.faint))
                    Spacer()
                    if let url = item["orderUrl"] as? String, let u = URL(string: url) {
                        Button {
                            Task { _ = await UIApplication.shared.open(u) }
                        } label: {
                            Label(unpaid ? "去付款" : "订单页", systemImage: "arrow.up.right.square")
                                .font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(unpaid ? t.warning : t.muted))
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
        .padding(13)
        .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(unpaid ? t.warning : t.border), lineWidth: unpaid ? 1.5 : 1)))
    }

    private func pill(_ text: String, _ color: String) -> some View {
        Text(text).font(.system(size: 9.5, weight: .bold)).foregroundColor(t.color(color))
            .padding(.horizontal, 8).padding(.vertical, 3)
            .background(Capsule().stroke(t.color(color), lineWidth: 1))
    }

    private func timeLeft(_ iso: String) -> String? {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let d = f.date(from: iso) ?? ISO8601DateFormatter().date(from: iso)
        guard let date = d else { return nil }
        let s = date.timeIntervalSinceNow
        guard s > 0 else { return nil }
        let h = Int(s) / 3600, m = (Int(s) % 3600) / 60
        return h >= 24 ? "\(h / 24) 天 \(h % 24) 小时" : "\(h) 小时 \(m) 分"
    }

    private func orderStatusLabel(_ s: String) -> String {
        ["notPaid": "待付款", "checking": "审核中", "delivering": "交付中", "delivered": "已交付",
         "cancelled": "已取消", "cancelling": "取消中", "documentsRequested": "待补文件", "unknown": "未知"][s] ?? (s.isEmpty ? "成功" : s)
    }
    private func phaseCn(_ k: String) -> String {
        ["availability": "查库存", "price": "验价", "cart": "建车", "checkout": "下单"][k] ?? k
    }
    private func msText(_ v: Double) -> String {
        v >= 1000 ? String(format: "%.1fs", v / 1000) : "\(Int(v))ms"
    }

    private func load() async {
        do { items = try await conn.client.getArray("/purchase-history"); err = nil }
        catch { err = error.localizedDescription }
        loading = false
    }
}

// MARK: - 日志

struct LogsOverlay: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let onClose: () -> Void

    @State private var logs: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            OverlayHeaderView(title: "运行日志", subtitle: "最近 200 条 · 5 秒自动刷新", onClose: onClose, t: t)
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 6) {
                    if let e = err {
                        ErrorCard(message: e, t: t).padding(.bottom, 8)
                    }
                    if logs.isEmpty && !loading && err == nil {
                        EmptyCard(t: t, text: "暂无日志")
                    }
                    ForEach(logs.indices, id: \.self) { i in
                        logRow(logs[i])
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .task(id: "logs") {
            // 前台每 5 秒刷新;退后台自然停止(iOS 挂起 Task)
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(5))
            }
        }
    }

    private func logRow(_ l: [String: Any]) -> some View {
        let level = l["level"] as? String ?? "info"
        let color = level == "error" ? t.danger : level == "warn" ? t.warning : t.muted
        return HStack(alignment: .top, spacing: 8) {
            Text(timeOnly(l["time"] as? String ?? l["timestamp"] as? String ?? ""))
                .font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.faint))
                .frame(width: 44, alignment: .leading)
            Text(l["msg"] as? String ?? "")
                .font(.system(size: 11, design: .monospaced)).foregroundColor(t.color(level == "error" ? t.danger : t.fg))
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, 10).padding(.vertical, 5)
        .background(RoundedRectangle(cornerRadius: 8).fill((color == t.muted ? Color.clear : t.color(color).opacity(0.06))))
    }

    private func timeOnly(_ iso: String) -> String {
        guard iso.count >= 19 else { return "" }
        let start = iso.index(iso.startIndex, offsetBy: 11)
        let end = iso.index(iso.startIndex, offsetBy: 19)
        return String(iso[start..<end])
    }

    private func load() async {
        do { logs = Array((try await conn.client.getArray("/logs")).prefix(200)); err = nil }
        catch { err = error.localizedDescription }
        loading = false
    }
}
