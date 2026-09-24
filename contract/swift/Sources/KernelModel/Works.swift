/// K9 Work ownership (contract/spec/K9-work-ownership.md).
public enum WorkState: String, Codable, Sendable {
    case running = "WORK_STATE_RUNNING"
    case completed = "WORK_STATE_COMPLETED"
    case failed = "WORK_STATE_FAILED"
    case cancelled = "WORK_STATE_CANCELLED"
}

public struct Work: Equatable, Sendable {
    public let workId: String
    public var ownerId = ""
    public var generation: UInt32 = 0
    public var state = WorkState.running
    public var progress: UInt32 = 0
    public var checkpoint = ""
}

public struct Works: Sendable {
    private var items: [String: Work] = [:]

    public init() {}

    private func running(_ id: String, generation: UInt32) throws(KernelError) -> Work {
        guard let work = items[id] else { throw .notFound }                                   // W6
        guard work.state == .running, work.generation == generation else { throw .conflict }  // W2
        return work
    }

    /// Begins the next generation; returns it and the checkpoint to resume from (W1).
    public mutating func start(_ id: String, owner: String) throws(KernelError) -> (generation: UInt32, resumeFrom: String) {
        guard !id.isEmpty, !owner.isEmpty else { throw .invalidArgument }
        var work = items[id] ?? Work(workId: id)
        if items[id] != nil, work.state == .running { throw .conflict }
        work.ownerId = owner
        work.generation += 1
        work.state = .running
        work.progress = 0
        items[id] = work
        return (work.generation, work.checkpoint)
    }

    public mutating func checkpoint(_ id: String, generation: UInt32, progress: UInt32, checkpoint: String = "") throws(KernelError) {
        var work = try running(id, generation: generation)
        guard progress >= work.progress else { throw .conflict }                               // W3
        work.progress = progress
        if !checkpoint.isEmpty { work.checkpoint = checkpoint }
        items[id] = work
    }

    public mutating func finish(_ id: String, generation: UInt32, failed: Bool = false) throws(KernelError) {
        var work = try running(id, generation: generation)
        work.state = failed ? .failed : .completed
        items[id] = work
    }

    public mutating func cancel(_ id: String) throws(KernelError) {
        var work = try running(id, generation: items[id]?.generation ?? 0)                     // W4
        work.state = .cancelled
        items[id] = work
    }

    /// Cancels the owner's running work; finished work stays (W5).
    public mutating func closeOwner(_ owner: String) {
        for (id, work) in items where work.ownerId == owner && work.state == .running {
            items[id]?.state = .cancelled
        }
    }

    public func work(_ id: String) throws(KernelError) -> Work {
        guard let work = items[id] else { throw .notFound }
        return work
    }
}
