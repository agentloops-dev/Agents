// Console ▸ Report Policy (#1804): the organisation's budget a month, which draws a line and a
// forecast on Reports ▸ Spend. Reports has no group lens, so no group leads (#5278).
//
// Edits not saved are kept (#2072): a person joining reloads the policy, not the edits;
// the edits stay while the admin opens another page and comes back; and closing the tab asks first,
// from any page (#2202), while the Console's list marks Report Policy "Not saved".
import { useEffect } from "preact/hooks";
import { signal, useSignal } from "@preact/signals";
import type { Store } from "../model/store";
import type { ReportPolicy } from "../protocol/generated";
import { budgetInput, budgetValue, failedWords, notSaved, orgKey, policyFoot } from "../model/console";
import { MessageBanner } from "./MessageKinds";

/** The edits not saved yet, of a store's control plane: kept while the page is closed. */
interface Draft {
  store: Store;
  policy: ReportPolicy;
  budgets: Record<string, string>;
}
const draft = signal<Draft | null>(null);

/** Closing the tab with edits not saved asks first, whichever page is open (#2202). */
function askBeforeClosing(e: BeforeUnloadEvent) {
  if (!draft.value) return;
  e.preventDefault();
  e.returnValue = "";
}
addEventListener("beforeunload", askBeforeClosing);

/** Whether the store's control plane has Report Policy edits not saved, for the Console's list (#2202). */
export function policyNotSaved(store: Store): boolean {
  return draft.value?.store === store;
}

export function ReportPolicyPage({ store, down }: { store: Store; down: boolean }) {
  const loaded = useSignal<ReportPolicy | null>(null);
  const loadedBudgets = useSignal<Record<string, string>>({});
  const problem = useSignal<string | null>(null);
  const saved = useSignal(false);
  const busy = useSignal(false);
  const edits = draft.value?.store === store ? draft.value : null;
  const policy = edits?.policy ?? loaded.value;
  const budgets = edits?.budgets ?? loadedBudgets.value;
  const load = async () => {
    try {
      const p = await store.link.call("reports/policy", {});
      loaded.value = p;
      loadedBudgets.value = Object.fromEntries(Object.entries(p.budgetsUSD).map(([k, v]) => [k, budgetInput(v)]));
      problem.value = null;
    } catch (error) {
      problem.value = failedWords(error);
    }
  };
  const changes = store.peopleChanges.value;
  useEffect(() => { if (!down) void load(); }, [down, changes]);
  const shown = policy;
  const edit = (next: Partial<Omit<Draft, "store">>) => {
    if (!shown) return;
    draft.value = { store, policy: next.policy ?? shown, budgets: next.budgets ?? budgets };
    saved.value = false;
  };
  const setBudget = (key: string, text: string) => edit({ budgets: { ...budgets, [key]: text } });
  const save = async () => {
    if (!shown) return;
    const budgetsUSD: Record<string, number> = {};
    for (const [key, text] of Object.entries(budgets)) {
      const usd = budgetValue(text);
      if (usd === null) { problem.value = `“${text}” is not an amount of dollars.`; return; }
      if (usd > 0) budgetsUSD[key] = usd;
    }
    busy.value = true;
    try {
      const p = await store.link.call("reports/setPolicy", { ...shown, budgetsUSD });
      loaded.value = p;
      loadedBudgets.value = Object.fromEntries(Object.entries(p.budgetsUSD).map(([k, v]) => [k, budgetInput(v)]));
      draft.value = null;
      problem.value = null;
      saved.value = true;
    } catch (error) {
      problem.value = failedWords(error);
    } finally {
      busy.value = false;
    }
  };
  const budgetField = (key: string, label: string) => (
    <label class="setting">
      <span>{label}</span>
      <span class="budget-field">$ <input type="text" inputMode="decimal" placeholder="None" aria-label={`${label}, US dollars a month`}
        value={budgets[key] ?? ""} onInput={(e) => setBudget(key, (e.currentTarget as HTMLInputElement).value)} /> a month</span>
    </label>
  );
  return (
    <form class="report-policy" onSubmit={(e) => { e.preventDefault(); void save(); }}>
      {problem.value && <MessageBanner tone="failure">{problem.value}</MessageBanner>}
      {!shown && !problem.value && <p class="hint">Loading…</p>}
      {shown && (
        <>
          <section aria-labelledby="policy-org">
            <h3 class="section-head" id="policy-org">Organisation</h3>
            {budgetField(orgKey, "Budget")}
          </section>
          <div class="sheet-actions">
            {edits ? <span class="quiet small attention" role="status">{notSaved}</span>
              : saved.value && <span class="quiet small" role="status">Saved.</span>}
            {edits && <button type="button" disabled={busy.value} onClick={() => { draft.value = null; }}>Revert</button>}
            <button type="submit" class="prominent" disabled={down || busy.value}>Save</button>
          </div>
          <p class="hint">{policyFoot}</p>
        </>
      )}
    </form>
  );
}
