// The rows of a machine page's Sessions groups (#5490).
import { test } from "node:test";
import assert from "node:assert/strict";
import { load } from "./load.mjs";

const m = await load("src/model/machineSessionRows.ts");

test("a group shows five, then Show N more", () => {
  const list = [1, 2, 3, 4, 5, 6, 7];
  assert.equal(m.shownSessions(list, false).length, 5);
  assert.equal(m.shownSessions(list, true).length, 7);
  assert.equal(m.showMoreWords(7), "Show 2 more");
});

test("an empty machine says so", () => {
  assert.equal(m.noSessionsWords("machine-29"), "No sessions on machine-29.");
});
