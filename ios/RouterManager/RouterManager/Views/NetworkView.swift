import SwiftUI

struct NetworkView: View {
    @EnvironmentObject var session: RouterSession

    @State private var mode = "dhcp"
    @State private var username = ""
    @State private var password = ""
    @State private var serviceName = ""
    @State private var ip = ""
    @State private var netmask = "255.255.255.0"
    @State private var gateway = ""
    @State private var dns = ""
    @State private var mtu = ""
    @State private var macClone = ""
    @State private var relaySSID = ""
    @State private var relayPassword = ""
    @State private var relayBand = "5g"
    @State private var networks: [APIClient.RelayNetwork] = []
    @State private var scanning = false
    @State private var showScan = false
    @State private var saved = false

    private let modes = [("dhcp", "自动获取 (DHCP)"), ("pppoe", "宽带拨号 (PPPoE)"),
                         ("static", "静态地址"), ("relay", "无线中继")]

    var body: some View {
        NavigationStack {
            Form {
                Section("上网方式") {
                    Picker("方式", selection: $mode) {
                        ForEach(modes, id: \.0) { key, title in
                            Text(title).tag(key)
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }

                if mode == "pppoe" {
                    Section("宽带账号") {
                        TextField("用户名", text: $username).autocapitalization(.none)
                        SecureField("密码", text: $password)
                        TextField("服务名（可选）", text: $serviceName)
                    }
                }

                if mode == "static" {
                    Section("静态地址") {
                        TextField("IP 地址", text: $ip).keyboardType(.decimalPad)
                        TextField("子网掩码", text: $netmask).keyboardType(.decimalPad)
                        TextField("网关", text: $gateway).keyboardType(.decimalPad)
                        TextField("DNS（逗号分隔）", text: $dns)
                    }
                }

                if mode == "relay" {
                    Section("中继目标") {
                        Button {
                            Task { await scan() }
                        } label: {
                            HStack {
                                Text("扫描周边热点")
                                Spacer()
                                if scanning { ProgressView() }
                            }
                        }
                        if !networks.isEmpty {
                            NavigationLink("选择热点 (\(networks.count))") {
                                RelayListView(networks: networks) { n in
                                    relaySSID = n.ssid
                                    if let b = n.band { relayBand = b }
                                }
                            }
                        }
                        TextField("SSID", text: $relaySSID)
                        SecureField("密码", text: $relayPassword)
                        Picker("频段", selection: $relayBand) {
                            Text("5GHz").tag("5g")
                            Text("2.4GHz").tag("2g")
                        }
                    }
                }

                Section("高级") {
                    TextField("MTU（可选）", text: $mtu).keyboardType(.numberPad)
                    TextField("MAC 克隆（可选，如 aa:bb:cc:dd:ee:ff）", text: $macClone)
                        .autocapitalization(.none)
                }

                Section {
                    Button {
                        Task { await save() }
                    } label: {
                        HStack { Spacer(); Text("保存并应用").bold(); Spacer() }
                    }
                    if saved {
                        Label("已下发，等待路由器拨号…", systemImage: "checkmark.circle.fill")
                            .foregroundColor(.green)
                            .font(.footnote)
                    }
                }

                if let err = session.errorMessage {
                    Section { Text(err).foregroundColor(.red).font(.footnote) }
                }
            }
            .navigationTitle("上网设置")
            .task { await load() }
        }
    }

    private func load() async {
        await session.loadNetworkConfig()
        guard let cfg = session.wanConfig else { return }
        mode = cfg.mode
        username = cfg.username ?? ""
        serviceName = cfg.serviceName ?? ""
        ip = cfg.ip ?? ""
        netmask = cfg.netmask ?? "255.255.255.0"
        gateway = cfg.gateway ?? ""
        dns = cfg.dns?.joined(separator: ",") ?? ""
        mtu = cfg.mtu.map(String.init) ?? ""
        macClone = cfg.macClone ?? ""
        relaySSID = cfg.relaySSID ?? ""
        relayBand = cfg.relayBand ?? "5g"
    }

    private func scan() async {
        scanning = true
        defer { scanning = false }
        do {
            networks = try await session.scanRelay()
            showScan = true
        } catch {
            session.errorMessage = error.localizedDescription
        }
    }

    private func save() async {
        saved = false
        let cfg = WANConfig(
            mode: mode,
            username: mode == "pppoe" ? username : nil,
            password: mode == "pppoe" ? password : nil,
            ip: mode == "static" ? ip : nil,
            netmask: mode == "static" ? netmask : nil,
            gateway: mode == "static" ? gateway : nil,
            dns: mode == "static" ? dns.split(separator: ",").map { String($0).trimmingCharacters(in: .whitespaces) } : nil,
            mtu: Int(mtu),
            macClone: macClone.isEmpty ? nil : macClone,
            relaySSID: mode == "relay" ? relaySSID : nil,
            relayPassword: mode == "relay" ? relayPassword : nil,
            relayBand: mode == "relay" ? relayBand : nil,
            serviceName: serviceName.isEmpty ? nil : serviceName
        )
        await session.applyWAN(cfg)
        saved = true
    }
}

private struct RelayListView: View {
    let networks: [APIClient.RelayNetwork]
    var onPick: (APIClient.RelayNetwork) -> Void

    var body: some View {
        List(networks) { n in
            Button {
                onPick(n)
            } label: {
                HStack {
                    VStack(alignment: .leading) {
                        Text(n.ssid.isEmpty ? "(隐藏网络)" : n.ssid)
                        Text("\(n.band ?? "-") · 信道 \(n.channel.map(String.init) ?? "-") · \(n.encryption ?? "none")")
                            .font(.caption).foregroundColor(.secondary)
                    }
                    Spacer()
                    Text("\(n.signal ?? 0) dBm").font(.caption).foregroundColor(.secondary)
                }
            }
        }
        .navigationTitle("选择热点")
    }
}
