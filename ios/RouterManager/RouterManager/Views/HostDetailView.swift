import SwiftUI

struct HostDetailView: View {
    @EnvironmentObject var session: RouterSession
    let host: Host

    @State private var alias = ""
    @State private var blocked = false
    @State private var rateLimit = 0
    @State private var selectedApps: Set<String> = []
    @State private var showAppPicker = false

    var body: some View {
        Form {
            Section("基本信息") {
                Row(title: "MAC", value: host.mac)
                Row(title: "IP", value: host.ip ?? "-")
                Row(title: "连接方式", value: host.isWireless ? "无线 (\(host.iface ?? "-"))" : "有线")
                Row(title: "厂商", value: host.vendor ?? "-")
                Row(title: "状态", value: host.online ? "在线" : "离线")
            }

            Section("备注") {
                TextField("给这台设备起个名字", text: $alias)
                    .onSubmit { Task { await session.rename(host, alias: alias) } }
            }

            Section("网络权限") {
                Toggle("禁止上网", isOn: $blocked)
                    .onChange(of: blocked) { newValue in
                        Task { await session.setBlocked(host, blocked: newValue) }
                    }
                Stepper(value: $rateLimit, in: 0...1000000, step: 512) {
                    Row(title: "限速", value: rateLimit == 0 ? "不限速" : "\(rateLimit) kbps")
                }
                .onChange(of: rateLimit) { newValue in
                    Task { await session.setRateLimit(host, kbps: newValue) }
                }
            }

            Section {
                Button {
                    showAppPicker = true
                } label: {
                    HStack {
                        Text("限制 App 访问")
                        Spacer()
                        Text(selectedApps.isEmpty ? "未限制" : "已选 \(selectedApps.count) 项")
                            .foregroundColor(.secondary)
                    }
                }
            } header: {
                Text("应用管控")
            } footer: {
                Text("由 DPI 引擎识别应用流量并拦截；可枚举域名的应用同时走域名过滤。")
            }
        }
        .navigationTitle(host.displayName)
        .onAppear(perform: setup)
        .sheet(isPresented: $showAppPicker) {
            AppPickerView(selected: $selectedApps, apps: session.apps) { picked in
                Task { await session.setBlockedApps(host, apps: Array(picked)) }
            }
        }
    }

    private func setup() {
        alias = host.alias ?? ""
        blocked = host.blocked
        rateLimit = host.rateLimitKbps
        selectedApps = Set(host.blockedApps)
    }

    private struct Row: View {
        let title: String
        let value: String
        var body: some View {
            HStack {
                Text(title).foregroundColor(.secondary)
                Spacer()
                Text(value).font(.footnote)
            }
        }
    }
}

/// 按分类分组的 App 多选列表
private struct AppPickerView: View {
    @Binding var selected: Set<String>
    let apps: [AppInfo]
    var onDone: (Set<String>) -> Void

    @Environment(\.dismiss) private var dismiss

    private var grouped: [(String, [AppInfo])] {
        let dict = Dictionary(grouping: apps, by: { $0.categoryName })
        return dict.sorted { $0.key < $1.key }
    }

    var body: some View {
        NavigationStack {
            List {
                ForEach(grouped, id: \.0) { category, list in
                    Section(category) {
                        ForEach(list) { app in
                            HStack {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(app.name)
                                    Text(app.engines.joined(separator: " + "))
                                        .font(.caption2)
                                        .foregroundColor(.secondary)
                                }
                                Spacer()
                                if selected.contains(app.id) {
                                    Image(systemName: "checkmark").foregroundColor(.blue)
                                }
                            }
                            .contentShape(Rectangle())
                            .onTapGesture {
                                if selected.contains(app.id) { selected.remove(app.id) }
                                else { selected.insert(app.id) }
                            }
                        }
                    }
                }
            }
            .navigationTitle("选择要限制的 App")
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("保存") {
                        onDone(selected)
                        dismiss()
                    }
                }
                ToolbarItem(placement: .cancellationAction) {
                    Button("取消") { dismiss() }
                }
            }
        }
    }
}
