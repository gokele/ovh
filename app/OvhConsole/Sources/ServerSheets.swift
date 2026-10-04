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
                    LineMark(x: .value("时间", p.ts), y: .value("下行", p.dl / 1e6))
                        .foregroundStyle(t.color(t.info))
                        .interpolationMethod(.monotone)
                    LineMark(x: .value("时间", p.ts), y: .value("上行", p.ul / 1e6))
                        .foregroundStyle(t.color(t.accent))
                        .interpolationMethod(.monotone)
                }
            }
            .chartYAxis {
                AxisMarks(position: .trailing, values: .automatic(desiredCount: 4)) { v in
                    AxisGridLine().foregroundStyle(t.color(t.border).opacity(0.4))
                    AxisValueLabel {
                        if let d = v.as(Double.self) {
                            Text(d >= 1000 ? String(format: "%.1fG", d / 1000) : String(format: "%.0fM", d))
                                .font(.system(size: 8.5)).foregroundStyle(t.color(t.muted))
                        }
                    }
                }
            }
            .chartXAxis {
                AxisMarks(values: .automatic(desiredCount: compact ? 3 : 5)) { v in
                    AxisValueLabel(format: .dateTime.month().day().hour(), centered: false)
                        .font(.system(size: 8.5)).foregroundStyle(t.color(t.muted))
                }
            }
            .frame(height: compact ? 130 : 190)

            HStack(spacing: 10) {
                HStack(spacing: 4) {
                    Circle().fill(t.color(t.info)).frame(width: 6, height: 6)
                    Text("↓ 峰值 \(fmtMbps(dlMax))").font(.system(size: 10)).foregroundColor(t.color(t.muted))
                }
                HStack(spacing: 4) {
                    Circle().fill(t.color(t.accent)).frame(width: 6, height: 6)
                    Text("↑ 峰值 \(fmtMbps(ulMax))").font(.system(size: 10)).foregroundColor(t.color(t.muted))
                }
                Spacer()
                if let c = cur {
                    Text("现在 ↓\(fmtMbps(c.dl)) ↑\(fmtMbps(c.ul))")
                        .font(.system(size: 9.5, design: .monospaced)).foregroundColor(t.color(t.faint))
                }
            }
        }
        .padding(10)
        .background(RoundedRectangle(cornerRadius: 12).fill(t.color(t.surfaceMuted)))
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
                            SheetField(placeholder: rescueMail.isEmpty ? "留空 = 发到 OVH 账户的联系邮箱" : rescueMail, text: $mail, keyboard: .emailAddress)
                            if !mail.isEmpty && !mail.contains("@") {
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
            _ = try await conn.client.post("/server-control/\(sn)/rescue", body: body)
            toast.show("已进入救援模式(重启后生效)")
            dismiss()
        } catch { toast.show(error.localizedDescription, error: true) }
    }

    private func exitRescue() async {
        busy = true
        defer { busy = false }
        let body = try? JSONSerialization.data(withJSONObject: ["confirm": true])
        let (ok, msg) = await conn.client.actionPostData("/server-control/\(sn)/rescue/exit", bodyData: body)
        toast.show(ok ? "已退出救援模式" : (msg.isEmpty ? "失败" : msg), error: !ok)
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

    @State private var templates: [[String: Any]] = []
    @State private var search = ""
    @State private var picked: String?
    @State private var hostname = ""
    @State private var useZFS = true
    @State private var zfsRaid = 1
    @State private var vzGB = 100.0
    @State private var schemes: [String] = []
    @State private var pickedScheme: String?
    @State private var confirmName = ""
    @State private var loading = true
    @State private var err: String?
    @State private var busy = false

    private var isProxmox9: Bool {
        (picked ?? "").lowercased().contains("proxmox") && (picked ?? "").contains("9")
    }

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "opticaldiscdrive.fill", tint: t.danger, title: "重装系统")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    SheetNote(text: "清空系统盘所有数据,不可逆。执行需要 Face ID / 密码确认。", tint: t.danger)

                    SheetField(placeholder: "搜索模板(debian / ubuntu / proxmox…)", text: $search)

                    if loading {
                        ProgressView().padding(20).frame(maxWidth: .infinity)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else {
                        VStack(spacing: 6) {
                            ForEach(filtered.indices, id: \.self) { i in tplRow(filtered[i]) }
                            if filtered.isEmpty {
                                Text("没有匹配的模板").font(.system(size: 11)).foregroundColor(t.color(t.faint)).padding(8)
                            }
                        }
                    }

                    if picked != nil {
                        Text("自定义主机名(可选)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetField(placeholder: sn, text: $hostname, mono: true)

                        if isProxmox9 {
                            Toggle(isOn: $useZFS) {
                                Text("Proxmox 9 + ZFS 预设").font(.system(size: 12.5)).foregroundColor(t.color(t.fg))
                            }.tint(t.color(t.accent))
                            if useZFS {
                                Picker("ZFS RAID", selection: $zfsRaid) {
                                    ForEach([0, 1, 5, 6, 7, 10], id: \.self) { r in Text("RAID \(r)").tag(r) }
                                }
                                .pickerStyle(.segmented)
                                VStack(alignment: .leading, spacing: 4) {
                                    Text("/var/lib/vz 容量:\(Int(vzGB)) GB").font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
                                    Slider(value: $vzGB, in: 10...500, step: 10).tint(t.color(t.accent))
                                }
                            }
                        } else if !schemes.isEmpty {
                            Text("分区方案").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                            FlowLayout(spacing: 7) {
                                ForEach(schemes, id: \.self) { s in
                                    Button { pickedScheme = pickedScheme == s ? nil : s } label: {
                                        Text(s).font(.system(size: 11)).foregroundColor(t.color(pickedScheme == s ? t.fg : t.muted))
                                            .padding(.horizontal, 10).padding(.vertical, 6)
                                            .background(RoundedRectangle(cornerRadius: 9).fill(pickedScheme == s ? t.color(t.accent).opacity(0.2) : t.color(t.surfaceMuted)))
                                    }.buttonStyle(.plain)
                                }
                            }
                        }

                        Text("输入机器名确认").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        SheetField(placeholder: sn, text: $confirmName, mono: true)

                        ActBtn(kind: .danger, icon: "faceid", label: busy ? "提交中…" : "面容确认并重装") {
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
        .onChange(of: picked) { _ in Task { await loadSchemes() } }
    }

    private var filtered: [[String: Any]] {
        let q = search.trimmingCharacters(in: .whitespaces).lowercased()
        guard !q.isEmpty else { return templates }
        return templates.filter {
            (($0["templateName"] as? String ?? "") + ($0["distribution"] as? String ?? "")).lowercased().contains(q)
        }
    }

    private func tplRow(_ tpl: [String: Any]) -> some View {
        let name = tpl["templateName"] as? String ?? ""
        let on = picked == name
        return Button { picked = name } label: {
            HStack(spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(tpl["distribution"] as? String ?? name).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                    Text("\(name) · \(tpl["bitFormat"] ?? 64) 位")
                        .font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.muted)).lineLimit(1)
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
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }

    private func loadSchemes() async {
        guard let tpl = picked, !isProxmox9 else { schemes = []; pickedScheme = nil; return }
        if let r = try? await conn.client.getDict("/server-control/\(sn)/partition-schemes?templateName=\(urlEncode(tpl))"),
           let list = r["schemes"] as? [[String: Any]] {
            schemes = list.compactMap { ($0["name"] as? String) ?? ($0["schemeName"] as? String) }
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
        if !h.isEmpty { body["customHostname"] = h }
        if isProxmox9 && useZFS {
            body["useProxmox9Zfs"] = true
            body["zfsRaidLevel"] = zfsRaid
            body["zfsVzSize"] = Int(vzGB) * 1024
        } else if let s = pickedScheme, !s.isEmpty {
            body["partitionSchemeName"] = s
        }
        do {
            _ = try await conn.client.post("/server-control/\(sn)/install", body: body)
            toast.show("重装任务已提交(通常 5-10 分钟)")
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
                                    if let u = (r["console"] as? [String: Any])?["value"] as? String ?? r["url"] as? String {
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
        let on = picked == id
        let iconName: String = type == "rescue" ? "lifepreserver" : type.contains("network") || type == "ipxe" ? "wifi" : "internaldrive"
        return Button { if id >= 0 { picked = id } } label: {
            HStack(spacing: 10) {
                Image(systemName: iconName).font(.system(size: 14)).foregroundColor(t.color(t.muted))
                VStack(alignment: .leading, spacing: 1) {
                    HStack(spacing: 6) {
                        Text(b["description"] as? String ?? type).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg)).lineLimit(1)
                        if isCurrent { Chip(text: "当前", color: t.success) }
                    }
                    Text("\(type) · bootId \(id)").font(.system(size: 9.5, design: .monospaced)).foregroundColor(t.color(t.muted))
                    if let e = b["error"] as? String, !e.isEmpty {
                        Text(e).font(.system(size: 9.5)).foregroundColor(t.color(t.danger))
                    }
                }
                Spacer()
                if on { Image(systemName: "checkmark.circle.fill").font(.system(size: 15)).foregroundColor(t.color(t.accent)) }
            }
            .padding(11)
            .background(RoundedRectangle(cornerRadius: 11).fill(t.color(on ? t.accent : t.surface).opacity(0.08)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(on && !isCurrent ? t.accent : t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
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
            toast.show(msg.isEmpty ? "启动模式读取失败" : msg, error: true)
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
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else {
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
                            Text("尚未登记任何授权").font(.system(size: 11)).foregroundColor(t.color(t.faint))
                        } else {
                            ForEach(list.indices, id: \.self) { i in
                                let it = list[i]
                                KV(k: splaTypeName(it["type"] as? String ?? ""), v: (it["serialNumber"] as? String ?? "—"), mono: true)
                            }
                        }

                        Divider().overlay(t.color(t.border))

                        Text("手动登记自己的 SPLA").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                        Picker("类型", selection: $type) {
                            Text("操作系统").tag("os")
                            Text("SQL Standard").tag("sqlstd")
                            Text("SQL Web").tag("sqlweb")
                        }
                        .pickerStyle(.segmented)
                        SheetField(placeholder: "SPLA 序列号", text: $serial, mono: true)
                        ActBtn(kind: .ghost, icon: "square.and.arrow.down", label: busy ? "登记中…" : "登记序列号") {
                            await register()
                        }

                        Divider().overlay(t.color(t.border))
                        let hasOs = list.contains { ($0["type"] as? String) == "os" }
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
        ["os": "操作系统", "sqlstd": "SQL Standard", "sqlweb": "SQL Web"][s] ?? s
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/spla")
            list = (r["splaList"] as? [[String: Any]]) ?? []
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
            let r = try await conn.client.post("/server-control/\(sn)/spla", body: ["type": "os", "serialNumber": Self.WINDOWS_GVLK])
            toast.show(r["message"] as? String ?? "一键解锁完成")
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
                            if tasks.isEmpty {
                                EmptyHint(icon: "checkmark.circle", text: "没有进行中的运维任务")
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
        return VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Dot(color: statusColor(status))
                Text(task["function"] as? String ?? "—").font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                Spacer()
                Chip(text: statusCn(status), color: statusColor(status))
            }
            if let c = task["comment"] as? String, !c.isEmpty {
                Text(c).font(.system(size: 10.5)).foregroundColor(t.color(t.muted)).lineLimit(2)
            }
            HStack {
                Text("\(fmtDate(task["startDate"] as? String)) → \(fmtDate(task["doneDate"] as? String))")
                    .font(.system(size: 10)).foregroundColor(t.color(t.faint))
                Spacer()
                if status == "todo" || status == "doing" {
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
                Button { slotsTaskId = nil; slots = [] } label: {
                    HStack(spacing: 4) {
                        Image(systemName: "chevron.left").font(.system(size: 11, weight: .semibold))
                        Text("返回任务列表").font(.system(size: 11.5, weight: .semibold))
                    }.foregroundColor(t.color(t.accent))
                }.buttonStyle(.plain)
                Spacer()
            }
            if let m = slotsMsg {
                SheetNote(text: m, tint: t.info)
            }
            if slots.isEmpty && slotsMsg == nil {
                Text("加载可用时段…").font(.system(size: 11)).foregroundColor(t.color(t.faint))
            }
            ForEach(slots.indices, id: \.self) { i in
                let s = slots[i]
                Button { Task { await schedule(s) } } label: {
                    HStack {
                        Text(slotText(s)).font(.system(size: 11.5)).foregroundColor(t.color(t.fg))
                        Spacer()
                        if busy { ProgressView().scaleEffect(0.7) }
                    }
                    .padding(11)
                    .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))
                }
                .buttonStyle(.plain)
                .disabled(busy)
            }
            Toggle(isOn: $backedUp) {
                Text("我已完成数据备份").font(.system(size: 12)).foregroundColor(t.color(t.fg))
            }.tint(t.color(t.accent))
            SheetNote(text: "预约的是机房物理干预(换硬件等)。未备份就预约可能丢数据。", tint: t.warning)
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
                            EmptyHint(icon: "cpu", text: "OVH 未返回 BIOS 信息")
                        }
                        if let s = sgx {
                            Card { VStack(spacing: 8) { SectionTitle(text: "SGX(Intel 软件防护扩展)"); kvRows(s) } }
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

    @ViewBuilder private func kvRows(_ dict: [String: Any]) -> some View {
        let keys = dict.keys.filter { !($0 == "success") }.sorted()
        if keys.isEmpty {
            Text("无数据").font(.system(size: 11)).foregroundColor(t.color(t.faint))
        }
        ForEach(keys, id: \.self) { k in
            KV(k: k, v: "\(dict[k] ?? "—")", mono: true)
        }
    }

    private func load() async {
        do {
            async let b = conn.client.getDict("/server-control/\(sn)/bios-settings")
            async let s = conn.client.getDict("/server-control/\(sn)/bios-settings/sgx")
            let (br, sr) = try await (b, s)
            bios = br["bios"] as? [String: Any] ?? br
            sgx = sr["sgx"] as? [String: Any] ?? sr
            err = nil
        } catch { err = error.localizedDescription }
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
                        LoadFailed(message: e) { Task { await load() } }
                    } else if let s = status {
                        if s["hasInstallation"] as? Bool == false {
                            EmptyHint(icon: "checkmark.circle", text: s["message"] as? String ?? "当前没有正在进行的安装")
                        } else {
                            // handler 形状:status.progressPercentage + steps[{comment,status}] + elapsedTime
                            let st = s["status"] as? [String: Any] ?? [:]
                            let pct = numToDoubleAny(st["progressPercentage"]).map(Int.init) ?? -1
                            let steps = (st["steps"] as? [[String: Any]]) ?? []
                            let currentStep = steps.last(where: { ($0["status"] as? String ?? "") != "done" })?["comment"] as? String
                            let doneSteps = steps.filter { ($0["status"] as? String ?? "") == "done" }.count
                            VStack(spacing: 10) {
                                if pct >= 0 {
                                    VStack(alignment: .leading, spacing: 6) {
                                        ProgressView(value: Double(pct) / 100).tint(t.color(t.accent))
                                        Text("总进度 \(pct)%").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                                    }
                                }
                                if let step = currentStep, !step.isEmpty {
                                    KV(k: "当前步骤", v: step)
                                }
                                if !steps.isEmpty {
                                    KV(k: "步骤", v: "\(doneSteps) / \(steps.count) 完成")
                                }
                                if let el = numToDoubleAny(st["elapsedTime"]), el > 0 {
                                    KV(k: "已耗时", v: el >= 60 ? String(format: "%.0f 分钟", el / 60) : String(format: "%.0f 秒", el))
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
        .presentationDetents([.medium])
        .task { await load() }
        .refreshable { await load() }
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
