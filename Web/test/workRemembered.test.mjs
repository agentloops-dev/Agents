// Each Work page opens with the filters, sort and range it had when last left (#5451), as WorkRememberedTests has it
// on the window and the Remote.
import { test } from "node:test";
import assert from "node:assert/strict";
import { load } from "./load.mjs";

class Memory {
  items = new Map();
  getItem(key) { return this.items.has(key) ? this.items.get(key) : null; }
  setItem(key, value) { this.items.set(key, String(value)); }
  removeItem(key) { this.items.delete(key); }
}

test("a list keeps its filters and sort, and the next visit opens with them", async () => {
  globalThis.localStorage = new Memory();
  try {
    const first = await load("src/model/workLists.ts");
    const l = first.workList("issue");
    assert.equal(l.filter.value.label, "", "nothing kept at first");
    l.filter.value = { ...l.filter.value, label: "bug", state: "closed", project: "agents" };
    l.sort.value = "newest";
    assert.equal(localStorage.getItem("agents.work.filters.issue"), "project=agents&state=closed&label=bug&sort=newest");
    assert.equal(localStorage.getItem("agents.work.filters.pr"), null, "each page its own");

    const again = (await load("src/model/workLists.ts")).workList("issue");
    assert.equal(again.filter.value.label, "bug");
    assert.equal(again.filter.value.state, "closed");
    assert.equal(again.filter.value.project, "agents");
    assert.equal(again.sort.value, "newest");

    // Clear and the defaults: nothing is kept, so the next visit opens on the defaults.
    again.filter.value = { ...again.filter.value, label: "", project: "", state: "open" };
    again.sort.value = "updated";
    assert.equal(localStorage.getItem("agents.work.filters.issue"), null);
  } finally {
    delete globalThis.localStorage;
  }
});

test("a sort or state a page does not have is the default", async () => {
  globalThis.localStorage = new Memory();
  try {
    localStorage.setItem("agents.work.filters.release", "state=merged&sort=updated&label=x");
    const l = (await load("src/model/workLists.ts")).workList("release");
    assert.equal(l.filter.value.state, "open");
    assert.equal(l.sort.value, "newest");
    assert.equal(l.filter.value.label, "x");
  } finally {
    delete globalThis.localStorage;
  }
});

test("Features and Timeline choices as words, each left out at its default", async () => {
  const r = await load("src/model/workRemembered.ts");
  const words = r.choiceWords({ range: "week", project: "p1", board: "" }, { range: "twoDays", project: "", board: "" });
  assert.equal(words, "range=week&project=p1");
  assert.equal(r.choiceFrom(words, "range", "twoDays", ["day", "twoDays", "week"]), "week");
  assert.equal(r.choiceFrom("range=year", "range", "twoDays", ["day", "twoDays", "week"]), "twoDays");
  assert.equal(r.choiceFrom(undefined, "board", ""), "");
  assert.equal(r.choiceFrom(words, "project", ""), "p1");
});

test("no storage, or a storage that refuses, loses only the memory", async () => {
  const r = await load("src/model/workRemembered.ts");
  assert.equal(r.remembered("issue", undefined), undefined);
  const refusing = { getItem() { throw new Error("no"); }, setItem() { throw new Error("full"); }, removeItem() {} };
  assert.equal(r.remembered("issue", refusing), undefined);
  assert.doesNotThrow(() => r.remember("issue", "label=bug", refusing));
});
