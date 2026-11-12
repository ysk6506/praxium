/**
 * Korean particles (조사) that depend on the preceding word: 은/는, 이/가, 을/를, 과/와, (으)로.
 * Messages name things like "crah-1" or "PDU", so Latin letters and digits are judged by how they are
 * read aloud (일, 유). When the ending cannot be judged, both forms are shown: "은(는)".
 */

type Pair = readonly [withFinal: string, withoutFinal: string];
const PAIRS: Readonly<Record<string, Pair>> = {
  은: ["은", "는"],
  는: ["은", "는"],
  이: ["이", "가"],
  가: ["이", "가"],
  을: ["을", "를"],
  를: ["을", "를"],
  과: ["과", "와"],
  와: ["과", "와"],
  으로: ["으로", "로"],
  로: ["으로", "로"],
  이다: ["이다", "다"],
  이에요: ["이에요", "예요"],
};

export type Particle = keyof typeof PAIRS;

/** Final consonant of how each digit / Latin letter is read: "b" = has one, "l" = ㄹ, "" = none. */
const DIGITS = ["b", "l", "", "b", "", "", "b", "l", "l", ""]; // 영 일 이 삼 사 오 육 칠 팔 구
/** Acronyms are read letter by letter (PDU → 피디유, html → 에이치티엠엘). */
const LETTER_NAMES: Readonly<Record<string, string>> = { l: "l", m: "b", n: "b", r: "l" };
/** Words are read as words (web → 웹, rack → 랙, panel → 패널, server → 서버, host → 호스트). */
const WORD_ENDINGS: Readonly<Record<string, string>> = {
  b: "b",
  c: "b",
  g: "b",
  k: "b",
  m: "b",
  n: "b",
  p: "b",
  l: "l",
};

/** `word` ends in a Latin letter (the caller checked), so the trailing run is never empty. */
function latinFinal(word: string): string {
  const run = (/[a-z]+$/i.exec(word) as RegExpExecArray)[0];
  const last = run.charAt(run.length - 1).toLowerCase();
  const acronym = run.length <= 2 || run === run.toUpperCase();
  return (acronym ? LETTER_NAMES[last] : WORD_ENDINGS[last]) ?? "";
}

/** Closing quotes and brackets are not read aloud: "'crah-1'" sounds like "crah-1". */
const CLOSERS = /['"’”`)\]}>»」』]+$/u;

/** "b" (final consonant), "l" (final ㄹ), "" (none), or undefined when unknown. */
function finalSound(raw: string): string | undefined {
  const word = raw.trimEnd().replace(CLOSERS, "");
  const last = word.at(-1);
  if (last === undefined) return undefined;
  const code = last.charCodeAt(0);
  if (code >= 0xac00 && code <= 0xd7a3) {
    const final = (code - 0xac00) % 28;
    return final === 0 ? "" : final === 8 ? "l" : "b";
  }
  if (/[0-9]/.test(last)) return DIGITS[Number(last)];
  if (/[a-z]/i.test(last)) return latinFinal(word);
  return undefined;
}

/** The particle that fits after `word`, e.g. josa("crah-1", "은") → "은", josa("PDU", "은") → "는". */
export function josa(word: string, particle: Particle): string {
  const [withFinal, withoutFinal] = PAIRS[particle] as Pair;
  const sound = finalSound(word);
  if (sound === undefined) return `${withFinal}(${withoutFinal})`;
  if (withFinal === "으로") return sound === "b" ? "으로" : "로";
  return sound === "" ? withoutFinal : withFinal;
}

/** `word` followed by the fitting particle, e.g. withJosa("분전반", "이") → "분전반이". */
export function withJosa(word: string, particle: Particle): string {
  return `${word}${josa(word, particle)}`;
}

/** A duration people read at a glance: "45초", "12분 30초", "1시간 5분" (seconds dropped past an hour). */
export function formatDuration(seconds: number): string {
  const total = Math.max(0, Math.round(seconds));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return m > 0 ? `${h}시간 ${m}분` : `${h}시간`;
  if (m > 0) return s > 0 ? `${m}분 ${s}초` : `${m}분`;
  return `${s}초`;
}
