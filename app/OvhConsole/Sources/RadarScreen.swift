import SwiftUI

/**
 * 雷达页:独服补货监控 / VPS 补货监控 两段。
 * 对齐 web monitor + vps-monitor:启停、订阅增删改、通知/自动下单配置、变化历史。
 */
struct RadarScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @EnvironmentObject var nav: AppNav
    var t: Tokens { theme.t }

    @State private var showAccountPicker = false

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                Picker("", selection: $nav.radarSegment) {
                    Text("独服监控").tag(0)
                    Text("VPS 监控").tag(1)
                }
                .pickerStyle(.segmented)
                .padding(.horizontal, 16).padding(.vertical, 8)

                if nav.radarSegment == 1 {
                    VpsMonitorPane()
                } else {
                    ServerMonitorPane()
                }
            }
            .background(t.color(t.bg))
            .navigationTitle("雷达")
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

// MARK: - 独服监控段

struct ServerMonitorPane: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    var t: Tokens { theme.t }

    @State private var status: [String: Any]?
    @State private var subs: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true
    @State private var editSub: [String: Any]?
    @State private var creating = false
    @State private var historyCode: String?
    @State private var clearConfirm = false
    @State private var intervalEdit = false
    @State private var deleteCode: String?

    var body: some View {
        ScrollView {
            VStack(spacing: 10) {
                if let e = err {
                    Card { LoadFailed(message: e) { Task { await load() } } }
                } else if loading {
                    ProgressView().padding(.top, 50)
                } else {
                    statusCard
                    regionIssues
                    ForEach(subs.indices, id: \.self) { i in subCard(subs[i]) }
                    if subs.isEmpty && status != nil {
                        Card { EmptyHint(icon: "dot.radiowaves.left.and.right", text: "没有监控订阅 —— 去目录页点「加监控」或点右上 + 新建") }
                    }
                }
            }
            .padding(16)
        }
        .background(t.color(t.bg))
        .refreshable { await load() }
        .task { await load() }
        .onChange(of: conn.accountId) { _ in Task { await load() } }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { creating = true } label: {
                    Image(systemName: "plus.circle.fill").font(.system(size: 16, weight: .semibold)).foregroundColor(t.color(t.accent))
                }
            }
        }
        .sheet(isPresented: $creating) {
            MonitorSubSheet(editing: nil, presets: [:]) { Task { await load() } }
                .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(item: Binding(
            get: { editSub.map { SubWrap(sub: $0) } },
            set: { editSub = $0?.sub }
        )) { w in
            MonitorSubSheet(editing: w.sub, presets: [:]) { Task { await load() } }
                .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(item: Binding(
            get: { historyCode.map { CodeWrap(code: $0) } },
            set: { historyCode = $0?.code }
        )) { w in
            MonitorHistorySheet(title: w.code, path: "/monitor/subscriptions/\(w.code)/history", listKey: "history")
                .environmentObject(theme).environmentObject(conn)
        }
        .sheet(isPresented: $intervalEdit) {
            MonitorIntervalSheet(current: status?["check_interval"] as? Int ?? 5) { Task { await load() } }
                .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(item: Binding(
            get: { deleteCode.map { CodeDel(c: $0) } },
            set: { deleteCode = $0?.c }
        )) { w in
            ConfirmSheet(title: "取消订阅", message: "确定要取消订阅 \(w.c) 吗?", confirmText: "确定") {
                _ = await conn.client.actionDelete("/monitor/subscriptions/\(w.c)")
                toast.show("已删除订阅")
                await load()
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(isPresented: $clearConfirm) {
            ConfirmSheet(title: "确认清空所有订阅?", message: "所有监控订阅将被删除,此操作不可撤销。", confirmText: "确认清空") {
                let (ok, msg) = await conn.client.actionDelete("/monitor/subscriptions/clear")
                toast.show(ok ? "已清空" : (msg.isEmpty ? "失败" : msg), error: !ok)
                await load()
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
    }

    private struct SubWrap: Identifiable {
        let sub: [String: Any]
        var id: String { sub["planCode"] as? String ?? UUID().uuidString }
    }
    private struct CodeWrap: Identifiable {
        let code: String
        var id: String { code }
    }
    private struct CodeDel: Identifiable {
        let c: String
        var id: String { c }
    }

    private var statusCard: some View {
        Card {
            VStack(spacing: 9) {
                HStack(spacing: 9) {
                    Image(systemName: (status?["running"] as? Bool ?? false) ? "dot.radiowaves.left.and.right" : "antenna.radiowaves.left.and.right.slash")
                        .font(.system(size: 15)).foregroundColor(t.color((status?["running"] as? Bool ?? false) ? t.success : t.faint))
                    VStack(alignment: .leading, spacing: 1) {
                        Text((status?["running"] as? Bool ?? false) ? "监控运行中" : "监控已停止").font(.system(size: 13, weight: .bold)).foregroundColor(t.color(t.fg))
                        Text("检查间隔 \(status?["check_interval"] as? Int ?? 0) 秒").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                    }
                    Spacer()
                    Button { intervalEdit = true } label: {
                        Text("改间隔").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.accent))
                            .padding(.horizontal, 10).padding(.vertical, 5)
                            .background(Capsule().stroke(t.color(t.accent), lineWidth: 1))
                    }.buttonStyle(.plain)
                }
                HStack(spacing: 10) {
                    stat("\(subs.count)", "订阅")
                    stat("\(status?["known_servers_count"] as? Int ?? 0)", "已知机型")
                    Spacer()
                    if !subs.isEmpty {
                        Button { clearConfirm = true } label: {
                            Text("清空").font(.system(size: 11)).foregroundColor(t.color(t.danger))
                        }.buttonStyle(.plain)
                    }
                }
            }
        }
    }

    private func stat(_ v: String, _ label: String) -> some View {
        VStack(spacing: 2) {
            Text(v).font(.system(size: 16, weight: .bold, design: .rounded)).foregroundColor(t.color(t.fg))
            Text(label).font(.system(size: 9.5)).foregroundColor(t.color(t.muted))
        }
    }

    @ViewBuilder private var regionIssues: some View {
        if let issues = status?["region_issues"] as? [[String: Any]], !issues.isEmpty {
            Card(border: t.warning) {
                VStack(alignment: .leading, spacing: 8) {
                    SectionTitle(text: "区域错配警告")
                    ForEach(issues.prefix(3).indices, id: \.self) { i in
                        let it = issues[i]
                        VStack(alignment: .leading, spacing: 2) {
                            Text(it["planCode"] as? String ?? "").font(.system(size: 11.5, weight: .semibold, design: .monospaced)).foregroundColor(t.color(t.fg))
                            Text(it["error"] as? String ?? "").font(.system(size: 10)).foregroundColor(t.color(t.warning))
                        }
                    }
                }
            }
        }
    }

    private func subCard(_ sub: [String: Any]) -> some View {
        let code = sub["planCode"] as? String ?? ""
        let dcs = sub["datacenters"] as? [String] ?? []
        let options = sub["options"] as? [String] ?? []
        let autoOrder = sub["autoOrder"] as? Bool ?? false
        // lastStatus 落库值只有 available / unavailable / price_check_failed(后端 monitor/check.go 约定)
        let hasStock = (sub["lastStatus"] as? [String: String] ?? [:]).values.contains { $0 == "available" }
        return Card(border: hasStock ? t.success : nil) {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 7) {
                    Dot(color: hasStock ? t.success : t.faint)
                    Text(code).font(.system(size: 12.5, weight: .bold, design: .monospaced)).foregroundColor(t.color(t.fg))
                    if hasStock { Chip(text: "有货", color: t.success) }
                    Spacer()
                }
                FlowLayout(spacing: 5) {
                    Chip(text: dcs.isEmpty ? "全部机房" : "\(dcs.count) 机房")
                    if !options.isEmpty { Chip(text: "只盯 \(options.count) 配置", color: t.info) }
                    if sub["notifyAvailable"] as? Bool == true { Chip(text: "有货提醒", color: t.success) }
                    if sub["notifyUnavailable"] as? Bool == true { Chip(text: "无货提醒") }
                    if autoOrder {
                        Chip(text: "自动下单 ×\(sub["quantity"] as? Int ?? 1)", color: t.danger)
                        if sub["autoPay"] as? Bool == true { Chip(text: "自动付款", color: t.danger) }
                    }
                }
                HStack(spacing: 10) {
                    Button { historyCode = code } label: {
                        hBtn("历史", "clock.arrow.circlepath")
                    }
                    Spacer()
                    Button { editSub = sub } label: {
                        hBtn("编辑", "square.and.pencil")
                    }
                    Button { deleteCode = code } label: {
                        hBtn("删除", "trash", danger: true)
                    }
                }
            }
        }
    }

    private func hBtn(_ label: String, _ icon: String, danger: Bool = false) -> some View {
        HStack(spacing: 3) {
            Image(systemName: icon).font(.system(size: 10))
            Text(label).font(.system(size: 11))
        }
        .foregroundColor(t.color(danger ? t.danger : t.muted))
        .padding(.horizontal, 9).padding(.vertical, 5)
        .background(Capsule().stroke(t.color(danger ? t.danger : t.border), lineWidth: 1))
    }

    private func load() async {
        err = nil
        do {
            let r = try await conn.client.getDict("/monitor/status")
            status = r
            subs = (r["subscriptions"] as? [[String: Any]]) ?? []
        } catch { err = error.localizedDescription }
        loading = false
    }
}

// MARK: - 监控间隔

struct MonitorIntervalSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let current: Int
    let onDone: () async -> Void
    var t: Tokens { theme.t }

    @State private var interval = 5
    @State private var busy = false

    var body: some View {
        VStack(spacing: 14) {
            SheetHeader(icon: "clock.arrow.circlepath", tint: t.accent, title: "检查间隔")
            VStack(alignment: .leading, spacing: 12) {
                SheetNote(text: "允许 5 ~ 3600 秒。再快会撞 OVH 限流,再慢基本抢不到。运行中即时生效。", tint: t.muted)
                VStack(alignment: .leading, spacing: 6) {
                    Text("\(interval) 秒").font(.system(size: 14, weight: .bold, design: .rounded)).foregroundColor(t.color(t.fg))
                    Slider(value: Binding(get: { Double(interval) }, set: { interval = Int($0) }), in: 5...600, step: 5)
                        .tint(t.color(t.accent))
                }
                SheetField(placeholder: "精确秒数", text: Binding(
                    get: { "\(interval)" },
                    set: { if let v = Int($0.filter(\.isNumber)), v >= 5, v <= 3600 { interval = v } }
                ), mono: true, keyboard: .numberPad)
                ActBtn(kind: .primary, icon: "checkmark", label: busy ? "保存中…" : "保存", busy: busy) {
                    await save()
                }
            }.padding(.horizontal, 16)
            Spacer()
        }
        .background(t.color(t.bg))
        .presentationDetents([.medium])
        .onAppear { interval = current }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        let body = try? JSONSerialization.data(withJSONObject: ["interval": interval])
        let (ok, msg) = await conn.client.actionPutData("/monitor/interval", bodyData: body)
        toast.show(ok ? "已保存" : (msg.isEmpty ? "失败" : msg), error: !ok)
        dismiss()
        if ok { await onDone() }
    }
}

// MARK: - 订阅编辑 sheet(独服)

struct MonitorSubSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let editing: [String: Any]?      // nil = 新建
    let presets: [String: Any]       // 目录页带入:planCode/datacenters/options
    let onDone: () async -> Void
    var t: Tokens { theme.t }

    @State private var planCode = ""
    @State private var dcs = ""
    @State private var options = ""
    @State private var notifyAvail = true
    @State private var notifyUnavail = false
    @State private var autoOrder = false
    @State private var quantity = 1
    @State private var autoPay = false
    @State private var busy = false

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: editing == nil ? "plus.circle" : "square.and.pencil", tint: t.accent, title: editing == nil ? "添加监控" : "编辑监控")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    Text("机型 planCode").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    SheetField(placeholder: "如 25sk-adv-01", text: $planCode, mono: true)
                        .disabled(editing != nil)   // PUT 按主键定位,改了就 404
                    if editing != nil {
                        SheetNote(text: "编辑时 planCode 只读(它是订阅主键)。", tint: t.muted)
                    }

                    Text("机房列表").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    SheetField(placeholder: "逗号分隔,如 gra1,rbx2;留空 = 全部机房", text: $dcs, mono: true)

                    Text("只盯这套配置(可选)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    SheetField(placeholder: "addon planCode,逗号分隔;留空 = 全部配置", text: $options, mono: true)

                    Toggle(isOn: $notifyAvail) { Text("有货提醒").font(.system(size: 12.5)).foregroundColor(t.color(t.fg)) }
                        .tint(t.color(t.accent))
                    Toggle(isOn: $notifyUnavail) { Text("无货提醒").font(.system(size: 12.5)).foregroundColor(t.color(t.fg)) }
                        .tint(t.color(t.accent))

                    Toggle(isOn: $autoOrder) {
                        VStack(alignment: .leading, spacing: 1) {
                            Text("有货自动下单").font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.danger))
                            Text("以当前全局账户下单").font(.system(size: 10)).foregroundColor(t.color(t.muted))
                        }
                    }.tint(t.color(t.danger))

                    if autoOrder {
                        HStack {
                            Text("数量:\(quantity)").font(.system(size: 12)).foregroundColor(t.color(t.muted))
                            Spacer()
                            Stepper("", value: $quantity, in: 1...100).labelsHidden()
                        }
                        Toggle(isOn: $autoPay) {
                            Text("下单后自动付款").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.danger))
                        }.tint(t.color(t.danger))
                        SheetNote(text: "自动付款有真实扣款风险,默认关闭。", tint: t.danger)
                    }

                    ActBtn(kind: .primary, icon: "checkmark", label: busy ? "保存中…" : "保存订阅", busy: busy) {
                        await save()
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.large])
        .onAppear {
            if let e = editing {
                planCode = e["planCode"] as? String ?? ""
                dcs = (e["datacenters"] as? [String])?.joined(separator: ",") ?? ""
                options = (e["options"] as? [String])?.joined(separator: ",") ?? ""
                notifyAvail = e["notifyAvailable"] as? Bool ?? true
                notifyUnavail = e["notifyUnavailable"] as? Bool ?? false
                autoOrder = e["autoOrder"] as? Bool ?? false
                quantity = e["quantity"] as? Int ?? 1
                autoPay = e["autoPay"] as? Bool ?? false
            } else {
                planCode = presets["planCode"] as? String ?? ""
                dcs = (presets["datacenters"] as? [String])?.joined(separator: ",") ?? ""
                options = (presets["options"] as? [String])?.joined(separator: ",") ?? ""
            }
        }
    }

    private func save() async {
        let code = planCode.trimmingCharacters(in: .whitespaces)
        guard !code.isEmpty else { return toast.show("planCode 必填", error: true) }
        busy = true
        defer { busy = false }
        var body: [String: Any] = [
            "planCode": code,
            "notifyAvailable": notifyAvail,
            "notifyUnavailable": notifyUnavail,
            "autoOrder": autoOrder,
        ]
        let d = dcs.split(whereSeparator: { ",;".contains($0) }).map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        if !d.isEmpty { body["datacenters"] = d }
        let o = options.split(whereSeparator: { ",".contains($0) }).map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        body["options"] = o     // PUT 显式传空数组才能清空
        if autoOrder {
            let accId = conn.accountId.isEmpty ? (conn.activeAccount?["id"] as? String ?? "") : conn.accountId
            guard !accId.isEmpty else { return }
            body["quantity"] = quantity
            body["autoPay"] = autoPay
            body["autoOrderAccountId"] = accId   // 空 = 只通知不下单,必须传实际账户
        }
        do {
            if editing == nil {
                _ = try await conn.client.post("/monitor/subscriptions", body: body)
            } else {
                _ = try await conn.client.put("/monitor/subscriptions/\(code)", body: body)
            }
            toast.show("已保存")
            dismiss()
            await onDone()
        } catch { toast.show(error.localizedDescription, error: true) }
    }
}

// MARK: - 变化历史(共用)

struct MonitorHistorySheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let title: String
    let path: String
    let listKey: String
    var t: Tokens { theme.t }

    @State private var entries: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: "clock.arrow.circlepath", tint: t.info, title: "变化历史 · \(title)")
            ScrollView {
                VStack(spacing: 8) {
                    if loading {
                        ProgressView().padding(30)
                    } else if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else if entries.isEmpty {
                        EmptyHint(icon: "clock.arrow.circlepath", text: "还没有状态变化记录")
                    } else {
                        ForEach(entries.prefix(50).indices, id: \.self) { i in
                            let it = entries.prefix(50)[i]
                            historyRow(it)
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

    private func historyRow(_ it: [String: Any]) -> some View {
        let status = it["status"] as? String ?? ""
        let ok = isOrderable(status) || status == "available"
        return HStack(spacing: 9) {
            Circle().fill(t.color(ok ? t.success : t.faint)).frame(width: 6, height: 6)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text((it["datacenter"] as? String ?? "").uppercased()).font(.system(size: 11.5, weight: .semibold, design: .monospaced)).foregroundColor(t.color(t.fg))
                    Text(ok ? "有货" : "无货").font(.system(size: 10.5, weight: .semibold)).foregroundColor(t.color(ok ? t.success : t.muted))
                }
                if let ts = it["timestamp"] as? String {
                    Text(fmtDate(ts)).font(.system(size: 9.5)).foregroundColor(t.color(t.faint))
                }
            }
            Spacer()
        }
        .padding(9)
        .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.surfaceMuted)))
    }

    private func load() async {
        do {
            // 这两个端点返回裸数组
            entries = try await conn.client.getArray(path)
            err = nil
        } catch { err = error.localizedDescription }
        loading = false
    }
}

// MARK: - VPS 监控段

struct VpsMonitorPane: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    var t: Tokens { theme.t }

    @State private var status: [String: Any]?
    @State private var subs: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true
    @State private var editSub: [String: Any]?
    @State private var creating = false
    @State private var historyId: String?
    @State private var toggling = false
    @State private var deleteVpsId: String?
    @State private var vpsClearConfirm = false

    var body: some View {
        ScrollView {
            VStack(spacing: 10) {
                if let e = err {
                    Card { LoadFailed(message: e) { Task { await load() } } }
                } else if loading {
                    ProgressView().padding(.top, 50)
                } else {
                    vpsStatusCard
                    ForEach(subs.indices, id: \.self) { i in vpsSubCard(subs[i]) }
                    if subs.isEmpty && status != nil {
                        Card { EmptyHint(icon: "cube.box", text: "没有 VPS 监控订阅 —— 点右上 + 新建") }
                    }
                }
            }
            .padding(16)
        }
        .background(t.color(t.bg))
        .refreshable { await load() }
        .task { await load() }
        .onChange(of: conn.accountId) { _ in Task { await load() } }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { creating = true } label: {
                    Image(systemName: "plus.circle.fill").font(.system(size: 16, weight: .semibold)).foregroundColor(t.color(t.accent))
                }
            }
        }
        .sheet(isPresented: $creating) {
            VpsSubSheet(editing: nil) { Task { await load() } }
                .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(item: Binding(
            get: { editSub.map { VpsSubWrap(sub: $0) } },
            set: { editSub = $0?.sub }
        )) { w in
            VpsSubSheet(editing: w.sub) { Task { await load() } }
                .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(item: Binding(
            get: { deleteVpsId.map { IdWrap(id: $0) } },
            set: { deleteVpsId = $0?.id }
        )) { w in
            ConfirmSheet(title: "取消订阅", message: "确定要取消订阅这台 VPS 吗?", confirmText: "确定") {
                _ = await conn.client.actionDelete("/vps-monitor/subscriptions/\(w.id)")
                toast.show("已删除")
                await load()
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(isPresented: $vpsClearConfirm) {
            ConfirmSheet(title: "确认清空所有 VPS 订阅?", message: "此操作不可撤销。", confirmText: "确认清空") {
                let (ok2, msg) = await conn.client.actionDelete("/vps-monitor/subscriptions/clear")
                toast.show(ok2 ? "已清空全部 VPS 订阅" : (msg.isEmpty ? "清空失败" : msg), error: !ok2)
                await load()
            }
            .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .sheet(item: Binding(
            get: { historyId.map { IdWrap(id: $0) } },
            set: { historyId = $0?.id }
        )) { w in
            MonitorHistorySheet(title: "VPS", path: "/vps-monitor/subscriptions/\(w.id)/history", listKey: "history")
                .environmentObject(theme).environmentObject(conn)
        }
    }

    private struct VpsSubWrap: Identifiable {
        let sub: [String: Any]
        var id: String { String(describing: sub["id"] ?? sub["model"] ?? UUID().uuidString) }
    }
    private struct IdWrap: Identifiable {
        let id: String
    }

    private var running: Bool { status?["running"] as? Bool ?? (status?["Running"] as? Bool ?? false) }

    private var vpsStatusCard: some View {
        Card {
            VStack(spacing: 9) {
                HStack(spacing: 9) {
                    Image(systemName: running ? "dot.radiowaves.left.and.right" : "antenna.radiowaves.left.and.right.slash")
                        .font(.system(size: 15)).foregroundColor(t.color(running ? t.success : t.faint))
                    VStack(alignment: .leading, spacing: 1) {
                        Text(running ? "VPS 监控运行中" : "VPS 监控已停止").font(.system(size: 13, weight: .bold)).foregroundColor(t.color(t.fg))
                        Text("\(subs.count) 个订阅 · 间隔 \(status?["checkInterval"] as? Int ?? (status?["check_interval"] as? Int ?? 0)) 秒").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                    }
                    Spacer()
                    if !subs.isEmpty {
                        Button { vpsClearConfirm = true } label: {
                            Text("清空").font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.danger))
                        }.buttonStyle(.plain)
                    }
                    Button { Task { await toggleRun() } } label: {
                        HStack(spacing: 4) {
                            if toggling { ProgressView().scaleEffect(0.6) }
                            else { Image(systemName: running ? "stop.fill" : "play.fill").font(.system(size: 10)) }
                            Text(running ? "停止" : "启动").font(.system(size: 11.5, weight: .semibold))
                        }
                        .foregroundColor(t.color(running ? t.danger : t.accent))
                        .padding(.horizontal, 12).padding(.vertical, 6)
                        .background(Capsule().stroke(t.color(running ? t.danger : t.accent), lineWidth: 1))
                    }.buttonStyle(.plain)
                }
            }
        }
    }

    private func vpsSubCard(_ sub: [String: Any]) -> some View {
        let id = String(describing: sub["id"] ?? "")
        let model = sub["planCode"] as? String ?? (sub["model"] as? String ?? "—")
        let dcs = sub["datacenters"] as? [String] ?? []
        let retired = sub["retired"] as? Bool ?? false
        let autoOrder = sub["autoOrder"] as? Bool ?? false
        let available = (sub["lastStatus"] as? [String: String] ?? [:]).values.contains { $0 == "available" }
        return Card(border: available ? t.success : (retired ? t.warning : nil)) {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 7) {
                    Dot(color: retired ? t.warning : (available ? t.success : t.faint))
                    Text(model).font(.system(size: 12.5, weight: .bold)).foregroundColor(t.color(t.fg))
                    if retired { Chip(text: "已停售", color: t.warning) }
                    if available { Chip(text: "有货", color: t.success) }
                    Spacer()
                }
                FlowLayout(spacing: 5) {
                    if let s = sub["ovhSubsidiary"] as? String, !s.isEmpty { Chip(text: s) }
                    Chip(text: dcs.isEmpty ? "全部机房" : "\(dcs.count) 机房")
                    if sub["monitorLinux"] as? Bool == true { Chip(text: "Linux") }
                    if sub["monitorWindows"] as? Bool == true { Chip(text: "Windows") }
                    if sub["notifyAvailable"] as? Bool == true { Chip(text: "有货提醒", color: t.success) }
                    if autoOrder {
                        Chip(text: "自动下单 ×\(sub["quantity"] as? Int ?? 1)", color: t.danger)
                        if sub["autoPay"] as? Bool == true { Chip(text: "自动付款", color: t.danger) }
                    }
                }
                HStack(spacing: 10) {
                    Button { historyId = id } label: {
                        HStack(spacing: 3) {
                            Image(systemName: "clock.arrow.circlepath").font(.system(size: 10))
                            Text("历史").font(.system(size: 11))
                        }
                        .foregroundColor(t.color(t.muted))
                        .padding(.horizontal, 9).padding(.vertical, 5)
                        .background(Capsule().stroke(t.color(t.border), lineWidth: 1))
                    }.buttonStyle(.plain)
                    Spacer()
                    Button { editSub = sub } label: {
                        HStack(spacing: 3) {
                            Image(systemName: "square.and.pencil").font(.system(size: 10))
                            Text("编辑").font(.system(size: 11))
                        }
                        .foregroundColor(t.color(t.muted))
                        .padding(.horizontal, 9).padding(.vertical, 5)
                        .background(Capsule().stroke(t.color(t.border), lineWidth: 1))
                    }.buttonStyle(.plain)
                    Button { deleteVpsId = id } label: {
                        HStack(spacing: 3) {
                            Image(systemName: "trash").font(.system(size: 10))
                            Text("删除").font(.system(size: 11))
                        }
                        .foregroundColor(t.color(t.danger))
                        .padding(.horizontal, 9).padding(.vertical, 5)
                        .background(Capsule().stroke(t.color(t.danger), lineWidth: 1))
                    }.buttonStyle(.plain)
                }
            }
        }
    }

    private func toggleRun() async {
        toggling = true
        defer { toggling = false }
        let (ok, msg) = await conn.client.actionPostData(running ? "/vps-monitor/stop" : "/vps-monitor/start", bodyData: nil)
        toast.show(ok ? (running ? "已停止" : "已启动") : (msg.isEmpty ? "失败" : msg), error: !ok)
        await load()
    }

    private func load() async {
        err = nil
        do {
            async let s = conn.client.getDict("/vps-monitor/status")
            async let l = conn.client.getArray("/vps-monitor/subscriptions")
            let (sr, lr) = try await (s, l)
            status = sr
            subs = lr
        } catch { err = error.localizedDescription }
        loading = false
    }
}

// MARK: - VPS 订阅编辑 sheet

struct VpsSubSheet: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @Environment(\.dismiss) private var dismiss
    let editing: [String: Any]?
    let onDone: () async -> Void
    var t: Tokens { theme.t }

    @State private var models: [[String: Any]] = []
    @State private var model = ""
    @State private var subsidiary = ""
    @State private var dcs = ""
    @State private var linux = true
    @State private var windows = false
    @State private var notifyAvail = true
    @State private var notifyUnavail = false
    @State private var autoOrder = false
    @State private var quantity = 1
    @State private var autoPay = false
    @State private var loadingModels = true
    @State private var busy = false

    private var subsidiaries: [String] {
        // 与后端 vps_monitor 的合法集一致(UK 不是 OVH 子公司,是 GB)
        ["", "FR", "IE", "DE", "GB", "EU", "US", "WE", "WS", "CA", "QC", "ASIA", "IN", "PL", "SG", "AU", "MA", "TN", "SN", "CZ", "ES", "IT", "LT", "NL", "PT", "FI"]
    }

    var body: some View {
        VStack(spacing: 0) {
            SheetHeader(icon: editing == nil ? "plus.circle" : "square.and.pencil", tint: t.accent, title: editing == nil ? "添加 VPS 监控" : "编辑 VPS 监控")
            ScrollView {
                VStack(alignment: .leading, spacing: 13) {
                    Text("VPS 型号").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    if loadingModels && editing == nil {
                        HStack(spacing: 8) {
                            ProgressView().scaleEffect(0.8)
                            Text("拉取 OVH 实时目录…").font(.system(size: 11)).foregroundColor(t.color(t.muted))
                        }
                    } else {
                        Menu {
                            ForEach(models.indices, id: \.self) { i in
                                let m = models[i]
                                Button(modelLabel(m)) { model = m["planCode"] as? String ?? (m["name"] as? String ?? "") }
                            }
                        } label: {
                            HStack {
                                Text(model.isEmpty ? "选择型号(\(models.count) 款)" : model)
                                    .font(.system(size: 12.5)).foregroundColor(t.color(model.isEmpty ? t.muted : t.fg))
                                Spacer()
                                Image(systemName: "chevron.down").font(.system(size: 10)).foregroundColor(t.color(t.faint))
                            }
                            .padding(.horizontal, 13).frame(height: 44)
                            .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))
                        }
                        if editing == nil {
                            SheetField(placeholder: "或手填型号名(目录没有的)", text: $model, mono: true)
                        }
                    }

                    Text("OVH 子公司(结算区)").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    Menu {
                        ForEach(subsidiaries, id: \.self) { s in
                            Button(s.isEmpty ? "跟随账户" : s) { subsidiary = s }
                        }
                    } label: {
                        HStack {
                            Text(subsidiary.isEmpty ? "跟随账户" : subsidiary)
                                .font(.system(size: 12.5)).foregroundColor(t.color(subsidiary.isEmpty ? t.muted : t.fg))
                            Spacer()
                            Image(systemName: "chevron.down").font(.system(size: 10)).foregroundColor(t.color(t.faint))
                        }
                        .padding(.horizontal, 13).frame(height: 44)
                        .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))
                    }

                    Text("机房").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    SheetField(placeholder: "逗号分隔,留空 = 全部", text: $dcs, mono: true)

                    Text("监控系统").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.fg))
                    HStack(spacing: 10) {
                        Toggle("Linux", isOn: $linux).tint(t.color(t.accent))
                        Toggle("Windows", isOn: $windows).tint(t.color(t.accent))
                    }.font(.system(size: 12)).foregroundColor(t.color(t.fg))

                    Toggle(isOn: $notifyAvail) { Text("有货提醒").font(.system(size: 12.5)).foregroundColor(t.color(t.fg)) }.tint(t.color(t.accent))
                    Toggle(isOn: $notifyUnavail) { Text("无货提醒").font(.system(size: 12.5)).foregroundColor(t.color(t.fg)) }.tint(t.color(t.accent))

                    Toggle(isOn: $autoOrder) {
                        VStack(alignment: .leading, spacing: 1) {
                            Text("有货自动下单").font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.danger))
                            Text("以当前全局账户下单,镜像用 OVH 默认").font(.system(size: 10)).foregroundColor(t.color(t.muted))
                        }
                    }.tint(t.color(t.danger))

                    if autoOrder {
                        HStack {
                            Text("数量:\(quantity)").font(.system(size: 12)).foregroundColor(t.color(t.muted))
                            Spacer()
                            Stepper("", value: $quantity, in: 1...100).labelsHidden()
                        }
                        Toggle(isOn: $autoPay) { Text("下单后自动付款").font(.system(size: 12, weight: .semibold)).foregroundColor(t.color(t.danger)) }.tint(t.color(t.danger))
                        SheetNote(text: "自动付款有真实扣款风险,默认关闭。", tint: t.danger)
                    }

                    ActBtn(kind: .primary, icon: "checkmark", label: busy ? "保存中…" : "保存订阅", busy: busy) {
                        await save()
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .presentationDetents([.large])
        .task { await loadModels() }
        .onAppear {
            if let e = editing {
                model = e["planCode"] as? String ?? (e["model"] as? String ?? "")
                subsidiary = e["ovhSubsidiary"] as? String ?? ""
                dcs = (e["datacenters"] as? [String])?.joined(separator: ",") ?? ""
                linux = e["monitorLinux"] as? Bool ?? true
                windows = e["monitorWindows"] as? Bool ?? false
                notifyAvail = e["notifyAvailable"] as? Bool ?? true
                notifyUnavail = e["notifyUnavailable"] as? Bool ?? false
                autoOrder = e["autoOrder"] as? Bool ?? false
                quantity = e["quantity"] as? Int ?? 1
                autoPay = e["autoPay"] as? Bool ?? false
            }
        }
    }

    private func modelLabel(_ m: [String: Any]) -> String {
        let n = m["name"] as? String ?? (m["planCode"] as? String ?? "?")
        // 后端 price 是已格式化字符串(如 "€ 4.49")
        if let p = m["price"] as? String, !p.isEmpty {
            return "\(n)(\(p))"
        }
        return n
    }

    private func loadModels() async {
        if let r = try? await conn.client.getDict("/vps-monitor/models") {
            models = (r["models"] as? [[String: Any]]) ?? []
        }
        loadingModels = false
    }

    private func save() async {
        let m = model.trimmingCharacters(in: .whitespaces)
        guard !m.isEmpty else { return toast.show("先选/填型号", error: true) }
        busy = true
        defer { busy = false }
        var body: [String: Any] = [
            "planCode": m,          // 后端字段是 planCode(不是 model)
            "monitorLinux": linux,
            "monitorWindows": windows,
            "notifyAvailable": notifyAvail,
            "notifyUnavailable": notifyUnavail,
            "autoOrder": autoOrder,
        ]
        if !subsidiary.isEmpty { body["ovhSubsidiary"] = subsidiary }
        let d = dcs.split(whereSeparator: { ",;".contains($0) }).map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        if !d.isEmpty { body["datacenters"] = d }
        if autoOrder {
            let accId = conn.accountId.isEmpty ? (conn.activeAccount?["id"] as? String ?? "") : conn.accountId
            if !accId.isEmpty { body["autoOrderAccountId"] = accId }
            body["quantity"] = quantity
            body["autoPay"] = autoPay
        }
        do {
            if let e = editing, let id = e["id"] {
                _ = try await conn.client.put("/vps-monitor/subscriptions/\(id)", body: body)
            } else {
                _ = try await conn.client.post("/vps-monitor/subscriptions", body: body)
            }
            toast.show("已保存")
            dismiss()
            await onDone()
        } catch { toast.show(error.localizedDescription, error: true) }
    }
}
