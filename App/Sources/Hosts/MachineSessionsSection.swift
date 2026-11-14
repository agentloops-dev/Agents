import AgentsKitCore
import SwiftUI

/// A machine page's Sessions section (#5489, specs/machine-sessions), first on the page: every
/// non-archived session on the machine, in Working, Waiting and Done, newest activity first.
/// The sidebar's "N running" reads the same `MachineSessions`, so the two never disagree.
/// An offline machine's list is the last one it sent, headed "Last update <time ago>" (#5139).
struct MachineSessionsSection: View {
    @Environment(AppModel.self) private var model
    let host: MachineRef
    /// A group longer than this shows "Show N more".
    static let shown = 5
    @State private var expanded: Set<MachineSessions.Group> = []

    var body: some View {
        let sessions = model.machineSessions(host)
        Section {
            if sessions.total == 0 {
                Text("No sessions on \(model.hosts.label(host)).").foregroundStyle(Paper.secondary)
            }
            ForEach(MachineSessions.Group.allCases, id: \.self) { group in
                let agents = sessions.agents(group)
                if !agents.isEmpty {
                    Text(group.rawValue).appText(.fine).foregroundStyle(Paper.secondary)
                    let more = expanded.contains(group) ? 0 : max(0, agents.count - Self.shown)
                    ForEach(agents.prefix(agents.count - more)) { agent in
                        SessionLine(agent: agent) { model.openAgent(agent.id) }
                    }
                    if more > 0 {
                        Button("Show \(more) more") { expanded.insert(group) }
                    }
                }
            }
        } header: {
            HStack {
                Text("Sessions")
                Spacer()
                Text(sessions.header).monospacedDigit()
            }
        } footer: {
            if model.hosts.isOffline(host), let since = model.hosts.offlineSince[host] {
                LastUpdateText(read: since)
            }
        }
    }
}

private struct SessionLine: View {
    let agent: Agent
    let open: () -> Void

    var body: some View {
        Button(action: open) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Circle().fill(Self.tint(agent)).frame(width: 7, height: 7)
                    .accessibilityHidden(true)
                Text(agent.title ?? "Untitled").lineLimit(1)
                Text(RuntimeCatalog.runtime(id: agent.runtimeID)?.name ?? agent.runtimeID)
                    .appText(.fine).foregroundStyle(Paper.secondary)
                Spacer(minLength: 8)
                if let branch = agent.worktree?.branch {
                    Text(branch).appText(.fine).foregroundStyle(Paper.secondary).lineLimit(1)
                }
                Text(agent.lastActivityAt.formatted(.relative(presentation: .named)))
                    .appText(.fine).foregroundStyle(Paper.secondary).monospacedDigit()
            }
        }
        .buttonStyle(.plain)
    }

    private static func tint(_ agent: Agent) -> Color {
        switch MachineSessions.Group(agent.group()) {
        case .working: Paper.accent
        case .waiting: Color.orange
        default: Color.secondary
        }
    }
}
