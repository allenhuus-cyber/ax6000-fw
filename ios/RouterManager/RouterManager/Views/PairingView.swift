import SwiftUI

struct PairingView: View {
    @EnvironmentObject var session: RouterSession

    @State private var host = "192.168.1.1"
    @State private var port = "8080"
    @State private var username = "root"
    @State private var password = ""
    @State private var cloudURL = ""
    @State private var busy = false
    @State private var error: String?
    @State private var showScanner = false

    var body: some View {
        NavigationStack {
            Form {
                Section("路由器地址") {
                    TextField("IP / 域名", text: $host)
                        .keyboardType(.decimalPad)
                        .autocapitalization(.none)
                    TextField("API 端口", text: $port)
                        .keyboardType(.numberPad)
                }
                Section("管理账号") {
                    TextField("用户名", text: $username).autocapitalization(.none)
                    SecureField("密码", text: $password)
                }
                Section {
                    TextField("云端地址（可选，如 https://cloud.example.com）", text: $cloudURL)
                        .autocapitalization(.none)
                        .keyboardType(.URL)
                } header: {
                    Text("远程管理")
                } footer: {
                    Text("填写后可在外网通过云端中继管理路由器并接收推送提醒。")
                }

                Section {
                    Button {
                        Task { await pair() }
                    } label: {
                        HStack {
                            Spacer()
                            if busy { ProgressView() } else { Text("配对并连接").bold() }
                            Spacer()
                        }
                    }
                    .disabled(busy || host.isEmpty || password.isEmpty)

                    Button {
                        showScanner = true
                    } label: {
                        Label("扫描路由器上的配对二维码", systemImage: "qrcode.viewfinder")
                    }
                }

                if let error {
                    Section { Text(error).foregroundColor(.red) }
                }
            }
            .navigationTitle("配对路由器")
            .sheet(isPresented: $showScanner) {
                QRScannerView { info in
                    showScanner = false
                    session.savePairing(info, cloudURL: cloudURL)
                }
            }
        }
    }

    private func pair() async {
        busy = true
        error = nil
        defer { busy = false }
        do {
            guard let p = Int(port) else { throw APIError.badURL }
            let info = try await APIClient.pair(host: host, port: p, username: username, password: password)
            session.savePairing(info, cloudURL: cloudURL.isEmpty ? nil : cloudURL)
        } catch {
            self.error = error.localizedDescription
        }
    }
}
