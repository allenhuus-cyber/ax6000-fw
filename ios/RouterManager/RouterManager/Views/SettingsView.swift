import SwiftUI

struct SettingsView: View {
    @EnvironmentObject var session: RouterSession

    @State private var showUnbindConfirm = false

    var body: some View {
        NavigationStack {
            Form {
                Section("提醒") {
                    Toggle("终端上下线提醒", isOn: $session.notifyEnabled)
                        .onChange(of: session.notifyEnabled) { newValue in
                            if newValue { session.registerPushIfNeeded() }
                        }
                    HStack {
                        Text("连接方式")
                        Spacer()
                        Text(session.mode == .lan ? "局域网直连" : "云端中继")
                            .foregroundColor(.secondary)
                    }
                    HStack {
                        Text("实时连接")
                        Spacer()
                        Circle()
                            .fill(session.connected ? Color.green : Color.gray)
                            .frame(width: 8, height: 8)
                    }
                }

                Section("路由器") {
                    Row2(title: "名称", value: session.identity?.name ?? "-")
                    Row2(title: "设备 ID", value: session.identity?.deviceId ?? "-")
                    Row2(title: "固件", value: session.status?.firmware ?? "-")
                    Row2(title: "局域网地址", value: session.identity?.lanHost ?? "-")
                }

                Section {
                    Button(role: .destructive) {
                        showUnbindConfirm = true
                    } label: {
                        Text("解绑此路由器")
                    }
                } footer: {
                    Text("解绑会删除本机保存的配对凭证，需要重新配对才能管理。")
                }
            }
            .navigationTitle("设置")
            .alert("确认解绑？", isPresented: $showUnbindConfirm) {
                Button("解绑", role: .destructive) { session.unbind() }
                Button("取消", role: .cancel) {}
            }
        }
    }
}

private struct Row2: View {
    let title: String
    let value: String
    var body: some View {
        HStack {
            Text(title).foregroundColor(.secondary)
            Spacer()
            Text(value).font(.footnote).lineLimit(1).truncationMode(.middle)
        }
    }
}
