import Foundation
import NetworkExtension
import Mobilecore

final class PacketTunnelProvider: NEPacketTunnelProvider {
    override func startTunnel(options: [String : NSObject]?, completionHandler: @escaping (Error?) -> Void) {
        do {
            guard let home = SharedState.container?.appendingPathComponent("mihomo", isDirectory: true) else {
                throw CoreBridgeError.core("Недоступно общее хранилище OrcheRoute")
            }
            try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
            guard !SharedState.proxyJSON.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
                throw CoreBridgeError.core("Активный сервер не настроен")
            }
            try CoreBridge.prepare(home: home.path)
            let configuration = try CoreBridge.buildConfig(
                proxy: SharedState.proxyJSON, routes: SharedState.routesJSON, dns: SharedState.dnsJSON
            )
            try CoreBridge.load(configuration)
            applyNetworkSettings { [weak self] error in
                guard let self else { return completionHandler(CoreBridgeError.core("Tunnel provider остановлен")) }
                if let error { return completionHandler(error) }
                do {
                    guard let fd = TunnelFileDescriptor.resolve(self.packetFlow) else {
                        throw CoreBridgeError.core("Система не предоставила utun для OrcheRoute")
                    }
                    try CoreBridge.start(fd: fd)
                    SharedState.suite.removeObject(forKey: SharedState.lastErrorKey)
                    completionHandler(nil)
                } catch {
                    SharedState.suite.set(error.localizedDescription, forKey: SharedState.lastErrorKey)
                    CoreBridge.stop()
                    completionHandler(error)
                }
            }
        } catch {
            SharedState.suite.set(error.localizedDescription, forKey: SharedState.lastErrorKey)
            completionHandler(error)
        }
    }

    override func stopTunnel(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
        CoreBridge.stop()
        completionHandler()
    }

    override func handleAppMessage(_ messageData: Data, completionHandler: ((Data?) -> Void)?) {
        let traffic = MobilecoreEngineTraffic()
        completionHandler?(traffic?.data(using: .utf8))
    }

    private func applyNetworkSettings(completion: @escaping (Error?) -> Void) {
        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: "127.0.0.1")
        let ipv4 = NEIPv4Settings(addresses: ["198.18.0.1"], subnetMasks: ["255.255.255.252"])
        ipv4.includedRoutes = [.default()]
        settings.ipv4Settings = ipv4
        let ipv6 = NEIPv6Settings(addresses: ["fdfe:dcba:9876::1"], networkPrefixLengths: [126])
        ipv6.includedRoutes = [.default()]
        settings.ipv6Settings = ipv6
        let dns = NEDNSSettings(servers: ["198.18.0.2"])
        dns.matchDomains = [""]
        settings.dnsSettings = dns
        settings.mtu = 1500
        setTunnelNetworkSettings(settings, completionHandler: completion)
    }
}
