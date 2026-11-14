// A Work page's trail (#5452), as WorkTrailTests has it on the window.
import { test } from "node:test";
import assert from "node:assert/strict";
import { load } from "./load.mjs";

const t = await load("src/model/workTrail.ts");
const ref = "github:alexec/Agents#5291";
const epic = { kind: "issue", ref: "github:alexec/Agents#5281" };
const words = (parts) => parts.map((p) => p.words);

test("an issue in a feature's epic: Features, the feature, the epic, itself", () => {
  const parts = t.workTrail("issue", ref, "agent loops", epic);
  assert.deepEqual(words(parts), ["Features", "Agent loops", "Epic #5281", "#5291"]);
  assert.deepEqual(parts.map((p) => p.target), [
    { kind: "list", section: "features" }, { kind: "feature", name: "agent loops" }, { kind: "item", section: "issue", ref: epic.ref }, undefined,
  ]);
  assert.equal(t.trailHelp(parts[0]), "All features");
  assert.equal(t.trailHelp(parts[1]), "Agent loops in Features");
});

test("an epic: Features, its feature, itself", () => {
  assert.deepEqual(words(t.workTrail("issue", epic.ref, "work")), ["Features", "Work", "#5281"]);
});

test("an issue in no feature: its list, a parent with no Epic word, itself", () => {
  assert.deepEqual(words(t.workTrail("issue", ref)), ["Issues", "#5291"]);
  assert.deepEqual(words(t.workTrail("issue", ref, "", epic)), ["Issues", "#5281", "#5291"]);
});

test("a pull request keeps to its list, whatever its labels say", () => {
  assert.deepEqual(words(t.workTrail("pr", "github:alexec/Agents#45", "work", epic)), ["Pull requests", "#45"]);
});
