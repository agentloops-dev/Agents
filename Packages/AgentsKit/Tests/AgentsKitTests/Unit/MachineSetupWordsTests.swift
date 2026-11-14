import Foundation
import Testing
@testable import AgentsKitCore

/// A machine's set-up as steps (#5357): the chip, the sidebar's grey line and each step's line.
@Suite("A machine's set-up, in words")
struct MachineSetupWordsTests {
    private let start = Date(timeIntervalSince1970: 1_000_000)
    private let ids = ["create", "join", "install", "compare", "runtimes", "projects", "signIn", "smoke"]

    private func setup(done: Int, running: Bool = true, failed: Bool = false, readyAfter: TimeInterval? = nil) -> DaemonAPI.MachineSetup {
        let steps = ids.enumerated().map { index, id -> DaemonAPI.MachineSetupStep in
            if index < done { return .init(id: id, state: .done, startedAt: start, endedAt: start.addingTimeInterval(10)) }
            if index == done, failed { return .init(id: id, state: .failed, startedAt: start, error: "No quota") }
            if index == done, running { return .init(id: id, state: .running, startedAt: start) }
            return .init(id: id, state: .waiting)
        }
        return .init(steps: steps, startedAt: start, readyAt: readyAfter.map { start.addingTimeInterval($0) })
    }

    @Test func theChipSaysWhereTheRunIs() {
        #expect(MachineSetupWords.chip(setup(done: 6)) == "Setting up · 7 of 8")
        #expect(MachineSetupWords.chip(setup(done: 8, readyAfter: 222)) == "Ready · 3 min 42 s")
        #expect(MachineSetupWords.chip(setup(done: 2, failed: true)) == "Set-up failed · step 3 of 8")
    }

    @Test func theSidebarLineNamesTheStep() {
        #expect(MachineSetupWords.sidebarLine(setup(done: 6)) == "Setting up · 7 of 8 · Sign in")
        #expect(MachineSetupWords.sidebarLine(setup(done: 2, failed: true)) == "Set-up failed · Install Agents")
    }

    @Test func aReadyMachineIsNotSettingUp() {
        #expect(MachineSetupWords.isSettingUp(setup(done: 7)))
        #expect(!MachineSetupWords.isSettingUp(setup(done: 8, readyAfter: 5)))
        #expect(!MachineSetupWords.isSettingUp(nil))
        var row = DaemonAPI.ControlMachine(id: MachineRef(rawValue: "dev"), name: "dev", platform: "linux", version: "1",
                                           state: "online", reach: "direct", machineID: nil)
        #expect(MachineNeed.most(row: row, offline: false, runtimes: []) == nil)
        row.setup = setup(done: 3)
        #expect(MachineNeed.most(row: row, offline: false, runtimes: []) == .settingUp(setup(done: 3)))
    }

    @Test func aStepSaysWhatItDoes() {
        let running = DaemonAPI.MachineSetupStep(id: "install", state: .running, detail: "Agents: 62 of 140 MB", slower: true)
        #expect(MachineSetupWords.line(running) == "Agents: 62 of 140 MB · Slower than usual")
        let failed = DaemonAPI.MachineSetupStep(id: "create", state: .failed, error: "Size not in region")
        #expect(MachineSetupWords.line(failed) == "Size not in region")
        #expect(MachineSetupWords.line(.init(id: "smoke", state: .waiting)) == "Not yet")
    }

    @Test func timesAreShort() {
        #expect(MachineSetupWords.duration(42) == "42 s")
        #expect(MachineSetupWords.duration(120) == "2 min")
        #expect(MachineSetupWords.duration(3900) == "1 h 5 min")
    }

    @Test func stepsFoldAfterADay() {
        let ready = setup(done: 8, readyAfter: 222)
        #expect(MachineSetupWords.foldedLine(ready, now: start.addingTimeInterval(3600)) == nil)
        #expect(MachineSetupWords.foldedLine(ready, now: start.addingTimeInterval(222 + 86400)) == "Set up in 3 min 42 s")
    }

    @Test func theSetupDecodes() throws {
        let json = #"{"id":"d","name":"d","platform":"linux","version":"1","state":"online","reach":"direct","setup":{"steps":[{"id":"create","state":"needs-you"}],"startedAt":"2026-11-14T10:00:00Z"}}"#
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let row = try decoder.decode(DaemonAPI.ControlMachine.self, from: Data(json.utf8))
        #expect(row.setup?.steps.first?.state == .needsYou)
    }
}
