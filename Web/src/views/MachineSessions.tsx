// A machine page's Sessions section (#5490, specs/machine-sessions), as the window's: every session on the machine in
// Working, Waiting and Done, newest activity first, from the same count as the sidebar's "N running" (#5488). A stopped
// or offline machine's list is the last one it sent, so it says when (#5139).
import { useState } from "preact/hooks";
import type { Store } from "../model/store";
import type { Agent } from "../protocol/generated";
import { machineSessionGroups, machineSessions, machineSessionTitles, sessionsHeader, type MachineSessionGroup } from "../model/machineSessions";
import { shownSessions, sessionLimit, showMoreWords, noSessionsWords } from "../model/machineSessionRows";
import { fromWireDate } from "../protocol/dates";
import { shortAgo } from "../model/activity";
import { go } from "../route";
import { LastUpdate } from "./LastUpdate";
import { SidebarMark } from "./SessionRow";

export function MachineSessionsSection({ store, host, name, online }: { store: Store; host: string; name: string; online: boolean }) {
  const counts = machineSessions(store.agents.value[host] ?? []);
  const [more, setMore] = useState<Partial<Record<MachineSessionGroup, boolean>>>({});
  const runtimes = store.runtimes.value[host] ?? [];
  const runtimeName = (id: string) => runtimes.find((r) => r.runtime.id === id)?.runtime.name ?? id;
  const readAt = online ? undefined : (store.downSince.value[host] ?? store.copyAt.value);
  const open = (agent: Agent) => go({ host, folder: agent.worktree?.folder || agent.cwd, session: agent.id });
  return (
    <section class="machine-sessions" aria-labelledby="machine-sessions">
      <h3 class="section-head" id="machine-sessions">Sessions</h3>
      <p class="quiet small">{sessionsHeader(counts)}{readAt !== undefined && <> · <LastUpdate at={readAt} /></>}</p>
      {counts.total === 0 && <p class="hint">{noSessionsWords(name)}</p>}
      {machineSessionGroups.map((group) => {
        const list = counts.groups[group];
        if (list.length === 0) return null;
        const shown = shownSessions(list, more[group] === true);
        return (
          <div key={group} class="machine-session-group">
            <h4 class="quiet small">{machineSessionTitles[group]} ({list.length})</h4>
            <ul>
              {shown.map((agent) => (
                <li key={agent.id}>
                  <button type="button" class="sub-app-row machine-session" onClick={() => open(agent)}>
                    <SidebarMark agent={agent} />
                    <span class="machine-session-title">{agent.title?.trim() || "New session"}</span>
                    <span class="quiet small">{runtimeName(agent.runtimeID)}</span>
                    {agent.worktree?.branch && <span class="quiet small machine-session-branch">{agent.worktree.branch}</span>}
                    <span class="quiet small">{shortAgo(fromWireDate(agent.lastActivityAt))}</span>
                  </button>
                </li>
              ))}
            </ul>
            {list.length > sessionLimit && more[group] !== true && (
              <p><button type="button" class="link small" onClick={() => setMore({ ...more, [group]: true })}>{showMoreWords(list.length)}</button></p>
            )}
          </div>
        );
      })}
    </section>
  );
}
