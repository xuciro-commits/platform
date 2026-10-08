// Names are written in one language (ADR-0083 D1): "Pallet/托盘" in a title is
// flagged, "WMS 仓库" (an abbreviation beside a word) is not. The other
// languages come from Control Panel → Languages and words, never from the name.
const cjk = /[\u3400-\u9fff\u3040-\u30ff\uac00-\ud7af]/;
const latinWord = /[a-z]{2,}/; // a word, not an acronym

export const mixesLanguages = (s: string | undefined) => !!s && cjk.test(s) && latinWord.test(s);

/** The texts of an object draft that mix languages, each named for the problems dock. */
export function mixedNames(object: { title?: string; plural?: string; fields?: { title?: string }[]; states?: { title?: string }[]; actions?: { title?: string }[] }): string[] {
  const out: string[] = [];
  if (mixesLanguages(object.title)) out.push(object.title!);
  if (mixesLanguages(object.plural)) out.push(object.plural!);
  for (const x of [...(object.fields ?? []), ...(object.states ?? []), ...(object.actions ?? [])]) if (mixesLanguages(x.title)) out.push(x.title!);
  return Array.from(new Set(out));
}
