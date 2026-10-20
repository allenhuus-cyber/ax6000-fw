import SwiftUI

struct DashboardView: View {
    @EnvironmentObject var session: RouterSession

    var body: some View {
        NavigationStack {
            List {
                if let st = session.status {
                    Section {
                        HStack {
                            Label(st.wan.up ? "已联网" : "未联网",
                                  systemImage: st.wan.up ? "checkmark.circle.fill" : "xmark.circle.fill")
                                .foregroundColor(st.wan.up ? .green : .red)
                            Spacer()
                            Text(modeText(st.wan.mode)).foregroundColor(.secondary)
                        }
                        Row(title: "WAN IP", value: st.wan.ip ?? "-")
                        Row(title: "网关", value: st.wan.gateway ?? "-")
                        Row(title: "DNS", value: st.wan.dns?.joined(separator: ", ") ?? "-")
                        Row(title: "局域网地址", value: st.lanAddr)
                    } header: {
                        Text("网络")
                    }

                    Section("设备") {
                        Row(title: "在线终端", value: "\(st.hostOnline) / \(st.hostTotal)")
                        Row(title: "运行时间", value: uptime(st.uptime))
                        Row(title: "CPU 占用", value: "\(st.cpuUsage)%")
                        Row(title: "内存剩余", value: byteText(st.memFree) + " / " + byteText(st.memTotal))
                        Row(title: "固件", value: st.firmware)
                    }
                } else if session.isLoading {
                    HStack { Spacer(); ProgressView(); Spacer() }
                } else {
                    Text("未连接到路由器").foregroundColor(.secondary)
                }

                if let err = session.errorMessage {
                    Section { Text(err).foregroundColor(.red).font(.footnote) }
                }
            }
            .refreshable { await session.connectAndLoad() }
            .navigationTitle(session.identity?.name ?? "路由器")
            .toolbar {
                ToolbarItem(placement: .navigationBarTrailing) {
                    HStack(spacing: 8) {
                        Image(systemName: session.mode == .lan ? "wifi" : "cloud")
                            .foregroundColor(session.connected ? .green : .gray)
                        Button { Task { await session.connectAndLoad() } } label: {
                            Image(systemName: "arrow.clockwise")
                        }
                    }
                }
            }
        }
    }

    private func modeText(_ mode: String) -> String {
        switch mode {
        case "pppoe": return "宽带拨号 (PPPoE)"
        case "dhcp": return "自动获取 (DHCP)"
        case "static": return "静态地址"
        case "relay": return "无线中继"
        default: return mode
        }
    }

    private func uptime(_ sec: Int64) -> String {
        let d = sec / 86400, h = (sec % 86400) / 3600, m = (sec % 3600) / 60
        if d > 0 { return "\(d) 天 \(h) 小时" }
        if h > 0 { return "\(h) 小时 \(m) 分" }
        return "\(m) 分钟"
    }

    private func byteText(_ b: Int64) -> String {
        let mb = Double(b) / 1024 / 1024
        if mb >= 1024 { return String(format: "%.1f GB", mb / 1024) }
        return String(format: "%.0f MB", mb)
    }
}

private struct Row: View {
    let title: String
    let value: String
    var body: some View {
        HStack {
            Text(title).foregroundColor(.secondary)
            Spacer()
            Text(value).lineLimit(1).truncationMode(.middle)
        }
    }
}
