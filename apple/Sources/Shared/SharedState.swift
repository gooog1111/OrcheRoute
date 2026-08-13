import Foundation

enum SharedState {
    static let group = "group.online.gooog1111.orcheroute"
    static let suite = UserDefaults(suiteName: group)!
    static let proxyKey = "active_proxy_json"
    static let routesKey = "routes_json"
    static let dnsKey = "dns_json"
    static let lastErrorKey = "last_tunnel_error"

    static var container: URL? {
        FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group)
    }

    static var proxyJSON: String { suite.string(forKey: proxyKey) ?? "" }
    static var routesJSON: String {
        suite.string(forKey: routesKey) ?? #"{"default":"proxy","lists":{"direct":[],"proxy":[],"block":[]}}"#
    }
    static var dnsJSON: String {
        suite.string(forKey: dnsKey) ?? #"{"direct":["1.1.1.1","8.8.8.8"],"proxy":["https://1.1.1.1/dns-query","https://dns.google/dns-query"],"vpn_underlay":["1.1.1.1","8.8.8.8"],"bootstrap":["1.1.1.1","8.8.8.8"],"cache_algorithm":"arc","prefer_h3":false,"use_hosts":true,"ipv6":false}"#
    }
}
