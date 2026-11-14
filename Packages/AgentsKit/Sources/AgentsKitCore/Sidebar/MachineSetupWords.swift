import Foundation

/// A machine's set-up in words (#5357, epic #5353): the header chip, the sidebar's grey line and
/// each step's line, from the control plane's record of the eight steps.
public enum MachineSetupWords {
    /// The steps' names, as the page lists them, by the wire's id.
    public static let names: [String: String] = [
        "create": "Create machine", "join": "Join machine", "install": "Install Agents",
        "compare": "Compare digits", "runtimes": "Install runtimes", "projects": "Send projects",
        "signIn": "Sign in to runtimes", "smoke": "Smoke session",
    ]

    public static func name(_ step: DaemonAPI.MachineSetupStep) -> String { names[step.id] ?? step.id }

    /// How many steps are done.
    public static func doneCount(_ setup: DaemonAPI.MachineSetup) -> Int {
        setup.steps.filter { $0.state == .done }.count
    }

    /// Whether the run is over and well: every step done. A machine is ready then, and `New Session` turns on.
    public static func isReady(_ setup: DaemonAPI.MachineSetup) -> Bool {
        setup.readyAt != nil || (!setup.steps.isEmpty && setup.steps.allSatisfy { $0.state == .done })
    }

    /// The step that stopped the run, if one failed.
    public static func failedStep(_ setup: DaemonAPI.MachineSetup) -> DaemonAPI.MachineSetupStep? {
        setup.steps.first { $0.state == .failed }
    }

    /// Whether the machine still sets itself up or stopped on a failure; false once it is ready.
    public static func isSettingUp(_ setup: DaemonAPI.MachineSetup?) -> Bool {
        guard let setup else { return false }
        return !isReady(setup)
    }

    /// 7 of 8: the step that is on, which is the number of the first step not done.
    public static func position(_ setup: DaemonAPI.MachineSetup) -> Int {
        min(doneCount(setup) + 1, max(setup.steps.count, 1))
    }

    /// The step the run is at: the failed one, else the one that needs the person, else the first running.
    public static func current(_ setup: DaemonAPI.MachineSetup) -> DaemonAPI.MachineSetupStep? {
        failedStep(setup)
            ?? setup.steps.first { $0.state == .needsYou }
            ?? setup.steps.first { $0.state == .running }
    }

    /// The chip in the page's header: *Setting up · 7 of 8*, *Ready · 3 min 42 s*, or *Set-up failed · 6 of 8*.
    public static func chip(_ setup: DaemonAPI.MachineSetup, now: Date = Date()) -> String {
        let total = setup.steps.count
        if isReady(setup) {
            let end = setup.readyAt ?? setup.steps.compactMap(\.endedAt).max() ?? now
            return "Ready · \(duration(end.timeIntervalSince(setup.startedAt)))"
        }
        if let failed = failedStep(setup) {
            let at = (setup.steps.firstIndex { $0.id == failed.id } ?? 0) + 1
            return "Set-up failed · step \(at) of \(total)"
        }
        return "Setting up · \(position(setup)) of \(total)"
    }

    /// The sidebar row's grey line: *Setting up · 7 of 8 · Sign in*; *Set-up failed · Install Agents*.
    public static func sidebarLine(_ setup: DaemonAPI.MachineSetup) -> String {
        if let failed = failedStep(setup) { return "Set-up failed · \(name(failed))" }
        var line = "Setting up · \(position(setup)) of \(setup.steps.count)"
        if let step = current(setup) ?? setup.steps.first(where: { $0.state == .waiting }) {
            line += " · \(shortName(step))"
        }
        return line
    }

    /// A step's name in the sidebar's one line, where room is short.
    static func shortName(_ step: DaemonAPI.MachineSetupStep) -> String {
        step.id == "signIn" ? "Sign in" : name(step)
    }

    /// The one line under a step's name: what it is doing, or why it failed, or why it waits.
    public static func line(_ step: DaemonAPI.MachineSetupStep) -> String {
        switch step.state {
        case .failed:
            return step.error ?? step.detail ?? "Failed"
        case .waiting:
            return step.detail ?? "Not yet"
        case .needsYou:
            return step.detail ?? "Waiting for you"
        case .running:
            let words = step.detail ?? "Working…"
            return step.slower == true ? "\(words) · Slower than usual" : words
        case .done:
            let words = step.detail ?? "Done"
            return step.fromPool == true ? "\(words) · done in the pool" : words
        }
    }

    /// The time a step took (or has taken); nil before it began.
    public static func time(_ step: DaemonAPI.MachineSetupStep, now: Date = Date()) -> String? {
        guard let start = step.startedAt else { return nil }
        return duration((step.endedAt ?? now).timeIntervalSince(start))
    }

    /// *42 s*, *3 min 42 s*, *1 h 5 min*.
    public static func duration(_ seconds: TimeInterval) -> String {
        let total = max(0, Int(seconds.rounded()))
        if total < 60 { return "\(total) s" }
        if total < 3600 {
            let rest = total % 60
            return rest == 0 ? "\(total / 60) min" : "\(total / 60) min \(rest) s"
        }
        let minutes = (total % 3600) / 60
        return minutes == 0 ? "\(total / 3600) h" : "\(total / 3600) h \(minutes) min"
    }

    /// After 24 hours the steps fold into one line: *Set up in 3 min 42 s*; nil before that or while not ready.
    public static func foldedLine(_ setup: DaemonAPI.MachineSetup, now: Date = Date()) -> String? {
        guard isReady(setup), let ready = setup.readyAt, now.timeIntervalSince(ready) >= 24 * 3600 else { return nil }
        return "Set up in \(duration(ready.timeIntervalSince(setup.startedAt)))"
    }

    /// Whether the steps have folded into one line (#5357): ready for 24 hours or more.
    public static func foldedAway(_ setup: DaemonAPI.MachineSetup, now: Date = Date()) -> Bool {
        foldedLine(setup, now: now) != nil
    }

    /// The notice's words when it is ready.
    public static func readyNotice(name: String) -> String { "\(name) is ready" }
}
