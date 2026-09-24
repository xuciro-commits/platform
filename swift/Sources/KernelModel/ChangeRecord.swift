import Foundation

/// K4 Change record (Contract/spec/K4-change-record.md).
public struct SchemaRef: Hashable, Codable, Sendable {
    public var name: String
    public var version: UInt32

    public init(name: String, version: UInt32) {
        self.name = name
        self.version = version
    }
}

public struct Submission: Hashable, Codable, Sendable {
    public var tenantId = ""
    public var principalId = ""
    public var authority = ""
    public var target = EntityRef(type: "", id: "")
    public var schema = SchemaRef(name: "", version: 0)
    public var validTime: Date?
    public var causationId = ""
    public var correlationId = ""
    public var idempotencyKey = ""
    public var payload = Data()

    public init() {}

    // Proto3 JSON omits default values.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tenantId = try c.decodeIfPresent(String.self, forKey: .tenantId) ?? ""
        principalId = try c.decodeIfPresent(String.self, forKey: .principalId) ?? ""
        authority = try c.decodeIfPresent(String.self, forKey: .authority) ?? ""
        target = try c.decodeIfPresent(EntityRef.self, forKey: .target) ?? target
        schema = try c.decodeIfPresent(SchemaRef.self, forKey: .schema) ?? schema
        validTime = try c.decodeIfPresent(Date.self, forKey: .validTime)
        causationId = try c.decodeIfPresent(String.self, forKey: .causationId) ?? ""
        correlationId = try c.decodeIfPresent(String.self, forKey: .correlationId) ?? ""
        idempotencyKey = try c.decodeIfPresent(String.self, forKey: .idempotencyKey) ?? ""
        payload = try c.decodeIfPresent(Data.self, forKey: .payload) ?? Data()
    }
}

public struct ChangeRecord: Equatable, Sendable {
    public let changeId: String
    public let submission: Submission
    public let validTime: Date
    public let recordedTime: Date
}

/// Accepts submissions for one authority; one append-only log per tenant.
public struct ChangeLog: Sendable {
    private let schemas: SchemaRegistry
    private var logs: [String: [ChangeRecord]] = [:]
    private var byKey: [String: [String: ChangeRecord]] = [:]   // tenant → key → record

    public init(schemas: SchemaRegistry) {
        self.schemas = schemas
    }

    public func records(tenant: String) -> [ChangeRecord] {
        logs[tenant] ?? []
    }

    public mutating func submit(_ s: Submission, at now: Date) throws(KernelError) -> ChangeRecord {
        let required = [s.tenantId, s.principalId, s.authority, s.idempotencyKey, s.target.type, s.target.id, s.schema.name]
        guard !required.contains(where: \.isEmpty) else { throw .invalidArgument }       // C1
        guard schemas.accepts(s.schema) else { throw .unknownSchema }              // C2
        if let existing = byKey[s.tenantId]?[s.idempotencyKey] {
            guard existing.submission == s else { throw .idempotencyConflict }           // C5
            return existing                                                              // C4
        }
        let log = logs[s.tenantId] ?? []
        if !s.causationId.isEmpty, !log.contains(where: { $0.changeId == s.causationId }) {
            throw .invalidReference                                                      // C3
        }
        let recorded = max(now, log.last?.recordedTime ?? now)                          // C6
        let record = ChangeRecord(changeId: UUID().uuidString, submission: s,
                                  validTime: s.validTime ?? recorded, recordedTime: recorded) // C7
        logs[s.tenantId, default: []].append(record)
        byKey[s.tenantId, default: [:]][s.idempotencyKey] = record
        return record
    }
}
