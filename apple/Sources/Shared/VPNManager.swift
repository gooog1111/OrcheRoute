import Foundation
import NetworkExtension
import Combine

@MainActor
final class VPNManager: ObservableObject {
    @Published var status: NEVPNStatus = .invalid
    @Published var error: String?
    private var manager: NETunnelProviderManager?
    private var observer: NSObjectProtocol?

    init() {
        observer = NotificationCenter.default.addObserver(
            forName: .NEVPNStatusDidChange, object: nil, queue: .main
        ) { [weak self] _ in Task { @MainActor in self?.refreshStatus() } }
        Task { await load() }
    }

    deinit { if let observer { NotificationCenter.default.removeObserver(observer) } }

    var connected: Bool { status == .connected || status == .connecting || status == .reasserting }
    var statusText: String {
        switch status {
        case .connected: return "Защищено"
        case .connecting: return "Подключение"
        case .disconnecting: return "Отключение"
        case .reasserting: return "Переключение сервера"
        case .disconnected: return "Выключено"
        case .invalid: return "Профиль не установлен"
        @unknown default: return "Неизвестно"
        }
    }

    func load() async {
        do {
            let managers = try await NETunnelProviderManager.loadAllFromPreferences()
            manager = managers.first(where: { ($0.protocolConfiguration as? NETunnelProviderProtocol)?.providerBundleIdentifier?.hasPrefix("online.gooog1111.orcheroute") == true })
            if manager == nil { try await install() }
            refreshStatus()
        } catch { self.error = error.localizedDescription }
    }

    func install() async throws {
        let value = NETunnelProviderManager()
        let configuration = NETunnelProviderProtocol()
#if os(macOS)
        configuration.providerBundleIdentifier = "online.gooog1111.orcheroute.macos.tunnel"
#else
        configuration.providerBundleIdentifier = "online.gooog1111.orcheroute.tunnel"
#endif
        configuration.serverAddress = "OrcheRoute"
        configuration.includeAllNetworks = true
        value.protocolConfiguration = configuration
        value.localizedDescription = "OrcheRoute"
        value.isEnabled = true
        try await value.saveToPreferences()
        try await value.loadFromPreferences()
        manager = value
    }

    func toggle() async {
        error = nil
        do {
            if manager == nil { try await install() }
            guard let manager else { throw CoreBridgeError.core("Не удалось установить системный VPN-профиль") }
            if connected { manager.connection.stopVPNTunnel() }
            else {
                guard !SharedState.proxyJSON.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
                    throw CoreBridgeError.core("Сначала сохраните активный сервер")
                }
                try await manager.loadFromPreferences()
                try manager.connection.startVPNTunnel()
            }
            refreshStatus()
        } catch { self.error = error.localizedDescription }
    }

    private func refreshStatus() { status = manager?.connection.status ?? .invalid }
}
