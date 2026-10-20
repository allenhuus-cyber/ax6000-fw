import SwiftUI

struct HostsView: View {
    @EnvironmentObject var session: RouterSession

    private var online: [Host] { session.hosts.filter { $0.online } }
    private var offline: [Host] { session.hosts.filter { !$0.online } }

    var body: some View {
        NavigationStack {
            List {
                Section("在线 (\(online.count))") {
                    if online.isEmpty {
                        Text("暂无在线设备").foregroundColor(.secondary)
                    } else {
                        ForEach(online) { host in
                            NavigationLink(value: host.mac) {
                                HostRow(host: host)
                            }
                            .swipeActions(edge: .trailing) {
                                Button(role: .destructive) {
                                    Task { await session.setBlocked(host, blocked: true) }
                                } label: { Label("断网", systemImage: "wifi.slash") }
                            }
                        }
                    }
                }

                Section("离线 (\(offline.count))") {
                    ForEach(offline) { host in
                        NavigationLink(value: host.mac) { HostRow(host: host) }
                    }
                }
            }
            .refreshable { await session.refresh() }
            .navigationTitle("终端管理")
            .navigationDestination(for: String.self) { mac in
                if let host = session.hosts.first(where: { $0.mac == mac }) {
                    HostDetailView(host: host)
                }
            }
        }
    }
}

private struct HostRow: View {
    let host: Host

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: host.isWireless ? "wifi" : "cable.connector")
                .foregroundColor(host.online ? .green : .gray)
                .frame(width: 24)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(host.displayName).font(.body)
                    if host.blocked {
                        Text("已断网")
                            .font(.caption2)
                            .padding(.horizontal, 6).padding(.vertical, 2)
                            .background(Color.red.opacity(0.15))
                            .foregroundColor(.red)
                            .clipShape(Capsule())
                    }
                }
                Text(subtitle).font(.caption).foregroundColor(.secondary)
            }
            Spacer()
            if !host.blockedApps.isEmpty {
                Image(systemName: "app.badge.checkmark")
                    .foregroundColor(.orange)
            }
        }
    }

    private var subtitle: String {
        var parts: [String] = []
        if let ip = host.ip { parts.append(ip) }
        if let v = host.vendor { parts.append(v) }
        if !host.blockedApps.isEmpty { parts.append("限制 \(host.blockedApps.count) 个 App") }
        return parts.joined(separator: " · ")
    }
}
