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
