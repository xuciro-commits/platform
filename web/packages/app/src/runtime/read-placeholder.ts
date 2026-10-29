export function readPlaceholder<T>(key: readonly unknown[], previous: T | undefined, previousKey?: readonly unknown[]): T | undefined {
  return previousKey?.length === key.length && key.slice(0, -1).every((part, index) => part === previousKey[index]) ? previous : undefined;
}
