import Foundation

/// K6 Tenancy and policy (contract/spec/K6-tenancy-policy.md).
public struct Caller: Hashable, Codable, Sendable {
    public var tenantId: String
    public var principalId: String

    public init(tenantId: String, principalId: String) {
        self.tenantId = tenantId
        self.principalId = principalId
    }
}

/// Applies the kernel's receiving order (T2) for one authority.
public struct Receiver: Sendable {
    public var changes: ChangeLog
    public var authorities: Authorities
    /// Supplied by the domain (T3).
    public var policy: @Sendable (Caller, Submission) -> Bool

    public init(changes: ChangeLog, authorities: Authorities, policy: @escaping @Sendable (Caller, Submission) -> Bool) {
        self.changes = changes
        self.authorities = authorities
        self.policy = policy
    }

    /// Accepts or rejects `s`; `domain` runs the domain's rules (K4 C10).
    public mutating func receive(_ caller: Caller, _ s: Submission, at now: Date,
                                 domain: (() throws(KernelError) -> Void)? = nil) throws(KernelError) -> ChangeRecord {
        guard s.tenantId == caller.tenantId, s.principalId == caller.principalId else { throw .policyDenied } // T1
        let authorities = authorities, policy = policy
        return try changes.submit(s, at: now) { () throws(KernelError) in
            try authorities.authorize(s)                                   // A3
            guard policy(caller, s) else { throw .policyDenied }           // T3
            try domain?()
        }
    }
}
