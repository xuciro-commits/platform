/// K7 Schema evolution (Contract/spec/K7-schema-evolution.md).
public struct UpgradeStep: Hashable, Codable, Sendable {
    public var name: String
    public var fromVersion: UInt32

    public init(name: String, fromVersion: UInt32) {
        self.name = name
        self.fromVersion = fromVersion
    }

    // Proto3 JSON omits default values.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        fromVersion = try c.decodeIfPresent(UInt32.self, forKey: .fromVersion) ?? 0
    }
}

/// What one receiver can read.
public struct SchemaRegistry: Sendable {
    private var known: Set<SchemaRef>
    private var upgrades: Set<UpgradeStep>

    public init(known: some Sequence<SchemaRef>, upgrades: some Sequence<UpgradeStep> = []) {
        self.known = Set(known)
        self.upgrades = Set(upgrades)
    }

    /// Known, or upgradeable to a known version (S1).
    public func accepts(_ s: SchemaRef) -> Bool {
        var v = s.version
        while !known.contains(SchemaRef(name: s.name, version: v)) {
            guard upgrades.contains(UpgradeStep(name: s.name, fromVersion: v)) else { return false }
            v += 1
        }
        return true
    }

    public mutating func add(_ u: UpgradeStep) throws(KernelError) {
        guard !u.name.isEmpty, u.fromVersion >= 1 else { throw .invalidArgument }                        // S2
        guard known.contains(SchemaRef(name: u.name, version: u.fromVersion + 1)) else { throw .invalidReference }
        guard upgrades.insert(u).inserted else { throw .conflict }
    }

    /// The versions a payload passes through when read at version `to` (S3).
    public func path(from s: SchemaRef, to: UInt32) throws(KernelError) -> [UInt32] {
        guard known.contains(SchemaRef(name: s.name, version: to)) else { throw .unknownSchema }
        guard to >= s.version else { throw .invalidArgument }
        for v in s.version..<to where !upgrades.contains(UpgradeStep(name: s.name, fromVersion: v)) {
            throw .unknownSchema
        }
        return Array(s.version...to)
    }

    /// The highest offered version the receiver accepts (S4).
    public func negotiate(name: String, offered: [UInt32]) throws(KernelError) -> UInt32 {
        guard let best = offered.filter({ accepts(SchemaRef(name: name, version: $0)) }).max() else { throw .unknownSchema }
        return best
    }

    /// Drops a known version unless a stored payload would become unacceptable (S5, S6).
    public mutating func retire(_ s: SchemaRef, stored: [SchemaRef]) throws(KernelError) {
        guard known.remove(s) != nil else { throw .notFound }
        if !stored.allSatisfy(accepts) {
            known.insert(s)
            throw .conflict
        }
    }
}
