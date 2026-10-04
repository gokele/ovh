import SwiftUI

/**
 * VPS 详情(全功能):概览 / 快照 / 防护 / 维护 四段。
 * 对齐 web vps-control:电源三键 + noVNC + 快照全生命周期 + DDoS + 终止两步。
 */
struct VpsDetailView: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @AppStorage("ovh_mask_ip") private var mask = false
    let item: [String: Any]
    var t: Tokens { theme.t }

    var name: String { item["name"] as? String ?? "" }
    @State private var seg = 0
    @State private var serviceinfo: [String: Any]?
    @State private var info: [String: Any]?
    @State private var currentOS: String?
    @State private var ips: [[String: Any]] = []
    @State private var osLoadFailed = false
    @State private var siLoadFailed = false
    @State private var sheet: VpsSheet?

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                headerCard
                alertCards
                Picker("", selection: $seg) {
                    Text("概览").tag(0)
                    Text("快照").tag(1)
                    Text("防护").tag(2)
                    Text("维护").tag(3)
                }
                .pickerStyle(.segmented)

                switch seg {
                case 1: VpsSnapshotSection(name: name)
                case 2: VpsMitigationSection(name: name)
                case 3: VpsMaintenanceSection(name: name, sheet: $sheet)
                default: overview
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
        .sheet(item: $sheet) { s in vpsSheet(s) }
    }

    private var displayName: String {
        (item["displayName"] as? String) ?? name.components(separatedBy: ".").first ?? name
    }

    // V-027/028 锁定与救援警示
    @ViewBuilder private var alertCards: some View {
        let state = (item["state"] as? String ?? "").lowercased()
        if state == "suspended" || state == "locked" {
            Card(border: "danger") {
                VStack(alignment: .leading, spacing: 5) {
                    Text("VPS 已锁定").font(.system(size: 13, weight: .bold)).foregroundColor(t.color(t.danger))
                    Text("状态: \(item["lockStatus"] as? String ?? state) · 通常因投诉(abuse)被 OVH 临时冻结,联系 OVH 客服处理")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                }
            }
        }
        if state == "rescued" {
            Card(border: "warning") {
                VStack(alignment: .leading, spacing: 5) {
                    Text("救援模式").font(.system(size: 13, weight: .bold)).foregroundColor(t.color(t.warning))
                    Text("下次重启会进入 OVH 救援镜像。修完故障后需要把 netboot 改回 local 再重启回正常系统")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                }
            }
        }
    }

    // MARK: 顶卡

    private var headerCard: some View {
        let state = item["state"] as? String ?? ""
        let running = ["running", "active"].contains(state.lowercased())
        return Card {
            VStack(spacing: 11) {
                HStack(spacing: 10) {
                    RoundedRectangle(cornerRadius: 10)
                        .fill(t.color(running ? t.success : t.danger).opacity(0.16))
                        .frame(width: 42, height: 42)
                        .overlay(Image(systemName: "cube.fill").font(.system(size: 18, weight: .semibold)).foregroundColor(t.color(running ? t.success : t.danger)))
                    VStack(alignment: .leading, spacing: 3) {
                        Text(displayName).font(.system(size: 16, weight: .bold)).foregroundColor(t.color(t.fg)).lineLimit(1)
                        Text(name).font(.system(size: 10.5, design: .monospaced)).foregroundColor(t.color(t.muted)).lineLimit(1)
                    }
                    Spacer()
                    Chip(text: VPS_STATE_CN[state.lowercased()] ?? state.uppercased(), color: vpsStateColor(state))
                }
                HStack(spacing: 6) {
                    Dot(color: running ? t.success : t.danger)
                    Text(mask ? maskIP(firstIP) : firstIP)
                        .font(.system(size: 12.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                    Spacer()
                    Text(renewalText).font(.system(size: 11)).foregroundColor(t.color(t.muted))
                }
                if let os = currentOS, !os.isEmpty {
                    Button { sheet = .init(kind: .reinstall) } label: {
                        HStack(spacing: 4) {
                            Image(systemName: "terminal").font(.system(size: 10))
                            Text("系统 · \(os)").font(.system(size: 11, weight: .semibold)).lineLimit(1)
                        }
                        .foregroundColor(t.color(t.info))
                        .padding(.horizontal, 8).padding(.vertical, 4)
                        .background(Capsule().fill(t.color(t.info).opacity(0.1)))
                    }.buttonStyle(.plain)
                } else if osLoadFailed {
                    Button { Task { await load() } } label: {
                        Text("当前系统读取失败 · 点击重试").font(.system(size: 10.5, weight: .semibold)).foregroundColor(t.color(t.danger))
                    }.buttonStyle(.plain)
                }
                if siLoadFailed {
                    Text("到期 / 续费信息读取失败 —— 点右上刷新重试;未读到不等于没有到期日")
                        .font(.system(size: 10)).foregroundColor(t.color(t.danger))
                }
                if let si = serviceinfo, let exp = si["expiration"] as? String, !exp.isEmpty {
                    FlowLayout(spacing: 6) {
                        Chip(text: "到期 \(fmtDate(exp))", color: t.muted)
                        if si["renewalDeleteAtExpiration"] as? Bool == true { Chip(text: "到期将终止", color: t.danger) }
                    }
                }
            }
        }
    }

    private var firstIP: String {
        let raw = ips.first?["ipAddress"] as? String ?? (ips.first?["ip"] as? String ?? "")
        return raw.isEmpty ? "—" : raw
    }

    @State private var ipsErr: String? = nil
    private func loadIPs() async {
        ipsErr = nil
        if let r = try? await conn.client.getDict("/vps-control/\(name)/ips") {
            ips = (r["ips"] as? [[String: Any]]) ?? []
        } else {
            ipsErr = "读取失败"
        }
    }

    private var renewalText: String {
        if let si = serviceinfo {
            if (si["terminationScheduled"] as? Bool ?? false) || (si["renewalDeleteAtExpiration"] as? Bool ?? false) { return "到期终止" }
            if si["renewalType"] as? Bool == true { return "自动续费" }
            return "手动续费"
        }
        return ""
    }

    // MARK: 概览(硬件 + 电源 + 控制台 + 重装)

    private var overview: some View {
        VStack(spacing: 12) {
            Card {
                VStack(spacing: 9) {
                    SectionTitle(text: "配置")
                    KV(k: "型号", v: item["model"] as? String ?? "—")
                    if let v = numToDoubleAny(item["vcore"]) {
                        KV(k: "vCore", v: "\(Int(v)) 核")
                    }
                    if let m = numToDoubleAny(item["memoryMB"]) {
                        KV(k: "内存", v: m >= 1024 ? String(format: "%.0f GB", m / 1024) : "\(Int(m)) MB")
                    }
                    if let d = numToDoubleAny(item["diskGB"]) {
                        KV(k: "磁盘", v: "\(Int(d)) GB")
                    }
                    if let z = item["zone"] as? String, !z.isEmpty {
                        KV(k: "区域", v: zoneCn(z))
                    }
                }
            }

            Card {
                VStack(spacing: 9) {
                    SectionTitle(text: "电源操作")
                    let state = (item["state"] as? String ?? "").lowercased()
                    let stopped = ["stopped", "suspended", "error"].contains(state)
                    HStack(spacing: 8) {
                        if stopped {
                            ActBtn(kind: .primary, icon: "play.fill", label: "启动") {
                                Task { await power("start") }
                            }
                        } else {
                            ActBtn(kind: .ghost, icon: "pause.fill", label: "关机") { sheet = .init(kind: .stop) }
                            ActBtn(kind: .danger, icon: "arrow.triangle.2.circlepath", label: "重启") { sheet = .init(kind: .reboot) }
                        }
                    }
                    SheetNote(text: "关机不停止计费。VPS 面板的电源是 ACPI 级别的,系统内 shutdown 更稳。", tint: t.muted)
                }
            }

            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 10) {
                vOp(.console, icon: "rectangle.and.pencil.and.selection", title: "Web 控制台", desc: "noVNC(5 分钟有效)", tint: t.info)
                vOp(.reinstall, icon: "opticaldiscdrive.fill", title: "重装系统", desc: "模板 + SSH key", tint: t.danger)
            }

            // V-029/030 IP 列表卡
            Card {
                VStack(spacing: 8) {
                    HStack {
                        SectionTitle(text: "IP 地址")
                        Spacer()
                        Text("主 IP:\(mask ? maskIP(firstIP) : firstIP)").font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.faint))
                    }
                    if ipsErr != nil {
                        LoadFailed(message: "IP 地址读取失败:\(ipsErr ?? "")") { Task { await loadIPs() } }
                    } else if ips.isEmpty {
                        Text("无 IP").font(.system(size: 11)).foregroundColor(t.color(t.faint)).padding(.vertical, 4)
                    } else {
                        ForEach(ips.indices, id: \.self) { i in
                            let ip = ips[i]
                            VStack(spacing: 3) {
                                HStack {
                                    Text(mask ? maskIP(ip["ipAddress"] as? String ?? "") : (ip["ipAddress"] as? String ?? "—"))
                                        .font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                                    Spacer()
                                    if let v = ip["version"] as? Int { Chip(text: "IPv\(v)") }
                                    if let ty = ip["type"] as? String, !ty.isEmpty { Chip(text: ty) }
                                }
                                if let rev = ip["reverse"] as? String, !rev.isEmpty {
                                    Text("↩ " + (mask ? maskIP(rev) : rev)).font(.system(size: 10)).foregroundColor(t.color(t.muted))
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    private func vOp(_ kind: VpsSheet.Kind, icon: String, title: String, desc: String, tint: String) -> some View {
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

    private func load() async {
        do {
            async let si = conn.client.getDict("/vps-control/\(name)/serviceinfo")
            async let i = conn.client.getDict("/vps-control/\(name)/info")
            async let os = conn.client.getDict("/vps-control/\(name)/current-os")
            let (s, inr, o) = try await (si, i, os)
            serviceinfo = (s["serviceInfo"] as? [String: Any]) ?? s
            info = (inr["info"] as? [String: Any]) ?? inr
            if let osObj = o["currentOS"] as? [String: Any] ?? (o["os"] as? [String: Any]) {
                currentOS = osObj["name"] as? String
            } else {
                currentOS = o["name"] as? String
            }
            osLoadFailed = false
        } catch {
            _ = error.localizedDescription
            osLoadFailed = true
            siLoadFailed = true
        }
        await loadIPs()
    }

    private func power(_ verb: String) async {
        let (ok, msg) = await conn.client.actionPostData("/vps-control/\(name)/\(verb)", bodyData: nil)
        let v2 = ["start": "启动", "stop": "关机", "reboot": "重启"][verb] ?? verb
        toast.show(ok ? "\(v2)任务已提交" : (msg.isEmpty ? "\(v2)失败" : msg), error: !ok)
        if ok { await load() }   // V-069:电源操作后刷新状态
    }

    // MARK: sheet 调度

    @ViewBuilder
    private func vpsSheet(_ s: VpsSheet) -> some View {
        switch s.kind {
        case .console: VpsConsoleSheet(name: name)
        case .reinstall: VpsReinstallSheet(vpsName: name)
        case .stop: ConfirmSheet(title: "确认关机?", message: "VPS 将立即停机,业务中断直到下次启动。注意:OVH 不会因为关机停止计费,VPS 仍占用 hypervisor 配额,只是物理上不再消耗 CPU/磁盘 IO。", confirmText: "确认关机") {
            await power("stop")
        }
        case .reboot: ConfirmSheet(title: "重启 VPS", message: "强制重启,未保存数据会丢失。", confirmText: "确认重启") {
            await power("reboot")
        }
        case .tasks: VpsTasksSheet(name: name)
        case .engagement: EngagementSheet(sn: name, isVps: true)
        case .renewal: RenewalSheet(sn: name, isVps: true, info: serviceinfo ?? [:])
        case .terminate: VpsTerminateSheet(name: name)
        case .options: JsonSheet(title: "附加选项", icon: "shippingbox", path: "/vps-control/\(name)/options")
        case .changeContact: ChangeContactSheet(sn: name, isVps: true)
        case .alias: VpsAliasSheet(name: name, current: displayName)
        }
    }
}

struct VpsSheet: Identifiable {
    enum Kind { case console, reinstall, stop, reboot, tasks, engagement, renewal, terminate, options, changeContact, alias }
    let kind: Kind
    var id: Kind { kind }
}

// MARK: - 快照段

struct VpsSnapshotSection: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    let name: String
    var t: Tokens { theme.t }

    @State private var snap: [String: Any]?
    @State private var err: String?
    @State private var loading = true
    @State private var desc = ""
    @State private var busy = false
    @State private var revertConfirm = false
    @State private var revertName = ""
    @State private var snapDeleteConfirm = false
    @State private var showDescEdit = false
    @State private var descEdit = ""
    @State private var createSheet = false

    var body: some View {
        VStack(spacing: 12) {
            if loading {
                Card { ProgressView().padding(20).frame(maxWidth: .infinity) }
            } else if let e = err {
                Card { LoadFailed(message: e) { Task { await load() } } }
            } else if let s = snap, s["exists"] as? Bool ?? true {
                existsCard(s)
            } else {
                Card {
                    VStack(spacing: 12) {
                        EmptyHint(icon: "camera", text: "还没有快照(免费档单快照)")
                        ActBtn(kind: .primary, icon: "plus.circle", label: "创建快照") { createSheet = true }
                    }
                }
            }
        }
        .task { await load() }
        .refreshable { await load() }
        .sheet(isPresented: $createSheet) {
            VpsSnapshotCreateSheet(name: name) {
                await load()
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(isPresented: $showDescEdit) {
            VStack(spacing: 14) {
                SheetHeader(icon: "pencil", tint: t.info, title: "修改快照描述")
                VStack(alignment: .leading, spacing: 12) {
                    SheetField(placeholder: "给快照一段描述,方便日后辨识", text: $descEdit)
                    ActBtn(kind: .primary, icon: "checkmark", label: "保存") {
                        let body = try? JSONSerialization.data(withJSONObject: ["description": descEdit])
                        let (ok2, msg) = await conn.client.actionPutData("/vps-control/\(name)/snapshot", bodyData: body)
                        toast.show(ok2 ? "快照描述已更新" : (msg.isEmpty ? "更新失败" : msg), error: !ok2)
                        showDescEdit = false
                        await load()
                    }
                }.padding(.horizontal, 16)
                Spacer()
            }
            .background(t.color(t.bg))
            .presentationDetents([.medium])
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(isPresented: $snapDeleteConfirm) {
            ConfirmSheet(title: "删除当前快照?",
                         message: "快照本身会删除,VPS 当前状态不受影响。",
                         confirmText: "确认删除") {
                let (ok, msg) = await conn.client.actionDelete("/vps-control/\(name)/snapshot")
                toast.show(ok ? "快照已删除" : (msg.isEmpty ? "删除失败" : msg), error: !ok)
                if ok { await load() }
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(isPresented: $revertConfirm) {
            VStack(spacing: 14) {
                Capsule().fill(t.color(t.border)).frame(width: 36, height: 4).padding(.top, 10)
                Image(systemName: "exclamationmark.triangle.fill").font(.system(size: 28)).foregroundColor(t.color(t.warning))
                Text("回滚快照").font(.system(size: 16, weight: .bold)).foregroundColor(t.color(t.fg))
                VStack(alignment: .leading, spacing: 4) {
                    Text("快照之后所有改动会丢失:").font(.system(size: 11, weight: .bold)).foregroundColor(t.color(t.danger))
                    Text("· 文件系统回到快照创建那一刻").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                    Text("· VPS 会自动重启,期间几分钟无法访问").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                    Text("· IP / 密码 等元数据不变").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                }
                .padding(10)
                .background(RoundedRectangle(cornerRadius: 10).stroke(t.color(t.danger).opacity(0.5), lineWidth: 1))
                Text("输入 VPS 名 \(name) 确认:")
                    .font(.system(size: 11.5)).foregroundColor(t.color(t.muted)).multilineTextAlignment(.center)
                SheetField(placeholder: name, text: $revertName, mono: true).padding(.horizontal, 16)
                HStack(spacing: 10) {
                    ActBtn(kind: .ghost, icon: nil, label: "取消") { revertConfirm = false }
                    ActBtn(kind: .danger, icon: "arrow.uturn.backward", label: busy ? "回滚中…" : "确认回滚", busy: busy) {
                        guard revertName.trimmingCharacters(in: .whitespaces) == name else {
                            toast.show("名称不匹配", error: true); return
                        }
                        await revert()
                    }
                }.padding(.horizontal, 16)
                Spacer(minLength: 12)
            }
            .background(t.color(t.bg))
        }
    }

    private func existsCard(_ s: [String: Any]) -> some View {
        Card {
            VStack(alignment: .leading, spacing: 10) {
                SectionTitle(text: "当前快照")
                KV(k: "创建于", v: fmtDate(s["creationDate"] as? String ?? s["createdAt"] as? String))
                KV(k: "区域", v: s["region"] as? String ?? "—")
                KV(k: "描述", v: (s["description"] as? String) ?? "—")

                HStack(spacing: 8) {
                    ActBtn(kind: .danger, icon: "arrow.uturn.backward", label: "回滚") { revertConfirm = true }
                    ActBtn(kind: .ghost, icon: "pencil", label: "改描述") { descEdit = (s["description"] as? String) ?? ""; showDescEdit = true }
                    ActBtn(kind: .ghost, icon: "trash", label: "删除快照") { snapDeleteConfirm = true }
                }
                Text("免费档单 VPS 只能存 1 个快照。要做新快照得先删旧的。")
                    .font(.system(size: 9.5)).foregroundColor(t.color(t.faint))
            }
        }
    }

    private func revert() async {
        busy = true
        defer { busy = false }
        let (ok, msg) = await conn.client.actionPostData("/vps-control/\(name)/snapshot/revert", bodyData: nil)
        toast.show(ok ? "回滚已开始" : (msg.isEmpty ? "失败" : msg), error: !ok)
        revertConfirm = false
        if ok { await load() }
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/vps-control/\(name)/snapshot")
            // handler:{"snapshot": {...}|null}
            snap = (r["snapshot"] as? [String: Any])
            if snap == nil, r["exists"] as? Bool == true { snap = r }   // 兼容旧形状
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }
}

struct VpsSnapshotCreateSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let name: String
    let onDone: () async -> Void
    var t: Tokens { theme.t }

    @State private var desc = ""
    @State private var busy = false

    var body: some View {
        VStack(spacing: 14) {
            SheetHeader(icon: "camera", tint: t.accent, title: "创建快照")
            VStack(alignment: .leading, spacing: 12) {
                SheetNote(text: "创建过程会暂停 VPS 约 30 秒到 3 分钟。", tint: t.warning)
                SheetField(placeholder: "描述(可选)", text: $desc)
                ActBtn(kind: .primary, icon: "camera.fill", label: busy ? "创建中…" : "开始创建", busy: busy) {
                    await create()
                }
            }.padding(.horizontal, 16)
            Spacer()
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium])
    }

    private func create() async {
        busy = true
        defer { busy = false }
        var body: [String: Any] = [:]
        let d = desc.trimmingCharacters(in: .whitespaces)
        if !d.isEmpty { body["description"] = d }
        let data = try? JSONSerialization.data(withJSONObject: body)
        let (ok, msg) = await conn.client.actionPostData("/vps-control/\(name)/snapshot", bodyData: data)
        toast.show(ok ? "快照创建已开始" : (msg.isEmpty ? "失败" : msg), error: !ok)
        dismiss()
        if ok { await onDone() }
    }
}

// MARK: - 防护段(DDoS)

struct VpsMitigationSection: View {
    let name: String
    var body: some View {
        MitigationSheet(sn: name, isVps: true)
    }
}

// MARK: - 维护段

struct VpsMaintenanceSection: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let name: String
    @Binding var sheet: VpsSheet?
    var t: Tokens { theme.t }

    /// 美区账户(V-034)
    var isUSAccount: Bool {
        (conn.activeAccount?["endpoint"] as? String)?.lowercased().contains("us") ?? false
    }

    var body: some View {
        VStack(spacing: 10) {
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 10) {
                m(.renewal, icon: "arrow.triangle.2.circlepath", title: "续费策略", desc: "自动/手动/终止")
                m(.engagement, icon: "doc.plaintext", title: "合同期", desc: "承诺期管理")
                m(.tasks, icon: "checklist", title: "任务历史", desc: "VPS 操作任务")
                m(.alias, icon: "tag", title: "别名", desc: "本地显示名")
                m(.options, icon: "shippingbox", title: "附加选项", desc: "已订阅选项")
                if isUSAccount {
                    // V-034:美区整卡替换为说明,不可点入
                    Card(border: "warning") {
                        VStack(alignment: .leading, spacing: 5) {
                            Text("美区限制").font(.system(size: 13, weight: .bold)).foregroundColor(t.color(t.warning))
                            Text("US OVHcloud 是独立公司,以下功能不可用:变更联系人 / 重置密码 / Backup FTP / IPMI 测试。密码可在 Web 控制台进系统后用 passwd 自助改;过户需直接联系 OVH US 客服。")
                                .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                        }
                    }
                } else {
                    m(.changeContact, icon: "person.2", title: "变更联系人", desc: "admin/tech/billing NIC")
                }
            }
            Card {
                VStack(spacing: 10) {
                    SectionTitle(text: "危险操作")
                    Text("提交终止请求后 OVH 邮件发 token,确认后立即销毁。数据不可恢复。")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                    HStack(spacing: 10) {
                        ActBtn(kind: .danger, icon: "paperplane", label: "提交终止请求(收 token)") {
                            sheet = .init(kind: .terminate)
                        }
                    }
                    Text("终止分两步:先申请(OVH 发邮件给 token),再回来输入 token 确认。")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                }
            }
        }
    }

    private func m(_ kind: VpsSheet.Kind, icon: String, title: String, desc: String) -> some View {
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
}

// MARK: - noVNC 控制台

struct VpsConsoleSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    let name: String
    var t: Tokens { theme.t }

    @State private var url: String?
    @State private var err: String?
    @State private var busy = false
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "rectangle.and.pencil.and.selection", tint: t.info, title: "Web 控制台")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    SheetNote(text: "生成 noVNC 地址(5 分钟有效)。复制到浏览器打开即可看到 VPS 屏幕。", tint: t.info)
                    if let u = url {
                        Card(border: t.success) {
                            VStack(alignment: .leading, spacing: 8) {
                                Text("已生成").font(.system(size: 12.5, weight: .bold)).foregroundColor(t.color(t.success))
                                Text(u).font(.system(size: 10.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                                    .textSelection(.enabled).lineLimit(4)
                                Button {
                                    UIPasteboard.general.string = u
                                    toast.show("已复制")
                                } label: {
                                    HStack(spacing: 5) {
                                        Image(systemName: "doc.on.doc").font(.system(size: 11))
                                        Text("复制地址").font(.system(size: 11.5, weight: .semibold))
                                    }.foregroundColor(t.color(t.accent))
                                }.buttonStyle(.plain)
                            }
                        }
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await gen() } }
                    }
                    ActBtn(kind: .primary, icon: "arrow.down.circle", label: busy ? "生成中…" : "生成控制台地址", busy: busy) {
                        await gen()
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium])
    }

    private func gen() async {
        busy = true
        defer { busy = false }
        do {
            let r = try await conn.client.post("/vps-control/\(name)/console")
            url = r["url"] as? String ?? (r["consoleUrl"] as? String)
            if url == nil, let m = r["message"] as? String { err = m } else if url == nil { err = "后端未返回地址" }
        } catch { err = error.localizedDescription }
    }
}

// MARK: - VPS 重装

struct VpsReinstallSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let vpsName: String
    var t: Tokens { theme.t }

    @State private var templates: [[String: Any]] = []
    @State private var search = ""
    @State private var pickedId: String?
    @State private var sshKey = ""
    @State private var noMail = false
    @State private var confirmName = ""
    @State private var loading = true
    @State private var busy = false

    private var filtered: [[String: Any]] {
        let q = search.trimmingCharacters(in: .whitespaces).lowercased()
        guard !q.isEmpty else { return templates }
        return templates.filter {
            (($0["name"] as? String ?? "") + String(describing: $0["id"] ?? "")).lowercased().contains(q)
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "opticaldiscdrive.fill", tint: t.danger, title: "重装 VPS")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    SheetNote(text: "重装清空整块盘,不可逆。需要 Face ID 确认。", tint: t.danger)
                    SheetField(placeholder: "搜索镜像", text: $search)
                    if loading {
                        ProgressView().padding(20).frame(maxWidth: .infinity)
                    } else {
                        VStack(spacing: 6) {
                            ForEach(Array(filtered.enumerated()), id: \.offset) { _, tpl in
                                let id = String(describing: tpl["id"] ?? "")
                                let on = pickedId == id
                                Button { pickedId = id } label: {
                                    HStack {
                                        Text(tpl["name"] as? String ?? id).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                                        Spacer()
                                        if on { Image(systemName: "checkmark.circle.fill").font(.system(size: 15)).foregroundColor(t.color(t.accent)) }
                                    }
                                    .padding(11)
                                    .background(RoundedRectangle(cornerRadius: 11).fill(t.color(on ? t.accent : t.surface).opacity(0.08)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(on ? t.accent : t.border), lineWidth: 1)))
                                }.buttonStyle(.plain)
                            }
                            if filtered.isEmpty {
                                Text("没有匹配镜像").font(.system(size: 11)).foregroundColor(t.color(t.faint)).padding(8)
                            }
                        }
                    }

                    if pickedId != nil {
                        Text("SSH key 名称(可选,逗号分隔)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetField(placeholder: "my-key", text: $sshKey, mono: true)
                        Toggle(isOn: $noMail) {
                            Text("不发送密码邮件(配了 SSH key 用)").font(.system(size: 12)).foregroundColor(t.color(t.fg))
                        }.tint(t.color(t.accent))
                        Text("输入 VPS 名确认").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetField(placeholder: vpsName, text: $confirmName, mono: true)
                        ActBtn(kind: .danger, icon: "faceid", label: busy ? "提交中…" : "面容确认并重装", busy: busy) {
                            await submit()
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

    private func load() async {
        if let r = try? await conn.client.getDict("/vps-control/\(vpsName)/templates") {
            templates = (r["templates"] as? [[String: Any]]) ?? []
        }
        loading = false
    }

    private func submit() async {
        guard let id = pickedId else { return }
        guard confirmName.trimmingCharacters(in: .whitespaces) == vpsName else {
            return toast.show("VPS 名不匹配", error: true)
        }
        guard await Biometric.require("重装 VPS") else { return }
        busy = true
        defer { busy = false }
        var body: [String: Any] = ["templateId": id, "doNotSendPassword": noMail]
        let k = sshKey.trimmingCharacters(in: .whitespaces)
        if !k.isEmpty { body["sshKey"] = k.components(separatedBy: ",").map { $0.trimmingCharacters(in: .whitespaces) } }
        do {
            _ = try await conn.client.post("/vps-control/\(vpsName)/reinstall", body: body)
            toast.show("重装已开始")
            dismiss()
        } catch { toast.show(error.localizedDescription, error: true) }
    }
}

// MARK: - VPS 任务历史

// V-064 状态中文 8 种
let VPS_TASK_STATE_CN: [String: String] = [
    "blocked": "已阻塞", "cancelled": "已取消", "doing": "进行中", "done": "完成",
    "error": "失败", "paused": "已暂停", "todo": "排队中", "waitingack": "待确认",
]
// V-065 类型中文(对齐 OVH vps.TaskTypeEnum 真实 key,web VpsTasksDialog.tsx:134-160)
let VPS_TASK_TYPE_CN: [String: String] = [
    "addVeeamBackup": "添加 Veeam 备份", "changeRootPassword": "重置 root 密码",
    "createSnapshot": "创建快照", "deleteSnapshot": "删除快照", "deliver": "交付 VM",
    "generateConsoleUrl": "生成控制台链接", "internalTask": "内部任务", "migrate": "迁移",
    "openConsole": "打开控制台", "orderAdditionalIp": "分配额外 IP", "reboot": "重启",
    "reinstall": "重装系统", "removeVeeamBackup": "移除 Veeam 备份", "revertSnapshot": "回滚快照",
    "setBackup": "调整自动备份", "setMonitoring": "设置监控", "setNetboot": "设置网络启动",
    "start": "启动", "stop": "关机", "veeamFullRestore": "Veeam 完整还原",
    "veeamRestoreFile": "Veeam 还原", "restore": "还原 VM", "updateVmResources": "升级 VM",
    "revertVm": "还原 VM", "reOpen": "重新打开工单", "rescheduleAutoBackup": "调整自动备份",
]

func vpsTaskStateColor(_ s: String) -> String {
    switch s.lowercased() {
    case "done": return "success"
    case "doing", "todo", "waitingack": return "warning"
    case "cancelled", "error", "blocked": return "danger"
    case "paused": return "info"
    default: return "muted"
    }
}

struct VpsTasksSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let name: String
    var t: Tokens { theme.t }

    @State private var tasks: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "checklist", tint: t.muted, title: "VPS 任务历史")
            ScrollView {
                VStack(spacing: 10) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else if tasks.isEmpty {
                        EmptyHint(icon: "checkmark.circle", text: "没有任务记录")
                    } else {
                        ForEach(tasks.prefix(10).indices, id: \.self) { i in
                            let it = tasks[i]
                            let act = it["action"] as? String ?? (it["type"] as? String ?? "")
                            let state = (it["state"] as? String ?? it["status"] as? String ?? "").lowercased()
                            let progress = numToDoubleAny(it["progress"]).map(Int.init) ?? -1
                            Card {
                                VStack(alignment: .leading, spacing: 6) {
                                    HStack {
                                        Text("#\(it["id"] as? Int ?? 0)").font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.faint))
                                        Text(VPS_TASK_TYPE_CN[act] ?? VPS_TASK_TYPE_CN[String(act.dropSuffix("Vm"))] ?? act).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                                        Spacer()
                                        if progress > 0 && progress < 100 { Chip(text: "\(progress)%") }
                                        Chip(text: VPS_TASK_STATE_CN[state] ?? state, color: vpsTaskStateColor(state))
                                    }
                                    if state == "doing" && progress >= 0 {
                                        ProgressView(value: Double(progress) / 100).tint(t.color(t.warning))
                                    }
                                    KV(k: "时间", v: fmtDate(it["updateDate"] as? String ?? it["date"] as? String))
                                }
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
        .refreshable { await load() }
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/vps-control/\(name)/tasks")
            tasks = (r["tasks"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }
}

// MARK: - 终止两步

struct VpsTerminateSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let name: String
    var t: Tokens { theme.t }

    @State private var token = ""
    @State private var requested = false
    @State private var busy = false
    @State private var confirming = false

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "xmark.octagon", tint: t.danger, title: "终止 VPS")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    SheetNote(text: "终止不可恢复。OVH 会发确认邮件,把邮件里的 token 填回来才算完成。", tint: t.danger)

                    if !requested {
                        ActBtn(kind: .danger, icon: "paperplane", label: busy ? "申请中…" : "第一步:申请终止(发确认邮件)") {
                            confirming = true
                        }
                    } else {
                        SheetNote(text: "已申请。请到邮箱查收 OVH 的终止确认邮件,把 token 填在下面。", tint: t.info)
                        SheetField(placeholder: "邮件里的 token", text: $token, mono: true)
                        ActBtn(kind: .danger, icon: "checkmark.seal", label: busy ? "确认中…" : "第二步:输入 token 确认终止", busy: busy) {
                            await confirm()
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .sheet(isPresented: $confirming) {
            ConfirmSheet(title: "申请终止 VPS", message: "OVH 将向账户邮箱发送终止确认邮件。此 VPS 的所有数据最终会被删除。", confirmText: "申请终止") {
                busy = true
                let (ok, msg) = await conn.client.actionPostData("/vps-control/\(name)/terminate", bodyData: nil)
                busy = false
                toast.show(ok ? "已申请,查收邮件" : (msg.isEmpty ? "失败" : msg), error: !ok)
                if ok { requested = true }
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
    }

    private func confirm() async {
        let tok = token.trimmingCharacters(in: .whitespaces)
        guard !tok.isEmpty else { return toast.show("先填 token", error: true) }
        guard await Biometric.require("确认终止 VPS") else { return }
        busy = true
        defer { busy = false }
        let body = try? JSONSerialization.data(withJSONObject: ["token": tok])
        let (ok, msg) = await conn.client.actionPostData("/vps-control/\(name)/confirm-termination", bodyData: body)
        toast.show(ok ? "终止已确认" : (msg.isEmpty ? "失败" : msg), error: !ok)
        if ok { dismiss() }
    }
}

// MARK: - 别名

struct VpsAliasSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let name: String
    let current: String
    var t: Tokens { theme.t }

    @State private var alias = ""
    @State private var busy = false

    var body: some View {
        VStack(spacing: 14) {
            SheetHeader(icon: "tag", tint: t.info, title: "别名")
            VStack(alignment: .leading, spacing: 12) {
                SheetNote(text: "别名只存在后端,用于列表和下拉里好认。留空保存 = 删除别名。", tint: t.muted)
                SheetField(placeholder: "我的小机器", text: $alias)
                HStack(spacing: 10) {
                    ActBtn(kind: .primary, icon: "checkmark", label: busy ? "保存中…" : "保存", busy: busy) {
                        await save()
                    }
                    ActBtn(kind: .ghost, icon: "trash", label: "删除别名") {
                        Task {
                            let (ok, msg) = await conn.client.actionDelete("/server-control/\(name)/alias")
                            toast.show(ok ? "已删除" : (msg.isEmpty ? "失败" : msg), error: !ok)
                            if ok { dismiss() }
                        }
                    }
                }
            }.padding(.horizontal, 16)
            Spacer()
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium])
        .onAppear { alias = current }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        let a = alias.trimmingCharacters(in: .whitespaces)
        let body = try? JSONSerialization.data(withJSONObject: ["alias": a])
        let (ok, msg) = await conn.client.actionPutData("/server-control/\(name)/alias", bodyData: body)
        toast.show(ok ? "已保存" : (msg.isEmpty ? "失败" : msg), error: !ok)
        if ok { dismiss() }
    }
}

/// 独服别名(S-008/009):长按机器标题呼出;PUT/DELETE /server-control/{sn}/alias
struct ServerAliasSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let sn: String
    let current: String
    var t: Tokens { theme.t }

    @State private var alias = ""
    @State private var busy = false

    var body: some View {
        VStack(spacing: 14) {
            SheetHeader(icon: "tag", tint: t.info, title: "设置别名")
            VStack(alignment: .leading, spacing: 12) {
                Text(sn).font(.system(size: 11, design: .monospaced)).foregroundColor(t.color(t.muted))
                SheetField(placeholder: "例如:kele(留空清除别名)", text: $alias)
                Text("别名仅在本程序里显示,不会下发到 OVH。")
                    .font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                HStack(spacing: 10) {
                    ActBtn(kind: .primary, icon: "checkmark", label: busy ? "保存中…" : (alias.isEmpty ? "清除并保存" : "保存"), busy: busy) {
                        await save()
                    }
                }
            }.padding(.horizontal, 16)
            Spacer()
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium])
        .task {
            // 预填真实别名(GET aliases),不是 OVH 计划名
            if let r = try? await conn.client.getDict("/server-control/aliases"),
               let map = r["aliases"] as? [String: String] ?? (r["aliases"] as? [String: Any]).flatMap({ dict in
                   dict.mapValues { $0 as? String ?? "" }
               }) {
                alias = map[sn] ?? ""
            }
        }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        let a = alias.trimmingCharacters(in: .whitespaces)
        if a.isEmpty {
            let (ok, msg) = await conn.client.actionDelete("/server-control/\(sn)/alias")
            toast.show(ok ? "已清除别名" : (msg.isEmpty ? "失败" : msg), error: !ok)
        } else {
            let body = try? JSONSerialization.data(withJSONObject: ["alias": String(a.prefix(64))])
            let (ok, msg) = await conn.client.actionPutData("/server-control/\(sn)/alias", bodyData: body)
            toast.show(ok ? "别名已保存" : (msg.isEmpty ? "失败" : msg), error: !ok)
        }
        dismiss()
    }
}

/// OpenStack zone → 中文
func zoneCn(_ z: String) -> String {
    let m: [String: String] = [
        // OS_ZONE_MAP(V-019 新式)
        "os-eu-west-fr-1": "法国·格拉夫林", "os-eu-west-fr-2": "法国·鲁贝", "os-eu-west-fr-3": "法国·斯特拉斯堡",
        "os-us-west-or-1": "美国西部·俄勒冈", "os-us-west-or-2": "美国西部·俄勒冈 2",
        "os-eu-west-de-1": "德国·法兰克福", "os-eu-west-pl-1": "波兰·华沙",
        "os-asia-southeast-sg-1": "新加坡", "os-asia-south-in-1": "印度·孟买",
        "os-au-southeast-syd-1": "澳大利亚·悉尼", "os-eu-south-it-1": "意大利", "os-eu-north-fi-1": "芬兰",
        "os-eu-west-par-1": "法国·巴黎", "os-eu-west-par-2": "法国·巴黎 2", "os-eu-west-par-3": "法国·巴黎 3",
        "os-eu-central-de-1": "德国·法兰克福", "os-eu-central-waw-1": "波兰·华沙", "os-eu-west-uk-1": "英国·伦敦",
        "os-ca-east-bhs-1": "加拿大·博阿尔诺", "os-ca-east-tor-2": "加拿大·多伦多", "os-us-east-vin-1": "美国·弗吉尼亚",
        "os-us-west-hil-1": "美国西部·俄勒冈", "os-ap-southeast-sgp-1": "新加坡", "os-ap-south-mum-1": "印度·孟买",
        "os-ap-southeast-syd-1": "澳大利亚·悉尼",
        // LEGACY_DC_MAP
        "gra": "法国·格拉沃利纳", "gra1": "法国·格拉沃利纳", "rbx": "法国·鲁贝", "sbg": "法国·斯特拉斯堡",
        "par": "法国·巴黎", "bhs": "加拿大·博阿尔诺", "tor": "加拿大·多伦多", "mum": "印度·孟买",
        "waw": "波兰·华沙", "fra": "德国·法兰克福", "lon": "英国·伦敦", "hil": "美国西部·俄勒冈",
        "vin": "美国·弗吉尼亚", "sgp": "新加坡", "syd": "澳大利亚·悉尼", "de1": "德国",
        "lim": "墨西哥·克雷塔罗", "eri": "土耳其·伊斯坦布尔",
    ]
    return m[z.lowercased()] ?? z.uppercased()
}

extension String {
    func dropSuffix(_ suffix: String) -> Substring {
        hasSuffix(suffix) ? dropLast(suffix.count) : Substring(self)
    }
}
