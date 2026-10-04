import SwiftUI

/**
 * 独服详情(全功能):概览 / 电源 / 维护 / 高级 四段。
 * 顶部服务胶囊条(到期 · 续费策略),对齐 web 独服控制台。
 */
struct ServerDetailView: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @AppStorage("ovh_mask_ip") private var mask = false
    let item: [String: Any]
    var t: Tokens { theme.t }

    var sn: String { item["serviceName"] as? String ?? "" }
    @State private var seg = 0
    @State private var serviceinfo: [String: Any]?
    @State private var sheet: ServerSheet?

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                headerCard
                Picker("", selection: $seg) {
                    Text("概览").tag(0)
                    Text("电源").tag(1)
                    Text("维护").tag(2)
                    Text("高级").tag(3)
                }
                .pickerStyle(.segmented)

                switch seg {
                case 1: PowerSection(sn: sn, serviceinfo: serviceinfo, sheet: $sheet)
                case 2: MaintenanceSection(sn: sn, sheet: $sheet)
                case 3: AdvancedSection(sn: sn, sheet: $sheet)
                default: OverviewSection(sn: sn, item: item, mask: mask, serviceinfo: serviceinfo, sheet: $sheet)
                }
            }
            .padding(16)
        }
        .background(t.color(t.bg))
        .navigationTitle(displayName)
        .navigationBarTitleDisplayMode(.inline)
        .toolbarColorScheme(theme.dark ? .dark : .light, for: .navigationBar)
        .task { await load() }
        .refreshable { await load() }
        .sheet(item: $sheet) { s in serverSheet(s) }
    }

    private var displayName: String {
        let n = item["name"] as? String ?? ""
        if n != sn, !n.isEmpty { return n.components(separatedBy: " | ").first ?? n }
        return sn
    }

    // MARK: 顶部卡

    private var headerCard: some View {
        let state = item["state"] as? String ?? ""
        let ok = ["ok", "active"].contains(state.lowercased())
        return Card {
            VStack(spacing: 11) {
                HStack(spacing: 10) {
                    RoundedRectangle(cornerRadius: 10)
                        .fill(t.color(ok ? t.success : t.danger).opacity(0.16))
                        .frame(width: 42, height: 42)
                        .overlay(Image(systemName: "server.rack").font(.system(size: 18, weight: .semibold)).foregroundColor(t.color(ok ? t.success : t.danger)))
                    VStack(alignment: .leading, spacing: 3) {
                        Text(displayName).font(.system(size: 16, weight: .bold)).foregroundColor(t.color(t.fg)).lineLimit(1)
                        Text("\(sn) · \((item["datacenter"] as? String ?? "—").uppercased())")
                            .font(.system(size: 10.5, design: .monospaced)).foregroundColor(t.color(t.muted)).lineLimit(1)
                    }
                    .onLongPressGesture { sheet = .init(kind: .alias) }   // S-008:长按=web 右键设别名
                    Spacer()
                    Chip(text: state.uppercased(), color: ok ? t.success : t.danger)
                }
                HStack(spacing: 6) {
                    Dot(color: ok ? t.success : t.danger)
                    Text(mask ? maskIP(item["ip"] as? String ?? "") : (item["ip"] as? String ?? "—"))
                        .font(.system(size: 12.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                    Spacer()
                    // OS 胶囊(S-016):点击进重装
                    if let os = item["os"] as? String, !os.isEmpty {
                        Button { sheet = .init(kind: .reinstall) } label: {
                            HStack(spacing: 3) {
                                Image(systemName: "terminal").font(.system(size: 9))
                                Text(os).font(.system(size: 10.5, design: .monospaced)).lineLimit(1)
                            }
                            .foregroundColor(t.color(t.info))
                            .padding(.horizontal, 8).padding(.vertical, 4)
                            .background(Capsule().fill(t.color(t.info).opacity(0.1)))
                        }.buttonStyle(.plain)
                    }
                    // 续费胶囊可点(S-015)
                    Button { sheet = .init(kind: .renewal) } label: {
                        Text(renewalText).font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.accent))
                    }.buttonStyle(.plain)
                }
                if serviceinfo == nil {
                    // S-013:胶囊骨架
                    HStack(spacing: 6) {
                        ForEach(0..<4, id: \.self) { _ in
                            RoundedRectangle(cornerRadius: 8).fill(t.color(t.surfaceMuted))
                                .frame(width: 72, height: 24)
                        }
                        Spacer()
                    }
                    .padding(.top, 4)
                }
                HStack(spacing: 6) {
                    // S-014 撤单倒计时胶囊(eligible 才显示,点击直达)
                    if (retraction?["eligible"] as? Bool) == true {
                        Button { sheet = .init(kind: .retraction) } label: {
                            HStack(spacing: 3) {
                                Image(systemName: "clock.badge.exclamationmark").font(.system(size: 9))
                                Text("可撤单 · \(retractionLeftText)").font(.system(size: 10.5, weight: .semibold))
                            }
                            .foregroundColor(t.color(t.warning))
                            .padding(.horizontal, 8).padding(.vertical, 4)
                            .background(Capsule().fill(t.color(t.warning).opacity(0.12)))
                        }.buttonStyle(.plain)
                    }
                    // S-018 OVH 监控三态胶囊
                    Button {
                        if monitoringOn == nil {
                            Task {   // 读失败时点击只重试,不发指令(防未知当 false 反向关掉)
                                monitoringLoading = true
                                if let r = try? await conn.client.getDict("/server-control/\(sn)/monitoring") {
                                    monitoringOn = r["monitoring"] as? Bool
                                }
                                monitoringLoading = false
                            }
                        } else {
                            sheet = .init(kind: .monitoring)
                        }
                    } label: {
                        HStack(spacing: 3) {
                            Image(systemName: "bell.badge").font(.system(size: 9))
                            Text(monitoringLabel).font(.system(size: 10.5, weight: .semibold))
                        }
                        .foregroundColor(t.color(monitoringColor))
                        .padding(.horizontal, 8).padding(.vertical, 4)
                        .background(Capsule().fill(t.color(monitoringColor).opacity(0.12)))
                    }.buttonStyle(.plain)
                    Spacer()
                }
                .padding(.top, 2)
                if let si = serviceinfo {
                    FlowLayout(spacing: 6) {
                        if let exp = si["expiration"] as? String, !exp.isEmpty {
                            Chip(text: "到期 \(fmtDate(exp))", color: daysLeft(exp) < 7 ? t.danger : t.muted)
                        }
                        if (si["terminationScheduled"] as? Bool ?? false) || (si["renewalDeleteAtExpiration"] as? Bool ?? false) {
                            Chip(text: "到期将终止", color: t.danger)
                        }
                        if si["renewalForced"] as? Bool == true {
                            Chip(text: "OVH 强制续费", color: t.warning)
                        }
                    }
                }
            }
        }
    }

    private var monitoringLabel: String {
        if monitoringLoading { return "监控 读取中…" }
        if monitoringOn == nil { return "监控 状态未知 · 重试" }
        return monitoringOn! ? "监控 已开" : "监控 已关"
    }
    private var monitoringColor: String {
        if monitoringLoading { return t.muted }
        if monitoringOn == nil { return t.danger }
        return monitoringOn! ? t.success : t.faint
    }

    /// 撤单剩余窗口(retractionDate 与当前差)
    private var retractionLeftText: String {
        guard let deadline = retraction?["retractionDate"] as? String ?? retraction?["deadline"] as? String else {
            return "窗口内"
        }
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        for fmt in ["yyyy-MM-dd'T'HH:mm:ssZZZZZ", "yyyy-MM-dd"] {
            f.dateFormat = fmt
            if let d = f.date(from: deadline) {
                let hours = Int(d.timeIntervalSinceNow / 3600)
                return hours >= 24 ? "\(hours / 24) 天" : "\(max(0, hours)) 小时"
            }
        }
        return "窗口内"
    }

    private var renewalText: String {
        if let si = serviceinfo {
            // 终止态优先(S-015 五种细分)
            let term = terminationLabel(si)
            if !term.isEmpty { return term }
            if si["renewalForced"] as? Bool == true {
                let p = si["renewalPeriod"] as? Int ?? 1
                return "强制自动 · \(p)月"
            }
            if si["renewalType"] as? Bool == true {
                let p = si["renewalPeriod"] as? Int ?? 1
                return "自动 · \(p)月"
            }
            if si["renewalType"] == nil { return "续费未知" }   // 没读到 ≠ 手动
            return "手动"
        }
        return ""
    }

    private func terminationLabel(_ si: [String: Any]) -> String {
        let scheduled = si["terminationScheduled"] as? Bool ?? false
        let deleteAtExp = si["renewalDeleteAtExpiration"] as? Bool ?? false
        guard scheduled || deleteAtExp else { return "" }
        if let policy = si["terminationPolicy"] as? String {
            switch policy.lowercased() {
            case "terminateservice": return "终止处理中(立即)"
            case "terminateatengagementdate": return "合同期结束终止"
            case "terminateatexpirationdate": return "到期终止"
            default: break
            }
        }
        return deleteAtExp ? "到期终止" : "已安排终止"
    }

    private func daysLeft(_ iso: String) -> Int {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd'T'HH:mm:ssZZZZZ"
        var d = f.date(from: iso.hasSuffix("Z") ? String(iso.dropLast()) + "+0000" : iso)
        if d == nil {
            let f2 = DateFormatter()
            f2.dateFormat = "yyyy-MM-dd"   // 到期日常是纯日期
            d = f2.date(from: iso)
        }
        guard let date = d else { return 999 }
        return Calendar.current.dateComponents([.day], from: Date(), to: date).day ?? 999
    }

    @State private var retraction: [String: Any]? = nil
    @State private var monitoringOn: Bool? = nil
    @State private var monitoringLoading = true

    private func load() async {
        // 不缓存:refreshable / 保存续费后回来都要拿到最新状态
        async let si = conn.client.getDict("/server-control/\(sn)/serviceinfo")
        async let rt = conn.client.getDict("/server-control/\(sn)/retraction", timeoutSec: 20)
        async let mo = conn.client.getDict("/server-control/\(sn)/monitoring")
        if let r = try? await si {
            serviceinfo = (r["serviceInfo"] as? [String: Any]) ?? r
        }
        if let r = try? await rt { retraction = r }
        if let r = try? await mo { monitoringOn = r["monitoring"] as? Bool }
        monitoringLoading = false
    }

    // MARK: sheet 调度

    @ViewBuilder
    private func serverSheet(_ s: ServerSheet) -> some View {
        switch s.kind {
        case .mrtg: MrtgSheet(sn: sn)
        case .reboot: ConfirmSheet(title: "硬重启服务器", message: "相当于直接断电再通电:未保存数据会丢失,磁盘检查可能耗时数分钟。", confirmText: "确认重启") {
            let (ok, msg) = await conn.client.actionPostData("/server-control/\(sn)/reboot", bodyData: nil)
            toast.show(ok ? "重启指令已下发" : (msg.isEmpty ? "失败" : msg), error: !ok)
        }
        case .rescue: RescueSheet(sn: sn)
        case .reinstall: ServerReinstallSheet(sn: sn)
        case .ipmi: IpmiSheet(sn: sn)
        case .bootMode: BootModeSheet(sn: sn)
        case .spla: SplaSheet(sn: sn)
        case .tasks: TasksSheet(sn: sn)
        case .bios: BiosSheet(sn: sn)
        case .installStatus: InstallStatusSheet(sn: sn)
        case .retraction: RetractionSheet(sn: sn)
        case .renewal: RenewalSheet(sn: sn, isVps: false, info: serviceinfo ?? [:])
        case .networkSpecs: JsonSheet(title: "网络规格", icon: "network", path: "/server-control/\(sn)/network-specs")
        case .engagement: EngagementSheet(sn: sn, isVps: false)
        case .hwReplace: HardwareReplaceSheet(sn: sn)
        case .changeContact: ChangeContactSheet(sn: sn, isVps: false)
        case .monitoring: MonitoringSheet(sn: sn)
        case .burst: ToggleSheet(title: "Burst 流量", icon: "bolt.horizontal", getPath: "/server-control/\(sn)/burst", putPath: "/server-control/\(sn)/burst")
        case .firewall: ToggleSheet(title: "网络防火墙", icon: "flame", getPath: "/server-control/\(sn)/firewall", putPath: "/server-control/\(sn)/firewall")
        case .backupFtp: BackupFtpSheet(sn: sn)
        case .secondaryDns: JsonSheet(title: "二级 DNS", icon: "globe", path: "/server-control/\(sn)/secondary-dns")
        case .virtualMac: JsonSheet(title: "虚拟 MAC", icon: "macpro.gen3", path: "/server-control/\(sn)/virtual-mac")
        case .vrack: JsonSheet(title: "vRack 成员", icon: "link", path: "/server-control/\(sn)/vrack")
        case .orderableBandwidth: JsonSheet(title: "可购带宽", icon: "speedometer", path: "/server-control/\(sn)/orderable/bandwidth")
        case .orderableTraffic: JsonSheet(title: "可购流量", icon: "arrow.up.arrow.down", path: "/server-control/\(sn)/orderable/traffic")
        case .orderableIp: JsonSheet(title: "可购 IP 块", icon: "number.square", path: "/server-control/\(sn)/orderable/ip")
        case .options: JsonSheet(title: "附加选项", icon: "shippingbox", path: "/server-control/\(sn)/options")
        case .ipSpecs: JsonSheet(title: "IP 规格", icon: "number", path: "/server-control/\(sn)/ip-specs")
        case .mitigation: MitigationSheet(sn: sn, isVps: false)
        case .alias: ServerAliasSheet(sn: sn, current: item["name"] as? String ?? "")
        }
    }
}

/// 独服弹窗枚举
struct ServerSheet: Identifiable {
    enum Kind {
        case mrtg, reboot, rescue, reinstall, ipmi, bootMode, spla, tasks, bios, installStatus
        case retraction, renewal, networkSpecs, engagement, hwReplace, changeContact, monitoring
        case burst, firewall, backupFtp, secondaryDns, virtualMac, vrack
        case orderableBandwidth, orderableTraffic, orderableIp, options, ipSpecs, mitigation
        case alias
    }
    let kind: Kind
    var id: Kind { kind }
}

// MARK: - 概览段

struct OverviewSection: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let sn: String
    let item: [String: Any]
    let mask: Bool
    let serviceinfo: [String: Any]?
    @Binding var sheet: ServerSheet?
    var t: Tokens { theme.t }

    @State private var hardware: [String: Any]?
    @State private var ips: [[String: Any]] = []
    @State private var nics: [[String: Any]] = []
    @State private var err: String?

    var body: some View {
        VStack(spacing: 12) {
            if let e = err { Card { LoadFailed(message: e) { Task { await load() } } } }

            Card {
                VStack(spacing: 9) {
                    HStack {
                        SectionTitle(text: "硬件")
                    }
                    if let hw = hardware {
                        KV(k: "处理器", v: "\(hw["processorName"] ?? "—")")
                        KV(k: "核心", v: "\(hw["numberOfProcessors"] ?? 0)×\(hw["coresPerProcessor"] ?? 0) 核 / \(hw["threadsPerProcessor"] ?? 0) 线程")
                        KV(k: "内存", v: memText(hw["memorySize"]))
                        KV(k: "主板", v: "\(hw["motherboard"] ?? "—")")
                        if let groups = hw["diskGroups"] as? [[String: Any]] {
                            ForEach(groups.indices, id: \.self) { i in
                                KV(k: "磁盘组 \(i+1)", v: diskText(groups[i]))
                            }
                        }
                    } else {
                        ProgressView().padding(6)
                    }
                }
            }

            Card {
                VStack(alignment: .leading, spacing: 10) {
                    HStack {
                        SectionTitle(text: "流量(网卡)")
                        Spacer()
                        Button { sheet = .init(kind: .mrtg) } label: {
                            Image(systemName: "arrow.up.left.and.arrow.down.right")
                                .font(.system(size: 12, weight: .semibold))
                                .foregroundColor(t.color(t.muted))
                        }.buttonStyle(.plain)
                    }
                    MrtgChartView(sn: sn, compact: true)
                }
            }

            Card {
                VStack(spacing: 8) {
                    SectionTitle(text: "IP 地址")
                    if ips.isEmpty {
                        // S-021:接口失败/空时回退列表自带主 IP
                        if let fallback = item["ip"] as? String, !fallback.isEmpty {
                            ipRow(["ip": fallback, "type": "IPv4"])
                        } else {
                            Text(err == nil ? "没有 IP 数据" : "—").font(.system(size: 11)).foregroundColor(t.color(t.faint))
                        }
                    }
                    ForEach(ips.indices, id: \.self) { i in ipRow(ips[i]) }
                }
            }

            Card {
                VStack(spacing: 8) {
                    SectionTitle(text: "网络接口")
                    if nics.isEmpty {
                        Text("无网卡数据").font(.system(size: 11)).foregroundColor(t.color(t.faint))
                    }
                    ForEach(nics.indices, id: \.self) { i in
                        let nic = nics[i]
                        KV(k: mask ? maskIP(nic["mac"] as? String ?? "") : (nic["mac"] as? String ?? "—"),
                           v: nic["virtualNetworkInterface"] as? String ?? (nic["linkType"] as? String ?? "public"), mono: true)
                    }
                }
            }
        }
        .task { await load() }
    }

    private func ipRow(_ ip: [String: Any]) -> some View {
        let raw = ip["ip"] as? String ?? (ip["address"] as? String ?? "—")
        return VStack(spacing: 4) {
            HStack {
                Text(mask ? maskIP(raw) : raw).font(.system(size: 12, design: .monospaced)).foregroundColor(t.color(t.fg))
                Spacer()
                if let t2 = ip["type"] as? String, !t2.isEmpty { Chip(text: t2) }
            }
            if let routed = ip["routedTo"] as? String, !routed.isEmpty {
                Text("→ " + (mask ? maskIP(routed) : routed)).font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
            }
            if let desc = ip["description"] as? String, !desc.isEmpty {
                Text(desc).font(.system(size: 10)).foregroundColor(t.color(t.faint))
            }
        }
    }

    private func memText(_ mem: Any?) -> String {
        if let m = mem as? [String: Any],
           let unit = (m["unit"] as? String)?.lowercased(),
           let val = numToDoubleAny(m["value"]) {
            // OVH 常给 MB(32768),换算成人话
            if unit == "mb" && val >= 1024 { return String(format: "%.0f GB", val / 1024) }
            if unit == "gi" { return "\(Int(val)) GiB" }
            return "\(Int(val)) \(unit.uppercased())"
        }
        return mem.flatMap { "\($0)" } ?? "—"
    }

    private func diskText(_ g: [String: Any]) -> String {
        let n = (g["numberOfDisks"] as? Int) ?? (g["diskCount"] as? Int) ?? 0
        var capText = ""
        if let ds = g["diskSize"] as? [String: Any],
           let v = numToDoubleAny(ds["value"]) {
            let unit = (ds["unit"] as? String ?? "GB").uppercased()
            if unit == "MB" && v >= 1024 { capText = String(format: "%.0f GB", v / 1024) }
            else if unit == "GB" && v >= 1024 { capText = String(format: "%.1f TB", v / 1024) }
            else { capText = "\(Int(v)) \(unit)" }
        }
        let t2 = g["diskType"] as? String ?? (g["type"] as? String ?? "")
        let parts = [n > 0 ? "\(n) ×" : nil, capText.isEmpty ? nil : capText, !t2.isEmpty ? t2 : nil].compactMap { $0 }
        return parts.isEmpty ? "—" : parts.joined(separator: " ")
    }

    private func load() async {
        do {
            async let h = conn.client.getDict("/server-control/\(sn)/hardware")
            async let i = conn.client.getDict("/server-control/\(sn)/ips")
            async let n = conn.client.getDict("/server-control/\(sn)/network-interfaces")
            let (hr, ir, nr) = try await (h, i, n)
            hardware = hr["hardware"] as? [String: Any]
            ips = (ir["ips"] as? [[String: Any]]) ?? []
            nics = (nr["interfaces"] as? [[String: Any]]) ?? (nr["networkInterfaces"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
    }
}

// MARK: - 电源段(操作卡网格)

struct PowerSection: View {
    @EnvironmentObject var theme: Theme
    let sn: String
    let serviceinfo: [String: Any]?
    @Binding var sheet: ServerSheet?
    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 10) {
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 10) {
                opCard(.reboot, icon: "power", title: "硬重启", desc: "断电级重启", tint: t.danger)
                opCard(.rescue, icon: "lifepreserver", title: "救援系统", desc: "进入/退出救援", tint: t.warning)
                opCard(.reinstall, icon: "opticaldiscdrive.fill", title: "重装系统", desc: "全功能安装器", tint: t.danger)
                opCard(.ipmi, icon: "keyboard.onehanded.left", title: "IPMI/KVM", desc: "远程控制台", tint: t.info)
                opCard(.bootMode, icon: "memorychip", title: "启动模式", desc: "硬盘/救援/网络", tint: t.muted)
                opCard(.spla, icon: "pc", title: "Windows 授权", desc: "SPLA / GVLK", tint: t.info)
                opCard(.tasks, icon: "checklist", title: "运维任务", desc: "列表与预约", tint: t.muted)
                opCard(.bios, icon: "cpu", title: "BIOS 设置", desc: "含 SGX", tint: t.muted)
                opCard(.installStatus, icon: "progress.indicator", title: "安装进度", desc: "重装实时进度", tint: t.info)
                opCard(.monitoring, icon: "bell.badge", title: "OVH 监控", desc: "异常邮件通知", tint: t.muted)
            }
        }
    }

    private func opCard(_ kind: ServerSheet.Kind, icon: String, title: String, desc: String, tint: String) -> some View {
        Button { sheet = .init(kind: kind) } label: {
            VStack(alignment: .leading, spacing: 8) {
                RoundedRectangle(cornerRadius: 9)
                    .fill(t.color(tint).opacity(0.14))
                    .frame(width: 34, height: 34)
                    .overlay(Image(systemName: icon).font(.system(size: 14, weight: .semibold)).foregroundColor(t.color(tint)))
                Text(title).font(.system(size: 13, weight: .semibold)).foregroundColor(t.color(t.fg))
                Text(desc).font(.system(size: 10)).foregroundColor(t.color(t.muted)).lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(12)
            .background(RoundedRectangle(cornerRadius: 14).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 14).stroke(t.color(t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }
}

// MARK: - 维护段

struct MaintenanceSection: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let sn: String
    @Binding var sheet: ServerSheet?
    var t: Tokens { theme.t }

    @State private var interventions: [[String: Any]] = []
    @State private var err: String?

    var body: some View {
        VStack(spacing: 10) {
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 10) {
                mRow(.networkSpecs, icon: "network", title: "网络规格", desc: "带宽与 IP 路由")
                mRow(.renewal, icon: "arrow.triangle.2.circlepath", title: "续费策略", desc: "自动/手动/终止")
                mRow(.engagement, icon: "doc.plaintext", title: "合同期", desc: "承诺期管理")
                mRow(.hwReplace, icon: "wrench.and.screwdriver", title: "硬件更换", desc: "硬盘/内存/散热")
                mRow(.changeContact, icon: "person.2", title: "变更联系人", desc: "admin/tech/billing")
                mRow(.retraction, icon: "arrow.uturn.left.circle", title: "撤单", desc: "14 天无理由")
            }

            Card {
                VStack(spacing: 9) {
                    SectionTitle(text: "维护记录(近 20 条)")
                    if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else if interventions.isEmpty {
                        Text("没有维护记录").font(.system(size: 11)).foregroundColor(t.color(t.faint)).padding(.vertical, 8)
                    } else {
                        ForEach(interventions.prefix(20).indices, id: \.self) { i in
                            let it = interventions[i]
                            VStack(alignment: .leading, spacing: 3) {
                                HStack {
                                    Text(fmtDate(it["date"] as? String ?? it["startDate"] as? String))
                                        .font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.fg))
                                    Spacer()
                                }
                                if let ty = it["type"] as? String, !ty.isEmpty {
                                    Text(ty).font(.system(size: 10.5, weight: .medium)).foregroundColor(t.color(t.muted)).lineLimit(2)
                                }
                            }
                            .padding(.vertical, 4)
                        }
                    }
                }
            }
        }
        .task { await load() }
    }

    private func mRow(_ kind: ServerSheet.Kind, icon: String, title: String, desc: String) -> some View {
        Button { sheet = .init(kind: kind) } label: {
            HStack(spacing: 10) {
                Image(systemName: icon).font(.system(size: 15)).foregroundColor(t.color(t.info))
                    .frame(width: 30, height: 30)
                    .background(RoundedRectangle(cornerRadius: 8).fill(t.color(t.info).opacity(0.12)))
                VStack(alignment: .leading, spacing: 1.5) {
                    Text(title).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                    Text(desc).font(.system(size: 9.5)).foregroundColor(t.color(t.muted))
                }
                Spacer()
            }
            .padding(11)
            .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 13).stroke(t.color(t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/interventions")
            interventions = (r["interventions"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
    }
}

// MARK: - 高级段

struct AdvancedSection: View {
    @EnvironmentObject var theme: Theme
    let sn: String
    @Binding var sheet: ServerSheet?
    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 10) {
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 10) {
                aRow(.burst, icon: "bolt.horizontal", title: "Burst 流量", desc: "突发带宽")
                aRow(.firewall, icon: "flame", title: "防火墙", desc: "网络层开关")
                aRow(.backupFtp, icon: "externaldrive.badge.icloud", title: "Backup FTP", desc: "备份与 ACL")
                aRow(.secondaryDns, icon: "globe", title: "二级 DNS", desc: "从 DNS 列表")
                aRow(.virtualMac, icon: "macpro.gen3", title: "虚拟 MAC", desc: "IP ↔ MAC 表")
                aRow(.vrack, icon: "link", title: "vRack", desc: "私网成员")
                aRow(.mitigation, icon: "shield.lefthalf.filled", title: "DDoS 缓解", desc: "按 IP 块开关")
                aRow(.ipSpecs, icon: "number.square", title: "IP 规格", desc: "v4/v6 块详情")
                aRow(.orderableBandwidth, icon: "speedometer", title: "可购带宽", desc: "套餐查询")
                aRow(.orderableTraffic, icon: "arrow.up.arrow.down", title: "可购流量", desc: "流量包")
                aRow(.orderableIp, icon: "number", title: "可购 IP 块", desc: "v4/v6")
                aRow(.options, icon: "shippingbox", title: "附加选项", desc: "已订阅清单")
            }
        }
    }

    private func aRow(_ kind: ServerSheet.Kind, icon: String, title: String, desc: String) -> some View {
        Button { sheet = .init(kind: kind) } label: {
            VStack(alignment: .leading, spacing: 8) {
                RoundedRectangle(cornerRadius: 9)
                    .fill(t.color(t.muted).opacity(0.12))
                    .frame(width: 34, height: 34)
                    .overlay(Image(systemName: icon).font(.system(size: 14, weight: .semibold)).foregroundColor(t.color(t.muted)))
                Text(title).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                Text(desc).font(.system(size: 9.5)).foregroundColor(t.color(t.muted)).lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(12)
            .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 13).stroke(t.color(t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }
}

/// NSNumber/Int/Double → Double(ServerDetailView 磁盘容量用)
func numToDoubleAny(_ v: Any?) -> Double? {
    if let d = v as? Double { return d }
    if let i = v as? Int { return Double(i) }
    if let n = v as? NSNumber { return n.doubleValue }
    if let s2 = v as? String { return Double(s2) }
    return nil
}
