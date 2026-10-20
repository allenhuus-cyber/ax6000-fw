import SwiftUI

struct RootView: View {
    @EnvironmentObject var session: RouterSession

    var body: some View {
        Group {
            if session.identity == nil {
                PairingView()
            } else {
                TabView {
                    DashboardView()
                        .tabItem { Label("概览", systemImage: "gauge") }
                    HostsView()
                        .tabItem { Label("终端", systemImage: "laptopcomputer.and.iphone") }
                    NetworkView()
                        .tabItem { Label("上网", systemImage: "globe") }
                    SettingsView()
                        .tabItem { Label("设置", systemImage: "gearshape") }
                }
            }
        }
    }
}
