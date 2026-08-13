import Foundation
import NetworkExtension

enum TunnelFileDescriptor {
    /// NetworkExtension backs NEPacketTunnelFlow with a system utun socket.
    /// Apple does not expose the descriptor as a typed property, while native
    /// Go packet engines require it. Fail closed if the implementation changes.
    static func resolve(_ flow: NEPacketTunnelFlow) -> Int32? {
        if let number = flow.value(forKeyPath: "socket.fileDescriptor") as? NSNumber {
            return number.int32Value
        }
        return nil
    }
}
