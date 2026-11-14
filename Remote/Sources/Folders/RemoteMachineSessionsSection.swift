import AgentsKitCore
import SwiftUI

/// The Remote's Sessions section on a machine's page (#5491, specs/machine-sessions), first on
/// the page, in the Mac's words: `N sessions · M running`, then Working, Waiting and Done, newest
/// activity first, a group of more than 5 with "Show N more". The Remote has no machine rows
/// with a count in its sidebar (tier mac+web, #2798). An offline machine's list is the last it
/// sent, headed "Last update <time ago>" (#5139).
struct RemoteMachineSessionsSection: View {
    @Environment(RemoteModel.self) private var model
    let host: MachineRef
    private static let shown = 5
    @State private var expanded: Set<MachineSessions.Group> = []

    var body: some View {
        let sessions = MachineSessions(model.work.agents.filter { $0.host == host })
        Section {
            if model.machineOffline(host), let read = model.lastReadAt {
                LastUpdateText(read: read)
            }
            if sessions.total == 0 {
                Text("No sessions on \(model.machineName(host)).").foregroundStyle(Paper.secondary)
            }
            ForEach(MachineSessions.Group.allCases, id: \.self) { group in
                let agents = sessions.agents(group)
                if !agents.isEmpty {
                    Text(group.rawValue).appText(.fine).foregroundStyle(Paper.secondary)
                    let more = expanded.contains(group) ? 0 : max(0, agents.count - Self.shown)
                    ForEach(agents.prefix(agents.count - more)) { agent in
                        Button { model.open(agent.id) } label: { line(agent) }
                            .buttonStyle(.plain)
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
        }
    }

    private func line(_ agent: Agent) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Circle().fill(Self.tint(agent)).frame(width: 7, height: 7).accessibilityHidden(true)
            Text(agent.title ?? "Untitled").lineLimit(1)
            Text(RuntimeCatalog.runtime(id: agent.runtimeID)?.name ?? agent.runtimeID)
                .appText(.fine).foregroundStyle(Paper.secondary).lineLimit(1)
            Spacer(minLength: 8)
            Text(agent.lastActivityAt.formatted(.relative(presentation: .named)))
                .appText(.fine).foregroundStyle(Paper.secondary).monospacedDigit()
        }
        .contentShape(Rectangle())
    }

    private static func tint(_ agent: Agent) -> Color {
        switch MachineSessions.Group(agent.group()) {
        case .working: Paper.accent
        case .waiting: .orange
        default: .secondary
        }
    }
}
