/// K1 Identity (Contract/spec/K1-identity.md).
public struct EntityRef: Hashable, Codable, Sendable {
    public var type: String
    public var id: String

    public init(type: String, id: String) {
        self.type = type
        self.id = id
    }
}

public enum RedirectKind: String, Codable, Sendable {
    case unspecified = "REDIRECT_KIND_UNSPECIFIED"
    case merge = "REDIRECT_KIND_MERGE"
    case split = "REDIRECT_KIND_SPLIT"
}

public struct Redirect: Codable, Sendable {
    public var from: EntityRef
    public var to: [EntityRef]
    public var kind: RedirectKind

    public init(from: EntityRef, to: [EntityRef], kind: RedirectKind) {
        self.from = from
        self.to = to
        self.kind = kind
    }

    // Proto3 JSON omits default values.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        from = try c.decode(EntityRef.self, forKey: .from)
        to = try c.decodeIfPresent([EntityRef].self, forKey: .to) ?? []
        kind = try c.decodeIfPresent(RedirectKind.self, forKey: .kind) ?? .unspecified
    }
}

public enum Resolution: Equatable, Sendable {
    case resolved(EntityRef)
    case ambiguous([EntityRef])
}

public struct IdentityRegistry: Sendable {
    private var entities: Set<EntityRef>
    private var redirects: [EntityRef: Redirect] = [:]

    public init(entities: some Sequence<EntityRef>) {
        self.entities = Set(entities)
    }

    public mutating func add(_ redirect: Redirect) throws(KernelError) {
        let arityOK = switch redirect.kind {
        case .merge: redirect.to.count == 1
        case .split: redirect.to.count >= 2
        case .unspecified: false
        }
        guard arityOK else { throw .invalidArgument }                                   // I3
        guard entities.contains(redirect.from) else { throw .notFound }                 // I4
        guard redirect.to.allSatisfy(entities.contains) else { throw .invalidReference } // I5
        guard redirects[redirect.from] == nil else { throw .conflict }                  // I6
        guard !redirect.to.contains(where: { reaches(redirect.from, from: $0) }) else {
            throw .redirectCycle                                                         // I7
        }
        redirects[redirect.from] = redirect
    }

    public func resolve(_ ref: EntityRef) throws(KernelError) -> Resolution {
        guard entities.contains(ref) else { throw .notFound }                           // I2
        var terminals: [EntityRef] = []
        func walk(_ current: EntityRef) {
            if let redirect = redirects[current] {
                redirect.to.forEach(walk)
            } else if !terminals.contains(current) {
                terminals.append(current)
            }
        }
        walk(ref)
        return terminals.count == 1 ? .resolved(terminals[0]) : .ambiguous(terminals)   // I1, I8
    }

    private func reaches(_ goal: EntityRef, from start: EntityRef) -> Bool {
        if start == goal { return true }
        return redirects[start]?.to.contains { reaches(goal, from: $0) } ?? false
    }
}
