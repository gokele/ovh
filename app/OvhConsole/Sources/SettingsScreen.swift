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
                        snipeDefaultsCard
                        appearanceCard
                        aboutCard
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
                if let ae = conn.accountsError {
                    Text("账户读取失败(\(ae))—— 账户相关功能可能不对,下拉重试")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.danger))
                } else if let acc = conn.activeAccount {
                    HStack(spacing: 9) {
                        Circle().fill(t.color(zoneTint(acc))).frame(width: 8, height: 8)
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

    // MARK: 抢购默认值(只读 —— 保存接口是全量覆盖,部分提交会清掉 TG 等配置,改值去网页端)

    @State private var snipeDefaults: [String: Any]?
    @State private var intervalA = ""   // 新任务默认
    @State private var intervalB = ""   // 自动抢
    @State private var snipeSaveBusy = false
    @State private var snipeLoaded = false

    private var snipeDefaultsCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 9) {
                SectionTitle(text: "抢购默认间隔")
                if !snipeLoaded {
                    ProgressView().padding(4)
                } else {
                    VStack(alignment: .leading, spacing: 5) {
                        Text("新任务默认重试间隔(秒)").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.muted))
                        SheetField(placeholder: "默认 60", text: $intervalA, mono: true, keyboard: .numberPad)
                        Text("网页新建任务、Telegram /buy、上架通知里的一键下单按钮都用它。留空 = 60 秒。范围 1 ~ 86400。")
                            .font(.system(size: 9.5)).foregroundColor(t.color(t.faint))
                    }
                    VStack(alignment: .leading, spacing: 5) {
                        Text("监控自动下单间隔(秒)").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.muted))
                        SheetField(placeholder: "默认 2", text: $intervalB, mono: true, keyboard: .numberPad)
                        Text("/watch 自动抢触发的任务用这个。货刚出现那一刻窗口可能只有几十秒,所以默认比普通任务激进(2 秒);但太密会吃 OVH 的 429,自己权衡。")
                            .font(.system(size: 9.5)).foregroundColor(t.color(t.faint))
                    }
                    let va = Int(intervalA.filter(\.isNumber)) ?? -1
                    let vb = Int(intervalB.filter(\.isNumber)) ?? -1
                    if (intervalA.isEmpty && intervalB.isEmpty) == false {
                        let badA = !intervalA.isEmpty && !(1...86400).contains(va)
                        let badB = !intervalB.isEmpty && !(1...86400).contains(vb)
                        if badA || badB {
                            Text("要在 1 ~ 86400 之间").font(.system(size: 10.5)).foregroundColor(t.color(t.danger))
                        }
                    }
                    SheetNote(text: "只影响之后新建的任务。已经在队列里跑的任务各自带着自己的间隔,要改单个任务去「抢购 → 队列」点那条任务的秒数。", tint: t.muted)
                    ActBtn(kind: .primary, icon: "checkmark", label: snipeSaveBusy ? "保存中…" : "保存抢购设置", busy: snipeSaveBusy) {
                        await saveSnipeDefaults()
                    }
                }
            }
        }
        .task {
            guard !snipeLoaded else { return }
            snipeDefaults = try? await conn.client.getDict("/settings")
            if let d = snipeDefaults {
                intervalA = "\(numToDoubleAny(d["defaultRetryInterval"]).map(Int.init) ?? 60)"
                intervalB = "\(numToDoubleAny(d["quickOrderRetryInterval"]).map(Int.init) ?? 2)"
            }
            snipeLoaded = true
        }
    }

    private func saveSnipeDefaults() async {
        let va = Int(intervalA.filter(\.isNumber)) ?? 60
        let vb = Int(intervalB.filter(\.isNumber)) ?? 2
        guard (1...86400).contains(va), (1...86400).contains(vb) else {
            toast.show("要在 1 ~ 86400 之间", error: true)
            return
        }
        snipeSaveBusy = true
        defer { snipeSaveBusy = false }
        // 后端 POST /settings 是全量覆盖 —— 必须先读旧配置合并,否则会清空 TG/Webhook 等。
        // 读失败时宁可不保存也不能拿空字典顶上(那等于一键抹掉全部配置)
        guard let old: [String: Any] = try? await conn.client.getDict("/settings") else {
            toast.show("读不到当前配置,已跳过保存(避免用空值覆盖);请检查网络后重试", error: true)
            return
        }
        var merged = old
        merged["defaultRetryInterval"] = va
        merged["quickOrderRetryInterval"] = vb
        let body = try? JSONSerialization.data(withJSONObject: merged)
        let (ok2, msg) = await conn.client.actionPostData("/settings", bodyData: body)
        toast.show(ok2 ? "设置已保存" : (msg.isEmpty ? "保存失败" : msg), error: !ok2)
    }

    // MARK: 关于(版本 + 构建时间;一眼判断跑的是不是最新构建)

    private var aboutCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 9) {
                SectionTitle(text: "关于")
                KV(k: "版本", v: "v\(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "—")")
                KV(k: "构建时间", v: buildTime)
                Text("构建时间 = 本次安装的二进制生成时刻。重新运行后如果这里没变,说明跑的还是旧构建。")
                    .font(.system(size: 10)).foregroundColor(t.color(t.faint))
            }
        }
    }

    /// 可执行文件的修改时刻 ≈ 构建时刻
    private var buildTime: String {
        guard let exec = Bundle.main.executableURL,
              let attrs = try? FileManager.default.attributesOfItem(atPath: exec.path),
              let date = attrs[.modificationDate] as? Date else { return "—" }
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm:ss"
        return f.string(from: date)
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
    @State private var tgToken = ""
    @State private var tgChatId = ""
    @State private var webhookUrl = ""
    @State private var saveBusy = false

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                if loading {
                    ProgressView().padding(.top, 50)
                } else if let e = err {
                    Card { LoadFailed(message: e) { Task { await load() } } }
                } else {
                // TG 配置表单(SET-053/054)
                Card {
                    VStack(alignment: .leading, spacing: 10) {
                        SectionTitle(text: "Telegram 配置")
                        VStack(alignment: .leading, spacing: 5) {
                            Text("Bot Token").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.muted))
                            SheetField(placeholder: "123456:ABCdef...", text: $tgToken, mono: true)
                        }
                        VStack(alignment: .leading, spacing: 5) {
                            Text("Chat ID").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.muted))
                            SheetField(placeholder: "-1001234567890", text: $tgChatId, mono: true, keyboard: .numbersAndPunctuation)
                        }
                        ActBtn(kind: .primary, icon: "checkmark", label: saveBusy ? "保存中…" : "保存设置", busy: saveBusy) {
                            await saveNotify()
                        }
                    }
                }
                // Webhook(SET-056)
                Card {
                    VStack(alignment: .leading, spacing: 10) {
                        SectionTitle(text: "自定义 Webhook")
                        SheetField(placeholder: "https://your.server/notify 或钉钉/飞书机器人地址", text: $webhookUrl, mono: true)
                        Text("方向是本程序 → 这个地址。发的是一个 JSON POST,同一条文本同时放进 text / message / text_content.text 几个字段。注意「一键下单」按钮只有 Telegram 有。")
                            .font(.system(size: 9.5)).foregroundColor(t.color(t.faint))
                        Text("会往所有已配置的通道各发一条。先保存设置再测 —— 测的是已保存的配置。")
                            .font(.system(size: 9.5)).foregroundColor(t.color(t.faint))
                    }
                }

                    if let chList = channels?["channels"] as? [[String: Any]] {
                        Card {
                            VStack(spacing: 10) {
                                SectionTitle(text: "通道状态")
                                ForEach(chList.indices, id: \.self) { i in channelRowObj(chList[i]) }
                                if chList.isEmpty {
                                    Text("没有配置任何通道").font(.system(size: 11)).foregroundColor(t.color(t.faint))
                                }
                            }
                        }
                    }
                    if let p = poller {
                        Card {
                            VStack(alignment: .leading, spacing: 9) {
                                SectionTitle(text: "Telegram 长轮询")
                                KV(k: "状态", v: (p["running"] as? Bool ?? false) ? "运行中" : "已停止")
                                if let last = p["lastPollAt"] as? String { KV(k: "最近拉取", v: fmtDate(last)) }
                                if let off = p["offset"] as? Int {
                                    KV(k: "已确认 update_id", v: "\(off)")
                                }
                                if let e2 = p["lastError"] as? String, !e2.isEmpty {
                                    if e2.contains("409") || e2.lowercased().contains("conflict") {
                                        Text("这是同一个 Bot Token 有另一个进程也在收:两边会互相把对方踢下线,表现就是「一键下单」按钮时灵时不灵、消息随机丢。先停掉另一份程序(另一台机器 / 另一个容器 / 本地调试进程),或者给这一份换一个 Bot Token。")
                                            .font(.system(size: 10)).foregroundColor(t.color(t.danger))
                                    } else {
                                        KV(k: "上次错误", v: e2)
                                    }
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
                    Text("会往所有已配置的通道各发一条。先保存设置再测 —— 测的是已保存的配置,不是输入框里的。")
                        .font(.system(size: 9.5)).foregroundColor(t.color(t.faint))
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

    /// handler:{"channels":[{name, configured, ok, detail}]}
    private func channelRowObj(_ ch: [String: Any]) -> some View {
        let name = ch["name"] as? String ?? "—"
        let configured = ch["configured"] as? Bool ?? false
        let good = ch["ok"] as? Bool ?? false
        let icon = name.lowercased().contains("telegram") ? "paperplane.fill" : "link"
        return HStack(spacing: 9) {
            Image(systemName: icon).font(.system(size: 13)).foregroundColor(t.color(configured && good ? t.success : t.faint))
            VStack(alignment: .leading, spacing: 1) {
                Text(name.capitalized).font(.system(size: 12.5)).foregroundColor(t.color(t.fg))
                if let d = ch["detail"] as? String, !d.isEmpty {
                    Text(d).font(.system(size: 10)).foregroundColor(t.color(t.muted)).lineLimit(1)
                }
            }
            Spacer()
            if !configured {
                Chip(text: "未配置")
            } else {
                Chip(text: good ? "可用" : "不可用", color: good ? t.success : t.danger)
            }
        }
    }

    private func sendTest() async {
        testing = true
        defer { testing = false }
        let r = try? await conn.client.post("/monitor/test-notification")
        // 后端契约:{delivered, channels:[{name, ok, error?}], message?}
        let delivered = numToDoubleAny(r?["delivered"]).map(Int.init) ?? 0
        let chs = (r?["channels"] as? [[String: Any]]) ?? []
        let failed = chs.filter { !(($0["ok"] as? Bool) ?? false) }
        if delivered == 0 {
            let reason = failed.compactMap { "\($0["name"] ?? "?"):\($0["error"] as? String ?? "不可用")" }.joined(separator: ";")
            toast.show(reason.isEmpty ? "一条都没发出去。请在设置页配置 Telegram 或 Webhook" : "一条都没发出去:\(reason)", error: true)
        } else if !failed.isEmpty {
            let names = failed.compactMap { $0["name"] as? String }.joined(separator: "、")
            let reason = failed.first?["error"] as? String ?? ""
            toast.show("\(delivered) 条已送达,但 \(names) 失败:\(reason)", error: true)
        } else {
            toast.show("已发往 \(delivered) 个通道")
        }
    }

    private func saveNotify() async {
        guard !tgToken.isEmpty || !webhookUrl.isEmpty else {
            toast.show("至少填一项(TG Token 或 Webhook)", error: true)
            return
        }
        saveBusy = true
        defer { saveBusy = false }
        // 全量合并(同上):只发改动字段会把其它配置清空;读失败直接不保存,不拿空字典顶上
        guard let old: [String: Any] = try? await conn.client.getDict("/settings") else {
            toast.show("读不到当前配置,已跳过保存(避免用空值覆盖);请检查网络后重试", error: true)
            return
        }
        var merged = old
        // 空串 = 显式清空该项(web 同款语义:清空输入框保存即清空配置)
        merged["tgToken"] = tgToken
        merged["tgChatId"] = tgChatId
        merged["notifyWebhookUrl"] = webhookUrl
        let data = try? JSONSerialization.data(withJSONObject: merged)
        let (ok2, msg) = await conn.client.actionPostData("/settings", bodyData: data)
        toast.show(ok2 ? "设置已保存" : (msg.isEmpty ? "保存失败" : msg), error: !ok2)
        await load()
    }

    private func load() async {
        do {
            async let c = conn.client.getDict("/notify/channels?verify=true")
            async let p = conn.client.getDict("/telegram/poller")
            async let s = conn.client.getDict("/settings")
            let (cr, pr, sr) = try await (c, p, s)
            channels = cr
            poller = (pr["poller"] as? [String: Any]) ?? pr
            if tgToken.isEmpty, let tok = sr["tgToken"] as? String, !tok.isEmpty { tgToken = tok }
            if tgChatId.isEmpty, let cid = sr["tgChatId"] as? String, !cid.isEmpty { tgChatId = cid }
            if webhookUrl.isEmpty, let w = sr["notifyWebhookUrl"] as? String, !w.isEmpty { webhookUrl = w }
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
    @State private var codeExpiresAt = Date.distantPast
    @State private var tick = 0
    private var codeCountdown: String {
        let left = Int(codeExpiresAt.timeIntervalSinceNow)
        if left <= 0 { return "已过期" }
        return String(format: "%d:%02d", left / 60, left % 60)
    }
    @State private var err: String?
    @State private var loading = true
    @State private var generating = false
    @State private var revokeId: String?

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
                            Text("剩 \(codeCountdown) · 手填也行:App 端输入框直接打这 8 位")
                                .font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.accent))
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
        .sheet(item: Binding(
            get: { revokeId.map { RevokeWrap(id: $0) } },
            set: { revokeId = $0?.id }
        )) { w in
            ConfirmSheet(title: "吊销设备", message: "该设备令牌立即失效,需要重新配对。", confirmText: "确认吊销") {
                let (ok, msg) = await conn.client.actionDelete("/app/devices/\(w.id)")
                toast.show(ok ? "已吊销" : (msg.isEmpty ? "失败" : msg), error: !ok)
                await load()
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .task {
            await load()
            // 配对码 2 分钟倒计时逐秒推进
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 1_000_000_000)
                tick &+= 1
            }
        }
    }

    private func deviceRow(_ d: [String: Any]) -> some View {
        let id = String(describing: d["id"] ?? d["deviceId"] ?? "")
        let revoked = d["revoked"] as? Bool ?? false
        return VStack(alignment: .leading, spacing: 5) {
            HStack {
                Image(systemName: "iphone").font(.system(size: 14)).foregroundColor(t.color(revoked ? t.faint : t.muted))
                Text(d["name"] as? String ?? "设备")
                    .font(.system(size: 12.5, weight: .semibold))
                    .foregroundColor(t.color(revoked ? t.faint : t.fg))
                    .strikethrough(revoked)
                if revoked { Chip(text: "已吊销") }
                Spacer()
                if !revoked {
                    Button { revokeId = id } label: {
                        Text("吊销").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.danger))
                            .padding(.horizontal, 10).padding(.vertical, 5)
                            .background(Capsule().stroke(t.color(t.danger), lineWidth: 1))
                    }.buttonStyle(.plain)
                }
            }
            if !revoked {
                HStack(spacing: 10) {
                    if let t2 = d["createdAt"] as? String ?? d["pairedAt"] as? String { Text("配对于 \(fmtDate(t2))") }
                    if let t3 = d["lastUsedAt"] as? String, !t3.isEmpty { Text("最近使用 \(fmtDate(t3))") }
                    Spacer()
                }
                .font(.system(size: 9.5)).foregroundColor(t.color(t.faint))
            }
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
            if let ttl = numToDoubleAny(r["expiresIn"] ?? r["ttl"]), ttl > 0 {
                codeExpiresAt = Date().addingTimeInterval(ttl)
            } else {
                codeExpiresAt = Date().addingTimeInterval(120)
            }
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
        // host 里的 :/#/& 等必须走 queryItems 编码,手拼会坏
        var comp = URLComponents()
        comp.scheme = "ovhconsole"; comp.host = "pair"
        comp.queryItems = [
            URLQueryItem(name: "host", value: host),
            URLQueryItem(name: "code", value: code),
            URLQueryItem(name: "auto", value: "1"),
        ]
        guard let deep = comp.url?.absoluteString else { return nil }
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
                    // handler:{backend:{serverCount,timestamp}, sqlite:{serverCount,path}}
                    let be = (i["backend"] as? [String: Any]) ?? [:]
                    let sq = (i["sqlite"] as? [String: Any]) ?? [:]
                    let beCount = numToDoubleAny(be["serverCount"]).map(Int.init) ?? 0
                    let sqCount = numToDoubleAny(sq["serverCount"]).map(Int.init) ?? 0
                    Card {
                        VStack(spacing: 9) {
                            SectionTitle(text: "缓存状态")
                        Text("缓存只指 OVH 服务器目录。订阅 / 队列 / 历史 等业务数据不在此清理范围内。")
                            .font(.system(size: 9.5)).foregroundColor(t.color(t.faint))
                            KV(k: "内存缓存", v: "\(beCount) 条")
                            KV(k: "SQLite", v: "\(sqCount) 条")
                            if let p = sq["path"] as? String { KV(k: "数据库", v: p, mono: true) }
                            if let ts = numToDoubleAny(be["timestamp"]), ts > 0 {
                                KV(k: "最近刷新", v: fmtDate(isoFromUnix(ts)))
                            }
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
                                let holder = [(i["firstname"] as? String ?? ""), (i["name"] as? String ?? "")].filter { !$0.isEmpty }.joined(separator: " ")
                                if !holder.isEmpty {
                                    KV(k: "持有人", v: holder)
                                }
                                if let city = i["city"] as? String {
                                    KV(k: "地址", v: "\(city) \(i["country"] as? String ?? "")")
                                }
                                if let sub = i["ovhSubsidiary"] as? String, !sub.isEmpty {
                                    KV(k: "OVH 子公司", v: sub)
                                }
                                if let cur = (i["currency"] as? [String: Any])?["code"] as? String, !cur.isEmpty {
                                    KV(k: "结算币种", v: cur)
                                }
                            }
                        }
                        if let sub = i["subsidiaryMismatch"] as? Bool, sub {
                            VStack(alignment: .leading, spacing: 4) {
                                Text("账户子公司配置与 OVH 实际归属不一致").font(.system(size: 11.5, weight: .bold)).foregroundColor(t.color(t.warning))
                                Text("OVH 返回的 ovhSubsidiary 与设置页里给这个账户填的子公司(zone)不同。EU / US / CA 是三套互不相通的系统,目录、价格、币种、库存、下单 region 全部由子公司决定 —— 请去网页端「设置 → OVH 账户」把 zone 改对。")
                                    .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                            }
                            .padding(10)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.warning).opacity(0.5), lineWidth: 1))
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
                            let price = r["priceWithoutTax"] as? [String: Any]
                            Card {
                                VStack(spacing: 6) {
                                    HStack {
                                        Text(r["refundId"] as? String ?? "—").font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                                        Spacer()
                                        if let txt = price?["text"] as? String {
                                            Text(txt).font(.system(size: 12, weight: .bold, design: .rounded)).foregroundColor(t.color(t.success))
                                        } else if let v = numToDoubleAny(price?["value"]) {
                                            Text(String(format: "%.2f %@", v, price?["currencyCode"] as? String ?? ""))
                                                .font(.system(size: 12, weight: .bold, design: .rounded)).foregroundColor(t.color(t.success))
                                        }
                                    }
                                    KV(k: "日期", v: fmtDate(r["date"] as? String))
                                    if let oid = r["orderId"] as? String, !oid.isEmpty {
                            Chip(text: "订单 \(oid)", mono: true)
                        }
                        if let url = r["pdfUrl"] as? String, let u = URL(string: url) {
                                        Link(destination: u) {
                                            HStack(spacing: 4) {
                                                Image(systemName: "arrow.down.doc").font(.system(size: 10))
                                                Text("下载 PDF").font(.system(size: 11, weight: .semibold))
                                            }.foregroundColor(t.color(t.info))
                                        }
                                    }
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
            async let e = conn.client.getArray("/ovh/account/email-history", timeoutSec: 30)
            async let r = conn.client.getArray("/ovh/account/refunds")
            let (ir, er, rr) = try await (i, e, r)
            info = ir["info"] as? [String: Any] ?? ir
            emails = er
            refunds = rr
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
    @EnvironmentObject var toast: Toast
    @AppStorage("ovh_mask_ip") private var mask = false
    var t: Tokens { theme.t }

    @State private var logs: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true
    @State private var auto = false
    @State private var level = 0
    @State private var search = ""
    @State private var logClearConfirm = false

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
                    Text("所有级别").tag(0)
                    Text("INFO").tag(1)
                    Text("WARNING").tag(2)
                    Text("ERROR").tag(3)
                    Text("DEBUG").tag(4)
                }
                .pickerStyle(.segmented)

                HStack {
                    Text("\(filtered.count) 条\(err != nil ? " · 刷新失败,显示旧内容" : "")")
                        .font(.system(size: 10.5)).foregroundColor(t.color(err != nil ? t.warning : t.faint))
                    Spacer()
                    Toggle(isOn: $auto) {
                        Text("自动刷新(5 秒)").font(.system(size: 11)).foregroundColor(t.color(t.muted))
                    }.toggleStyle(.button).tint(t.color(t.accent))
                    Button { logClearConfirm = true } label: {
                        Text("清空").font(.system(size: 11.5)).foregroundColor(t.color(t.danger))
                    }.buttonStyle(.plain)
                }

                if let e = err, logs.isEmpty {
                    Card { LoadFailed(message: e) { Task { await load() } } }
                } else if filtered.isEmpty && !loading {
                    Card { EmptyHint(icon: "doc.text", text: logs.isEmpty ? "没有日志" : "没有匹配的日志(共 \(logs.count) 条,当前筛选条件下一条都没命中)") }
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
        .sheet(isPresented: $logClearConfirm) {
            ConfirmSheet(title: "清空日志", message: "删除后端全部日志记录,排障历史不可恢复。", confirmText: "确认清空") {
                _ = await conn.client.actionDelete("/logs")
                await load()
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .task {
            await load()
            // 自动刷新 5 秒轮询
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 5_000_000_000)
                if Task.isCancelled { break }   // 取消发生在 sleep 中时不再多发请求
                if auto { await load() }
            }
        }
    }

    private var filtered: [[String: Any]] {
        var list = logs
        if level > 0 {
            let lvName = ["", "info", "warning", "error", "debug"][level]
            list = list.filter { raw in
                let l = (raw["level"] as? String ?? "").lowercased()
                if l == "warn" { return lvName == "warning" }
                return l == lvName
            }
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

/// 顶栏账户切换:轻量文字按钮(色点+名称+小箭头)。
/// 不加底色/边框/内边距 —— 那些装饰会让系统给的 toolbar 宽度装不下而吞掉文字,
/// 且和导航栏风格不融合。
struct AccountButton: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let onTap: () -> Void
    var t: Tokens { theme.t }

    var body: some View {
        Button(action: onTap) {
            HStack(spacing: 5) {
                if let acc = conn.activeAccount {
                    Circle().fill(t.color(zoneTint(acc))).frame(width: 6, height: 6)
                    Text(acc["name"] as? String ?? "账户")
                        .font(.system(size: 13.5, weight: .semibold))
                        .lineLimit(1)
                    Image(systemName: "chevron.down")
                        .font(.system(size: 8, weight: .bold))
                        .foregroundColor(t.color(t.faint))
                } else {
                    Image(systemName: "person.crop.circle.badge.plus").font(.system(size: 13, weight: .semibold))
                    Text("添加账户").font(.system(size: 13.5, weight: .semibold))
                }
            }
            .foregroundColor(t.color(t.fg))
            .fixedSize(horizontal: true, vertical: false)
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
        return Button {
            if isActive { dismiss(); return }
            conn.setAccount(id)
            toast.show("已切换到 \(acc["name"] as? String ?? "")")
            dismiss()
        } label: {
            HStack(spacing: 10) {
                Circle().fill(t.color(tint)).frame(width: 9, height: 9)
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

/// 设备吊销确认的标识
private struct RevokeWrap: Identifiable {
    let id: String
}

/// unix 秒 → ISO(缓存刷新时间用)
func isoFromUnix(_ ts: Double) -> String {
    ISO8601DateFormatter().string(from: Date(timeIntervalSince1970: ts))
}
