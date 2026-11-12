/** Reading an SPDX license expression as written in package.json (`MIT`, `(MIT OR Apache-2.0)`, ...). */

export type Expr =
  | { readonly op: "id"; readonly id: string }
  | { readonly op: "or" | "and"; readonly args: readonly Expr[] };

const TOKEN = /\(|\)|[A-Za-z0-9.+_:-]+/g;

/** Parse an expression; undefined when it is not one (for example "SEE LICENSE IN LICENSE.md"). */
export function parseExpr(text: string): Expr | undefined {
  const tokens = text.match(TOKEN) ?? [];
  let pos = 0;
  const atom = (): Expr | undefined => {
    const t = tokens[pos++];
    if (t === undefined || t === ")") return undefined;
    if (t === "(") {
      const inner = orExpr();
      if (tokens[pos++] !== ")") return undefined;
      return inner;
    }
    if (/^(and|or)$/i.test(t)) return undefined;
    // "GPL-2.0 WITH Classpath-exception-2.0": the exception is part of the identifier.
    if (/^with$/i.test(tokens[pos] ?? "")) {
      const exception = tokens[pos + 1];
      pos += 2;
      return exception === undefined ? undefined : { op: "id", id: `${t} WITH ${exception}` };
    }
    return { op: "id", id: t };
  };
  const chain = (op: "and" | "or", next: () => Expr | undefined): Expr | undefined => {
    const first = next();
    if (first === undefined) return undefined;
    const args = [first];
    while (new RegExp(`^${op}$`, "i").test(tokens[pos] ?? "")) {
      pos++;
      const more = next();
      if (more === undefined) return undefined;
      args.push(more);
    }
    return args.length === 1 ? first : { op, args };
  };
  const andExpr = (): Expr | undefined => chain("and", atom);
  const orExpr = (): Expr | undefined => chain("or", andExpr);
  const out = orExpr();
  return out !== undefined && pos === tokens.length ? out : undefined;
}

export interface Verdict {
  /** Every alternative that applies is allowed. */
  readonly ok: boolean;
  /** The identifiers that apply: for OR the first allowed alternative in the order written, for AND all of them. */
  readonly applies: readonly string[];
}

/** Which identifiers apply, and whether all of them are allowed (`allowed` returns the canonical spelling or undefined). */
export function decide(expr: Expr, allowed: (id: string) => string | undefined): Verdict {
  if (expr.op === "id") {
    const canonical = allowed(expr.id);
    return { ok: canonical !== undefined, applies: [canonical ?? expr.id] };
  }
  const parts = expr.args.map((arg) => decide(arg, allowed));
  if (expr.op === "and")
    return { ok: parts.every((p) => p.ok), applies: parts.flatMap((p) => p.applies) };
  const first = parts.find((p) => p.ok);
  return first ?? { ok: false, applies: parts.flatMap((p) => p.applies) };
}

/** The expression written back as text. */
export const show = (expr: Expr): string =>
  expr.op === "id" ? expr.id : `(${expr.args.map(show).join(` ${expr.op.toUpperCase()} `)})`;
