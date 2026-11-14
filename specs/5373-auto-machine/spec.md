# The Auto machine rule (#5373, epic #5372)

New Session's machine picker (#5287) has one extra row at the top, **Auto**, selected by default (Alex, 2026-11-14). This spec settles which machine Auto picks, in what order, what the person sees, and what Auto does when nothing fits. The UI says **Auto**, never "any machine".

## The rule

Auto picks **once, at start**, on the control plane, from the state it holds at that moment. The session is never moved afterwards.

### Candidates

A machine is a candidate when all hold:

1. It is **running**: `ControlMachine.state` is online and joined. Auto never wakes, starts or makes a machine (a sleeping or stopped cloud machine is not a candidate).
2. The project has a **folder** on it (#2317). Its state does not matter: a folder with uncommitted work, or on another branch, counts. The session starts in a **fresh worktree from the branch's last commit**, and the session says which commit.
3. The chosen **runtime** is installed and signed in there (`runtimes/list` on that machine).
4. The person may use it (the machine's owner or a machine they were given, as the picker already lists).

### Tiers and ranking

Candidates are split by capacity level (#4578): `ok`, then `near`, then `full`. Auto takes the best tier that has a member, then ranks inside it:

1. The machine this person **last started a session of this project on** (warm caches, same worktrees), when it is in the tier.
2. The lowest **load per core, 5-minute** (`loadPerCore5`; steadier than the 1-minute figure and less likely to flip a pick on one burst).
3. The machine's name, alphabetical, so the same state always gives the same pick.

A start is **never refused for capacity** (#4584): when no candidate is `ok` the pick comes from `near`, then from `full`, and the reason says so. A machine with no capacity figure yet (a host older than #4578, or before its first look) counts as `ok`, last among the `ok` ones.

### One machine

A project with one candidate-able machine (one machine with a folder): the picker draws no Auto row and no choice; the machine is shown by name, as today. The session shows no "Auto chose" line, because nothing was chosen. When that machine fails the candidate test, the start says why (below).

### Picker memory

Auto is selected at **every** start. A machine chosen by name starts there and is **not remembered**: the next start is Auto again. (The person's last machine still feeds rank 1, and the picker's recent list, #5287.) Choosing a machine by name is never second-guessed: capacity warns, never refuses (#4584).

### Not in this version

- A per-project "never Auto on this machine" setting. No evidence it is needed; choosing a machine by name covers it. Revisit when someone asks.
- Moving a running session, or retrying on another machine when the start fails: the start fails as any start does, and the person starts again.
- Weighing cost, price or distance.

## What the person sees

| State | Picker row | Session |
|---|---|---|
| A pick from `ok` (last used) | **Auto** | "Auto chose Mac mini: you last started here." |
| A pick from `ok` (by load) | **Auto** | "Auto chose Mac mini: least busy, load 0.4 per core." |
| A pick from `near` | **Auto** | "Auto chose Mac mini: the least busy, though near capacity (load 1.8 per core)." |
| A pick from `full` | **Auto** | "Auto chose Mac mini: the least busy, though at capacity (load 2.9 per core)." |
| Single machine | no Auto row | no line |
| No machine has the project | **Auto**, greyed | "No machine has this project. Add a folder on one." (action: Add a Folder on Another Machine) |
| No machine has the runtime ready | greyed | "No machine with this project has <runtime> installed and signed in." |
| Every machine with the project is offline | greyed | "Every machine with this project is offline: <names>." |

- The reason is one line, put in the session's first notice and kept in its details (machine, tier, the load figure, the commit). It uses the capacity sentence words from `capacity/state` where they exist.
- A greyed Auto is not selected by default: the picker falls back to the first usable machine, if any, and says why Auto is off.
- The same words on all three clients (Core). The reason strings are made by the control plane, so a client draws what it is given.

## Where it runs

The control plane decides (it holds the machines, the folders and the person's history). The client sends `machine: auto` with the start; the reply names the machine, the tier and the reason fields (`reason`: `lastUsed` | `leastLoaded`, `tier`, `loadPerCore`, `commit`). The client builds nothing.

### Findings from the check

- **#4578 gives the signal, and the control plane does not yet hold it.** `CapacityLevel` and `loadPerCore5` are on the daemon's `machine/usage` and `capacity/state`. `ControlMachine` (`machines/list`) has no capacity, no folders and no runtimes. A child must add `capacity` (level) and `loadPerCore5` to what the control plane keeps per machine, from the pushed `capacity/changed`, and say how old it is (the "Last update" rule applies if shown).
- **#5287's picker** asks `machines/list` with a query. Auto is not a machine, so it is not a result of that call: the client draws the row itself, and enables it from a **start preview** call (`sessions/previewStart`: the same inputs as a start, result: the pick or the "none fits" reason). That also gives the greyed state without the client knowing the rule. The preview is advisory; the pick at start may differ if the state moved.
- **Staleness:** the capacity levels the control plane holds are as old as the last push. A machine that stops pushing for 2 minutes counts as `ok` last, not best; the ranking never trusts a figure older than that.

## Children this implies

1. Control plane: per-machine capacity and `loadPerCore5` kept; the rule as a pure function with table tests of every row above.
2. Wire: `machine: auto` on start, `sessions/previewStart`, the reply's reason fields.
3. Picker (#5375): the Auto row, default, greyed states, the session's "Auto chose" line, on all three clients.

## Decided (Alex, 2026-11-14, and this spike)

- Auto is the default for a new session (Alex).
- One machine: no Auto row, no line.
- Auto is chosen at every start; a named machine is not remembered.
- Uncommitted work or a different branch does not exclude a machine; a fresh worktree from the last commit, which the session names.
- Only running machines; Auto never wakes or makes one.
- A `full` or `near` machine is a last resort, never a refusal.
- No per-project exclusion in this version.
