// The rows of a machine page's Sessions groups (#5490): five of a group, then "Show N more".
export const sessionLimit = 5;

export function shownSessions<T>(list: readonly T[], all: boolean): readonly T[] {
  return all ? list : list.slice(0, sessionLimit);
}

/** "Show 3 more", for a group longer than the limit. */
export function showMoreWords(total: number): string {
  return `Show ${total - sessionLimit} more`;
}

/** "No sessions on machine-29." */
export function noSessionsWords(name: string): string {
  return `No sessions on ${name}.`;
}
