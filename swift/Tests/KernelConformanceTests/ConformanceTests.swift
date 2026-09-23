import Foundation
import Testing
import KernelModel

/// Runs the shared, language-neutral vectors in Contract/vectors.
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
            var log = ChangeLog(knownSchemas: vector.given.schemas)
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
}

// MARK: - Vector format (see Docs/Platform.md, Kernel Contract)

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
    let schemas: [SchemaRef]
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
