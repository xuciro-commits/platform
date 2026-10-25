/** Bulk retirement freezes one intent before it can run: the reviewed list,
 * the composite key and the edits are one object, so a submit and any retry
 * send exactly the same batch under exactly the same key. The host accepts at
 * most 64 edits in one composite; a larger batch is refused up front instead
 * of being truncated. */
export const retireLimit = 64;

export type RetireItem = { kind: { type: string }; record?: { id: string } | undefined };

/** The batch that will actually be submitted: every selected, retireable item. */
export const retirementBatch = <T extends RetireItem>(items: readonly T[], picked: ReadonlySet<string>, keyOf: (item: T) => string): T[] =>
  items.filter(item => picked.has(keyOf(item)) && !!item.record);

export const retirementEdits = (items: readonly RetireItem[]): { schema: string; target: { type: string; id: string }; payload: Record<string, never> }[] =>
  items.map(item => ({ schema: `${item.kind.type}.archive`, target: { type: item.kind.type, id: item.record!.id }, payload: {} }));

export function retirementIntent<T extends RetireItem>(key: string, items: readonly T[]) {
  const reviewed = structuredClone([...items]);
  return { key, items: reviewed, edits: retirementEdits(reviewed) };
}
