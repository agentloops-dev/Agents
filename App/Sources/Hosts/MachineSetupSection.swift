import AgentsKitCore
import SwiftUI

/// A machine's set-up as steps on its page (#5357, epic #5353, boards 2 and 3): a mark, the
/// name, one line and the time for each of the eight steps. A failed step stops the run, says
/// why, and offers Try Again and Delete Machine…. After 24 hours the steps fold into one line.
/// Read from the control plane's row, which it pushes as the machine changes.
struct MachineSetupSection: View {
    @Environment(AppModel.self) private var model
    let host: MachineRef
    let row: DaemonAPI.ControlMachine
    let setup: DaemonAPI.MachineSetup

    @State private var deleting = false

    var body: some View {
        if let folded = MachineSetupWords.foldedLine(setup) {
            Section("Agents") { Text(folded).foregroundStyle(.secondary) }
        } else {
            // A second's tick, so a running step's time moves.
            TimelineView(.periodic(from: .now, by: 1)) { context in
                Section("Set-up") {
                    ForEach(setup.steps) { step in stepRow(step, now: context.date) }
                    if let failed = MachineSetupWords.failedStep(setup) {
                        Text(failed.error ?? "\(MachineSetupWords.name(failed)) failed.")
                            .foregroundStyle(StateTint.failure.style(or: .primary))
                        HStack {
                            Button("Try Again") { Task { await model.retrySetup(host) } }
                            Button("Delete Machine…", role: .destructive) { deleting = true }
                        }
                    }
                }
            }
            .confirmationDialog("Delete \(row.name)?", isPresented: $deleting, titleVisibility: .visible) {
                Button("Delete", role: .destructive) { Task { await model.deleteMachine(host) } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(MachineRowActions.deleteMessage(row, sessions: 0))
            }
        }
    }

    private func stepRow(_ step: DaemonAPI.MachineSetupStep, now: Date) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(icon: Self.symbol(step.state))
                .foregroundStyle(Self.tint(step.state).style(or: .secondary))
                .frame(width: 18)
                .accessibilityLabel(Self.stateWords(step.state))
            VStack(alignment: .leading, spacing: 2) {
                Text(MachineSetupWords.name(step))
                Text(MachineSetupWords.line(step))
                    .appText(.fine)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
            Spacer(minLength: 8)
            if let time = MachineSetupWords.time(step, now: now) {
                Text(time).monospacedDigit().foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private static func symbol(_ state: DaemonAPI.MachineSetupStep.State) -> String {
        switch state {
        case .done: "checkmark.circle.fill"
        case .running: "arrow.triangle.2.circlepath"
        case .needsYou: "person.crop.circle.badge.exclamationmark"
        case .failed: "xmark.octagon.fill"
        case .waiting: "circle"
        }
    }

    private static func tint(_ state: DaemonAPI.MachineSetupStep.State) -> StateTint {
        switch state {
        case .done: .vouched
        case .running: .none
        case .needsYou: .attention
        case .failed: .failure
        case .waiting: .none
        }
    }

    private static func stateWords(_ state: DaemonAPI.MachineSetupStep.State) -> String {
        switch state {
        case .done: "Done"
        case .running: "Running"
        case .needsYou: "Needs you"
        case .failed: "Failed"
        case .waiting: "Not yet"
        }
    }
}
