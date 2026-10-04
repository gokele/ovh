import SwiftUI
import Charts

/**
 * 独服弹窗 · 电源篇:流量图 / 救援 / 重装 / IPMI / 启动模式 / SPLA / 任务预约 / BIOS / 安装进度 / OVH 监控
 */

// MARK: - 共用件

struct SheetHeader: View {
    @EnvironmentObject var theme: Theme
    @Environment(\.dismiss) private var dismiss
    let icon: String
    let tint: String
    let title: String
    var t: Tokens { theme.t }

    var body: some View {
        HStack(spacing: 9) {
            Image(systemName: icon).font(.system(size: 15)).foregroundColor(t.color(tint))
            Text(title).font(.system(size: 15.5, weight: .bold)).foregroundColor(t.color(t.fg))
            Spacer()
            Button {
                dismiss()
            } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundColor(t.color(t.muted))
                    .frame(width: 30, height: 30)
                    .background(Circle().fill(t.color(t.surfaceMuted)))
            }
            .buttonStyle(.plain)
            .accessibilityLabel("关闭")
        }
        .padding(.horizontal, 16).padding(.top, 14).padding(.bottom, 6)
    }
}

struct SheetField: View {
    @EnvironmentObject var theme: Theme
    let placeholder: String
    @Binding var text: String
    var mono: Bool = false
    var keyboard: UIKeyboardType = .default
    var t: Tokens { theme.t }

    var body: some View {
        TextField(placeholder, text: $text)
            .font(.system(size: 13, design: mono ? .monospaced : .default))
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .keyboardType(keyboard)
            .foregroundColor(t.color(t.fg))
            .padding(.horizontal, 13)
            .frame(height: 44)
            .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))
    }
}

struct SheetNote: View {
    @EnvironmentObject var theme: Theme
    let text: String
    var tint: String
    var t: Tokens { theme.t }
    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: "info.circle.fill").font(.system(size: 12)).foregroundColor(t.color(tint))
            Text(text).font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
        }
        .padding(9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 10).fill(t.color(tint).opacity(0.07)))
    }
}

// MARK: - MRTG 流量图

/// MRTG 流量图(可内嵌概览页 / sheet 放大复用)。
/// 数据形状对齐 web:download/upload 两次请求,interfaces 按 mac 配对,
/// 每张网卡一条时间线(不跨网卡累加),点 = {timestamp(秒), value:{value(bps), unit}}。
struct MrtgChartView: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let sn: String
    var compact = false
    var t: Tokens { theme.t }

    @State private var period = "daily"
    @State private var ifaces: [(mac: String, points: [(ts: Date, dl: Double, ul: Double)])] = []
    @State private var err: String?
    @State private var loading = true

    private let periods: [(String, String)] = [("hourly", "时"), ("daily", "日"), ("weekly", "周"), ("monthly", "月"), ("yearly", "年")]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Picker("", selection: $period) {
                ForEach(periods, id: \.0) { p in Text(p.1).tag(p.0) }
            }
            .pickerStyle(.segmented)
            .onChange(of: period) { _ in Task { await load() } }

            if loading {
                ProgressView().frame(maxWidth: .infinity).padding(.vertical, compact ? 18 : 30)
            } else if let e = err {
                LoadFailed(message: e) { Task { await load() } }
            } else if ifaces.isEmpty {
                VStack(spacing: 8) {
                    EmptyHint(icon: "chart.dots.scatter", text: emptyText)
                    Button {
                        Task { await load() }
                    } label: {
                        HStack(spacing: 5) {
                            Image(systemName: "arrow.clockwise").font(.system(size: 11))
                            Text("重新加载").font(.system(size: 12, weight: .semibold))
                        }.foregroundColor(t.color(t.accent))
                    }.buttonStyle(.plain)
                }
            } else {
                ForEach(ifaces.indices, id: \.self) { i in
                    ifaceCard(ifaces[i])
                }
            }
        }
        .task { await load() }
    }

    /// 空态文案:请求成功但 interfaces 为空 → 多半是机器确实没数据;
    /// 请求本身失败会走 err 分支(现在超时是常见原因,已放宽到 45s)
    private var emptyText: String {
        let raw = lastRawInterfacesCount
        return raw >= 0
            ? "OVH 返回了 \(raw) 张网卡的数据,当前周期没有流量点(web 端正常的话试试切换周期)"
            : "该服务器尚未上报 MRTG 数据,或周期内没有流量"
    }
    @State private var lastRawInterfacesCount = -1

    private func ifaceCard(_ it: (mac: String, points: [(ts: Date, dl: Double, ul: Double)])) -> some View {
        let dl = it.points.map(\.dl)
        let ul = it.points.map(\.ul)
        let dlAvg = dl.isEmpty ? 0 : dl.reduce(0, +) / Double(dl.count)
        let ulAvg = ul.isEmpty ? 0 : ul.reduce(0, +) / Double(ul.count)
        let dlMax = dl.max() ?? 0
        let ulMax = ul.max() ?? 0
        let cur = it.points.last
        return VStack(alignment: .leading, spacing: 8) {
            if ifaces.count > 1 {
                Text("网卡 " + it.mac).font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.faint))
            }
            Chart {
                ForEach(Array(it.points.enumerated()), id: \.offset) { _, p in
                    // 下行:亮蓝粗线;上行:亮绿粗线(深底上高饱和才看得清)
                    LineMark(x: .value("时间", p.ts), y: .value("下行", p.dl / 1e6))
                        .foregroundStyle(Color(hex: 0x38BDF8))
                        .lineStyle(StrokeStyle(lineWidth: 2.2, lineCap: .round))
                        .interpolationMethod(.monotone)
                    AreaMark(x: .value("时间", p.ts), y: .value("下行", p.dl / 1e6))
                        .foregroundStyle(Color(hex: 0x38BDF8).opacity(0.10))
                        .interpolationMethod(.monotone)
                    LineMark(x: .value("时间", p.ts), y: .value("上行", p.ul / 1e6))
                        .foregroundStyle(Color(hex: 0x4ADE80))
                        .lineStyle(StrokeStyle(lineWidth: 2.2, lineCap: .round))
                        .interpolationMethod(.monotone)
                }
            }
            .chartYAxis {
                AxisMarks(position: .trailing, values: .automatic(desiredCount: 3)) { v in
                    AxisGridLine().foregroundStyle(t.color(t.border).opacity(0.3))
                    AxisValueLabel {
                        if let d = v.as(Double.self) {
                            Text(d >= 1000 ? String(format: "%.1fG", d / 1000) : String(format: "%.0fM", d))
                                .font(.system(size: 9.5, weight: .medium)).foregroundStyle(t.color(t.muted))
                        }
                    }
                }
            }
            .chartXAxis {
                AxisMarks(values: .automatic(desiredCount: compact ? 2 : 3)) { v in
                    AxisValueLabel(format: .dateTime.month().day(), centered: false)
                        .font(.system(size: 9.5)).foregroundStyle(t.color(t.muted))
                }
            }
            .frame(height: compact ? 132 : 192)
            .padding(.horizontal, 4)

            HStack(spacing: 12) {
                HStack(spacing: 5) {
                    RoundedRectangle(cornerRadius: 2).fill(Color(hex: 0x38BDF8)).frame(width: 10, height: 3)
                    Text("↓峰 \(fmtMbps(dlMax)) · 均 \(fmtMbps(dlAvg))").font(.system(size: 10.5, weight: .medium)).foregroundColor(t.color(t.muted))
                }
                HStack(spacing: 5) {
                    RoundedRectangle(cornerRadius: 2).fill(Color(hex: 0x4ADE80)).frame(width: 10, height: 3)
                    Text("↑峰 \(fmtMbps(ulMax)) · 均 \(fmtMbps(ulAvg))").font(.system(size: 10.5, weight: .medium)).foregroundColor(t.color(t.muted))
                }
                Spacer()
                if let c = cur {
                    Text("现在 ↓\(fmtMbps(c.dl)) ↑\(fmtMbps(c.ul))")
                        .font(.system(size: 9.5, design: .monospaced)).foregroundColor(t.color(t.faint))
                }
            }
            .padding(.top, 2)
        }
        .padding(11)
        .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surface)))
        .overlay(RoundedRectangle(cornerRadius: 12).stroke(t.color(t.border).opacity(0.6), lineWidth: 1))
    }

    private func load() async {
        loading = true
        err = nil
        do {
            async let dl = conn.client.getDict("/server-control/\(sn)/mrtg?period=\(period)&type=traffic:download", timeoutSec: 45)
            async let ul = conn.client.getDict("/server-control/\(sn)/mrtg?period=\(period)&type=traffic:upload", timeoutSec: 45)
            let (dr, ur) = try await (dl, ul)
            let dIf = (dr["interfaces"] as? [[String: Any]]) ?? []
            let uIf = (ur["interfaces"] as? [[String: Any]]) ?? []
            lastRawInterfacesCount = dIf.count
            MrtgLog.log("dl keys=\(dr.keys.sorted()) ifaces=\(dIf.count) success=\(dr["success"] ?? "nil") error=\(dr["error"] ?? "nil") msg=\(dr["message"] ?? "nil")")
            var out: [(String, [(Date, Double, Double)])] = []
            for d in dIf {
                let mac = d["mac"] as? String ?? ""
                guard let dData = d["data"] as? [[String: Any]], !dData.isEmpty else { continue }
                let uData = (uIf.first { ($0["mac"] as? String) == mac }?["data"] as? [[String: Any]]) ?? []
                // web 口径:该网卡 download/upload 双向都有数据点才出图,缺一向整卡丢弃
                // (upload 缺失按 0 画会把"单向断流"画成"上行空闲",误导排查)
                guard !uData.isEmpty else { continue }
                var pts: [(Date, Double, Double)] = []
                for (i, dp) in dData.enumerated() {
                    guard let ts = numToDouble(dp["timestamp"]),
                          let val = (dp["value"] as? [String: Any]),
                          let dlV = numToDouble(val["value"]) else { continue }
                    let ulV = (i < uData.count)
                        ? ((uData[i]["value"] as? [String: Any]).flatMap { numToDouble($0["value"]) } ?? 0)
                        : 0
                    pts.append((Date(timeIntervalSince1970: ts), dlV, ulV))
                }
                if !pts.isEmpty { out.append((mac, pts)) }
            }
            ifaces = out
            if out.isEmpty, let msg = dr["message"] as? String { err = msg }
        } catch {
            MrtgLog.log("FAILED: \(error.localizedDescription)")
            err = error.localizedDescription
        }
        loading = false
    }
}

import os
enum MrtgLog {
    static let l = os.Logger(subsystem: "com.gokele.ovhconsole", category: "mrtg")
    static func log(_ m: String) { l.info("\(m, privacy: .public)") }
}

private func numToDouble(_ v: Any?) -> Double? {
    if let d = v as? Double { return d }
    if let i = v as? Int { return Double(i) }
    if let n = v as? NSNumber { return n.doubleValue }
    if let s = v as? String { return Double(s) }
    return nil
}

/// 放大版流量图 sheet(点"流量图"按钮用)
struct MrtgSheet: View {
    @EnvironmentObject var theme: Theme
    let sn: String
    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "chart.xyaxis.line", tint: t.info, title: "MRTG 流量图")
            ScrollView {
                MrtgChartView(sn: sn)
                    .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.large])
    }
}

// MARK: - 救援系统

struct RescueSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let sn: String
    var t: Tokens { theme.t }

    @State private var inRescue = false
    @State private var rescueMail = ""
    @State private var mail = ""
    @State private var loading = true
    @State private var err: String?
    @State private var busy = false
    @State private var rescueConfirm = false

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "lifepreserver", tint: t.warning, title: "救援系统")
            ScrollView {
                VStack(spacing: 12) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else {
                        Card(border: inRescue ? t.warning : t.border) {
                            VStack(spacing: 8) {
                                HStack(spacing: 8) {
                                    Image(systemName: inRescue ? "exclamationmark.triangle.fill" : "checkmark.shield.fill")
                                        .font(.system(size: 14)).foregroundColor(t.color(inRescue ? t.warning : t.success))
                                    Text(inRescue ? "当前处于救援模式" : "运行在正常系统")
                                        .font(.system(size: 13, weight: .semibold)).foregroundColor(t.color(t.fg))
                                    Spacer()
                                }
                                if !rescueMail.isEmpty {
                                    KV(k: "救援通知邮箱", v: rescueMail, mono: true)
                                }
                            }
                        }

                        if inRescue {
                            SheetNote(text: "机器已经在救援模式里。修完之后点「退出救援模式」切回正常系统 —— 不切回去的话,每次重启都会再进救援。", tint: t.warning)
                            SheetNote(text: "退出救援会重启机器并从硬盘正常引导,救援环境里改过的数据保留在盘上。", tint: t.info)
                            ActBtn(kind: .primary, icon: "arrow.uturn.backward", label: busy ? "退出中…" : "退出救援模式") {
                                rescueConfirm = false
                                await exitRescue()
                            }
                        } else {
                            SheetNote(text: "救援系统是一个独立的临时 Linux,从网络启动,不会动你硬盘上的数据。进去之后可以挂载硬盘修配置、改密码、拷数据。进入救援后 OVH 会把 root 密码发到邮箱,约 3~5 分钟后可以 SSH 登录。", tint: t.muted)
                            SheetField(placeholder: "留空 = 发到 OVH 账户的联系邮箱", text: $mail, keyboard: .emailAddress)
                            let mailOK = mail.range(of: #"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}"#, options: .regularExpression) != nil
                            if !mail.isEmpty && !mailOK {
                                Text("这个邮箱格式不对。救援系统的 root 密码会发到这里,填错就收不到。")
                                    .font(.system(size: 10.5)).foregroundColor(t.color(t.danger))
                            }
                            SheetNote(text: "点下去会立刻重启服务器,上面正在跑的服务会中断。硬盘数据不受影响。", tint: t.warning)
                            if rescueConfirm {
                                SheetNote(text: "再点一次「确认进入救援」就会重启。", tint: t.danger)
                            }
                            ActBtn(kind: .primary, icon: "lifepreserver.fill", label: busy ? "提交中…" : "进入救援模式(重启)") {
                                rescueConfirm = true
                            }
                            .disabled(!mail.isEmpty && !mailOK)   // 邮箱非法时拦下,别等后端 400
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .task { await load() }
        .sheet(isPresented: $rescueConfirm) {
            ConfirmSheet(title: "进入救援模式", message: "服务器将立即重启并进入救援镜像(相当于断电重启)。", confirmText: "确认进入") {
                await enter()
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/rescue")
            inRescue = r["inRescue"] as? Bool ?? false
            rescueMail = r["rescueMail"] as? String ?? ""
            // 预填上次用的邮箱并显式重发(web 同款;placeholder 提示会被误当"会自动带上")
            if mail.isEmpty { mail = rescueMail }
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func enter() async {
        busy = true
        defer { busy = false }
        var body: [String: Any] = ["confirm": true]
        let m = mail.trimmingCharacters(in: .whitespaces)
        if !m.isEmpty { body["email"] = m }
        do {
            let r = try await conn.client.post("/server-control/\(sn)/rescue", body: body)
            // 后端 message 含"密码发到哪个邮箱/记得退出救援"这些关键指引,别用硬编码盖掉
            toast.show(r["message"] as? String ?? "已进入救援模式(重启后生效)")
            dismiss()
        } catch { toast.show(error.localizedDescription, error: true) }
    }

    private func exitRescue() async {
        busy = true
        defer { busy = false }
        let body = try? JSONSerialization.data(withJSONObject: ["confirm": true])
        let (ok, msg) = await conn.client.actionPostData("/server-control/\(sn)/rescue/exit", bodyData: body)
        toast.show(ok ? (msg.isEmpty ? "已退出救援模式" : msg) : (msg.isEmpty ? "失败" : msg), error: !ok)
        if ok { dismiss() }
    }
}

// MARK: - 重装系统(全功能)

struct ServerReinstallSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let sn: String
    var t: Tokens { theme.t }

    // MARK: 数据模型(磁盘组对齐 web:后端 diskGroups 是 {id: {disks:[…], diskType, raidController}} 的字典)

    struct SmartDisk { let capacity: Double; let unit: String }
    struct DiskGroupInfo {
        let id: Int
        let diskType: String?
        let descriptionText: String?
        let raidController: String?
        let disks: [SmartDisk]
    }
    /// 智能配置方案(与 web lib/smart-storage.ts 同规则)
    struct SmartPlan {
        let partitions: [[String: String]]
        let targetGroupId: Int
        let notes: [String]
        let blocked: String?
    }
    enum StorageMode { case tplDefault, zfs, scheme, advanced }

    @State private var templates: [[String: Any]] = []
    @State private var search = ""
    @State private var picked: String?
    @State private var hostname = ""
    @State private var storageMode: StorageMode = .tplDefault
    @State private var zfsRaid = 1
    @State private var vzGB = 100.0
    // 内置分区方案
    @State private var schemes: [(name: String, priority: Int)] = []
    @State private var pickedScheme: String?
    @State private var schemeFailed: String? = nil
    // 高级存储
    @State private var groups: [DiskGroupInfo] = []
    @State private var diskInfoFailed: String? = nil
    @State private var hwRaidSupported = true
    @State private var hwRaidFailed: String? = nil
    @State private var hwRaidByGroup: [Int: Int] = [:]      // diskGroupId → raidLevel
    @State private var softRaid = false
    @State private var softRaidLevel = 1
    @State private var partitions: [[String: String]] = []  // 留空 = 默认分区(web 同款)
    @State private var showSmart = false
    @State private var smartPlan: SmartPlan? = nil
    @State private var templatesAt: Date? = nil
    @State private var confirmName = ""
    @State private var loading = true
    @State private var err: String?
    @State private var busy = false

    // 枚举与 web ReinstallDialog 同源(schema 全集):文件系统 14 个,软 RAID 含 raid7(仅 ZFS,7+ 盘)
    private let fsOptions = ["ext4","ext3","xfs","btrfs","zfs","swap","reiserfs","ntfs","fat16","ufs","vmfs5","vmfs6","vmfsl","none"]
    private let hwRaidOptions: [(Int, String)] = [
        (0, "RAID 0 · 条带(最大容量,无冗余)"),
        (1, "RAID 1 · 镜像(数据冗余)"),
        (5, "RAID 5 · 分布式奇偶(平衡)"),
        (6, "RAID 6 · 双重奇偶(高冗余)"),
        (10, "RAID 10 · 镜像+条带(高性能+冗余)"),
    ]
    private let softRaidOptions: [(Int, String)] = [
        (0, "RAID 0 · 2+ 盘"),
        (1, "RAID 1 · 2+ 盘(推荐)"),
        (5, "RAID 5 · 3+ 盘"),
        (6, "RAID 6 · 4+ 盘"),
        (7, "RAID 7 · 7+ 盘(仅 ZFS)"),
        (10, "RAID 10 · 4+ 盘"),
    ]

    // MARK: 存储四选一组件(S-036)
    private func storageModeRow(_ m: StorageMode, title: String, desc: String) -> some View {
        let on = storageMode == m
        return Button { storageMode = m } label: {
            HStack(spacing: 9) {
                Image(systemName: on ? "largecircle.fill.circle" : "circle").font(.system(size: 15)).foregroundColor(t.color(on ? t.accent : t.faint))
                VStack(alignment: .leading, spacing: 1) {
                    Text(title).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                    Text(desc).font(.system(size: 10)).foregroundColor(t.color(t.muted))
                }
                Spacer()
            }
            .padding(10)
            .background(RoundedRectangle(cornerRadius: 11).fill(on ? t.color(t.accent).opacity(0.07) : t.color(t.surfaceMuted)))
        }.buttonStyle(.plain)
    }

    private func raidOption(_ level: Int, _ title: String, _ desc: String) -> some View {
        let on = zfsRaid == level
        return Button { zfsRaid = level } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(on ? t.accent : t.fg))
                Text(desc).font(.system(size: 9.5)).foregroundColor(t.color(t.muted))
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(9)
            .background(RoundedRectangle(cornerRadius: 10).stroke(t.color(on ? t.accent : t.border), lineWidth: on ? 1.5 : 1))
        }.buttonStyle(.plain)
    }

    /// 高级存储:磁盘组 + 硬件 RAID + 软 RAID + 自定义分区(每行可选磁盘组 / RAID 级别)
    private var advancedStoragePanel: some View {
        VStack(alignment: .leading, spacing: 11) {
            if let df = diskInfoFailed {
                LoadFailed(message: "磁盘组信息读取失败:\(df)") { Task { await loadDiskInfo() } }
            } else if groups.isEmpty {
                Text("未检测到磁盘组信息").font(.system(size: 11)).foregroundColor(t.color(t.muted))
            } else {
                Text("磁盘组配置").font(.system(size: 11.5, weight: .bold)).foregroundColor(t.color(t.fg))
                ForEach(groups, id: \.id) { g in
                    VStack(alignment: .leading, spacing: 6) {
                        HStack(spacing: 6) {
                            Text("磁盘组 \(g.id)").font(.system(size: 11.5, weight: .bold)).foregroundColor(t.color(t.fg))
                            if let rc = g.raidController, !rc.isEmpty {
                                Text(rc).font(.system(size: 9)).foregroundColor(t.color(t.muted))
                                    .padding(.horizontal, 6).padding(.vertical, 2)
                                    .background(Capsule().stroke(t.color(t.border), lineWidth: 0.5))
                            }
                            Spacer()
                        }
                        ForEach(g.disks.indices, id: \.self) { di in
                            let d = g.disks[di]
                            HStack(spacing: 5) {
                                Circle().fill(t.color(t.faint)).frame(width: 4, height: 4)
                                Text("\(Int(d.capacity)) \(d.unit) \(diskTypeLabel(g.diskType))")
                            }.font(.system(size: 10)).foregroundColor(t.color(t.muted))
                        }
                        if let h = hwRaidFailed {
                            // 读失败 ≠ 不支持:不能言之凿凿写「不支持」指挥用户改软 RAID(web 修过的坑)
                            VStack(alignment: .leading, spacing: 3) {
                                Text("硬件 RAID 支持情况读取失败(\(h))").font(.system(size: 10)).foregroundColor(t.color(t.danger))
                                Text("现在无法判断这台机器能不能做硬件 RAID,请点上方「重试」读回来再决定。").font(.system(size: 10)).foregroundColor(t.color(t.muted))
                            }
                        } else if hwRaidSupported {
                            Picker("硬件 RAID", selection: Binding(
                                get: { hwRaidByGroup[g.id] ?? -1 },
                                set: { hwRaidByGroup[g.id] = $0 }
                            )) {
                                Text("默认(无 RAID)").tag(-1)
                                ForEach(hwRaidOptions, id: \.0) { o in Text(o.1).tag(o.0) }
                            }
                            .pickerStyle(.menu)
                        } else {
                            Text("此服务器不支持硬件 RAID,可改用下方「软 RAID」。").font(.system(size: 10.5)).foregroundColor(t.color(t.warning))
                        }
                    }
                    .padding(10)
                    .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.surfaceMuted)))
                }
            }

            Divider()
            Toggle(isOn: $softRaid) {
                Text("使用软 RAID(Software RAID)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
            }.tint(t.color(t.accent))
            if softRaid {
                Picker("软 RAID 级别", selection: $softRaidLevel) {
                    ForEach(softRaidOptions, id: \.0) { o in Text(o.1).tag(o.0) }
                }
                .pickerStyle(.menu)
                Text("软 RAID 由 Linux mdadm 管理,不需要硬件 RAID 控制器。所有磁盘将自动加入软 RAID 阵列。")
                    .font(.system(size: 10)).foregroundColor(t.color(t.faint))
            }

            Divider()
            HStack {
                Text("自定义分区方案(可选)").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.muted))
                Spacer()
                Button {
                    smartPlan = buildSmartPlan()
                    showSmart = true
                } label: {
                    HStack(spacing: 4) {
                        Image(systemName: "wand.and.stars").font(.system(size: 10))
                        Text("智能配置").font(.system(size: 11, weight: .semibold))
                    }.foregroundColor(t.color(groups.isEmpty ? t.faint : t.accent))
                }.buttonStyle(.plain).disabled(groups.isEmpty)
                Button {
                    partitions.append(["mount": "/", "fs": "ext4", "size": "0", "raid": softRaid ? "raid\(softRaidLevel)" : "", "group": ""])
                } label: {
                    HStack(spacing: 4) {
                        Image(systemName: "plus.circle").font(.system(size: 11))
                        Text("添加分区").font(.system(size: 11, weight: .semibold))
                    }.foregroundColor(t.color(t.accent))
                }.buttonStyle(.plain)
            }
            Text("留空则使用默认分区。size=0 表示剩余空间。").font(.system(size: 10)).foregroundColor(t.color(t.faint))
            ForEach(partitions.indices, id: \.self) { pi in partitionRow(pi) }
        }
        .padding(11)
        .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surfaceMuted).opacity(0.5)))
    }

    /// 单条自定义分区:第一行挂载点/大小/删除,第二行文件系统/磁盘组(多组时)/RAID 级别
    private func partitionRow(_ pi: Int) -> some View {
        VStack(spacing: 7) {
            HStack(spacing: 6) {
                TextField("挂载点", text: Binding(get: { partitions[pi]["mount"] ?? "" }, set: { partitions[pi]["mount"] = $0 }))
                    .font(.system(size: 11, design: .monospaced))
                TextField("MB", text: Binding(get: { partitions[pi]["size"] ?? "0" }, set: { partitions[pi]["size"] = $0.filter(\.isNumber) }))
                    .font(.system(size: 11, design: .monospaced)).frame(width: 56)
                    .keyboardType(.numberPad)
                Button {
                    partitions.remove(at: pi)
                } label: {
                    Image(systemName: "minus.circle.fill").font(.system(size: 15)).foregroundColor(t.color(t.danger))
                }.buttonStyle(.plain)
            }
            HStack(spacing: 6) {
                Menu {
                    ForEach(fsOptions, id: \.self) { fs in
                        Button(fs) { partitions[pi]["fs"] = fs }
                    }
                } label: {
                    Text(partitions[pi]["fs"] ?? "ext4").font(.system(size: 11, design: .monospaced))
                        .padding(.horizontal, 8).padding(.vertical, 5)
                        .background(RoundedRectangle(cornerRadius: 7).fill(t.color(t.surfaceMuted)))
                }
                if multiGroup {
                    Menu {
                        Button("默认组") { partitions[pi]["group"] = "" }
                        ForEach(groups, id: \.id) { g in
                            Button("磁盘组 \(g.id)") { partitions[pi]["group"] = String(g.id) }
                        }
                    } label: {
                        Text(groupShortLabel(partitions[pi]["group"])).font(.system(size: 10.5, weight: .semibold))
                            .padding(.horizontal, 8).padding(.vertical, 5)
                            .background(RoundedRectangle(cornerRadius: 7).fill(t.color(t.surfaceMuted)))
                    }
                }
                Menu {
                    Button("无 RAID") { partitions[pi]["raid"] = "" }
                    ForEach(softRaidOptions, id: \.0) { o in
                        Button("RAID \(o.0)") { partitions[pi]["raid"] = "raid\(o.0)" }
                    }
                } label: {
                    Text(perRowRaidLabel(partitions[pi]["raid"])).font(.system(size: 10.5, weight: .semibold))
                        .padding(.horizontal, 8).padding(.vertical, 5)
                        .background(RoundedRectangle(cornerRadius: 7).fill(t.color(t.surfaceMuted)))
                }
                Spacer()
            }
        }
        .padding(9)
        .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.surfaceMuted)))
    }

    private func groupShortLabel(_ raw: String?) -> String {
        guard let g = raw, let n = Int(g), n > 0 else { return "默认组" }
        return "组 \(n)"
    }
    private func perRowRaidLabel(_ raw: String?) -> String {
        guard let r = raw, !r.isEmpty else { return "无 RAID" }
        return r.uppercased()
    }

    // MARK: ZFS /var/lib/vz 上限(口径与后端/web 完全一致,否则前端放行、后端 400)
    // 可用 = (RAID0 ? 单盘×盘数 : 单盘)×1024×0.92,再扣 /boot 1GB + swap 8GB + 根目录预留 20GB
    private var zfsCap: (singleDiskGB: Double, diskCount: Int, usableGB: Double, maxVzGB: Int)? {
        guard let g = groups.first, let d = g.disks.first else { return nil }
        let single = d.unit.lowercased().hasPrefix("t") ? d.capacity * 1024 : d.capacity
        let total = zfsRaid == 0 ? single * Double(g.disks.count) : single
        let usableMB = (total * 1024 * 0.92).rounded(.down)
        return (single, g.disks.count, (usableMB / 1024).rounded(.down), max(10, Int((usableMB - 1024 - 8192 - 20480) / 1024)))
    }

    private var zfsCapHint: String {
        guard let c = zfsCap else {
            return diskInfoFailed != nil
                ? "剩余分给根目录(/),上限未知(磁盘信息读取失败,上方可重试)"
                : "剩余分给根目录(/),上限未知(磁盘信息读取中)"
        }
        var s = "剩余分给根目录(/),最大 \(c.maxVzGB) GB"
        if c.singleDiskGB > 0 {
            s += "(\(c.diskCount) × \(Int(c.singleDiskGB))GB,RAID\(zfsRaid) 下实际可用约 \(Int(c.usableGB))GB,已扣除 /boot 1GB、swap 8GB 与根目录预留 20GB)"
        }
        return s
    }

    private var isProxmox9: Bool {
        // web 契约:ZFS 预设仅对 proxmox9_64 有效(精确匹配;模糊 contains 会把其它含
        // "proxmox"+"9" 的模板也当成它,ZFS 配置会被发给不认它的模板)
        (picked ?? "") == "proxmox9_64"
    }
    private var isWindows: Bool { (picked ?? "").lowercased().contains("win") }
    private var multiGroup: Bool { groups.count > 1 }

    /// 挡住提交的读失败清单:只挑当前存储模式真正依赖的项,不搞"任何一处失败全锁死"(web 同口径)
    private var blocking: [(label: String, msg: String, retry: () -> Void)] {
        var list: [(label: String, msg: String, retry: () -> Void)] = []
        if let e = err {
            list.append((label: "系统模板列表", msg: e, retry: { Task { await load() } }))
        }
        if storageMode == .advanced || storageMode == .zfs, let d = diskInfoFailed {
            list.append((label: "磁盘组信息", msg: d, retry: { Task { await loadDiskInfo() } }))
        }
        if storageMode == .advanced, let h = hwRaidFailed {
            list.append((label: "硬件 RAID 支持情况", msg: h, retry: { Task { await loadDiskInfo() } }))
        }
        if storageMode == .scheme, let s = schemeFailed {
            list.append((label: "内置分区方案", msg: s, retry: { Task { await loadSchemes() } }))
        }
        return list
    }

    private var templateCountText: String {
        if let e = err, templates.isEmpty { return "模板列表读取失败,数量未知(\(e))" }
        var base = search.trimmingCharacters(in: .whitespaces).isEmpty
            ? "共 \(templates.count) 个模板"
            : "找到 \(filtered.count) 个匹配模板"
        if let at = templatesAt {
            let f = DateFormatter()
            f.dateFormat = "HH:mm"
            base += " · 拉取于 " + f.string(from: at)
        }
        return base
    }

    // MARK: 智能配置(规则出处 OVH 官方分区文档,与 web lib/smart-storage.ts 同源)

    /// 磁盘快慢排序:数字越小越快,混合盘挑系统盘用
    private func speedRank(_ s: String?) -> Int {
        let k = (s ?? "").lowercased()
        if k.contains("nvme") { return 0 }
        if k.contains("ssd") { return 1 }
        if k.contains("sas") { return 2 }
        if k.contains("sata") || k.contains("hdd") { return 3 }
        return 9
    }
    private func diskTypeLabel(_ s: String?) -> String {
        switch speedRank(s) {
        case 0: return "NVMe 固态"
        case 1: return "SSD 固态"
        case 2: return "SAS 机械"
        case 3: return "SATA 机械"
        default: return (s?.isEmpty == false) ? s! : "未知类型"
        }
    }
    private func groupLabel(_ g: DiskGroupInfo) -> String {
        let d = g.disks.first
        return "\(g.disks.count)×\(Int(d?.capacity ?? 0))\(d?.unit ?? "GB") \(diskTypeLabel(g.diskType))"
    }
    /// 按盘数选软 RAID:2 盘 RAID1,3+ 盘 RAID5;刻意不默认 RAID0(坏一块就全丢,想要满容量是用户的显式选择)
    private func pickRaidLevel(_ diskCount: Int) -> Int? {
        if diskCount <= 1 { return nil }
        if diskCount == 2 { return 1 }
        return 5
    }
    private func buildSmartPlan() -> SmartPlan {
        let gs = groups.filter { $0.id > 0 && !$0.disks.isEmpty }.sorted { $0.id < $1.id }
        if gs.isEmpty {
            return SmartPlan(partitions: [], targetGroupId: 0, notes: [], blocked: "没读到磁盘组信息,无法生成方案")
        }
        if isWindows {
            return SmartPlan(partitions: [], targetGroupId: gs[0].id, notes: [],
                             blocked: "Windows 的分区规则和 Linux 不同(NTFS 只支持 RAID 1),这里不自动生成,请用默认分区方案")
        }
        // 系统装最快的那组;同样快用编号小的(OVH 默认装在 diskGroupId 1)
        let target = gs.sorted { a, b in
            let ra = speedRank(a.diskType), rb = speedRank(b.diskType)
            return ra != rb ? ra < rb : a.id < b.id
        }[0]
        let raid = pickRaidLevel(target.disks.count)
        var notes: [String] = []
        notes.append("系统装在磁盘组 \(target.id)(\(groupLabel(target)))\(gs.count > 1 ? " —— 这组最快" : "")")
        if raid == nil {
            notes.append("只有一块盘,不做 RAID")
        } else if raid == 1 {
            notes.append("2 块盘 → RAID 1 镜像:坏一块数据还在,可用容量是一半")
        } else {
            notes.append("\(target.disks.count) 块盘 → RAID 5:可用约 \(target.disks.count - 1) 块盘的容量,允许坏一块")
        }
        notes.append("/boot 用 ext4 —— OVH 文档明确 /boot 不能用 XFS")
        notes.append("根分区留空 = 占满剩余空间(整份方案只允许一个这样的分区)")
        if gs.count > 1 {
            let others = gs.filter { $0.id != target.id }
            notes.append("另外 \(others.count) 个磁盘组(\(others.map(groupLabel).joined(separator: "、")))这次不动 —— OVH 接口只支持对一个磁盘组做自定义分区。装完进系统自己分区挂载即可,上面的数据不受影响")
        }
        if raid == 5 {
            notes.append("想要满容量可以把根分区改成 RAID 0,但那样坏任意一块盘就全丢,请自行权衡")
        }
        var boot: [String: String] = ["mount": "/boot", "fs": "ext4", "size": "1024", "group": String(target.id)]
        if raid != nil { boot["raid"] = "raid1" }   // /boot 固定镜像:1GB 换「坏盘还能开机」
        var root: [String: String] = ["mount": "/", "fs": "ext4", "size": "0", "group": String(target.id)]
        if let r = raid { root["raid"] = "raid\(r)" }
        return SmartPlan(partitions: [boot, root], targetGroupId: target.id, notes: notes, blocked: nil)
    }

    /// 智能配置确认页:分区是不可逆操作的入口,先看清「装在哪个组、为什么、另一个组会怎样」再应用
    private var smartSheet: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "wand.and.stars", tint: t.accent, title: "智能配置")
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    Text("按这台机器的实际磁盘生成一份分区方案,生成后还能逐条改")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                    VStack(alignment: .leading, spacing: 5) {
                        Text("检测到的磁盘").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        ForEach(groups.filter { $0.id > 0 }, id: \.id) { g in
                            HStack(spacing: 6) {
                                Text("磁盘组 \(g.id):\(groupLabel(g))").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                                if let p = smartPlan, g.id == p.targetGroupId, p.blocked == nil {
                                    Text("← 装系统").font(.system(size: 10.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                                }
                                Spacer()
                            }
                        }
                    }
                    .padding(10)
                    .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.surfaceMuted)))
                    if let b = smartPlan?.blocked {
                        Text(b).font(.system(size: 11)).foregroundColor(t.color(t.warning))
                            .padding(10).frame(maxWidth: .infinity, alignment: .leading)
                            .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.warning).opacity(0.08)))
                    } else if let p = smartPlan {
                        VStack(alignment: .leading, spacing: 5) {
                            Text("将生成的分区").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                            ForEach(p.partitions.indices, id: \.self) { i in
                                let pr = p.partitions[i]
                                let raidTxt = pr["raid"].flatMap { $0.isEmpty ? nil : " \($0.uppercased())" } ?? ""
                                Text("\(pr["mount"] ?? "")  \(pr["fs"] ?? "")  \(pr["size"] == "0" ? "剩余空间" : "\(pr["size"] ?? "")MB")  磁盘组\(pr["group"] ?? "")\(raidTxt)")
                                    .font(.system(size: 10.5, design: .monospaced)).foregroundColor(t.color(t.muted))
                            }
                        }
                        .padding(10)
                        .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.surfaceMuted)))
                        VStack(alignment: .leading, spacing: 5) {
                            ForEach(p.notes.indices, id: \.self) { i in
                                HStack(alignment: .top, spacing: 6) {
                                    Text("·").foregroundColor(t.color(t.accent))
                                    Text(p.notes[i]).font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                                }
                            }
                        }
                    }
                }
                .padding(16)
            }
            HStack(spacing: 10) {
                ActBtn(kind: .ghost, icon: nil, label: "取消") { showSmart = false }
                ActBtn(kind: .primary, icon: "wand.and.stars", label: "应用这份方案") {
                    if let p = smartPlan, p.blocked == nil, !p.partitions.isEmpty {
                        partitions = p.partitions
                        toast.show("已生成 \(p.partitions.count) 个分区,可继续手动调整")
                    }
                    showSmart = false
                }
                .disabled(smartPlan?.blocked != nil || (smartPlan?.partitions.isEmpty ?? true))
            }
            .padding(16)
        }
        .background(t.color(t.bg))
    }

    private func loadDiskInfo() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/hardware-disk-info")
            groups = Self.parseGroups(r["diskGroups"])
            diskInfoFailed = nil
            if let c = zfsCap { vzGB = min(vzGB, Double(max(10, c.maxVzGB))) }
        } catch {
            groups = []
            diskInfoFailed = error.localizedDescription
        }
        // 404/501 = OVH 明说没有硬件 RAID 控制器(业务事实,显示"不支持");
        // 其余错误是"没问到",必须报出来 —— 说成"不支持"会指挥用户白折腾一次不可逆重装
        do {
            let rp = try await conn.client.getDict("/server-control/\(sn)/hardware-raid-profiles")
            hwRaidSupported = (rp["supported"] as? Bool) ?? true
            hwRaidFailed = nil
        } catch let e as ApiClient.ApiError where e.status == 404 || e.status == 501 {
            hwRaidSupported = false
            hwRaidFailed = nil
        } catch {
            hwRaidFailed = error.localizedDescription
        }
    }

    /// 后端 diskGroups 是 {id: {id, diskType, description, raidController, disks:[{capacity,unit,number}]}} 的字典
    private static func parseGroups(_ raw: Any?) -> [DiskGroupInfo] {
        guard let dict = raw as? [String: [String: Any]] else { return [] }
        return dict.values.compactMap { g -> DiskGroupInfo? in
            let id = numToDoubleAny(g["id"]).map(Int.init) ?? 0
            let disks = ((g["disks"] as? [[String: Any]]) ?? []).compactMap { d -> SmartDisk? in
                guard let cap = numToDoubleAny(d["capacity"]) else { return nil }
                return SmartDisk(capacity: cap, unit: (d["unit"] as? String) ?? "GB")
            }
            guard id > 0, !disks.isEmpty else { return nil }
            return DiskGroupInfo(id: id, diskType: g["diskType"] as? String, descriptionText: g["description"] as? String,
                                 raidController: g["raidController"] as? String, disks: disks)
        }.sorted { $0.id < $1.id }
    }

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "opticaldiscdrive.fill", tint: t.danger, title: "重装系统")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    SheetNote(text: "清空系统盘所有数据,不可逆。执行需要 Face ID / 密码确认。", tint: t.danger)
                    SheetNote(text: "已解锁 Windows 后请点「刷新」重新拉模板列表。不熟悉 Windows 系统时,建议直接选 Windows Std 系列。", tint: t.info)

                    HStack {
                        Text("操作系统模板").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        Spacer()
                        Button {
                            Task { await load() }
                        } label: {
                            HStack(spacing: 4) {
                                Image(systemName: "arrow.clockwise").font(.system(size: 10))
                                Text("刷新").font(.system(size: 11, weight: .semibold))
                            }.foregroundColor(t.color(t.accent))
                        }.buttonStyle(.plain)
                    }
                    SheetField(placeholder: "搜索模板(ubuntu / debian / proxmox / windows…)", text: $search)
                    Text(templateCountText).font(.system(size: 10)).foregroundColor(t.color(t.faint))
                    if let e = err, !templates.isEmpty {
                        // 屏幕上这份是旧数据时必须说清楚,不然挑到已下架的模板只会在提交时被 OVH 打回
                        SheetNote(text: "本次刷新失败(\(e)),下面是上次拉到的列表,OVH 那边可能已经增删过模板 —— 请点「刷新」成功后再选。", tint: t.warning)
                    }

                    if loading {
                        VStack(spacing: 8) {
                            ProgressView()
                            Text("正在加载操作系统模板(首次拉取需 3-8 秒,之后会缓存)…")
                                .font(.system(size: 11)).foregroundColor(t.color(t.muted))
                        }.padding(20).frame(maxWidth: .infinity)
                    } else if let e = err, templates.isEmpty {
                        LoadFailed(message: e) { Task { await load() } }
                    } else {
                        VStack(spacing: 6) {
                            // 按发行版分段(web 左右栏的手机版:组头带计数,组内排过序)
                            ForEach(grouped, id: \.kind) { g in
                                VStack(alignment: .leading, spacing: 6) {
                                    HStack(spacing: 6) {
                                        Text(g.label).font(.system(size: 11.5, weight: .bold)).foregroundColor(t.color(t.fg))
                                        Text("\(g.items.count)").font(.system(size: 10, weight: .semibold)).foregroundColor(t.color(t.muted))
                                            .padding(.horizontal, 6).padding(.vertical, 1)
                                            .background(Capsule().fill(t.color(t.surfaceMuted)))
                                        Spacer()
                                    }
                                    .padding(.top, 4)
                                    ForEach(g.items.indices, id: \.self) { i in tplRow(g.items[i]) }
                                }
                            }
                            if filtered.isEmpty {
                                Text("没有匹配的模板").font(.system(size: 11)).foregroundColor(t.color(t.faint)).padding(8)
                            }
                        }
                    }

                    if picked != nil {
                        Text("自定义主机名(可选)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetField(placeholder: "如 server1.example.com", text: $hostname, mono: true)

                        // 存储配置四选一(S-036:OVH 只接受其中一种)
                        VStack(spacing: 8) {
                            storageModeRow(.tplDefault, title: "使用模板默认分区", desc: "最省事,OVH 按模板推荐布局装")
                            if isProxmox9 {
                                storageModeRow(.zfs, title: "Proxmox 9 + ZFS 预设(推荐)", desc: "ZFS 根文件系统 + 独立 /var/lib/vz")
                            }
                            storageModeRow(.scheme, title: "内置分区方案", desc: "选用该模板自带的分区方案")
                            storageModeRow(.advanced, title: "高级存储配置", desc: "硬件 / 软 RAID + 自定义分区")
                        }

                        switch storageMode {
                        case .zfs where isProxmox9:
                            VStack(alignment: .leading, spacing: 8) {
                                Text("Proxmox VE 9 + ZFS 根文件系统").font(.system(size: 12, weight: .bold)).foregroundColor(t.color(t.fg))
                                Text("使用 ZFS 作为根文件系统,提供快照、压缩、数据完整性检查等高级功能。")
                                    .font(.system(size: 10)).foregroundColor(t.color(t.muted))
                                HStack(spacing: 8) {
                                    raidOption(1, "RAID1 · 镜像", "冗余")
                                    raidOption(0, "RAID0 · 条带", "最大容量")
                                }
                                if let c = zfsCap, c.diskCount > 0, c.diskCount < 2, zfsRaid != 0 {
                                    Text("该服务器只检测到 1 块磁盘,无法做镜像,请改用 RAID0。")
                                        .font(.system(size: 10.5)).foregroundColor(t.color(t.danger))
                                }
                                VStack(alignment: .leading, spacing: 4) {
                                    Text("/var/lib/vz 容量(VM/容器存储):\(Int(vzGB)) GB").font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
                                    Slider(value: $vzGB, in: 10...Double(max(10, zfsCap?.maxVzGB ?? 500)), step: 10).tint(t.color(t.accent))
                                    Text(zfsCapHint).font(.system(size: 10)).foregroundColor(t.color(t.faint))
                                }
                            }
                            .padding(10)
                            .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.accent).opacity(0.06)))
                        case .scheme:
                            if let sf = schemeFailed {
                                // 读失败 ≠ 该模板没有方案:这句话会直接指挥用户改重装方案(web 修过的坑)
                                LoadFailed(message: "内置分区方案读取失败:\(sf)") { Task { await loadSchemes() } }
                            } else if schemes.isEmpty {
                                Text("该模板没有内置分区方案,请改用其它存储模式。").font(.system(size: 10.5)).foregroundColor(t.color(t.muted)).padding(6)
                            } else {
                                ForEach(schemes.indices, id: \.self) { i in
                                    let sc = schemes[i]
                                    Button { pickedScheme = pickedScheme == sc.name ? nil : sc.name } label: {
                                        HStack {
                                            Image(systemName: pickedScheme == sc.name ? "checkmark.circle.fill" : "circle").font(.system(size: 14)).foregroundColor(t.color(pickedScheme == sc.name ? t.accent : t.faint))
                                            Text("\(sc.name) · 优先级 \(sc.priority)").font(.system(size: 11.5)).foregroundColor(t.color(t.fg))
                                            Spacer()
                                        }
                                        .padding(9)
                                        .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.surfaceMuted)))
                                    }.buttonStyle(.plain)
                                }
                            }
                        case .advanced:
                            advancedStoragePanel
                        default:
                            EmptyView()
                        }

                        // 读失败总览:有任何一条就锁住提交(web blockingErrors 同口径)
                        if !blocking.isEmpty {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("有配置没读出来,已暂时锁住重装按钮").font(.system(size: 11, weight: .bold)).foregroundColor(t.color(t.danger))
                                Text("重装会清空全部数据且不可撤销。这些数据没拿到时,界面对应位置显示的并不是这台机器的真实情况,照着选可能装出完全不同的系统。")
                                    .font(.system(size: 10)).foregroundColor(t.color(t.muted))
                                ForEach(blocking.indices, id: \.self) { i in
                                    let b = blocking[i]
                                    HStack(alignment: .top) {
                                        Text("\(b.label)读取失败 · \(b.msg)").font(.system(size: 10)).foregroundColor(t.color(t.muted)).lineLimit(3)
                                        Spacer()
                                        Button("重试", action: b.retry)
                                            .font(.system(size: 10.5, weight: .semibold)).foregroundColor(t.color(t.accent))
                                    }
                                }
                            }
                            .padding(10)
                            .background(RoundedRectangle(cornerRadius: 10).stroke(t.color(t.danger).opacity(0.5), lineWidth: 1))
                        }

                        Text("输入机器名确认").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetField(placeholder: sn, text: $confirmName, mono: true)

                        let blocked = !blocking.isEmpty
                        ActBtn(kind: .danger, icon: "faceid", label: busy ? "提交中…" : (blocked ? "配置未读全,暂不可重装" : "面容确认并重装"), busy: busy) {
                            await submit()
                        }.disabled(blocked)
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.large])
        .task { await load() }
        .task { await loadDiskInfo() }
        .onChange(of: picked) { _ in
            // web 同款收敛:选 proxmox9 → 默认分区切到 ZFS 预设;离开 proxmox9 → ZFS 非法回落默认
            if isProxmox9 {
                if storageMode == .tplDefault { storageMode = .zfs }
            } else if storageMode == .zfs {
                storageMode = .tplDefault
            }
            pickedScheme = nil
            Task { await loadSchemes() }
        }
        .onChange(of: zfsRaid) { _ in
            // RAID0→RAID1 可用容量砍半,vz 跟着收(web 同款)
            if let c = zfsCap { vzGB = min(vzGB, Double(max(10, c.maxVzGB))) }
        }
        .sheet(isPresented: $showSmart) { smartSheet }
    }

    private var filtered: [[String: Any]] {
        let q = search.trimmingCharacters(in: .whitespaces).lowercased()
        guard !q.isEmpty else { return templates }
        // 搜索匹配 templateName/distribution/family(web 同款)
        return templates.filter {
            (($0["templateName"] as? String ?? "") + ($0["distribution"] as? String ?? "") + ($0["family"] as? String ?? "")).lowercased().contains(q)
        }
    }

    /// 按使用频率排序的 OS 分组(web OS_GROUPS 同款);组内按 templateName 排序
    private var grouped: [(kind: String, label: String, items: [[String: Any]])] {
        var buckets: [String: [[String: Any]]] = [:]
        for tpl in filtered {
            let k = Self.osKindOf(tpl)
            buckets[k, default: []].append(tpl)
        }
        var out: [(String, String, [[String: Any]])] = []
        for (kind, label) in Self.osGroupLabels {
            guard var arr = buckets[kind] else { continue }
            arr.sort { ($0["templateName"] as? String ?? "") < ($1["templateName"] as? String ?? "") }
            out.append((kind, label, arr))
        }
        return out
    }

    /// OS 分组判定(web OsIcon detectOsKind 同款:templateName+distribution+family 拼串包含匹配)
    private static func osKindOf(_ tpl: [String: Any]) -> String {
        let s = (((tpl["templateName"] as? String ?? "") + " " + (tpl["distribution"] as? String ?? "") + " " + (tpl["family"] as? String ?? ""))).lowercased()
        if s.contains("byolinux") { return "byolinux" }
        if s.contains("byoi") { return "byoi" }
        if s.contains("proxmox") { return "proxmox" }
        if s.contains("esxi") || s.contains("vmware") { return "esxi" }
        if s.contains("windows") || s.contains("win-") { return "windows" }
        if s.contains("debian") { return "debian" }
        if s.contains("ubuntu") { return "ubuntu" }
        if s.contains("rocky") { return "rocky" }
        if s.contains("alma") { return "alma" }
        if s.contains("fedora") { return "fedora" }
        if s.contains("centos") { return "centos" }
        if s.contains("suse") { return "opensuse" }
        if s.contains("freebsd") { return "freebsd" }
        return "linux"
    }

    private static let osGroupLabels: [(String, String)] = [
        ("debian", "Debian"), ("ubuntu", "Ubuntu"), ("windows", "Windows"), ("proxmox", "Proxmox VE"),
        ("rocky", "Rocky Linux"), ("alma", "AlmaLinux"), ("fedora", "Fedora"), ("esxi", "VMware ESXi"),
        ("centos", "CentOS"), ("opensuse", "openSUSE"), ("freebsd", "FreeBSD"),
        ("byoi", "BYOI(镜像导入)"), ("byolinux", "BYO Linux"), ("linux", "其他 Linux"),
    ]

    private func tplRow(_ tpl: [String: Any]) -> some View {
        let name = tpl["templateName"] as? String ?? ""
        let on = picked == name
        let family = tpl["family"] as? String ?? ""
        return Button { picked = name } label: {
            HStack(spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(name).font(.system(size: 12, weight: .semibold, design: .monospaced)).foregroundColor(t.color(t.fg)).lineLimit(1)
                    Text("\(tpl["distribution"] as? String ?? "")\(family.isEmpty ? "" : " · \(family)") · \(tpl["bitFormat"] ?? 64) 位")
                        .font(.system(size: 10)).foregroundColor(t.color(t.muted)).lineLimit(1)
                }
                Spacer()
                if on {
                    Image(systemName: "checkmark.circle.fill").font(.system(size: 16)).foregroundColor(t.color(t.accent))
                }
            }
            .padding(11)
            .background(RoundedRectangle(cornerRadius: 12).fill(t.color(on ? t.accent : t.surface).opacity(0.08)).overlay(RoundedRectangle(cornerRadius: 12).stroke(t.color(on ? t.accent : t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/templates")
            templates = (r["templates"] as? [[String: Any]]) ?? []
            templatesAt = Date()
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func loadSchemes() async {
        guard let tpl = picked else { schemes = []; pickedScheme = nil; return }
        schemeFailed = nil
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/partition-schemes?templateName=\(urlEncode(tpl))")
            schemes = ((r["schemes"] as? [[String: Any]]) ?? []).compactMap { raw in
                guard let n = (raw["name"] as? String) ?? (raw["schemeName"] as? String) else { return nil }
                return (name: n, priority: numToDoubleAny(raw["priority"]).map(Int.init) ?? 0)
            }
        } catch {
            // 读失败 ≠ 该模板没有方案:清空并记错,锁住 scheme 模式的提交并给重试(web 修过的坑)
            schemes = []
            schemeFailed = error.localizedDescription
        }
    }

    private func submit() async {
        guard let tpl = picked else { return }
        guard confirmName.trimmingCharacters(in: .whitespaces) == sn else {
            return toast.show("机器名不匹配,请输入完整机器名", error: true)
        }
        guard await Biometric.require("重装系统") else { return }
        busy = true
        defer { busy = false }
        var body: [String: Any] = ["templateName": tpl]
        let h = hostname.trimmingCharacters(in: .whitespaces)
        if !h.isEmpty {
            // OVH customizations.hostname 只接受合法主机名/FQDN,非法值会被 OVH 以英文错误码打回(正则与 web 同源)
            let okHost = h.range(of: #"^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$"#, options: .regularExpression) != nil
            guard okHost else {
                toast.show("Hostname 只能包含字母、数字、连字符和点,且不能以连字符开头或结尾", error: true)
                return
            }
            body["customHostname"] = h
        }
        let blocked = !blocking.isEmpty
        if blocked {
            toast.show("\(blocking.map(\.label).joined(separator: "、"))没读出来,重试成功后再提交重装", error: true)
            return
        }
        switch storageMode {
        case .zfs where isProxmox9:
            // 单盘机器做不了镜像,后端会 400;这里提前拦(web 同款)
            if let c = zfsCap, c.diskCount > 0, c.diskCount < 2, zfsRaid != 0 {
                toast.show("该服务器只有 1 块磁盘,无法使用 ZFS RAID1,请改用 RAID0", error: true)
                return
            }
            body["useProxmox9Zfs"] = true
            body["zfsRaidLevel"] = zfsRaid
            body["zfsVzSize"] = Int(vzGB) * 1024
        case .scheme:
            if let s = pickedScheme, !s.isEmpty { body["partitionSchemeName"] = s }
        case .advanced:
            // 组装契约与 web useReinstallServer 完全一致:
            // 分区按磁盘组分组成多个 storage 条目;没选组不带 diskGroupId(交给 OVH 默认组);
            // 软 RAID 开但没填分区 → 默认根分区软 RAID;硬件 RAID 与分区同组时并入同一条目
            struct Entry {
                var gid: Int? = nil
                var layout: [[String: Any]] = []
                var hwRaid: [[String: Any]] = []
            }
            var parts = partitions.filter { !($0["mount"] ?? "").trimmingCharacters(in: .whitespaces).isEmpty }
            let useCustom = softRaid || hwRaidByGroup.values.contains(where: { $0 >= 0 }) || !parts.isEmpty
            if useCustom {
                if softRaid && parts.isEmpty {
                    parts = [["mount": "/", "fs": "ext4", "size": "0", "raid": "raid\(softRaidLevel)", "group": ""]]
                }
                var order: [String] = []
                var entries: [String: Entry] = [:]
                for p in parts {
                    let gid = Int(p["group"] ?? "") ?? 0
                    let key = gid > 0 ? String(gid) : "default"
                    if entries[key] == nil { order.append(key); entries[key] = Entry(gid: gid > 0 ? gid : nil) }
                    var item: [String: Any] = [
                        "mountPoint": p["mount"] ?? "/",
                        "fileSystem": p["fs"] ?? "ext4",
                        "size": Int(p["size"] ?? "0") ?? 0,
                    ]
                    if let r = p["raid"], !r.isEmpty, let lvl = Int(r.dropFirst(4)) { item["raidLevel"] = lvl }
                    entries[key]?.layout.append(item)
                }
                let hw = hwRaidByGroup.filter { $0.value >= 0 }.sorted { $0.key < $1.key }
                for (gid, lvl) in hw {
                    // 只有一个(默认)分组时,硬件 RAID 必须和分区落在同一个条目,否则成了"两个组各配一半"
                    var key = gid > 0 ? String(gid) : "default"
                    if order.count == 1 && order.first == "default" { key = "default" }
                    if entries[key] == nil { order.append(key); entries[key] = Entry(gid: key == "default" ? nil : gid) }
                    var item: [String: Any] = ["raidLevel": lvl]
                    let count = groups.first(where: { $0.id == gid })?.disks.count ?? 0
                    if count > 0 { item["disks"] = count }   // 0 = 不知道几块盘,省略让 OVH 用默认
                    entries[key]?.hwRaid.append(item)
                }
                let storage: [[String: Any]] = order.map { key in
                    var e: [String: Any] = [:]
                    let entry = entries[key]!
                    if let g = entry.gid { e["diskGroupId"] = g }
                    if !entry.layout.isEmpty { e["partitioning"] = ["layout": entry.layout] }
                    if !entry.hwRaid.isEmpty { e["hardwareRaid"] = entry.hwRaid }
                    return e
                }.filter { !$0.isEmpty }
                if !storage.isEmpty { body["storageConfig"] = storage }
            }
        default:
            break   // 模板默认分区:不带任何存储字段
        }
        do {
            let resp = try await conn.client.post("/server-control/\(sn)/install", body: body)
            // 后端忽略了哪份配置必须让用户看见:装是装得成,但他填的东西没生效
            let warns = (resp["warnings"] as? [String]) ?? []
            if warns.isEmpty {
                toast.show("重装任务已提交(通常 5-10 分钟)")
            } else {
                toast.show("重装任务已提交;但部分配置被后端忽略:" + warns.joined(separator: ";"), error: true)
            }
            dismiss()
        } catch { toast.show(error.localizedDescription, error: true) }
    }
}

func urlEncode(_ s: String) -> String {
    s.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? s
}

// MARK: - IPMI / KVM

struct IpmiSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let sn: String
    var t: Tokens { theme.t }

    @State private var types: [[String: Any]] = []
    @State private var pickedType: String?
    @State private var loading = true
    @State private var err: String?
    @State private var requesting = false
    @State private var result: [String: Any]?
    @State private var ipmiActivated: Bool? = nil

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "keyboard.onehanded.left", tint: t.info, title: "IPMI / KVM 控制台")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else {
                        SheetNote(text: "链接仅当次有效。", tint: t.faint)
                        if let act = ipmiActivated, act == false {
                            SheetNote(text: "该服务器 IPMI 显示未激活,申请可能失败;如失败请先到 OVH 后台启用 IPMI。", tint: t.warning)
                        }
                        if types.isEmpty { EmptyHint(icon: "keyboard", text: "该服务器不支持 KVM / SOL 控制台。") }
                        else {
                        ForEach(types.indices, id: \.self) { i in
                            let name = types[i]["type"] as? String ?? "—"
                            if name.contains("Jnlp") {
                                SheetNote(text: "kvmipJnlp:需要本机装 Java Web Start。新版 JDK 已移除它,可用 OpenWebStart 打开 .jnlp。", tint: t.muted)
                            } else if name.lowercased().contains("ssh") {
                                SheetNote(text: "serialOverLanSshKey:用 OVH 账户里已登记的 SSH 公钥连接。", tint: t.muted)
                            }
                        }
                        }

                        Text("接入方式").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        VStack(spacing: 6) {
                            ForEach(types.indices, id: \.self) { i in typeRow(types[i]) }
                            if types.isEmpty { Text("OVH 未返回接入方式").font(.system(size: 11)).foregroundColor(t.color(t.faint)) }
                        }

                        if let r = result {
                            Card(border: t.success) {
                                VStack(alignment: .leading, spacing: 7) {
                                    Text("申请成功").font(.system(size: 12.5, weight: .bold)).foregroundColor(t.color(t.success))
                                    if let at = r["accessType"] as? String, !at.isEmpty {
                                        KV(k: "访问类型", v: at, mono: true)
                                    }
                                    if let u = (r["console"] as? [String: Any])?["value"] as? String ?? r["url"] as? String, !u.isEmpty {
                                        if let link = URL(string: u) {
                                            Link(destination: link) {
                                                HStack(spacing: 4) {
                                                    Image(systemName: "safari").font(.system(size: 11))
                                                    Text("在 Safari 打开控制台").font(.system(size: 12, weight: .semibold))
                                                }.foregroundColor(t.color(t.info))
                                            }
                                        }
                                        Text(u).font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.fg)).lineLimit(3)
                                            .textSelection(.enabled)
                                    }
                                    if let m = r["message"] as? String ?? r["note"] as? String {
                                        Text(m).font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                                    }
                                }
                            }
                        }

                        ActBtn(kind: .primary, icon: "arrow.down.circle", label: requesting ? "申请中(最长 20 秒)…" : "申请远程控制台") {
                            await request()
                        }
                        .disabled(pickedType == nil || pickedType?.isEmpty == true)
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .task { await load() }
    }

    private func typeRow(_ ty: [String: Any]) -> some View {
        let name = ty["type"] as? String ?? (ty["name"] as? String ?? "—")
        let _ = name
        let on = pickedType == name
        return Button { pickedType = name } label: {
            HStack {
                VStack(alignment: .leading, spacing: 1) {
                    Text(name).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                    if let d = ty["description"] as? String, !d.isEmpty {
                        Text(d).font(.system(size: 10)).foregroundColor(t.color(t.muted)).lineLimit(1)
                    }
                }
                Spacer()
                if on { Image(systemName: "checkmark.circle.fill").font(.system(size: 15)).foregroundColor(t.color(t.accent)) }
            }
            .padding(11)
            .background(RoundedRectangle(cornerRadius: 11).fill(t.color(on ? t.accent : t.surface).opacity(0.08)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(on ? t.accent : t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/ipmi-types")
            ipmiActivated = r["activated"] as? Bool
            // handler: supportedTypes(字符串数组)+ typeLabels + defaultType
            let labels = r["typeLabels"] as? [String: String] ?? [:]
            let sup = (r["supportedTypes"] as? [String]) ?? []
            types = sup.map { ["type": $0, "description": labels[$0] ?? ""] }
            pickedType = (r["defaultType"] as? String) ?? types.first?["type"] as? String
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func request() async {
        guard let ty = pickedType else { return }
        requesting = true
        defer { requesting = false }
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/console?type=\(urlEncode(ty))")
            result = r
        } catch {
            toast.show(error.localizedDescription, error: true)
        }
    }
}

// MARK: - 启动模式

struct BootModeSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let sn: String
    var t: Tokens { theme.t }

    @State private var boots: [[String: Any]] = []
    @State private var picked: Int?
    @State private var loading = true
    @State private var err: String?
    @State private var busy = false

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "memorychip", tint: t.muted, title: "启动模式")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else {
                        SheetNote(text: "切换启动模式后 OVH 会自动重启服务器。硬盘 = 正常引导;救援 = 救援镜像;网络 =网络启动(iPXE)。", tint: t.info)
                        if boots.isEmpty {
                            EmptyHint(icon: "memorychip", text: "暂无可选启动模式")
                        }
                        VStack(spacing: 6) {
                            ForEach(boots.indices, id: \.self) { i in bootRow(boots[i]) }
                        }
                        if picked != nil {
                            ActBtn(kind: .primary, icon: "arrow.triangle.2.circlepath", label: busy ? "切换中…" : "切换并重启") {
                                await apply()
                            }
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .task { await load() }
    }

    private func bootRow(_ b: [String: Any]) -> some View {
        let id = (b["id"] as? Int) ?? -1
        let type = (b["bootType"] as? String ?? "unknown").lowercased()
        let isCurrent = (b["active"] as? Bool ?? false) || (b["isCurrent"] as? Bool ?? false)
        let rowErr = (b["error"] as? String ?? "").isEmpty ? nil : (b["error"] as! String)
        let on = picked == id
        // 图标映射对齐 web:硬盘/救援盾/电源/网络,其余数据库
        let iconName: String
        switch type {
        case "rescue", "ipxe": iconName = "lifepreserver"
        case "harddisk", "disk": iconName = "internaldrive"
        case "power", "poweroff", "off": iconName = "power"
        case "network": iconName = "wifi"
        default: iconName = "internaldrive"
        }
        return Button { if id >= 0, rowErr == nil { picked = id } } label: {
            HStack(spacing: 10) {
                Image(systemName: iconName).font(.system(size: 14)).foregroundColor(t.color(rowErr != nil ? t.faint : t.muted))
                VStack(alignment: .leading, spacing: 1) {
                    HStack(spacing: 6) {
                        Text(b["description"] as? String ?? type).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(rowErr != nil ? t.faint : t.fg)).lineLimit(1)
                        if isCurrent { Chip(text: "当前", color: t.success) }
                    }
                    Text("\(type) · bootId \(id)").font(.system(size: 9.5, design: .monospaced)).foregroundColor(t.color(t.muted))
                    if let e = rowErr {
                        // 详情拉取失败的占位行:选中它提交等于拿未知配置重启机器(web 会禁用)
                        Text("该启动模式详情获取失败,暂不可选(\(e))").font(.system(size: 9.5)).foregroundColor(t.color(t.danger))
                    }
                }
                Spacer()
                if on { Image(systemName: "checkmark.circle.fill").font(.system(size: 15)).foregroundColor(t.color(t.accent)) }
            }
            .padding(11)
            .background(RoundedRectangle(cornerRadius: 11).fill(t.color(on ? t.accent : t.surface).opacity(0.08)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(on && !isCurrent ? t.accent : t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
        .disabled(rowErr != nil)
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/boot-mode")
            boots = (r["bootModes"] as? [[String: Any]]) ?? (r["boots"] as? [[String: Any]]) ?? []
            if let cur = boots.first(where: { ($0["active"] as? Bool ?? false) || ($0["isCurrent"] as? Bool ?? false) }) {
                picked = numToDoubleAny(cur["id"]).map(Int.init) ?? nil
            }
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func apply() async {
        guard let id = picked else { return }
        busy = true
        defer { busy = false }
        let body = try? JSONSerialization.data(withJSONObject: ["bootId": id])
        let (ok, msg) = await conn.client.actionPutData("/server-control/\(sn)/boot-mode", bodyData: body)
        guard ok else {
            toast.show(msg.isEmpty ? "切换启动模式失败" : msg, error: true)
            return
        }
        // 不重启模式不生效(web BootModeDialog:PUT 成功后自动 reboot)
        let (rok, rmsg) = await conn.client.actionPostData("/server-control/\(sn)/reboot", bodyData: nil)
        if rok {
            toast.show("启动模式已切换,重启任务已提交,几分钟后生效")
            dismiss()
        } else {
            toast.show("启动模式已切换,但重启失败(\(rmsg)),请手动重启", error: true)
        }
    }
}

// MARK: - Windows SPLA

struct SplaSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let sn: String
    var t: Tokens { theme.t }

    @State private var list: [[String: Any]] = []
    @State private var loading = true
    @State private var err: String?
    @State private var splaPartial = false
    @State private var serial = ""
    @State private var type = "os"
    @State private var busy = false

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "pc", tint: t.info, title: "Windows 授权 (SPLA)")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    if loading {
                        ProgressView().padding(30)
                    } else {
                        if let e = err {
                            // 读失败不锁死表单(web 同款):手动登记不受影响,
                            // 只警告"无法判断是否已解锁"——OVH 会直接拒绝重复登记
                            VStack(alignment: .leading, spacing: 6) {
                                Text("已登记列表读取失败:\(e)。无法判断这台机器是否已解锁 —— OVH 会直接拒绝重复登记,确定没登记过再点一键解锁。")
                                    .font(.system(size: 10.5)).foregroundColor(t.color(t.warning))
                                Button("重试") { Task { await load() } }
                                    .font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.accent))
                            }
                            .padding(10)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.warning).opacity(0.08)))
                        }
                        if splaPartial {
                            // 有部分记录详情拉失败,"尚未登记任何授权"是断言不是事实
                            SheetNote(text: "部分授权记录的详情没读到,下面的列表可能不完整,\"已解锁\"判断按未登记处理。", tint: t.warning)
                        }
                        VStack(alignment: .leading, spacing: 6) {
                            Text("OVH 把 Windows 模板锁在「这台机器名下有操作系统授权记录」后面。点一下只登记这一类,用的是微软公开发布的 Windows KMS 客户端密钥 —— 它只让 OVH 的检查通过,不代表你持有 Windows Server 授权,系统装好后仍需能连上 KMS 服务器才会真正激活。")
                                .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                            Text("SQL Server 的两类授权不在这里 —— 那要你真的买了 SQL 授权才谈得上登记,请用下面的表单填自己的序列号。")
                                .font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                        }
                        .padding(10)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.info).opacity(0.06)))

                        Text("已登记授权").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        if list.isEmpty {
                            Text(splaPartial ? "授权记录状态未知" : "尚未登记任何授权").font(.system(size: 11)).foregroundColor(t.color(t.faint))
                        } else {
                            ForEach(list.indices, id: \.self) { i in
                                let it = list[i]
                                KV(k: splaTypeName(it["type"] as? String ?? ""), v: (it["serialNumber"] as? String ?? "—"), mono: true)
                            }
                        }

                        Divider().overlay(t.color(t.border))

                        Text("手动登记自己的 SPLA").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetNote(text: "这里填你自己购买的 SPLA 授权序列号(SQL Server 的两类只能走这里)。这一步是把授权登记到 OVH 名下,不是申请或生成授权 —— 登记本身不会让你凭空拥有授权。", tint: t.warning)
                        Picker("类型", selection: $type) {
                            Text("操作系统 (Windows Server)").tag("os")
                            Text("SQL Server 标准版").tag("sqlstd")
                            Text("SQL Server 网页版").tag("sqlweb")
                        }
                        .pickerStyle(.segmented)
                        SheetField(placeholder: "SPLA 序列号", text: $serial, mono: true)
                        ActBtn(kind: .ghost, icon: "square.and.arrow.down", label: busy ? "登记中…" : "登记序列号") {
                            await register()
                        }

                        Divider().overlay(t.color(t.border))
                        // 已解锁 = 存在未终止的 os 授权(web hasActiveSpla:status != terminated,
                        // waitingToCheck 也算;terminated 的记录不算,允许重新登记)
                        let hasOs = !splaPartial && list.contains { ($0["type"] as? String) == "os" && (($0["status"] as? String ?? "") != "terminated") }
                        ActBtn(kind: hasOs ? .ghost : .primary, icon: hasOs ? "checkmark.shield.fill" : "unlock.fill",
                               label: loading ? "检查中…" : (hasOs ? "已解锁,无需重复登记" : "一键解锁 Windows 安装")) {
                            await quickUnlock()
                        }.disabled(hasOs)
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .task { await load() }
    }

    private func splaTypeName(_ s: String) -> String {
        ["os": "操作系统", "sqlstd": "SQL Server 标准版", "sqlweb": "SQL Server 网页版"][s] ?? s
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/spla")
            list = (r["splaList"] as? [[String: Any]]) ?? []
            splaPartial = (r["partial"] as? Bool ?? false) || !(r["success"] as? Bool ?? true)
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func register() async {
        let s = serial.trimmingCharacters(in: .whitespaces)
        guard !s.isEmpty else { return toast.show("先填序列号", error: true) }
        busy = true
        defer { busy = false }
        do {
            _ = try await conn.client.post("/server-control/\(sn)/spla", body: ["type": type, "serialNumber": s])
            toast.show("授权已登记")
            await load()
        } catch { toast.show(error.localizedDescription, error: true) }
    }

    /// 微软公开的 Windows 10 Pro KMS 客户端安装密钥(GVLK),
    /// 与 web 端 SplaDialog 的"一键解锁"同一实现
    private static let WINDOWS_GVLK = "W269N-WFGWX-YVC9B-4J6C9-T83GX"

    private func quickUnlock() async {
        busy = true
        defer { busy = false }
        do {
            _ = try await conn.client.post("/server-control/\(sn)/spla", body: ["type": "os", "serialNumber": Self.WINDOWS_GVLK])
            toast.show("已登记,刷新后重装列表里就会出现 Windows 模板")
            await load()
        } catch { toast.show(error.localizedDescription, error: true) }
    }
}

// MARK: - 运维任务 + 预约

struct TasksSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    let sn: String
    var t: Tokens { theme.t }

    @State private var tasks: [[String: Any]] = []
    @State private var loading = true
    @State private var err: String?
    @State private var slotsTaskId: String?
    @State private var slots: [[String: Any]] = []
    @State private var slotsMsg: String?
    @State private var backedUp = false
    @State private var busy = false
    @State private var pickedSlot: Int? = nil

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "checklist", tint: t.muted, title: "运维任务")
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else {
                        if slotsTaskId == nil {
                            HStack {
                                Text("近 10 条任务记录").font(.system(size: 11)).foregroundColor(t.color(t.faint))
                                Spacer()
                                Button { Task { await load() } } label: {
                                    HStack(spacing: 4) {
                                        Image(systemName: "arrow.clockwise").font(.system(size: 10))
                                        Text("刷新").font(.system(size: 11, weight: .semibold))
                                    }.foregroundColor(t.color(t.accent))
                                }.buttonStyle(.plain)
                            }
                            if tasks.isEmpty {
                                EmptyHint(icon: "checkmark.circle", text: "暂无任务记录")
                            } else {
                                ForEach(tasks.indices, id: \.self) { i in taskRow(tasks[i]) }
                            }
                        } else {
                            slotPicker
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

    private func taskRow(_ task: [String: Any]) -> some View {
        let id = String(describing: task["taskId"] ?? "")
        let status = task["status"] as? String ?? "unknown"
        // 详情拉取失败的占位行(function=N/A/status=unknown/error):标出来,
        // 不然渲染成一个"unknown 状态的 N/A 任务"会被当成机器真实状态
        let rowErr = (task["error"] as? String ?? "").isEmpty ? nil : (task["error"] as! String)
        return VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Dot(color: rowErr != nil ? t.faint : statusColor(status))
                Text(task["function"] as? String ?? "—").font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                Spacer()
                if rowErr != nil {
                    Chip(text: "获取失败", color: t.danger)
                } else {
                    Chip(text: statusCn(status), color: statusColor(status))
                }
            }
            if let re = rowErr {
                Text(re).font(.system(size: 9.5)).foregroundColor(t.color(t.danger))
            }
            if let c = task["comment"] as? String, !c.isEmpty {
                Text(c).font(.system(size: 10.5)).foregroundColor(t.color(t.muted)).lineLimit(2)
            }
            HStack {
                Text("#" + id + " · \(fmtDate(task["startDate"] as? String)) → \(fmtDate(task["doneDate"] as? String))")
                    .font(.system(size: 10)).foregroundColor(t.color(t.faint))
                Spacer()
                if rowErr == nil {
                    Button { Task { await loadSlots(id) } } label: {
                        Text("预约时段").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.info))
                    }.buttonStyle(.plain)
                }
            }
        }
        .padding(11)
        .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 12).stroke(t.color(t.border), lineWidth: 1)))
    }

    private var slotPicker: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Button { slotsTaskId = nil; slots = []; pickedSlot = nil } label: {
                    HStack(spacing: 4) {
                        Image(systemName: "chevron.left").font(.system(size: 11, weight: .semibold))
                        Text("返回任务列表").font(.system(size: 11.5, weight: .semibold))
                    }.foregroundColor(t.color(t.accent))
                }.buttonStyle(.plain)
                Spacer()
                Button { Task { await loadSlots(slotsTaskId ?? "") } } label: {
                    HStack(spacing: 4) {
                        Image(systemName: "arrow.clockwise").font(.system(size: 10))
                        Text("重新查询").font(.system(size: 11, weight: .semibold))
                    }.foregroundColor(t.color(t.accent))
                }.buttonStyle(.plain)
            }
            if let m = slotsMsg {
                SheetNote(text: m, tint: t.info)
            }
            if slots.isEmpty && slotsMsg == nil {
                Text("加载可用时段…").font(.system(size: 11)).foregroundColor(t.color(t.faint))
            }
            // 先点选时段、再点「预约此时段」确认(web 同款):物理干预一点就提交太容易误触
            ForEach(slots.indices, id: \.self) { i in
                let s = slots[i]
                let on = pickedSlot == i
                Button { pickedSlot = i } label: {
                    HStack {
                        Text(slotText(s)).font(.system(size: 11.5)).foregroundColor(t.color(t.fg))
                        Spacer()
                        if on {
                            Image(systemName: "checkmark.circle.fill").font(.system(size: 14)).foregroundColor(t.color(t.accent))
                        }
                    }
                    .padding(11)
                    .background(RoundedRectangle(cornerRadius: 11).fill(t.color(on ? t.accent : t.surface).opacity(on ? 0.1 : 1)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(on ? t.accent : t.border), lineWidth: 1)))
                }
                .buttonStyle(.plain)
                .disabled(busy)
            }
            Toggle(isOn: $backedUp) {
                Text("我已完成数据备份").font(.system(size: 12)).foregroundColor(t.color(t.fg))
            }.tint(t.color(t.accent))
            SheetNote(text: "预约的是机房物理干预(换硬件等)。未备份就预约可能丢数据。", tint: t.warning)
            if !slots.isEmpty {
                ActBtn(kind: .primary, icon: "calendar.badge.plus",
                       label: busy ? "预约中…" : "预约此时段",
                       busy: busy) {
                    if let i = pickedSlot { await schedule(slots[i]) }
                }
                .disabled(pickedSlot == nil)
            }
        }
    }

    private func slotText(_ s: [String: Any]) -> String {
        let begin = s["beginAt"] as? String ?? (s["startDate"] as? String ?? "")
        let end = s["endAt"] as? String ?? (s["endDate"] as? String ?? "")
        return "\(fmtDate(begin)) ~ \(fmtDate(end).components(separatedBy: " ").last ?? "")"
    }

    private func statusColor(_ s: String) -> String {
        switch s.lowercased() {
        case "done": return t.success
        case "todo", "doing": return t.warning
        case "error", "cancelled": return t.danger
        default: return t.muted
        }
    }
    private func statusCn(_ s: String) -> String {
        ["done": "已完成", "todo": "待执行", "doing": "执行中", "error": "出错", "cancelled": "已取消"][s.lowercased()] ?? s
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/tasks")
            tasks = (r["tasks"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func loadSlots(_ taskId: String) async {
        slotsTaskId = taskId
        slotsMsg = nil
        pickedSlot = nil
        do {
            let end = ISO8601DateFormatter().string(from: Calendar.current.date(byAdding: .day, value: 14, to: Date()) ?? Date())
            let now = ISO8601DateFormatter().string(from: Date())
            let r = try await conn.client.getDict("/server-control/\(sn)/tasks/\(taskId)/available-timeslots?periodStart=\(urlEncode(now))&periodEnd=\(urlEncode(end))")
            slots = (r["timeslots"] as? [[String: Any]]) ?? []
            if r["scheduleNotRequired"] as? Bool == true || slots.isEmpty {
                slotsMsg = r["message"] as? String ?? "未来 14 天没有可选时段"
            }
        } catch {
            slotsMsg = error.localizedDescription
        }
    }

    private func schedule(_ slot: [String: Any]) async {
        guard let taskId = slotsTaskId,
              let begin = slot["beginAt"] as? String ?? slot["startDate"] as? String else { return }
        busy = true
        defer { busy = false }
        let body = try? JSONSerialization.data(withJSONObject: ["wantedBeginingDate": begin, "hasPerformedBackup": backedUp])
        let (ok, msg) = await conn.client.actionPostData("/server-control/\(sn)/tasks/\(taskId)/schedule", bodyData: body)
        toast.show(ok ? "已预约" : (msg.isEmpty ? "预约失败" : msg), error: !ok)
        if ok { slotsTaskId = nil; await load() }
    }
}

// MARK: - BIOS

struct BiosSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let sn: String
    var t: Tokens { theme.t }

    @State private var bios: [String: Any]?
    @State private var sgx: [String: Any]?
    @State private var err: String?
    @State private var loading = true

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "cpu", tint: t.muted, title: "BIOS 设置(只读)")
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else {
                        if let b = bios {
                            Card { VStack(spacing: 8) { SectionTitle(text: "BIOS"); kvRows(b) } }
                        } else {
                            EmptyHint(icon: "cpu", text: "未获取到 BIOS 设置")
                        }
                        if let s = sgx {
                            Card { VStack(spacing: 8) { SectionTitle(text: "SGX(Intel 软件防护扩展)"); kvRows(s) } }
                        }
                        Button {
                            Task { await load() }
                        } label: {
                            HStack(spacing: 4) {
                                Image(systemName: "arrow.clockwise").font(.system(size: 10))
                                Text("刷新").font(.system(size: 11, weight: .semibold))
                            }.foregroundColor(t.color(t.accent))
                        }.buttonStyle(.plain)
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .task { await load() }
    }

    @ViewBuilder private func kvRows(_ dict: [String: Any]) -> some View {
        let keys = dict.keys.filter { !($0 == "success") }.sorted()
        if keys.isEmpty {
            Text("无数据").font(.system(size: 11)).foregroundColor(t.color(t.faint))
        }
        ForEach(keys, id: \.self) { k in
            KV(k: k, v: renderBiosValue(dict[k]), mono: true)
        }
    }

    /// 值渲染对齐 web:null→"—"、布尔→true/false(NSNumber 布尔直接插值会显示"1")、嵌套→JSON
    private func renderBiosValue(_ v: Any?) -> String {
        guard let v = v else { return "—" }
        if let b = v as? Bool { return b ? "true" : "false" }
        if let n = v as? NSNumber {
            // CFBooleanType 会被桥成 NSNumber,先认布尔
            if CFGetTypeID(n) == CFBooleanGetTypeID() { return n.boolValue ? "true" : "false" }
            return n.stringValue
        }
        if let s = v as? String { return s.isEmpty ? "—" : s }
        if let data = try? JSONSerialization.data(withJSONObject: v),
           let json = String(data: data, encoding: .utf8) {
            return json
        }
        return "\(v)"
    }

    private func load() async {
        async let b = conn.client.getDict("/server-control/\(sn)/bios-settings")
        // SGX 对不支持的机器 404 —— 单独容错,不拖垮 BIOS 表(S-042)
        async let s = try? await conn.client.getDict("/server-control/\(sn)/bios-settings/sgx")
        do {
            let br = try await b
            bios = br["bios"] as? [String: Any] ?? br
            err = nil
        } catch {
            if let ae = error as? ApiClient.ApiError, ae.status == 404 || ae.status == 501 {
                bios = [:]
            } else {
                err = error.localizedDescription
            }
        }
        if let sr = await s {
            let inner = sr["sgx"] as? [String: Any] ?? sr
            sgx = inner.isEmpty ? nil : inner
        }
        loading = false
    }
}

// MARK: - 安装进度

struct InstallStatusSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let sn: String
    var t: Tokens { theme.t }

    @State private var status: [String: Any]?
    @State private var err: String?
    @State private var loading = true

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "progress.indicator", tint: t.info, title: "安装进度")
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: "安装进度读取失败:\(e)") { Task { await load() } }
                    } else if let s = status {
                        if s["hasInstallation"] as? Bool == false {
                            EmptyHint(icon: "checkmark.circle", text: s["message"] as? String ?? "当前无安装任务")
                        } else {
                            let st = s["status"] as? [String: Any] ?? s
                            let steps = (st["steps"] as? [[String: Any]]) ?? []
                            // 状态判定直接用后端字段(web 同口径):hasError 含 expired 超时,
                            // stopping 单列"正在中止";自己从 steps 推导会把超时/中止都显示成"进行中"
                            let hasError = st["hasError"] as? Bool ?? false
                            let stopping = st["stopping"] as? Bool ?? false
                            let allDone = st["allDone"] as? Bool ?? false
                            // progressUnknown:OVH 本次没返回 progress(percentage 恒 0),显示 0% 进度条只会让人以为卡死
                            let progressUnknown = st["progressUnknown"] as? Bool ?? false
                            let pct = numToDoubleAny(st["progressPercentage"]).map(Int.init) ?? 0
                            let stateText = hasError ? "出错" : (stopping ? "正在中止" : (allDone ? "已完成" : "进行中"))
                            let stateTint = hasError ? t.danger : (allDone ? t.success : t.fg)
                            VStack(spacing: 10) {
                                // 状态字
                                Text(stateText)
                                    .font(.system(size: 13, weight: .bold)).foregroundColor(t.color(stateTint))
                                if progressUnknown {
                                    Text("进度暂不可用")
                                        .font(.system(size: 11)).foregroundColor(t.color(t.muted))
                                    Text("OVH 本次没有返回安装进度,这不代表安装没有推进。稍等几秒会自动重试。")
                                        .font(.system(size: 10)).foregroundColor(t.color(t.faint))
                                } else {
                                    VStack(alignment: .leading, spacing: 6) {
                                        ProgressView(value: Double(pct) / 100)
                                            .tint(t.color(hasError ? t.danger : t.accent))
                                        Text("\(steps.filter { ($0["status"] as? String ?? "") == "done" }.count) / \(steps.count) 步 · \(pct)%")
                                            .font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                                    }
                                }
                                if let el = numToDoubleAny(st["elapsedTime"]), el > 0 {
                                    KV(k: "耗时", v: el >= 60 ? String(format: "%.0f 分钟", el / 60) : String(format: "%.0f 秒", el))
                                }
                                // Step 列表(S-043);出错步骤带出 step.error 详情(装机失败要看得到原因)
                                if !steps.isEmpty {
                                    VStack(spacing: 5) {
                                        ForEach(steps.indices, id: \.self) { i in
                                            let sp = steps[i]
                                            let sst = sp["status"] as? String ?? "todo"
                                            let icon = sst == "done" ? "checkmark.circle.fill" : (sst == "error" ? "xmark.circle.fill" : (sst == "doing" ? "arrow.triangle.2.circlepath" : "circle"))
                                            let ic = sst == "done" ? t.success : (sst == "error" ? t.danger : (sst == "doing" ? t.info : t.faint))
                                            VStack(alignment: .leading, spacing: 2) {
                                                HStack(spacing: 7) {
                                                    Image(systemName: icon).font(.system(size: 12)).foregroundColor(t.color(ic))
                                                    Text(sp["comment"] as? String ?? sp["commentOriginal"] as? String ?? "—")
                                                        .font(.system(size: 11)).foregroundColor(t.color(sst == "done" ? t.muted : t.fg))
                                                    Spacer()
                                                }
                                                if sst == "error", let se = sp["error"] as? String, !se.isEmpty {
                                                    Text(se).font(.system(size: 9.5)).foregroundColor(t.color(t.danger))
                                                        .frame(maxWidth: .infinity, alignment: .leading).padding(.leading, 19)
                                                }
                                            }
                                        }
                                    }
                                    .padding(10)
                                    .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted).opacity(0.5)))
                                }
                            }
                            .padding(14)
                            .background(RoundedRectangle(cornerRadius: 14).fill(t.color(t.surface)))
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium, .large])
        .task {
            await load()
            // 5s 轮询:安装推进自动刷新(web 同款)
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 5_000_000_000)
                if Task.isCancelled { break }
                await load()
            }
        }
    }

    private func load() async {
        do {
            status = try await conn.client.getDict("/server-control/\(sn)/install/status")
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }
}

// MARK: - OVH 监控开关

struct MonitoringSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let sn: String
    var t: Tokens { theme.t }

    @State private var enabled: Bool?
    @State private var loading = true
    @State private var err: String?
    @State private var busy = false

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "bell.badge", tint: t.muted, title: "OVH 监控通知")
            ScrollView {
                VStack(spacing: 13) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else {
                        SheetNote(text: "OVH 官方异常监控:机器硬件/网络异常时向账户邮箱发告警。与补货雷达无关。", tint: t.info)
                        if let on = enabled {
                            HStack {
                                Text(on ? "已开启" : "已关闭").font(.system(size: 14, weight: .bold)).foregroundColor(t.color(on ? t.success : t.faint))
                                Spacer()
                                Toggle("", isOn: Binding(
                                    get: { on },
                                    set: { newValue in Task { await setMonitor(newValue) } }
                                )).labelsHidden().tint(t.color(t.accent)).disabled(busy)
                            }
                            .padding(14)
                            .background(RoundedRectangle(cornerRadius: 14).fill(t.color(t.surface)))
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium])
        .task { await load() }
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/monitoring")
            enabled = r["monitoring"] as? Bool
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func setMonitor(_ on: Bool) async {
        busy = true
        defer { busy = false }
        let body = try? JSONSerialization.data(withJSONObject: ["monitoring": on])
        let (ok, msg) = await conn.client.actionPutData("/server-control/\(sn)/monitoring", bodyData: body)
        if ok { enabled = on; toast.show(on ? "监控已开启" : "监控已关闭") }
        else { toast.show(msg.isEmpty ? "设置失败" : msg, error: true) }
    }
}
