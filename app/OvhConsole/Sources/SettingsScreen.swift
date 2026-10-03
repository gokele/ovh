import SwiftUI

/**
 * 设置页:OVH 账户(切换/链路检测/验证)/ 通知(TG+Webhook)/ App 配对设备 /
 * 缓存管理 / 外观 / 日志入口 / OVH 账户信息(邮件·退款)/ 断开连接。
 */
struct SettingsScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    var t: Tokens { theme.t }

    @State private var subpath: [Subpage] = []

    enum Subpage: String, Hashable {
        case logs, accountInfo, notify, pairing, cache
    }

    var body: some View {
        NavigationStack(path: $subpath) {
            ScrollView {
                VStack(spacing: 12) {
                    accountSection
                    Group {
                        NavRow(icon: "bell.badge.fill", title: "通知通道", desc: "Telegram / Webhook 状态与测试", tint: t.info) { subpath.append(.notify) }
                        NavRow(icon: "iphone.gen3.radiowaves.left.and.right", title: "App 配对设备", desc: "配对码与设备吊销", tint: t.info) { subpath.append(.pairing) }
                        NavRow(icon: "internaldrive.fill", title: "缓存管理", desc: "目录/价格缓存查看与清除", tint: t.muted) { subpath.append(.cache) }
                        NavRow(icon: "person.text.rectangle.fill", title: "OVH 账户信息", desc: "客户资料 / 邮件历史 / 退款", tint: t.muted) { subpath.append(.accountInfo) }
                        NavRow(icon: "doc.text.magnifyingglass", title: "运行日志", desc: "最近 200 条,自动刷新", tint: t.muted) { subpath.append(.logs) }
                        appearanceCard
                        dangerCard
                    }
                }
                .padding(16)
            }
            .background(t.color(t.bg))
            .navigationTitle("设置")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarColorScheme(theme.dark ? .dark : .light, for: .navigationBar)
            .refreshable { await conn.loadAccounts() }
            .navigationDestination(for: Subpage.self) { page in
                switch page {
                case .logs: LogsScreen()
                case .accountInfo: OvhAccountScreen()
                case .notify: NotifyScreen()
                case .pairing: PairingDevicesScreen()
                case .cache: CacheScreen()
                }
            }
        }
        .task { await conn.loadAccounts() }
    }

    // MARK: 账户区(只读摘要;切换在任意页面顶栏的账户胶囊)

    private var accountSection: some View {
        Card {
            VStack(alignment: .leading, spacing: 10) {
                SectionTitle(text: "当前账户")
                if let acc = conn.activeAccount {
                    HStack(spacing: 9) {
                        Dot(color: (acc["valid"] as? Bool ?? false) ? t.success : t.danger)
                        Text(acc["name"] as? String ?? "").font(.system(size: 14, weight: .semibold)).foregroundColor(t.color(t.fg))
                        if zoneBadgeVisible(name: acc["name"] as? String ?? "", zone: acc["zone"] as? String ?? "") {
                            Chip(text: acc["zone"] as? String ?? "—", color: zoneTint(acc))
                        }
                        Spacer()
                    }
                    Text(acc["endpoint"] as? String ?? "").font(.system(size: 10.5, design: .monospaced)).foregroundColor(t.color(t.muted))
                    Text("切换账户:点任意页面顶栏的账户胶囊。共 \(conn.accounts.count) 个账户,增删与凭据验证在网页端。")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                } else {
                    Text("没有 OVH 账户 —— 在网页端「设置 → OVH 账户」添加")
                        .font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
                }
            }
        }
    }

    // MARK: 外观(自绘三选,不依赖系统分段控件)

    private var appearanceCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 10) {
                SectionTitle(text: "外观")
                HStack(spacing: 8) {
                    appearanceBtn("跟随系统", mode: .system, icon: "circle.lefthalf.filled")
                    appearanceBtn("深色", mode: .dark, icon: "moon.fill")
                    appearanceBtn("浅色", mode: .light, icon: "sun.max.fill")
                }
            }
        }
    }

    private func appearanceBtn(_ label: String, mode: Theme.Mode, icon: String) -> some View {
        let on = theme.mode == mode
        return Button {
            theme.mode = mode
        } label: {
            VStack(spacing: 4) {
                Image(systemName: icon).font(.system(size: 13, weight: .semibold))
                Text(label).font(.system(size: 11, weight: .semibold))
            }
            .foregroundColor(t.color(on ? t.accent : t.muted))
            .frame(maxWidth: .infinity)
            .padding(.vertical, 9)
            .background(RoundedRectangle(cornerRadius: 11).fill(on ? t.color(t.accent).opacity(0.12) : t.color(t.surfaceMuted)))
        }
        .buttonStyle(.plain)
    }

    // MARK: 危险区

    private var dangerCard: some View {
        Card(border: t.danger) {
            VStack(spacing: 10) {
                SectionTitle(text: "连接")
                KV(k: "后端", v: conn.serverUrl, mono: true)
                ActBtn(kind: .danger, icon: "rectangle.portrait.and.arrow.right", label: "断开连接(清除本机令牌)") {
                    conn.forget()
                }
                Text("吊销设备请在重新配对后于网页端操作,或留在本页的配对设备里。")
                    .font(.system(size: 10)).foregroundColor(t.color(t.muted))
            }
        }
    }
}

// MARK: - 通知通道

struct NotifyScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    var t: Tokens { theme.t }

    @State private var channels: [String: Any]?
    @State private var poller: [String: Any]?
    @State private var err: String?
    @State private var loading = true
    @State private var testing = false

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                if loading {
                    ProgressView().padding(.top, 50)
                } else if let e = err {
                    Card { LoadFailed(message: e) { Task { await load() } } }
                } else {
                    if let ch = channels {
                        Card {
                            VStack(spacing: 10) {
                                SectionTitle(text: "通道状态")
                                channelRow("Telegram", "telegram", ch["telegram"] as? [String: Any])
                                channelRow("Webhook", "link", ch["webhook"] as? [String: Any])
                            }
                        }
                    }
                    if let p = poller {
                        Card {
                            VStack(alignment: .leading, spacing: 9) {
                                SectionTitle(text: "Telegram 长轮询")
                                KV(k: "状态", v: (p["running"] as? Bool ?? false) ? "运行中" : "已停止")
                                if let last = p["lastPollAt"] as? String { KV(k: "最近拉取", v: fmtDate(last)) }
                                if let e2 = p["lastError"] as? String, !e2.isEmpty {
                                    KV(k: "上次错误", v: e2.contains("409") ? "409 冲突:另一个进程在收消息" : e2)
                                }
                            }
                        }
                        Card(border: (p["running"] as? Bool ?? false) ? nil : t.warning) {
                            Text((p["running"] as? Bool ?? false)
                                 ? "轮询正常。Bot Token / Chat ID 的修改在网页端设置 → Telegram。"
                                 : "长轮询未运行 —— Bot Token 未配置或已停止,去网页端设置里填。")
                                .font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
                        }
                    }
                    ActBtn(kind: .primary, icon: "paperplane.fill", label: testing ? "发送中…" : "发一条测试通知", busy: testing) {
                        await sendTest()
                    }
                }
            }
            .padding(16)
        }
        .background(t.color(t.bg))
        .navigationTitle("通知通道")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await load() }
        .task { await load() }
    }

    private func channelRow(_ name: String, _ icon: String, _ ch: [String: Any]?) -> some View {
        let available = ch?["available"] as? Bool ?? (ch?["ok"] as? Bool ?? false)
        let configured = ch?["configured"] as? Bool ?? true
        return HStack(spacing: 9) {
            Image(systemName: icon).font(.system(size: 13)).foregroundColor(t.color(available ? t.success : t.faint))
            Text(name).font(.system(size: 12.5)).foregroundColor(t.color(t.fg))
            Spacer()
            if !configured {
                Chip(text: "未配置")
            } else {
                Chip(text: available ? "可用" : "不可用", color: available ? t.success : t.danger)
            }
        }
    }

    private func sendTest() async {
        testing = true
        defer { testing = false }
        let (ok, msg) = await conn.client.actionPostData("/monitor/test-notification", bodyData: nil)
        toast.show(ok ? "已发送,看手机/TG" : (msg.isEmpty ? "发送失败" : msg), error: !ok)
    }

    private func load() async {
        do {
            async let c = conn.client.getDict("/notify/channels?verify=true")
            async let p = conn.client.getDict("/telegram/poller")
            let (cr, pr) = try await (c, p)
            channels = cr
            poller = pr
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }
}

// MARK: - App 配对设备

struct PairingDevicesScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    var t: Tokens { theme.t }

    @State private var devices: [[String: Any]] = []
    @State private var code: String?
    @State private var err: String?
    @State private var loading = true
    @State private var generating = false

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                if let e = err {
                    Card { LoadFailed(message: e) { Task { await load() } } }
                }

                Card(border: t.accent) {
                    VStack(spacing: 12) {
                        SectionTitle(text: "新设备配对码")
                        if let c = code {
                            Text(c).font(.system(size: 30, weight: .bold, design: .monospaced)).foregroundColor(t.color(t.fg))
                                .kerning(3)
                            Text("2 分钟内有效,一码一机。在手机 App 配对页输入,或用深链 ovhconsole://pair?host=…&code=\(c)")
                                .font(.system(size: 10)).foregroundColor(t.color(t.muted))
                                .multilineTextAlignment(.center)
                            QRCodeView(code: c, host: conn.serverUrl)
                        } else {
                            Text("生成后在新设备的 App 配对页输入").font(.system(size: 11)).foregroundColor(t.color(t.muted))
                        }
                        ActBtn(kind: .primary, icon: generating ? nil : "qrcode", label: generating ? "生成中…" : (code == nil ? "生成配对码" : "重新生成"), busy: generating) {
                            await gen()
                        }
                    }
                }

                Card {
                    VStack(spacing: 10) {
                        SectionTitle(text: "已配对设备(\(devices.count))")
                        if devices.isEmpty && !loading {
                            Text("没有其他设备").font(.system(size: 11)).foregroundColor(t.color(t.faint)).padding(.vertical, 6)
                        }
                        ForEach(devices.indices, id: \.self) { i in
                            deviceRow(devices[i])
                        }
                    }
                }
            }
            .padding(16)
        }
        .background(t.color(t.bg))
        .navigationTitle("App 配对设备")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await load() }
        .task { await load() }
    }

    private func deviceRow(_ d: [String: Any]) -> some View {
        let id = String(describing: d["id"] ?? d["deviceId"] ?? "")
        return VStack(alignment: .leading, spacing: 5) {
            HStack {
                Image(systemName: "iphone").font(.system(size: 14)).foregroundColor(t.color(t.muted))
                Text(d["name"] as? String ?? "设备").font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                Spacer()
                Button {
                    Task {
                        let (ok, msg) = await conn.client.actionDelete("/app/devices/\(id)")
                        toast.show(ok ? "已吊销" : (msg.isEmpty ? "失败" : msg), error: !ok)
                        await load()
                    }
                } label: {
                    Text("吊销").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.danger))
                        .padding(.horizontal, 10).padding(.vertical, 5)
                        .background(Capsule().stroke(t.color(t.danger), lineWidth: 1))
                }.buttonStyle(.plain)
            }
            HStack(spacing: 10) {
                if let t2 = d["pairedAt"] as? String { Text("配对于 \(fmtDate(t2))") }
                if let t3 = d["lastUsedAt"] as? String, !t3.isEmpty { Text("最近使用 \(fmtDate(t3))") }
                Spacer()
            }
            .font(.system(size: 9.5)).foregroundColor(t.color(t.faint))
        }
        .padding(9)
        .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.surfaceMuted)))
    }

    private func gen() async {
        generating = true
        defer { generating = false }
        do {
            let r = try await conn.client.post("/app/pairing-codes")
            code = r["code"] as? String
        } catch { toast.show(error.localizedDescription, error: true) }
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/app/devices")
            devices = (r["devices"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }
}

/// 纯 SwiftUI 二维码(CoreImage 生成)
struct QRCodeView: View {
    let code: String
    let host: String

    var body: some View {
        if let img = generate() {
            Image(uiImage: img)
                .interpolation(.none)
                .resizable()
                .scaledToFit()
                .frame(width: 150, height: 150)
                .background(Color.white)
                .cornerRadius(10)
        }
    }

    private func generate() -> UIImage? {
        let deep = "ovhconsole://pair?host=\(host)&code=\(code)&auto=1"
        guard let filter = CIFilter(name: "CIQRCodeGenerator"),
              let data = deep.data(using: .utf8) else { return nil }
        filter.setValue(data, forKey: "inputMessage")
        filter.setValue("M", forKey: "inputCorrectionLevel")
        guard let out = filter.outputImage else { return nil }
        let scaled = out.transformed(by: CGAffineTransform(scaleX: 8, y: 8))
        let ctx = CIContext()
        guard let cg = ctx.createCGImage(scaled, from: scaled.extent) else { return nil }
        return UIImage(cgImage: cg)
    }
}

// MARK: - 缓存管理

struct CacheScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    var t: Tokens { theme.t }

    @State private var info: [String: Any]?
    @State private var err: String?
    @State private var loading = true
    @State private var clearing: String?

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                if loading {
                    ProgressView().padding(.top, 50)
                } else if let e = err {
                    Card { LoadFailed(message: e) { Task { await load() } } }
                } else if let i = info {
                    Card {
                        VStack(spacing: 9) {
                            SectionTitle(text: "缓存状态")
                            KV(k: "内存缓存条数", v: "\(i["memoryCount"] as? Int ?? (i["memory_count"] as? Int ?? 0))")
                            KV(k: "SQLite 条数", v: "\(i["sqliteCount"] as? Int ?? (i["sqlite_count"] as? Int ?? 0))")
                            if let p = i["dbPath"] as? String { KV(k: "数据库", v: p, mono: true) }
                            if let ts = i["lastRefresh"] as? String { KV(k: "最近刷新", v: fmtDate(ts)) }
                        }
                    }
                    Card {
                        VStack(spacing: 10) {
                            SectionTitle(text: "清除")
                            clearBtn("内存缓存", "memory")
                            clearBtn("SQLite 缓存", "sqlite")
                            clearBtn("全部", "all", danger: true)
                        }
                    }
                }
            }
            .padding(16)
        }
        .background(t.color(t.bg))
        .navigationTitle("缓存管理")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await load() }
        .task { await load() }
    }

    private func clearBtn(_ label: String, _ type: String, danger: Bool = false) -> some View {
        ActBtn(kind: danger ? .danger : .ghost, icon: "trash", label: clearing == type ? "清除中…" : "清除\(label)", busy: clearing == type) {
            clearing = type
            let body = try? JSONSerialization.data(withJSONObject: ["type": type])
            let (ok, msg) = await conn.client.actionPostData("/cache/clear", bodyData: body)
            clearing = nil
            toast.show(ok ? "已清除\(label)" : (msg.isEmpty ? "失败" : msg), error: !ok)
            await load()
        }
    }

    private func load() async {
        do {
            info = try await conn.client.getDict("/cache/info")
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }
}

// MARK: - OVH 账户信息(邮件/退款)

struct OvhAccountScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    var t: Tokens { theme.t }

    @State private var info: [String: Any]?
    @State private var emails: [[String: Any]] = []
    @State private var refunds: [[String: Any]] = []
    @State private var seg = 0
    @State private var err: String?
    @State private var loading = true
    @State private var openEmail: [String: Any]?

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                if loading {
                    ProgressView().padding(.top, 50)
                } else if let e = err {
                    Card { LoadFailed(message: e) { Task { await load() } } }
                } else {
                    if let i = info {
                        Card {
                            VStack(spacing: 9) {
                                SectionTitle(text: "账户资料")
                                KV(k: "客户代码", v: i["customerCode"] as? String ?? (i["nichandle"] as? String ?? "—"), mono: true)
                                if let kyc = i["kycValidated"] as? Bool {
                                    KV(k: "KYC 验证", v: kyc ? "已验证" : "未验证")
                                }
                                KV(k: "邮箱", v: i["email"] as? String ?? "—", mono: true)
                                if let city = i["city"] as? String {
                                    KV(k: "持有人", v: "\(city) \(i["country"] as? String ?? "")")
                                }
                            }
                        }
                        if let sub = i["subsidiaryMismatch"] as? Bool, sub {
                            SheetNote(text: "账户 zone 与 OVH 子公司不一致,目录可能错区 —— 去网页端设置改 zone。", tint: t.warning)
                        }
                    }

                    Picker("", selection: $seg) {
                        Text("邮件(\(emails.count))").tag(0)
                        Text("退款(\(refunds.count))").tag(1)
                    }
                    .pickerStyle(.segmented)

                    if seg == 0 {
                        if emails.isEmpty {
                            Card { EmptyHint(icon: "envelope", text: "没有邮件记录") }
                        }
                        ForEach(emails.prefix(50).indices, id: \.self) { i in
                            emailRow(emails[i])
                        }
                    } else {
                        if refunds.isEmpty {
                            Card { EmptyHint(icon: "banknote", text: "没有退款记录") }
                        }
                        ForEach(refunds.indices, id: \.self) { i in
                            let r = refunds[i]
                            Card {
                                VStack(spacing: 6) {
                                    HStack {
                                        Text(r["refundId"] as? String ?? "—").font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                                        Spacer()
                                        Text(String(format: "%.2f %@", (r["amount"] as? Double) ?? 0, r["currency"] as? String ?? ""))
                                            .font(.system(size: 12, weight: .bold, design: .rounded)).foregroundColor(t.color(t.success))
                                    }
                                    KV(k: "日期", v: fmtDate(r["date"] as? String))
                                }
                            }
                        }
                    }
                }
            }
            .padding(16)
        }
        .background(t.color(t.bg))
        .navigationTitle("OVH 账户")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await load() }
        .task { await load() }
        .sheet(item: Binding(
            get: { openEmail.map { EmailWrap(e: $0) } },
            set: { openEmail = $0?.e }
        )) { w in
            EmailDetailSheet(email: w.e).environmentObject(theme)
        }
    }

    private struct EmailWrap: Identifiable {
        let e: [String: Any]
        var id: String { String(describing: e["id"] ?? e["date"] ?? UUID().uuidString) }
    }

    private func emailRow(_ e: [String: Any]) -> some View {
        Button { openEmail = e } label: {
            HStack(spacing: 9) {
                Image(systemName: "envelope.fill").font(.system(size: 12)).foregroundColor(t.color(t.muted))
                VStack(alignment: .leading, spacing: 2) {
                    Text(e["subject"] as? String ?? "(无主题)").font(.system(size: 12)).foregroundColor(t.color(t.fg)).lineLimit(1)
                    Text(fmtDate(e["date"] as? String)).font(.system(size: 9.5)).foregroundColor(t.color(t.faint))
                }
                Spacer()
                Image(systemName: "chevron.right").font(.system(size: 10)).foregroundColor(t.color(t.faint))
            }
            .padding(11)
            .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 12).stroke(t.color(t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }

    private func load() async {
        do {
            async let i = conn.client.getDict("/ovh/account/info")
            async let e = conn.client.getDict("/ovh/account/email-history")
            async let r = conn.client.getDict("/ovh/account/refunds")
            let (ir, er, rr) = try await (i, e, r)
            info = ir["info"] as? [String: Any] ?? ir
            emails = (er["emails"] as? [[String: Any]]) ?? (er["history"] as? [[String: Any]]) ?? []
            refunds = (rr["refunds"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }
}

struct EmailDetailSheet: View {
    @EnvironmentObject var theme: Theme
    let email: [String: Any]
    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "envelope.fill", tint: t.info, title: email["subject"] as? String ?? "邮件")
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    KV(k: "日期", v: fmtDate(email["date"] as? String))
                    Text(email["body"] as? String ?? email["content"] as? String ?? "(无正文)")
                        .font(.system(size: 11.5, design: .monospaced))
                        .foregroundColor(t.color(t.fg))
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(12)
                        .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surfaceMuted)))
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.large])
    }
}

// MARK: - 日志页

struct LogsScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @AppStorage("ovh_mask_ip") private var mask = false
    var t: Tokens { theme.t }

    @State private var logs: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true
    @State private var auto = false
    @State private var level = 0
    @State private var search = ""

    var body: some View {
        ScrollView {
            VStack(spacing: 10) {
                HStack(spacing: 8) {
                    Image(systemName: "magnifyingglass").font(.system(size: 12)).foregroundColor(t.color(t.faint))
                    TextField("搜索日志", text: $search)
                        .font(.system(size: 12.5)).textInputAutocapitalization(.never).autocorrectionDisabled()
                        .foregroundColor(t.color(t.fg))
                }
                .padding(.horizontal, 12).padding(.vertical, 9)
                .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surface)))

                Picker("", selection: $level) {
                    Text("全部").tag(0)
                    Text("INFO+").tag(1)
                    Text("警告+").tag(2)
                    Text("错误").tag(3)
                }
                .pickerStyle(.segmented)

                HStack {
                    Text("\(filtered.count) 条\(err != nil ? " · 刷新失败,显示旧内容" : "")")
                        .font(.system(size: 10.5)).foregroundColor(t.color(err != nil ? t.warning : t.faint))
                    Spacer()
                    Toggle(isOn: $auto) {
                        Text("自动刷新").font(.system(size: 11)).foregroundColor(t.color(t.muted))
                    }.toggleStyle(.button).tint(t.color(t.accent))
                    Button {
                        Task { try? await Task.sleep(nanoseconds: 300_000_000); _ = await conn.client.actionDelete("/logs"); await load() }
                    } label: {
                        Text("清空").font(.system(size: 11.5)).foregroundColor(t.color(t.danger))
                    }.buttonStyle(.plain)
                }

                if let e = err, logs.isEmpty {
                    Card { LoadFailed(message: e) { Task { await load() } } }
                } else if filtered.isEmpty && !loading {
                    Card { EmptyHint(icon: "doc.text", text: "没有日志") }
                } else {
                    ForEach(filtered.indices, id: \.self) { i in
                        logRow(filtered[i])
                    }
                }
            }
            .padding(16)
        }
        .background(t.color(t.bg))
        .navigationTitle("运行日志")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await load() }
        .task {
            await load()
            // 自动刷新 5 秒轮询
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 5_000_000_000)
                if auto { await load() }
            }
        }
    }

    private var filtered: [[String: Any]] {
        var list = logs
        let order = ["debug": 0, "info": 1, "warning": 2, "warn": 2, "error": 3]
        if level > 0 {
            list = list.filter { (order[($0["level"] as? String ?? "").lowercased()] ?? 1) >= level }
        }
        let q = search.trimmingCharacters(in: .whitespaces).lowercased()
        if !q.isEmpty {
            list = list.filter { (($0["message"] as? String ?? "") + ($0["source"] as? String ?? "")).lowercased().contains(q) }
        }
        return list
    }

    private func logRow(_ l: [String: Any]) -> some View {
        let lv = (l["level"] as? String ?? "info").lowercased()
        let color = lv == "error" ? t.danger : (lv == "warning" || lv == "warn") ? t.warning : (lv == "debug" ? t.faint : t.muted)
        var msg = l["message"] as? String ?? ""
        if mask { msg = maskIP(msg) }
        return HStack(alignment: .top, spacing: 8) {
            Text(lv.uppercased().prefix(3))
                .font(.system(size: 8.5, weight: .bold, design: .monospaced)).foregroundColor(t.color(color))
                .frame(width: 30, alignment: .leading)
            VStack(alignment: .leading, spacing: 2) {
                Text(msg).font(.system(size: 11)).foregroundColor(t.color(t.fg))
                HStack(spacing: 6) {
                    Text(fmtDate(l["timestamp"] as? String ?? l["time"] as? String))
                    if let s = l["source"] as? String, !s.isEmpty { Text("· \(s)") }
                }
                .font(.system(size: 9)).foregroundColor(t.color(t.faint))
            }
            Spacer(minLength: 0)
        }
        .padding(9)
        .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.surfaceMuted).opacity(0.5)))
    }

    private func load() async {
        do {
            logs = try await conn.client.getArray("/logs")
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }
}

// MARK: - 账户切换器(顶栏胶囊 + 底部选择 sheet)
// 对齐 web 顶栏 AccountSwitcher:三区目录互不相通,切账户是高频操作,
// 不该埋在设置页里 —— 主要页面顶栏随时可切。

/// 账户名与 zone 同名(如账户就叫 "IE")时,区域徽章不重复显示
func zoneBadgeVisible(name: String, zone: String) -> Bool {
    !zone.isEmpty && name.trimmingCharacters(in: .whitespaces).uppercased() != zone.uppercased()
}

/// endpoint → 区色:美区蓝 / 加区橙 / 欧区绿
func zoneTint(_ acc: [String: Any]) -> String {
    switch acc["endpoint"] as? String ?? "" {
    case "ovh-us": return "info"
    case "ovh-ca": return "warning"
    default: return "accent"
    }
}

/// 顶栏账户胶囊:色点 + 名称 + 下拉箭头
struct AccountButton: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let onTap: () -> Void
    var t: Tokens { theme.t }

    var body: some View {
        Button(action: onTap) {
            HStack(spacing: 6) {
                if let acc = conn.activeAccount {
                    Circle().fill(t.color(zoneTint(acc))).frame(width: 7, height: 7)
                    Text(acc["name"] as? String ?? "账户").font(.system(size: 12.5, weight: .semibold)).lineLimit(1)
                    if zoneBadgeVisible(name: acc["name"] as? String ?? "", zone: acc["zone"] as? String ?? "") {
                        Chip(text: acc["zone"] as? String ?? "", color: zoneTint(acc))
                    }
                } else {
                    Image(systemName: "person.crop.circle.badge.plus").font(.system(size: 12, weight: .semibold))
                    Text("添加账户").font(.system(size: 12.5, weight: .semibold))
                }
                Image(systemName: "chevron.down").font(.system(size: 9, weight: .bold))
            }
            .foregroundColor(t.color(t.fg))
            .padding(.horizontal, 10).padding(.vertical, 6)
            .background(Capsule().fill(t.color(t.surfaceMuted)))
            .lineLimit(1)
        }
        .buttonStyle(.plain)
    }
}

/// 账户选择 sheet:单选切换,全局立即生效(依赖 conn.accountId 的页面自动重载)
struct AccountPickerSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            Capsule().fill(t.color(t.border)).frame(width: 36, height: 4).padding(.top, 10)
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text("切换账户").font(.system(size: 17, weight: .bold)).foregroundColor(t.color(t.fg))
                    Text("目录、库存、下单都跟随所选账户的站点").font(.system(size: 11)).foregroundColor(t.color(t.muted))
                }
                Spacer()
                Button { dismiss() } label: {
                    Image(systemName: "xmark").font(.system(size: 14, weight: .semibold)).foregroundColor(t.color(t.muted))
                }.buttonStyle(.plain)
            }
            .padding(16)

            ScrollView {
                VStack(spacing: 9) {
                    if conn.accounts.isEmpty {
                        EmptyHint(icon: "person.crop.circle.badge.exclamationmark", text: "还没有账户 —— 在网页端「设置 → OVH 账户」添加")
                    }
                    ForEach(conn.accounts.indices, id: \.self) { i in
                        row(conn.accounts[i])
                    }
                    SheetNote(text: "账户的增删和凭据验证在网页端设置页完成。", tint: t.muted)
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .task { await conn.loadAccounts() }
    }

    private func row(_ acc: [String: Any]) -> some View {
        let id = acc["id"] as? String ?? ""
        let isActive = id == conn.accountId || (conn.accountId.isEmpty && (acc["isDefault"] as? Bool == true))
        let tint = zoneTint(acc)
        let valid = acc["valid"] as? Bool ?? false
        return Button {
            conn.setAccount(isActive ? "" : id)
            toast.show("已切换到 \(acc["name"] as? String ?? "")")
            dismiss()
        } label: {
            HStack(spacing: 10) {
                Circle().fill(t.color(valid ? tint : t.danger)).frame(width: 9, height: 9)
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Text(acc["name"] as? String ?? "").font(.system(size: 14, weight: .semibold)).foregroundColor(t.color(t.fg))
                        if zoneBadgeVisible(name: acc["name"] as? String ?? "", zone: acc["zone"] as? String ?? "") {
                            Chip(text: acc["zone"] as? String ?? "—", color: tint)
                        }
                    }
                    Text(acc["endpoint"] as? String ?? "").font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.muted))
                }
                Spacer()
                if isActive {
                    Image(systemName: "checkmark.circle.fill").font(.system(size: 17)).foregroundColor(t.color(t.accent))
                } else if acc["isDefault"] as? Bool == true {
                    Text("默认").font(.system(size: 10)).foregroundColor(t.color(t.faint))
                }
            }
            .padding(12)
            .background(RoundedRectangle(cornerRadius: 13).fill(isActive ? t.color(t.accent).opacity(0.08) : t.color(t.surfaceMuted)))
        }
        .buttonStyle(.plain)
    }
}
