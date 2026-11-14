/** Join class names, leaving out what is false or empty. */
export const cx = (...parts: readonly (string | false | null | undefined)[]): string =>
  parts.filter(Boolean).join(" ");
