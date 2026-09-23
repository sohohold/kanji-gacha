export type Input = { mode: "on" | "kun"; reading: string; okurigana?: string };
export type Issue = {
  field: string;
  code: string;
  position?: number;
  normalized?: string;
};
export const normalize = (s: string) =>
  [...s.normalize("NFKC")]
    .map((c) => {
      const n = c.codePointAt(0)!;
      return n >= 0x3041 && n <= 0x3096 ? String.fromCodePoint(n + 0x60) : c;
    })
    .join("");
const basic =
  "アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲンガギグゲゴザジズゼゾダヂヅデドバビブベボパピプペポヴヰヱ";
const small = "ァィゥェォャュョ";
const pair = (a: string, b: string) =>
  ("キギシジチヂニヒビピミリ".includes(a) && "ャュョ".includes(b)) ||
  ("イウヴシジチツフテデトドクグ".includes(a) && "ァィゥェォ".includes(b)) ||
  ("テデフヴ".includes(a) && b === "ュ");
export function morae(s: string): string[] | Issue {
  const chars = [...s],
    out: string[] = [];
  const fail = (i: number, code: string): Issue => ({
    field: "",
    code,
    position: i,
    normalized: s,
  });
  for (let i = 0; i < chars.length; i++) {
    const c = chars[i];
    if (!(basic + small + "ッー").includes(c)) return fail(i, "character");
    if (i === 0 && (small + "ッーン").includes(c)) return fail(i, "leading");
    if (small.includes(c)) return fail(i, "small_kana");
    if (i > 0 && chars[i - 1] === "ッ" && "ッンー".includes(c))
      return fail(i, "sequence");
    if (c === "ー" && i > 0 && "ンッ".includes(chars[i - 1]))
      return fail(i, "sequence");
    if (i + 1 < chars.length && pair(c, chars[i + 1])) out.push(c + chars[++i]);
    else out.push(c);
  }
  return out;
}
// charset reports the first character outside the allowed set, so that spec 2.1
// can check the character set before the length.
export function charset(s: string): Issue | undefined {
  const chars = [...s];
  for (let i = 0; i < chars.length; i++)
    if (!(basic + small + "ッー").includes(chars[i]))
      return { field: "", code: "character", position: i, normalized: s };
}
export function validate(input: Input): Issue | undefined {
  if (!["on", "kun"].includes(input.mode))
    return { field: "mode", code: "enum" };
  const r = normalize(input.reading),
    o = normalize(input.okurigana ?? "");
  // spec 2.1 orders the checks as character set, then length, then ordering.
  for (const [field, value] of [
    ["reading", r],
    ["okurigana", o],
  ]) {
    const issue = charset(value);
    if (issue) return { ...issue, field };
  }
  if ([...r].length < 1 || [...r].length > 5)
    return { field: "reading", code: "length" };
  if ([...o].length > 3 || (input.mode === "on" && o !== ""))
    return { field: "okurigana", code: "length" };
  if ([...r].length + [...o].length > 6)
    return { field: "okurigana", code: "total_length" };
  const rm = morae(r);
  if (!Array.isArray(rm)) return { ...rm, field: "reading" };
  // 音読みは漢字1字の1音節に当たるため、2モーラ以下に限る。
  if (input.mode === "on" && rm.length > 2)
    return { field: "reading", code: "morae" };
  const om = morae(o);
  if (!Array.isArray(om)) return { ...om, field: "okurigana" };
}
export function issueMessage(issue: Issue): string {
  const label = issue.field === "okurigana" ? "送り仮名" : "読み";
  const position =
    issue.position === undefined
      ? ""
      : `（正規化後「${issue.normalized}」の${issue.position + 1}文字目）`;
  return (
    ({
      length: `${label}の文字数を確認してください。`,
      morae:
        "音読みは2拍（セイ、キョウなど）までです。長い読みは訓読みでお試しください。",
      total_length: "語幹と送り仮名は合わせて6文字以内です。",
      leading: `${label}の先頭には使えない文字です。`,
      character: "ひらがな・カタカナで入力してください。",
      small_kana: "小書き文字の組み合わせを確認してください。",
      sequence: "「ッ」「ン」「ー」の並びを確認してください。",
    }[issue.code] ?? "入力を確認してください。") + position
  );
}
