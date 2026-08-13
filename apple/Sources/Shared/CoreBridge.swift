import Foundation
import Mobilecore

enum CoreBridgeError: LocalizedError {
    case invalidResponse
    case core(String)

    var errorDescription: String? {
        switch self {
        case .invalidResponse: return "Ядро OrcheRoute вернуло некорректный ответ"
        case .core(let message): return message
        }
    }
}

enum CoreBridge {
    static func result(_ payload: String?) throws -> [String: Any] {
        guard let payload,
              let data = payload.data(using: .utf8),
              let envelope = try JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { throw CoreBridgeError.invalidResponse }
        if (envelope["ok"] as? Bool) == true {
            return envelope["result"] as? [String: Any] ?? [:]
        }
        let error = envelope["error"] as? [String: Any]
        throw CoreBridgeError.core(error?["error"] as? String ?? "Ошибка ядра OrcheRoute")
    }

    static func prepare(home: String) throws {
        _ = try result(MobilecoreEngineInit(home, nil))
    }

    static func buildConfig(proxy: String, routes: String, dns: String) throws -> String {
        let value = try result(MobilecoreBuildMobileProxyConfigWithNetwork(proxy, routes, dns))
        guard let config = value["config"] as? String else { throw CoreBridgeError.invalidResponse }
        return config
    }

    static func load(_ config: String) throws { _ = try result(MobilecoreEngineLoadConfig(config)) }
    static func start(fd: Int32) throws {
        _ = try result(MobilecoreEngineStartTun(Int(fd), "gvisor", "198.18.0.1/30,fdfe:dcba:9876::1/126", "198.18.0.2"))
    }
    static func stop() { _ = try? result(MobilecoreEngineStopTun()) }
}
