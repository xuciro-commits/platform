/** Retain an answer while only its metadata scope changes. Credentials,
 * tenants, paths and inventory bounds must never inherit another read. */
export function readPlaceholder<T>(key: readonly unknown[], previous: T | undefined, previousKey?: readonly unknown[]): T | undefined {
  return previousKey?.length === key.length && key.slice(0, -1).every((part, index) => part === previousKey[index]) ? previous : undefined;
}
