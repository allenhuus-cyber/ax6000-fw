import Foundation

/// 基于 URLSessionWebSocketTask 的事件流客户端（局域网 ws:// 或云端 wss://）。
final class WebSocketClient: NSObject, URLSessionWebSocketDelegate {
    private var task: URLSessionWebSocketTask?
    private var session: URLSession?
    private var pingTimer: Timer?

    /// 收到事件时的回调（主线程）
    var onEvent: ((RouterEvent) -> Void)?
    /// 连接状态回调（主线程）
    var onStateChange: ((Bool) -> Void)?

    private let urlProvider: () -> URL?
    private var reconnectDelay: TimeInterval = 1

    init(urlProvider: @escaping () -> URL?) {
        self.urlProvider = urlProvider
        super.init()
    }

    func start() {
        stop()
        guard let url = urlProvider() else { return }
        let session = URLSession(configuration: .default, delegate: self, delegateQueue: nil)
        self.session = session
        let task = session.webSocketTask(with: url)
        self.task = task
        task.resume()
        onStateChange?(true)
        receive()
        startPing()
    }

    func stop() {
        pingTimer?.invalidate()
        pingTimer = nil
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
        session?.invalidateAndCancel()
        session = nil
    }

    private func startPing() {
        pingTimer?.invalidate()
        pingTimer = Timer.scheduledTimer(withTimeInterval: 25, repeats: true) { [weak self] _ in
            self?.task?.sendPing { error in
                if error != nil { self?.scheduleReconnect() }
            }
        }
    }

    private func receive() {
        task?.receive { [weak self] result in
            guard let self else { return }
            switch result {
            case .success(let message):
                self.reconnectDelay = 1
                if case .string(let text) = message,
                   let data = text.data(using: .utf8),
                   let event = try? JSONDecoder().decode(RouterEvent.self, from: data) {
                    DispatchQueue.main.async { self.onEvent?(event) }
                } else if case .data(let data) = message,
                          let event = try? JSONDecoder().decode(RouterEvent.self, from: data) {
                    DispatchQueue.main.async { self.onEvent?(event) }
                }
                self.receive()
            case .failure:
                self.onStateChange?(false)
                self.scheduleReconnect()
            }
        }
    }

    private func scheduleReconnect() {
        DispatchQueue.main.asyncAfter(deadline: .now() + reconnectDelay) { [weak self] in
            guard let self else { return }
            self.reconnectDelay = min(self.reconnectDelay * 2, 30)
            self.start()
        }
    }

    // MARK: - URLSessionWebSocketDelegate

    func urlSession(_ session: URLSession,
                    webSocketTask: URLSessionWebSocketTask,
                    didOpenWithProtocol protocol: String?) {
        reconnectDelay = 1
        DispatchQueue.main.async { self.onStateChange?(true) }
    }

    func urlSession(_ session: URLSession,
                    webSocketTask: URLSessionWebSocketTask,
                    didCloseWith closeCode: URLSessionWebSocketTask.CloseCode,
                    reason: Data?) {
        DispatchQueue.main.async { self.onStateChange?(false) }
        scheduleReconnect()
    }
}
