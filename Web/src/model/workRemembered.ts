// Each Work page opens with the filters, sort and range it had when the person last left it (#5451), kept across
// visits in localStorage, as the window and the Remote keep them in defaults (WorkRemembered.swift). One entry per page,
// in the words of the page's address (`workQuery`), so a change to a filter's words changes both together. Clear on the
// page is the way back to the defaults.
import { effect } from "@preact/signals";

/** The pages that remember: the lists by kind, then Features and Timeline. */
export type WorkPage = "issue" | "pr" | "release" | "features" | "timeline";

export const rememberedKey = (page: WorkPage) => `agents.work.filters.${page}`;

function storage(): Storage | undefined {
  try {
    return globalThis.localStorage;
  } catch {
    return undefined;
  }
}

/** What the page had when it was last left, in its address's words; undefined when it has nothing kept. */
export function remembered(page: WorkPage, from: Storage | undefined = storage()): string | undefined {
  try {
    return from?.getItem(rememberedKey(page)) ?? undefined;
  } catch {
    return undefined;
  }
}

/** Keeps what the page has now; the defaults (no words) remove the entry. */
export function remember(page: WorkPage, words: string, to: Storage | undefined = storage()): void {
  try {
    if (words) to?.setItem(rememberedKey(page), words);
    else to?.removeItem(rememberedKey(page));
  } catch {
    // A full or refused storage only loses the memory, never the page.
  }
}

/** Keeps a page's choices each time one of them changes: `words` reads the signals it is made of. */
export function rememberEach(page: WorkPage, words: () => string): () => void {
  return effect(() => { remember(page, words()); });
}

/** A page's choices as address words, each left out while it is its default. */
export function choiceWords(choices: Readonly<Record<string, string>>, defaults: Readonly<Record<string, string>>): string {
  const q = new URLSearchParams();
  for (const [name, value] of Object.entries(choices)) if (value && value !== defaults[name]) q.set(name, value);
  return q.toString();
}

/** One choice from words made by `choiceWords`; `fallback` when it is not there or not one of `allowed`. */
export function choiceFrom<T extends string>(words: string | undefined, name: string, fallback: T, allowed?: readonly T[]): T {
  const value = new URLSearchParams(words ?? "").get(name);
  if (!value) return fallback;
  return allowed && !allowed.includes(value as T) ? fallback : value as T;
}

