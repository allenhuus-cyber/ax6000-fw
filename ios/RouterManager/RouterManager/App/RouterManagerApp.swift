import SwiftUI

@main
struct RouterManagerApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var session = RouterSession()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(session)
                .onAppear {
                    session.bootstrap()
                    delegate.onToken = { data in
                        session.didReceiveAPNsToken(data)
                    }
                }
        }
    }
}

/// 处理 APNs 注册回调（远程推送的 device token 需要上报到云端）
final class AppDelegate: NSObject, UIApplicationDelegate {
    var onToken: ((Data) -> Void)?

    func application(_ application: UIApplication,
                     didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        onToken?(deviceToken)
    }

    func application(_ application: UIApplication,
                     didFailToRegisterForRemoteNotificationsWithError error: Error) {
        print("APNs 注册失败: \(error.localizedDescription)")
    }
}
