/**
 * Contrast of the colors in the token file of the web UI (ADR-0045).
 *
 * WEB040 a pair of tokens (text on a background, a border on a background) is below the contrast it must have in
 *        the light or in the dark theme, or a token named in the pair list does not exist
 *
 * The token file writes colors as oklch(); the conversion to sRGB and the WCAG 2.x contrast ratio are computed here
 * with no dependency. A color outside the sRGB gamut is clipped per channel, which is what a browser shows or
 * very nearly (a browser maps the chroma instead; the difference is far below what a margin of 0.05 hides).
 */
import { readText, type Violation, violation } from "../common";

export interface Rgb {
  readonly r: number;
  readonly g: number;
  readonly b: number;
}

/** One pair the page relies on: `on` is drawn over `background`. */
export interface ContrastPair {
  readonly on: string;
  readonly background: string;
  readonly min: number;
  readonly note?: string;
}

const OKLCH = /^oklch\(\s*([\d.]+%?)\s+([\d.]+)\s+([\d.]+)(?:deg)?\s*(?:\/\s*([\d.]+%?))?\s*\)$/;

/** Parse `oklch(L C H [/ A])` (L as a number or a percentage). Undefined for any other notation. */
export function parseOklch(
  value: string,
): { l: number; c: number; h: number; a: number } | undefined {
  const m = OKLCH.exec(value.trim());
  if (!m) return undefined;
  const l = (m[1] as string).endsWith("%") ? Number.parseFloat(m[1] as string) / 100 : Number(m[1]);
  const alpha =
    m[4] === undefined ? 1 : m[4].endsWith("%") ? Number.parseFloat(m[4]) / 100 : Number(m[4]);
  return { l, c: Number(m[2]), h: Number(m[3]), a: alpha };
}

const clamp01 = (x: number): number => Math.min(1, Math.max(0, x));
const gamma = (x: number): number => (x <= 0.0031308 ? 12.92 * x : 1.055 * x ** (1 / 2.4) - 0.055);

/** OKLCH to sRGB, each channel 0..1 (gamma encoded, clipped to the gamut). */
export function oklchToSrgb(l: number, c: number, h: number): Rgb {
  const hr = (h * Math.PI) / 180;
  const a = c * Math.cos(hr);
  const b = c * Math.sin(hr);
  const l_ = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m_ = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s_ = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const r = 4.0767416621 * l_ - 3.3077115913 * m_ + 0.2309699292 * s_;
  const g = -1.2684380046 * l_ + 2.6097574011 * m_ - 0.3413193965 * s_;
  const bl = -0.0041960863 * l_ - 0.7034186147 * m_ + 1.707614701 * s_;
  return { r: gamma(clamp01(r)), g: gamma(clamp01(g)), b: gamma(clamp01(bl)) };
}

const linear = (x: number): number => (x <= 0.04045 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4);

/** Relative luminance of an sRGB color (WCAG 2.x). */
export function luminance(color: Rgb): number {
  return 0.2126 * linear(color.r) + 0.7152 * linear(color.g) + 0.0722 * linear(color.b);
}

/** The WCAG contrast ratio of two colors, 1 to 21. */
export function contrastRatio(a: Rgb, b: Rgb): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

export type Theme = "light" | "dark";
export type TokenTable = Readonly<Record<string, string>>;

const DECLARATION = /(--px-[\w-]+)\s*:\s*([^;]+);/g;

/** The `--px-*` values of each theme. The dark theme is the light one with the values of the dark block laid over it. */
export function readThemes(css: string): Record<Theme, TokenTable> {
  const plain = css.replace(/\/\*[\s\S]*?\*\//g, "");
  const dark = /@media\s*\(prefers-color-scheme:\s*dark\)\s*\{([\s\S]*)\}\s*$/.exec(plain);
  const lightPart = dark ? plain.slice(0, dark.index) : plain;
  const light: Record<string, string> = {};
  for (const m of lightPart.matchAll(DECLARATION)) light[m[1] as string] = (m[2] as string).trim();
  const over: Record<string, string> = { ...light };
  for (const m of (dark?.[1] ?? "").matchAll(DECLARATION))
    over[m[1] as string] = (m[2] as string).trim();
  return { light, dark: over };
}

/** The sRGB color of a token, or undefined when it is missing or not an opaque oklch(). */
export function tokenColor(table: TokenTable, name: string): Rgb | undefined {
  const parsed = parseOklch(table[`--px-${name}`] ?? "");
  if (!parsed || parsed.a < 1) return undefined;
  return oklchToSrgb(parsed.l, parsed.c, parsed.h);
}

/** Every pair, in both themes: the ratio, and why the pair fails when it does. */
export function contrastResults(
  css: string,
  pairs: readonly ContrastPair[],
): { theme: Theme; pair: ContrastPair; ratio: number | undefined }[] {
  const themes = readThemes(css);
  const out: { theme: Theme; pair: ContrastPair; ratio: number | undefined }[] = [];
  for (const theme of ["light", "dark"] as const) {
    for (const pair of pairs) {
      const on = tokenColor(themes[theme], pair.on);
      const background = tokenColor(themes[theme], pair.background);
      out.push({
        theme,
        pair,
        ratio: on && background ? contrastRatio(on, background) : undefined,
      });
    }
  }
  return out;
}

export function contrastViolations(
  root: string,
  tokenFile: string,
  pairs: readonly ContrastPair[],
): Violation[] {
  const css = readText(`${root}/${tokenFile}`);
  const out: Violation[] = [];
  for (const { theme, pair, ratio } of contrastResults(css, pairs)) {
    const label = `${pair.on} on ${pair.background} (${theme})`;
    if (ratio === undefined) {
      out.push(
        violation(
          "WEB040",
          tokenFile,
          0,
          `${label}: a token of the pair is missing or is not an opaque oklch() color`,
          "Name both colors of the pair as --px-<name> in the token file, or correct the pair list in harness/config/web.yaml.",
        ),
      );
    } else if (ratio < pair.min) {
      out.push(
        violation(
          "WEB040",
          tokenFile,
          0,
          `${label}: contrast ${ratio.toFixed(2)}:1 is below ${pair.min}:1${pair.note ? ` (${pair.note})` : ""}`,
          `Change the lightness of --px-${pair.on} in the ${theme} theme until the ratio is ${pair.min}:1 or more (WCAG 2.2 AA: 4.5:1 for text, 3:1 for the edge of a control). Do not lower the minimum.`,
        ),
      );
    }
  }
  return out;
}
