import Foundation

/// K4 Change record (contract/spec/K4-change-record.md).
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
    public var evidenceFactIds: [String] = []
    /// Precondition on the target's revision (C12); nil: no check.
    public var expectedRevision: UInt32?

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
        evidenceFactIds = try c.decodeIfPresent([String].self, forKey: .evidenceFactIds) ?? []
        expectedRevision = try c.decodeIfPresent(UInt32.self, forKey: .expectedRevision)
    }
}

public struct ChangeRecord: Equatable, Sendable {
    public let changeId: String
    public let submission: Submission
    public let validTime: Date
    public let recordedTime: Date
    /// The target's revision after this change (C12).
    public let revision: UInt32

    public init(changeId: String, submission: Submission, validTime: Date, recordedTime: Date, revision: UInt32 = 0) {
        self.changeId = changeId
        self.submission = submission
        self.validTime = validTime
        self.recordedTime = recordedTime
        self.revision = revision
    }
}

/// Accepts submissions for one authority; one append-only log per tenant.
public struct ChangeLog: Sendable {
    private let schemas: SchemaRegistry
    private var logs: [String: [ChangeRecord]] = [:]
    private var byKey: [String: [String: ChangeRecord]] = [:]   // tenant → key → record
    private var revisions: [[String]: UInt32] = [:]                 // [tenant, type, id] → revision (C12)
    /// Whether a fact is recorded in a tenant (K2); the default knows none (C11).
    public var facts: @Sendable (_ tenant: String, _ factId: String) -> Bool = { _, _ in false }

    public init(schemas: SchemaRegistry) {
        self.schemas = schemas
    }

    public func records(tenant: String) -> [ChangeRecord] {
        logs[tenant] ?? []
    }

    public mutating func submit(_ s: Submission, at now: Date) throws(KernelError) -> ChangeRecord {
        try submit(s, at: now, check: nil)
    }

    /// Runs `check` after replay detection and before the append (C10); a replay skips it.
    public mutating func submit(_ s: Submission, at now: Date, check: (() throws(KernelError) -> Void)?) throws(KernelError) -> ChangeRecord {
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
        for fact in s.evidenceFactIds where !facts(s.tenantId, fact) {
            throw .invalidReference                                                      // C11
        }
        let target = [s.tenantId, s.target.type, s.target.id]
        if let expected = s.expectedRevision, expected != revisions[target, default: 0] { throw .conflict } // C12
        try check?()                                                                     // C10
        let recorded = max(now, log.last?.recordedTime ?? now)                          // C6
        revisions[target, default: 0] += 1
        let record = ChangeRecord(changeId: UUID().uuidString, submission: s,
                                  validTime: s.validTime ?? recorded, recordedTime: recorded,  // C7
                                  revision: revisions[target, default: 0])
        logs[s.tenantId, default: []].append(record)
        byKey[s.tenantId, default: [:]][s.idempotencyKey] = record
        return record
    }

    /// Takes over a previous authority's accepted records unchanged (K5 A10): into an
    /// empty tenant log, in recorded order, with unique change IDs and keys.
    public mutating func adopt(_ records: [ChangeRecord]) throws(KernelError) {
        guard let tenant = records.first?.submission.tenantId else { return }
        guard (logs[tenant] ?? []).isEmpty,
              records.allSatisfy({ $0.submission.tenantId == tenant }),
              Set(records.map(\.changeId)).count == records.count,
              Set(records.map(\.submission.idempotencyKey)).count == records.count,
              zip(records, records.dropFirst()).allSatisfy({ $0.recordedTime <= $1.recordedTime }) else { throw .conflict }
        logs[tenant] = records
        byKey[tenant] = Dictionary(uniqueKeysWithValues: records.map { ($0.submission.idempotencyKey, $0) })
        for r in records { revisions[[tenant, r.submission.target.type, r.submission.target.id], default: 0] += 1 }
    }
}
