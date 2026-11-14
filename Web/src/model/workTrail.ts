// A Work page's trail (#5452), as WorkTrail has it on the window and the Remote:
// Features › Agent loops › Epic #5281 › #5291 for an issue in a feature's epic, Issues › #12 for one in none.
import type { LineageNode } from "../protocol/generated";
import { type WorkKind, shortRef, workKindTitle } from "./work";
import { featureTitle } from "./workFeatures";

/** Where a part opens: a Work list, Work ▸ Features with a feature open, or an item's page (the epic). */
export type TrailTarget =
  | { kind: "list"; section: WorkKind | "features" }
  | { kind: "feature"; name: string }
  | { kind: "item"; section: WorkKind; ref: string };

/** A part of the trail; the last, the page itself, has no target. */
export type TrailPart = { words: string; target?: TrailTarget };

/** The trail of an item's page. feature and parent are the item's, from its read: an issue with a feature starts at
 * Features, else at its kind's list; an issue with a parent has it before itself, as Epic #n when the issue has a
 * feature (an epic's children take its feature: label, #3271). */
export function workTrail(kind: WorkKind, ref: string, feature?: string, parent?: LineageNode): TrailPart[] {
  const parts: TrailPart[] = [];
  const f = feature || undefined;
  if (kind === "issue" && f) {
    parts.push({ words: "Features", target: { kind: "list", section: "features" } });
    parts.push({ words: featureTitle(f), target: { kind: "feature", name: f } });
  } else {
    parts.push({ words: workKindTitle[kind], target: { kind: "list", section: kind } });
  }
  if (kind === "issue" && parent?.ref && parent.ref !== ref) {
    const short = shortRef(parent.ref);
    parts.push({ words: f ? `Epic ${short}` : short, target: { kind: "item", section: "issue", ref: parent.ref } });
  }
  parts.push({ words: shortRef(ref) });
  return parts;
}

/** The tip of a part: what it opens. */
export function trailHelp(part: TrailPart): string | undefined {
  const t = part.target;
  if (!t) return undefined;
  if (t.kind === "list") return `All ${t.section === "features" ? "features" : workKindTitle[t.section].toLowerCase()}`;
  if (t.kind === "feature") return `${part.words} in Features`;
  return `The page of ${part.words}`;
}
