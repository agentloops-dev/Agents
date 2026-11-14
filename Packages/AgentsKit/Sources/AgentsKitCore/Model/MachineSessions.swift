import Foundation

/// One count of a machine's sessions (#5488, specs/machine-sessions), as `machineSessions.ts`:
/// the sidebar line ("N running") and the machine's page ("N sessions · M running", the groups)
/// both read it, so the two never disagree. A stopped or offline machine's list is the last one
/// it sent, so it counts as last known.
public struct MachineSessions: Sendable {
    public enum Group: String, CaseIterable, Sendable {
        case working = "Working", waiting = "Waiting", done = "Done"

        public init?(_ group: AgentGroup) {
            switch group {
            case .running: self = .working
            case .needsAttention, .blocked, .waiting: self = .waiting
            case .finished, .stopped: self = .done
            case .archived: return nil
            }
        }
    }

    /// Every non-archived session, newest activity first, by group.
    public let groups: [Group: [Agent]]

    public init(_ agents: [Agent]) {
        var groups: [Group: [Agent]] = [:]
        for agent in agents {
            if let group = Group(agent.group()) { groups[group, default: []].append(agent) }
        }
        self.groups = groups.mapValues { $0.sorted { $0.lastActivityAt > $1.lastActivityAt } }
    }

    public func agents(_ group: Group) -> [Agent] { groups[group] ?? [] }
    public var total: Int { groups.values.reduce(0) { $0 + $1.count } }
    /// Sessions with status Working.
    public var running: Int { agents(.working).count }

    /// The sidebar's line under an online machine: "3 running", or "Idle".
    public var runningLine: String { running > 0 ? "\(running) running" : "Idle" }
    /// The page's header: "7 sessions · 2 running".
    public var header: String { "\(total) \(total == 1 ? "session" : "sessions") · \(running) running" }
}
