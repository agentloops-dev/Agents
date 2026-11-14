import Foundation
import Testing
@testable import AgentsKit
@testable import AgentsKitCore

@Suite("Machine sessions")
struct MachineSessionsTests {
    private func agent(_ state: AgentState, at offset: Double) -> Agent {
        Agent(runtimeID: "claude", cwd: URL(fileURLWithPath: "/tmp/work"), state: state,
              lastActivityAt: Date(timeIntervalSince1970: offset))
    }

    @Test func runningIsWorkingAndArchivedIsLeftOut() {
        let counts = MachineSessions([agent(.running, at: 1), agent(.starting, at: 2), agent(.queued, at: 3),
                                      agent(.waitingOnUser, at: 4), agent(.archived, at: 5)])
        #expect(counts.running == 2)
        #expect(counts.total == 4)
        #expect(counts.agents(.working).map(\.lastActivityAt.timeIntervalSince1970) == [2, 1])
        #expect(counts.agents(.waiting).count == 2)
    }

    @Test func theLineAndTheHeaderAgree() {
        #expect(MachineSessions([]).runningLine == "Idle")
        let one = MachineSessions([agent(.running, at: 1)])
        #expect(one.runningLine == "1 running")
        #expect(one.header == "1 session · 1 running")
    }
}
