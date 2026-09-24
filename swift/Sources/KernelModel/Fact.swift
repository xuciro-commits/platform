import Foundation

/// K2 Fact kinds and K3 Provenance (Contract/spec/K2-K3-facts.md).
public enum FactKind: String, Codable, Sendable {
    case unspecified = "FACT_KIND_UNSPECIFIED"
    case observation = "FACT_KIND_OBSERVATION"
    case claim = "FACT_KIND_CLAIM"
    case derived = "FACT_KIND_DERIVED"
}

public struct Provenance: Hashable, Codable, Sendable {
    public var principalId: String?
    public var connectorId: String?
    public var sourceTime: Date?
    public var confidence: Double?

    public init(principalId: String? = nil, connectorId: String? = nil, sourceTime: Date? = nil, confidence: Double? = nil) {
        self.principalId = principalId
        self.connectorId = connectorId
        self.sourceTime = sourceTime
        self.confidence = confidence
    }

    /// The single source, or nil when there is none or two (P1).
    public var source: String? {
        switch (principalId, connectorId) {
        case (let id?, nil) where !id.isEmpty: "principal:\(id)"
        case (nil, let id?) where !id.isEmpty: "connector:\(id)"
        default: nil
        }
    }
}

public struct Fact: Hashable, Codable, Sendable {
    public var tenantId = ""
    public var kind = FactKind.unspecified
    public var subject = EntityRef(type: "", id: "")
    public var attribute = ""
    public var schema = SchemaRef(name: "", version: 0)
    public var provenance = Provenance()
    public var derivedFrom: [String] = []
    public var idempotencyKey = ""
    public var payload = Data()

    public init() {}

    // Proto3 JSON omits default values.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tenantId = try c.decodeIfPresent(String.self, forKey: .tenantId) ?? ""
        kind = try c.decodeIfPresent(FactKind.self, forKey: .kind) ?? .unspecified
        subject = try c.decodeIfPresent(EntityRef.self, forKey: .subject) ?? subject
        attribute = try c.decodeIfPresent(String.self, forKey: .attribute) ?? ""
        schema = try c.decodeIfPresent(SchemaRef.self, forKey: .schema) ?? schema
        provenance = try c.decodeIfPresent(Provenance.self, forKey: .provenance) ?? provenance
        derivedFrom = try c.decodeIfPresent([String].self, forKey: .derivedFrom) ?? []
        idempotencyKey = try c.decodeIfPresent(String.self, forKey: .idempotencyKey) ?? ""
        payload = try c.decodeIfPresent(Data.self, forKey: .payload) ?? Data()
    }
}

public struct FactRecord: Equatable, Sendable {
    public let factId: String
    public let fact: Fact
    public let recordedTime: Date
}

/// Accepts facts; one append-only fact log per tenant.
public struct FactLog: Sendable {
    private let knownSchemas: Set<SchemaRef>
    private var logs: [String: [FactRecord]] = [:]
    private var byKey: [String: [String: FactRecord]] = [:]   // tenant → key → record

    public init(knownSchemas: some Sequence<SchemaRef>) {
        self.knownSchemas = Set(knownSchemas)
    }

    public func records(tenant: String) -> [FactRecord] {
        logs[tenant] ?? []
    }

    public mutating func record(_ f: Fact, at now: Date) throws(KernelError) -> FactRecord {
        let required = [f.tenantId, f.subject.type, f.subject.id, f.attribute, f.schema.name, f.idempotencyKey]
        guard !required.contains(where: \.isEmpty), f.kind != .unspecified else { throw .invalidArgument } // F1
        guard f.provenance.source != nil, f.provenance.sourceTime != nil else { throw .invalidArgument }  // P1
        let confidence = f.provenance.confidence ?? 0
        guard f.kind == .claim ? (confidence > 0 && confidence <= 1) : confidence == 0 else { throw .invalidArgument } // P2
        guard (f.kind == .derived) == !f.derivedFrom.isEmpty else { throw .invalidArgument }          // F6
        guard knownSchemas.contains(f.schema) else { throw .unknownSchema }                           // F2
        if let existing = byKey[f.tenantId]?[f.idempotencyKey] {
            guard existing.fact == f else { throw .idempotencyConflict }                              // F3
            return existing
        }
        let log = logs[f.tenantId] ?? []
        for input in f.derivedFrom where !log.contains(where: { $0.factId == input }) {
            throw .invalidReference                                                                   // F7
        }
        let record = FactRecord(factId: UUID().uuidString, fact: f,
                                recordedTime: max(now, log.last?.recordedTime ?? now))                // P3
        logs[f.tenantId, default: []].append(record)                                                  // F4
        byKey[f.tenantId, default: [:]][f.idempotencyKey] = record
        return record
    }

    /// Per source, its latest claim about subject and attribute, in recorded order (F5).
    public func currentClaims(tenant: String, subject: EntityRef, attribute: String) -> [FactRecord] {
        var current: [String: FactRecord] = [:]
        for r in records(tenant: tenant)
        where r.fact.kind == .claim && r.fact.subject == subject && r.fact.attribute == attribute {
            let source = r.fact.provenance.source ?? ""
            if let old = current[source], old.fact.provenance.sourceTime! > r.fact.provenance.sourceTime! { continue }
            current[source] = r
        }
        let ids = Set(current.values.map(\.factId))
        return records(tenant: tenant).filter { ids.contains($0.factId) }
    }
}
