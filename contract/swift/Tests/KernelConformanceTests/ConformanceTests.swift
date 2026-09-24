import Foundation
import Testing
import KernelModel

/// Runs the shared, language-neutral vectors in contract/vectors.
@Suite("Kernel contract conformance")
struct ConformanceTests {
    @Test("K1 Identity vectors")
    func identity() throws {
        let file: VectorFile<IdentityGiven, IdentityStep> = try load("k1-identity.json")
        for vector in file.vectors {
            var registry = IdentityRegistry(entities: vector.given.entities)
            for redirect in vector.given.redirects ?? [] {
                try registry.add(redirect)
            }
            for (index, step) in vector.steps.enumerated() {
                let label = Comment(rawValue: "\(vector.id) step \(index)")
                let actual: Expect
                do {
                    if let redirect = step.addRedirect {
                        try registry.add(redirect)
                        actual = Expect(ok: true)
                    } else if let ref = step.resolve {
                        switch try registry.resolve(ref) {
                        case .resolved(let target): actual = Expect(resolved: target)
                        case .ambiguous(let refs): actual = Expect(ambiguous: refs)
                        }
                    } else {
                        Issue.record(label)
                        continue
                    }
                } catch {
                    actual = Expect(error: error.rawValue)
                }
                #expect(actual == step.expect, label)
            }
        }
    }

    @Test("K4 Change record vectors")
    func changeRecord() throws {
        let file: VectorFile<ChangeGiven, ChangeStep> = try load("k4-change-record.json")
        for vector in file.vectors {
            var log = ChangeLog(schemas: SchemaRegistry(known: vector.given.schemas))
            let facts = Set((vector.given.facts ?? []).map { [$0.tenantId, $0.factId] })
            log.facts = { tenant, id in facts.contains([tenant, id]) }
            var changeIDs: [Int: String] = [:]
            for (index, step) in vector.steps.enumerated() {
                let label = Comment(rawValue: "\(vector.id) step \(index)")
                var submission = step.submit
                if submission.causationId.hasPrefix("$step:"), let n = Int(submission.causationId.dropFirst(6)) {
                    submission.causationId = changeIDs[n] ?? ""
                }
                do {
                    let record = try log.submit(submission, at: try date(step.at))
                    changeIDs[index] = record.changeId
                    let expected = try #require(step.expect.accepted, label)
                    #expect(record.validTime == (try date(expected.validTime)), label)
                    #expect(record.recordedTime == (try date(expected.recordedTime)), label)
                    if let original = expected.sameAs {
                        #expect(record.changeId == changeIDs[original], label)
                    }
                } catch let error as KernelError {
                    #expect(step.expect.error == error.rawValue, label)
                }
            }
            for (tenant, count) in vector.expectLog ?? [:] {
                #expect(log.records(tenant: tenant).count == count, Comment(rawValue: "\(vector.id) log \(tenant)"))
            }
        }
    }

    @Test("K2 Fact kinds and K3 Provenance vectors")
    func facts() throws {
        let file: VectorFile<ChangeGiven, FactStep> = try load("k2-k3-facts.json")
        for vector in file.vectors {
            var log = FactLog(schemas: SchemaRegistry(known: vector.given.schemas))
            var factIDs: [Int: String] = [:]
            for (index, step) in vector.steps.enumerated() {
                let label = Comment(rawValue: "\(vector.id) step \(index)")
                if let query = step.claims {
                    let current = log.currentClaims(tenant: query.tenantId, subject: query.subject, attribute: query.attribute)
                    #expect(current.map(\.factId) == (step.expect.current ?? []).map { factIDs[$0] ?? "" }, label)
                    continue
                }
                var fact = try #require(step.record, label)
                fact.derivedFrom = fact.derivedFrom.map { input in
                    guard input.hasPrefix("$step:"), let n = Int(input.dropFirst(6)) else { return input }
                    return factIDs[n] ?? ""
                }
                do {
                    let record = try log.record(fact, at: try date(try #require(step.at, label)))
                    factIDs[index] = record.factId
                    let expected = try #require(step.expect.accepted, label)
                    #expect(record.recordedTime == (try date(expected.recordedTime)), label)
                    if let original = expected.sameAs {
                        #expect(record.factId == factIDs[original], label)
                    }
                } catch let error as KernelError {
                    #expect(step.expect.error == error.rawValue, label)
                }
            }
            for (tenant, count) in vector.expectLog ?? [:] {
                #expect(log.records(tenant: tenant).count == count, Comment(rawValue: "\(vector.id) log \(tenant)"))
            }
        }
    }

    @Test("K7 Schema evolution vectors")
    func schemaEvolution() throws {
        let file: VectorFile<SchemaGiven, SchemaStep> = try load("k7-schema-evolution.json")
        for vector in file.vectors {
            var registry = SchemaRegistry(known: vector.given.known, upgrades: vector.given.upgrades)
            for (index, step) in vector.steps.enumerated() {
                var actual = SchemaExpect()
                do throws(KernelError) {
                    if let s = step.accepts {
                        guard registry.accepts(s) else { throw .unknownSchema }
                        actual.ok = true
                    } else if let u = step.addUpgrade {
                        try registry.add(u)
                        actual.ok = true
                    } else if let s = step.retire {
                        try registry.retire(s, stored: vector.given.stored)
                        actual.ok = true
                    } else if let u = step.upgrade {
                        actual.path = try registry.path(from: u.from, to: u.toVersion)
                    } else if let n = step.negotiate {
                        actual.version = try registry.negotiate(name: n.name, offered: n.versions)
                    }
                } catch {
                    actual.error = error.rawValue
                }
                #expect(actual == step.expect, Comment(rawValue: "\(vector.id) step \(index)"))
            }
        }
    }

    @Test("K5 Authority and sync vectors")
    func authority() throws {
        let file: VectorFile<AuthorityGiven, AuthorityStep> = try load("k5-authority.json")
        for vector in file.vectors {
            var authorities = Authorities(edge: vector.given.edge)
            for declaration in vector.given.declarations {
                try authorities.declare(declaration)
            }
            for (index, step) in vector.steps.enumerated() {
                var actual = AuthorityExpect()
                do throws(KernelError) {
                    if let d = step.declare {
                        try authorities.declare(d)
                        actual.ok = true
                    } else if let s = step.authorize {
                        try authorities.authorize(s)
                        actual.ok = true
                    } else if let s = step.enqueue {
                        actual.state = try authorities.enqueue(s).rawValue
                    } else if let t = step.transition {
                        actual.state = try authorities.transition(tenant: t.tenantId, idempotencyKey: t.idempotencyKey, event: t.event).rawValue
                    } else if let a = step.answer {
                        actual.state = try authorities.answer(tenant: a.tenantId, idempotencyKey: a.idempotencyKey,
                                                              code: a.code.flatMap(KernelError.init(rawValue:))).rawValue
                    }
                } catch {
                    actual.error = error.rawValue
                }
                #expect(actual == step.expect, Comment(rawValue: "\(vector.id) step \(index)"))
            }
        }
    }

    @Test("K6 Receive vectors")
    func receive() throws {
        let file: VectorFile<ReceiveGiven, ReceiveStep> = try load("k6-receive.json")
        for vector in file.vectors {
            var authorities = Authorities(edge: "")
            for declaration in vector.given.declarations {
                try authorities.declare(declaration)
            }
            let allowed = Set(vector.given.policy.map { [$0.principalId, $0.action] })
            var receiver = Receiver(changes: ChangeLog(schemas: SchemaRegistry(known: vector.given.schemas)),
                                    authorities: authorities) { caller, s in allowed.contains([caller.principalId, s.schema.name]) }
            var changeIDs: [Int: String] = [:]
            for (index, step) in vector.steps.enumerated() {
                let label = Comment(rawValue: "\(vector.id) step \(index)")
                let domainError = step.domain.flatMap(KernelError.init(rawValue:))
                do {
                    let record = try receiver.receive(step.caller, step.submit, at: try date(step.at)) { () throws(KernelError) in
                        if let domainError { throw domainError }
                    }
                    changeIDs[index] = record.changeId
                    let expected = try #require(step.expect.accepted, label)
                    #expect(record.recordedTime == (try date(expected.recordedTime)), label)
                    if let original = expected.sameAs {
                        #expect(record.changeId == changeIDs[original], label)
                    }
                } catch let error as KernelError {
                    #expect(step.expect.error == error.rawValue, label)
                }
            }
            for (tenant, count) in vector.expectLog ?? [:] {
                #expect(receiver.changes.records(tenant: tenant).count == count, Comment(rawValue: "\(vector.id) log \(tenant)"))
            }
        }
    }

    @Test("K8 Connector vectors")
    func connectors() throws {
        let file: VectorFile<Empty, ConnectorStep> = try load("k8-connectors.json")
        for vector in file.vectors {
            var connectors = Connectors()
            for (index, step) in vector.steps.enumerated() {
                let label = Comment(rawValue: "\(vector.id) step \(index)")
                var actual = ConnectorExpect()
                do throws(KernelError) {
                    let at = try? date(step.at ?? "")
                    if let d = step.register {
                        try connectors.register(d)
                        actual.ok = true
                    } else if let d = step.deliver {
                        try connectors.deliver(tenant: d.tenantId, connector: d.connectorId, dataClass: d.dataClass,
                                               cursorFrom: d.cursorFrom ?? "", cursorTo: d.cursorTo ?? "", at: at!)
                        actual.ok = true
                    } else if let h = step.heartbeat {
                        try connectors.heartbeat(tenant: h.tenantId, connector: h.connectorId, at: at!)
                        actual.ok = true
                    } else if let s = step.status {
                        let status = try connectors.status(tenant: s.tenantId, connector: s.connectorId, at: at!)
                        actual.health = status.health.rawValue
                        actual.lastSeen = status.lastSeen.map { $0.formatted(.iso8601) }
                        actual.cursor = status.cursor.isEmpty ? nil : status.cursor
                    }
                } catch {
                    actual.error = error.rawValue
                }
                #expect(actual == step.expect, label)
            }
        }
    }

    @Test("K5 Migration vectors")
    func migration() throws {
        let file: VectorFile<ChangeGiven, MigrationStep> = try load("k5-migration.json")
        for vector in file.vectors {
            var log = ChangeLog(schemas: SchemaRegistry(known: vector.given.schemas))
            for (index, step) in vector.steps.enumerated() {
                let label = Comment(rawValue: "\(vector.id) step \(index)")
                do {
                    if let adopt = step.adopt {
                        try log.adopt(try adopt.map { ChangeRecord(changeId: $0.changeId, submission: $0.submission,
                                                                   validTime: try date($0.validTime), recordedTime: try date($0.recordedTime)) })
                        #expect(step.expect.ok == true, label)
                    } else if let submission = step.submit, let at = step.at {
                        let record = try log.submit(submission, at: try date(at))
                        let expected = try #require(step.expect.accepted, label)
                        #expect(expected.changeId == nil || record.changeId == expected.changeId, label)
                        #expect(record.validTime == (try date(expected.validTime)), label)
                        #expect(record.recordedTime == (try date(expected.recordedTime)), label)
                    }
                } catch let error as KernelError {
                    #expect(step.expect.error == error.rawValue, label)
                }
            }
            for (tenant, count) in vector.expectLog ?? [:] {
                #expect(log.records(tenant: tenant).count == count, Comment(rawValue: "\(vector.id) log \(tenant)"))
            }
        }
    }
}

// MARK: - Vector format (see docs/Platform.md, Kernel Contract)

struct VectorFile<Given: Decodable, Step: Decodable>: Decodable {
    let contract: String
    let vectors: [Vector<Given, Step>]
}

struct Vector<Given: Decodable, Step: Decodable>: Decodable {
    let id: String
    let given: Given
    let steps: [Step]
    let expectLog: [String: Int]?
}

struct IdentityGiven: Decodable {
    let entities: [EntityRef]
    let redirects: [Redirect]?
}

struct IdentityStep: Decodable {
    let resolve: EntityRef?
    let addRedirect: Redirect?
    let expect: Expect
}

struct Expect: Decodable, Equatable {
    var ok: Bool?
    var resolved: EntityRef?
    var ambiguous: [EntityRef]?
    var error: String?
}

struct ChangeGiven: Decodable {
    struct Fact: Decodable { let tenantId, factId: String }
    let schemas: [SchemaRef]
    let facts: [Fact]?
}

struct ChangeStep: Decodable {
    let submit: Submission
    let at: String
    let expect: ChangeExpect
}

struct ChangeExpect: Decodable {
    struct Accepted: Decodable {
        let validTime: String
        let recordedTime: String
        let sameAs: Int?
    }
    let accepted: Accepted?
    let error: String?
}

struct FactStep: Decodable {
    struct ClaimsQuery: Decodable {
        let tenantId: String
        let subject: EntityRef
        let attribute: String
    }
    struct Expect: Decodable {
        struct Accepted: Decodable {
            let recordedTime: String
            let sameAs: Int?
        }
        let accepted: Accepted?
        let current: [Int]?
        let error: String?
    }
    let record: Fact?
    let claims: ClaimsQuery?
    let at: String?
    let expect: Expect
}

struct SchemaGiven: Decodable {
    let known: [SchemaRef]
    let upgrades: [UpgradeStep]
    let stored: [SchemaRef]
}

struct SchemaStep: Decodable {
    struct Upgrade: Decodable {
        let from: SchemaRef
        let toVersion: UInt32
    }
    struct Negotiate: Decodable {
        let name: String
        let versions: [UInt32]
    }
    let accepts: SchemaRef?
    let addUpgrade: UpgradeStep?
    let retire: SchemaRef?
    let upgrade: Upgrade?
    let negotiate: Negotiate?
    let expect: SchemaExpect
}

struct SchemaExpect: Decodable, Equatable {
    var ok: Bool?
    var path: [UInt32]?
    var version: UInt32?
    var error: String?
}

struct AuthorityGiven: Decodable {
    let edge: String
    let declarations: [AuthorityDeclaration]
}

struct AuthorityStep: Decodable {
    struct Transition: Decodable {
        let tenantId: String
        let idempotencyKey: String
        let event: String
    }
    let declare: AuthorityDeclaration?
    let authorize: Submission?
    let enqueue: Submission?
    struct Answer: Decodable {
        let tenantId: String
        let idempotencyKey: String
        let code: String?
    }
    let transition: Transition?
    let answer: Answer?
    let expect: AuthorityExpect
}

struct AuthorityExpect: Decodable, Equatable {
    var ok: Bool?
    var state: String?
    var error: String?
}

struct ReceiveGiven: Decodable {
    struct Allow: Decodable { let principalId, action: String }
    let schemas: [SchemaRef]
    let declarations: [AuthorityDeclaration]
    let policy: [Allow]
}

struct ReceiveStep: Decodable {
    let caller: Caller
    let submit: Submission
    let at: String
    let domain: String?
    let expect: ChangeExpect
}

struct Empty: Decodable {}

struct ConnectorStep: Decodable {
    struct Delivery: Decodable {
        let tenantId, connectorId, dataClass: String
        let cursorFrom, cursorTo: String?
    }
    struct Ref: Decodable { let tenantId, connectorId: String }
    let register: ConnectorDescriptor?
    let deliver: Delivery?
    let heartbeat: Ref?
    let status: Ref?
    let at: String?
    let expect: ConnectorExpect
}

struct ConnectorExpect: Decodable, Equatable {
    var ok: Bool?
    var health: String?
    var lastSeen: String?
    var cursor: String?
    var error: String?
}

struct MigrationStep: Decodable {
    struct Record: Decodable {
        let changeId: String
        let submission: Submission
        let validTime, recordedTime: String
    }
    struct Expect: Decodable {
        struct Accepted: Decodable { let changeId: String?; let validTime, recordedTime: String }
        let ok: Bool?
        let accepted: Accepted?
        let error: String?
    }
    let adopt: [Record]?
    let submit: Submission?
    let at: String?
    let expect: Expect
}

private let vectorsDirectory = URL(fileURLWithPath: #filePath)
    .deletingLastPathComponent().appendingPathComponent("../../../vectors").standardizedFileURL

private func load<T: Decodable>(_ name: String) throws -> T {
    let decoder = JSONDecoder()
    decoder.dateDecodingStrategy = .iso8601
    return try decoder.decode(T.self, from: Data(contentsOf: vectorsDirectory.appendingPathComponent(name)))
}

private func date(_ text: String) throws -> Date {
    try Date(text, strategy: .iso8601)
}
