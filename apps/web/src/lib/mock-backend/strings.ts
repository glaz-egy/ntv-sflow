/**
 * Code-unit string ordering. Used instead of localeCompare so that sort
 * order is locale-independent and identical to Go's byte-wise comparison
 * for the ASCII identifiers used here (D-039).
 */
export function compareStrings(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}
