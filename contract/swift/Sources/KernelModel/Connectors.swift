import Foundation

/// K8 Connectors (contract/spec/K8-connectors.md).
public struct ConnectorDescriptor: Hashable, Codable, Sendable {
    public var tenantId = ""
    public var connectorId = ""
    public var direction = "CONNECTOR_DIRECTION_UNSPECIFIED"
    public var dataClasses: [String] = []
    /// Seconds; Protobuf JSON writes durations as "60s".
    public var heartbeat: TimeInterval = 0
    public var disabled = false

    public init() {}

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tenantId = try c.decodeIfPresent(String.self, forKey: .tenantId) ?? ""
        connectorId = try c.decodeIfPresent(String.self, forKey: .connectorId) ?? ""
        direction = try c.decodeIfPresent(String.self, forKey: .direction) ?? direction
        dataClasses = try c.decodeIfPresent([String].self, forKey: .dataClasses) ?? []
        heartbeat = TimeInterval((try c.decodeIfPresent(String.self, forKey: .heartbeat) ?? "0s").dropLast()) ?? 0
        disabled = try c.decodeIfPresent(Bool.self, forKey: .disabled) ?? false
    }
}

public enum ConnectorHealth: String, Codable, Sendable {
    case ok = "CONNECTOR_HEALTH_OK"
    case stale = "CONNECTOR_HEALTH_STALE"
    case disabled = "CONNECTOR_HEALTH_DISABLED"
}

public struct ConnectorStatus: Equatable, Sendable {
    public let health: ConnectorHealth
    public let lastSeen: Date?
    public let cursor: String
}

public struct Connectors: Sendable {
    private struct Key: Hashable { let tenant, connector: String }
    private var descriptors: [Key: ConnectorDescriptor] = [:]
    private var cursors: [Key: String] = [:]
    private var lastSeen: [Key: Date] = [:]

    public init() {}

    public mutating func register(_ d: ConnectorDescriptor) throws(KernelError) {
        guard !d.tenantId.isEmpty, !d.connectorId.isEmpty, !d.dataClasses.isEmpty,
              d.direction != "CONNECTOR_DIRECTION_UNSPECIFIED" else { throw .invalidArgument } // N1
        let key = Key(tenant: d.tenantId, connector: d.connectorId)
        guard descriptors[key] == nil else { throw .conflict }
        descriptors[key] = d
    }

    /// One batch about `dataClass`; poll pages move the cursor from → to (N2, N3).
    public mutating func deliver(tenant: String, connector: String, dataClass: String, cursorFrom: String = "",
                                 cursorTo: String = "", at now: Date) throws(KernelError) {
        let key = Key(tenant: tenant, connector: connector)
        guard let d = descriptors[key] else { throw .notFound }
        guard !d.disabled, d.dataClasses.contains(dataClass) else { throw .policyDenied }
        if d.direction == "CONNECTOR_DIRECTION_PUSH" {
            guard cursorFrom.isEmpty, cursorTo.isEmpty else { throw .invalidArgument }
        } else {
            guard cursorFrom == cursors[key, default: ""] else { throw .conflict }
            cursors[key] = cursorTo
        }
        lastSeen[key] = now
    }

    public mutating func heartbeat(tenant: String, connector: String, at now: Date) throws(KernelError) {
        let key = Key(tenant: tenant, connector: connector)
        guard descriptors[key] != nil else { throw .notFound }
        lastSeen[key] = now
    }

    /// Health at `now` (N4).
    public func status(tenant: String, connector: String, at now: Date) throws(KernelError) -> ConnectorStatus {
        let key = Key(tenant: tenant, connector: connector)
        guard let d = descriptors[key] else { throw .notFound }
        let seen = lastSeen[key]
        let health: ConnectorHealth = d.disabled ? .disabled
            : seen.map { now.timeIntervalSince($0) > d.heartbeat } ?? true ? .stale : .ok
        return ConnectorStatus(health: health, lastSeen: seen, cursor: cursors[key, default: ""])
    }
}
