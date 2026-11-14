// The one count of a machine's sessions (#5488): the sidebar line and the page read it.
import { test } from "node:test";
import assert from "node:assert/strict";
import { load } from "./load.mjs";

const m = await load("src/model/machineSessions.ts");
const a = (id, state, at, extra = {}) => ({ id, state, lastActivityAt: at, ...extra });
const list = [
  a("1", "running", "2026-11-14T10:00:00Z"),
  a("2", "starting", "2026-11-14T11:00:00Z"),
  a("3", "queued", "2026-11-14T09:00:00Z"),
  a("4", "waitingOnUser", "2026-11-14T08:00:00Z"),
  a("5", "finished", "2026-11-14T07:00:00Z", { report: { outcome: "done" } }),
  a("6", "archived", "2026-11-14T12:00:00Z"),
];

test("machine sessions: running is Working, archived is left out", () => {
  const c = m.machineSessions(list);
  assert.equal(c.running, 2);
  assert.equal(c.total, 5);
  assert.deepEqual(c.groups.working.map((x) => x.id), ["2", "1"]);
  assert.deepEqual(c.groups.waiting.map((x) => x.id), ["3", "4"]);
  assert.deepEqual(c.groups.done.map((x) => x.id), ["5"]);
});

test("machine sessions: the line and the header agree", () => {
  assert.equal(m.runningLineOf(m.machineSessions(list)), "2 running");
  assert.equal(m.sessionsHeader(m.machineSessions(list)), "5 sessions · 2 running");
  assert.equal(m.runningLineOf(m.machineSessions([])), "Idle");
  assert.equal(m.sessionsHeader(m.machineSessions([a("1", "running", "2026-11-14T10:00:00Z")])), "1 session · 1 running");
});
