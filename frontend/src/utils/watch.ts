// 下の帯の「モニタリング」（レジスタ・変数・接続先の値をまとめて見る欄）の、値と名前の組み立て。state に依存しない純粋な関数

// 変数の値のうち、画面から 1 つの入力欄で書けるもの（数値・真偽・文字列）。構造体・配列は書けない
export function isWritableValue(value: unknown): boolean {
  return (
    typeof value === "number" ||
    typeof value === "boolean" ||
    typeof value === "string"
  );
}

export type ParsedInput =
  { ok: true; value: unknown } | { ok: false; error: string };

// parseVariableInput は、入力欄の文字を、いまの値と同じ種類の値にする。
//   - いまの値が数値: 10 進・16 進（0x）・2 進（0b）・小数。範囲はサーバー側が確かめる
//   - いまの値が真偽: TRUE / FALSE / ON / OFF / 1 / 0（大文字小文字は問わない）
//   - いまの値が文字列（STRING・TIME など・LINT / ULINT）: そのまま渡す（読み方はサーバー側が決める）
export function parseVariableInput(
  text: string,
  current: unknown,
): ParsedInput {
  const t = text.trim();
  if (typeof current === "boolean") {
    const u = t.toUpperCase();
    if (u === "TRUE" || u === "ON" || u === "1")
      return { ok: true, value: true };
    if (u === "FALSE" || u === "OFF" || u === "0") {
      return { ok: true, value: false };
    }
    return { ok: false, error: "TRUE か FALSE を入れてください" };
  }
  if (typeof current === "number") {
    if (t === "") return { ok: false, error: "数値を入れてください" };
    const n = Number(t);
    if (!Number.isFinite(n))
      return { ok: false, error: "数値を入れてください" };
    return { ok: true, value: n };
  }
  if (typeof current === "string") return { ok: true, value: text };
  return { ok: false, error: "この値は、ここからは書けません" };
}

// モニタリングに足す候補（変数・接続先の監視項目と読み取り項目）の 1 つ
export interface WatchCandidate {
  key: string;
  label: string;
  detail?: string;
}

// filterCandidates は、候補を、打った文字で絞る（名前か補足に含まれるもの。大文字小文字は問わない）
export function filterCandidates<T extends WatchCandidate>(
  candidates: T[],
  query: string,
): T[] {
  const q = query.trim().toLowerCase();
  if (!q) return candidates;
  return candidates.filter(
    (c) =>
      c.label.toLowerCase().includes(q) ||
      (c.detail ?? "").toLowerCase().includes(q),
  );
}

// 値の中身の 1 行（application.MonitoringValueRowDTO の一部）。構造体・配列の行（composite）は、中身が下の行に続く
export interface ValueRowLike {
  depth: number;
  path?: string;
  composite?: boolean;
}

// visibleRows は、中身の行のうち、畳んだ行（isCollapsed が true の構造体・配列の行）より下にぶら下がる行を除く。
// 行は、親のすぐ下に子が続く並び（depth は、親より 1 深い）
export function visibleRows<T extends ValueRowLike>(
  rows: T[],
  isCollapsed: (row: T) => boolean,
): T[] {
  const out: T[] = [];
  // この深さより深い行は、畳まれた行の中身なので出さない
  let hideBelow = Infinity;
  for (const row of rows) {
    if (row.depth > hideBelow) continue;
    hideBelow = Infinity;
    out.push(row);
    if (row.composite && isCollapsed(row)) hideBelow = row.depth;
  }
  return out;
}

// variablePathLabel は、変数名とパスをつないだ名前（"Motor1" + "speed" → "Motor1.speed"、"Values" + "[1]" → "Values[1]"）
export function variablePathLabel(name: string, path: string): string {
  if (!path) return name;
  return path.startsWith("[") || path.startsWith(".")
    ? name + path
    : `${name}.${path}`;
}

// variableNameOf は、項目の名前（変数名 + 項目のパス）から変数名を取り出す
export function variableNameOf(label: string, fieldPath: string): string {
  const suffix = variablePathLabel("", fieldPath);
  return suffix && label.endsWith(suffix)
    ? label.slice(0, label.length - suffix.length)
    : label;
}
