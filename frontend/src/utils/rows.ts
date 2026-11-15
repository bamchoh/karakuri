// 「行を足せる表」（項目の種類 "rows"）の値の操作。値は行の並びで、1 行は列の name をキーにしたオブジェクト。
// どの関数も、渡された配列とオブジェクトを書き換えずに、新しいものを返す（項目の既定値と同じものを共有していることがあるため）

export type Row = Record<string, any>;

// 列の定義のうち、ここで使うところ（application.FieldDTO の一部）
export interface RowColumn {
  name: string;
  default?: any;
}

// rowDefaults は、列の既定値で埋めた 1 行
export function rowDefaults(columns: RowColumn[]): Row {
  const row: Row = {};
  for (const c of columns) row[c.name] = c.default;
  return row;
}

// rowsOf は、項目の値を行の並びにする。値が行の並びでなければ、項目の既定値（行の並び）、それもなければ既定値の 1 行
export function rowsOf(
  value: unknown,
  fieldDefault: unknown,
  columns: RowColumn[],
): Row[] {
  const source = Array.isArray(value)
    ? value
    : Array.isArray(fieldDefault)
      ? fieldDefault
      : [rowDefaults(columns)];
  return source
    .filter((r) => r !== null && typeof r === "object")
    .map((r) => ({ ...rowDefaults(columns), ...r }));
}

// addRow は、行を 1 つ足す。新しい行は、最後の行を写したもの（続けて似たデバイスを入れやすいように）。行がなければ既定値
export function addRow(rows: Row[], columns: RowColumn[]): Row[] {
  const last = rows[rows.length - 1];
  return [...rows, last ? { ...last } : rowDefaults(columns)];
}

// removeRow は、index の行を消す
export function removeRow(rows: Row[], index: number): Row[] {
  return rows.filter((_, i) => i !== index);
}

// updateCell は、index の行の列 name を value にする
export function updateCell(
  rows: Row[],
  index: number,
  name: string,
  value: unknown,
): Row[] {
  return rows.map((r, i) => (i === index ? { ...r, [name]: value } : r));
}

// canAddRow は、まだ行を足せるか（max がなければ、いくつでも）
export function canAddRow(rows: Row[], max?: number | null): boolean {
  return max === undefined || max === null || rows.length < max;
}
