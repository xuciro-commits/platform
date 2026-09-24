/// K5 Authority and sync (Contract/spec/K5-authority.md).
public enum AuthorityKind: String, Codable, Sendable {
    case unspecified = "AUTHORITY_KIND_UNSPECIFIED"
    case device = "AUTHORITY_KIND_DEVICE"
    case tenantServer = "AUTHORITY_KIND_TENANT_SERVER"
    case external = "AUTHORITY_KIND_EXTERNAL"
}

public struct AuthorityDeclaration: Hashable, Codable, Sendable {
    public var tenantId = ""
    public var dataClass = ""
    public var kind = AuthorityKind.unspecified
    public var authorityId = ""
    public var epoch: UInt32 = 0

    public init() {}

    // Proto3 JSON omits default values.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tenantId = try c.decodeIfPresent(String.self, forKey: .tenantId) ?? ""
        dataClass = try c.decodeIfPresent(String.self, forKey: .dataClass) ?? ""
        kind = try c.decodeIfPresent(AuthorityKind.self, forKey: .kind) ?? .unspecified
        authorityId = try c.decodeIfPresent(String.self, forKey: .authorityId) ?? ""
        epoch = try c.decodeIfPresent(UInt32.self, forKey: .epoch) ?? 0
    }
}

public enum SubmissionState: String, Codable, Sendable {
    case unspecified = "SUBMISSION_STATE_UNSPECIFIED"
    case pending = "SUBMISSION_STATE_PENDING"
    case sending = "SUBMISSION_STATE_SENDING"
    case confirmed = "SUBMISSION_STATE_CONFIRMED"
    case conflict = "SUBMISSION_STATE_CONFLICT"
    case rejected = "SUBMISSION_STATE_REJECTED"
    case unknown = "SUBMISSION_STATE_UNKNOWN"
}

public enum OutboxEvent: String, Sendable {
    case send, confirm, conflict, reject, timeout, retry

    /// The allowed from → to states (A5).
    var transitions: [SubmissionState: SubmissionState] {
        switch self {
        case .send: [.pending: .sending]
        case .confirm: [.sending: .confirmed]
        case .conflict: [.sending: .conflict]
        case .reject: [.sending: .rejected]
        case .timeout: [.sending: .unknown]
        case .retry: [.unknown: .sending]
        }
    }
}

/// Declarations, the receiver's check, and the outbox of one edge.
public struct Authorities: Sendable {
    private struct Key: Hashable { let tenant, name: String }
    private let edge: String
    private var current: [Key: AuthorityDeclaration] = [:]                         // (tenant, data class)
    private var outbox: [Key: (submission: Submission, state: SubmissionState)] = [:] // (tenant, idempotency key)

    public init(edge: String) {
        self.edge = edge
    }

    public mutating func declare(_ d: AuthorityDeclaration) throws(KernelError) {
        guard ![d.tenantId, d.dataClass, d.authorityId].contains(where: \.isEmpty), d.kind != .unspecified else {
            throw .invalidArgument                                                                 // A1
        }
        let key = Key(tenant: d.tenantId, name: d.dataClass)
        guard d.epoch == (current[key]?.epoch ?? 0) + 1 else { throw .conflict }                   // A2
        current[key] = d
    }

    private func declaration(for s: Submission) throws(KernelError) -> AuthorityDeclaration {
        guard let d = current[Key(tenant: s.tenantId, name: s.target.type)] else { throw .notFound }
        return d
    }

    /// The receiver's check before K4 accepts a submission (A3).
    public func authorize(_ s: Submission) throws(KernelError) {
        guard try declaration(for: s).authorityId == s.authority else { throw .notAuthority }
    }

    /// Puts a submission in this edge's outbox (A4, A6).
    public mutating func enqueue(_ s: Submission) throws(KernelError) -> SubmissionState {
        let d = try declaration(for: s)
        let key = Key(tenant: s.tenantId, name: s.idempotencyKey)
        if let existing = outbox[key] {
            guard existing.submission == s else { throw .idempotencyConflict }
            return existing.state
        }
        let state: SubmissionState = d.authorityId == edge ? .confirmed : .pending
        outbox[key] = (s, state)
        return state
    }

    public mutating func transition(tenant: String, idempotencyKey: String, event: String) throws(KernelError) -> SubmissionState {
        let key = Key(tenant: tenant, name: idempotencyKey)
        guard let entry = outbox[key] else { throw .notFound }
        guard let next = OutboxEvent(rawValue: event)?.transitions[entry.state] else { throw .invalidArgument } // A5
        outbox[key]?.state = next
        return next
    }
}
