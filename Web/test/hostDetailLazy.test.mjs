// A refresh reads a machine's own settings and page reads only when a page shows it (#5291, epic #5281).
import { test } from "node:test";
import assert from "node:assert/strict";
import { load } from "./load.mjs";

const { Store } = await load("test/kept-entry.ts");

const row = (id) => ({ id, name: id, platform: id === "mac" ? "macOS 26.1" : "linux", version: "1", state: "online", reach: "direct" });
const own = ["runtimes/accounts", "modes/remembered", "sandbox/state", "runtimes/accountProfiles",
  "resources/snapshot", "achievements/state", "multiplier/state"];

function fakeLink() {
  const asked = [];
  return { asked, onNotification: () => {}, onState: () => {},
    call: async (method, _params, host) => {
      asked.push({ method, host });
      if (method === "machines/list") return [];
      if (method === "agents/list") return [];
      if (method === "folders/list" || method === "pins/list") return [];
      throw new Error(`no ${method}`);
    } };
}

const askedOf = (link, host) => link.asked.filter((a) => a.host === host).map((a) => a.method);

test("a machine no page shows is not asked for its own settings or page reads", async () => {
  const link = fakeLink();
  const store = new Store(link);
  store.takeHosts([row("mac"), row("other")]);
  await store.loadHost("other");
  await new Promise((r) => setTimeout(r, 20));
  const methods = askedOf(link, "other");
  for (const method of own) assert.ok(!methods.includes(method), `${method} was asked of a machine no page shows`);
  assert.ok(methods.includes("runtimes/list"), "the lists across machines still load");
});

test("a machine a page shows takes them once, and again on each reload", async () => {
  const link = fakeLink();
  const store = new Store(link);
  store.takeHosts([row("mac"), row("other")]);
  await store.loadRuntimes("other");
  await store.loadRuntimes("other");
  await new Promise((r) => setTimeout(r, 20));
  const once = askedOf(link, "other");
  for (const method of own) assert.equal(once.filter((m) => m === method).length, 1 + (method.includes("/") && ["runtimes/accounts", "modes/remembered", "sandbox/state", "runtimes/accountProfiles"].includes(method) ? 1 : 0), method);
  link.asked.length = 0;
  await store.loadHost("other");
  await new Promise((r) => setTimeout(r, 20));
  const again = askedOf(link, "other");
  for (const method of own) assert.ok(again.includes(method), `${method} is read again for a shown machine`);
  store.forgetHost("other");
  assert.ok(!store.detailed.has("other"));
});

test("a machine no page shows takes fewer calls on a refresh than one a page shows", async () => {
  const link = fakeLink();
  const store = new Store(link);
  store.takeHosts([row("mac"), row("a"), row("b")]);
  await store.loadRuntimes("b");
  link.asked.length = 0;
  await store.loadHost("a");
  await store.loadHost("b");
  await new Promise((r) => setTimeout(r, 20));
  const quiet = askedOf(link, "a").length, shown = askedOf(link, "b").length;
  console.log(`web: ${quiet} calls on a refresh for a machine no page shows, ${shown} for one a page shows`);
  assert.equal(shown - quiet, own.length);
});
