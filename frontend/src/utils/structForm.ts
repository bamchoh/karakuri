// 構造体型のダイアログ（フィールドを 1 行ずつの表で編集する）の、行と型の文字列の変換・検査。state に依存しない純粋な関数

export interface DimBound {
  lower: number;
  upper: number;
}

// 表の 1 行（フィールド 1 つ）
export interface FieldRow {
  // 行を見分ける番号（並べ替えても変わらない。React の key に使う）
  key: number;
  name: string;
  // 要素の型: スカラーの型（INT など）・"STRING"・構造体型の名前
  type: string;
  // type が STRING のときのバイト長
  stringLength: number;
  isArray: boolean;
  // 配列の範囲（"0..9"、多次元は "0..2, 0..4"）
  bounds: string;
}

export const DEFAULT_STRING_LENGTH = 20;
export const DEFAULT_BOUNDS = "0..9";
export const MAX_DIMS = 3;

export const emptyRow = (key: number): FieldRow => ({
  key,
  name: "",
  type: "INT",
  stringLength: DEFAULT_STRING_LENGTH,
  isArray: false,
  bounds: DEFAULT_BOUNDS,
});

export type BoundsResult =
  { ok: true; dims: DimBound[] } | { ok: false; error: string };

// parseBounds は、配列の範囲の文字を読む。
//   "0..9"        → 1 次元
//   "1..3, 0..4"  → 2 次元（3 次元まで）
//   "10"          → 個数だけ書いたもの。0..9 として読む
export function parseBounds(text: string): BoundsResult {
  const parts = text.split(",").map((p) => p.trim());
  if (parts.length === 1 && parts[0] === "") {
    return { ok: false, error: "範囲を 0..9 の形で入れてください" };
  }
  if (parts.length > MAX_DIMS) {
    return { ok: false, error: `配列は ${MAX_DIMS} 次元までです` };
  }
  const dims: DimBound[] = [];
  for (const part of parts) {
    const count = part.match(/^\d+$/);
    if (count) {
      const n = parseInt(part, 10);
      if (n < 1) return { ok: false, error: "個数は 1 以上にしてください" };
      dims.push({ lower: 0, upper: n - 1 });
      continue;
    }
    const m = part.match(/^(-?\d+)\s*\.\.\s*(-?\d+)$/);
    if (!m) {
      return {
        ok: false,
        error: `範囲「${part}」を 0..9 の形で入れてください`,
      };
    }
    const lower = parseInt(m[1], 10);
    const upper = parseInt(m[2], 10);
    if (upper < lower) {
      return {
        ok: false,
        error: `範囲「${part}」は、終わりを始まり以上にしてください`,
      };
    }
    dims.push({ lower, upper });
  }
  return { ok: true, dims };
}

export const formatBounds = (dims: DimBound[]): string =>
  dims.map((d) => `${d.lower}..${d.upper}`).join(", ");

// 行の要素の型の文字列（STRING はバイト長を付ける）
const elementType = (row: FieldRow): string =>
  row.type === "STRING" ? `STRING[${row.stringLength}]` : row.type;

// rowDataType は、行からデータ型の文字列を作る（"INT"、"STRING[10]"、"ARRAY[0..9] OF DINT"）。
// 配列の範囲が読めなければ null
export function rowDataType(row: FieldRow): string | null {
  if (!row.isArray) return elementType(row);
  const bounds = parseBounds(row.bounds);
  if (!bounds.ok) return null;
  return `ARRAY[${formatBounds(bounds.dims)}] OF ${elementType(row)}`;
}

// rowFromField は、登録済みのフィールド（名前とデータ型の文字列）を行にする。
// 配列は "ARRAY[0..9] OF INT" の形と、古い "ARRAY[INT;10]"（入れ子も）を読む
export function rowFromField(
  key: number,
  name: string,
  dataType: string,
): FieldRow {
  const row = emptyRow(key);
  row.name = name;
  let elem = dataType;
  const dims: DimBound[] = [];
  // 配列の次元を外側からはがす
  for (;;) {
    const iec = elem.match(/^ARRAY\[(.*?)\] OF (.+)$/);
    if (iec) {
      const parsed = parseBounds(iec[1]);
      if (!parsed.ok) break;
      dims.push(...parsed.dims);
      elem = iec[2];
      continue;
    }
    const old = elem.match(/^ARRAY\[(.+);\s*(\d+)\]$/);
    if (old) {
      dims.push({ lower: 0, upper: parseInt(old[2], 10) - 1 });
      elem = old[1].trim();
      continue;
    }
    break;
  }
  if (dims.length > 0) {
    row.isArray = true;
    row.bounds = formatBounds(dims);
  }
  const str = elem.match(/^STRING\[(\d+)\]$/);
  if (str) {
    row.type = "STRING";
    row.stringLength = parseInt(str[1], 10);
  } else {
    row.type = elem;
  }
  return row;
}

// 構造体型の定義のうち、ここで使うところ（application.StructTypeDTO の一部）
export interface StructTypeLike {
  name: string;
  fields: { name: string; dataType: string }[];
}

// 型の文字列が指す構造体型の名前（配列なら要素の型。スカラーなら null）
function referencedStruct(
  dataType: string,
  types: StructTypeLike[],
): string | null {
  const elem = rowFromField(0, "", dataType).type;
  return types.some((t) => t.name === elem) ? elem : null;
}

// usesType は、構造体型 typeName が、target を（フィールド・配列の要素として、入れ子も含めて）使っているか
export function usesType(
  typeName: string,
  target: string,
  types: StructTypeLike[],
  visited: Set<string> = new Set(),
): boolean {
  if (typeName === target) return true;
  if (visited.has(typeName)) return false;
  visited.add(typeName);
  const st = types.find((t) => t.name === typeName);
  if (!st) return false;
  return st.fields.some((f) => {
    const ref = referencedStruct(f.dataType, types);
    return ref !== null && usesType(ref, target, types, visited);
  });
}

// selectableStructs は、型 editing のフィールドに選べる構造体型（自分と、自分を使っている型は選べない。循環するため）
export function selectableStructs<T extends StructTypeLike>(
  editing: string,
  types: T[],
): T[] {
  if (!editing) return types;
  return types.filter((t) => !usesType(t.name, editing, types));
}

export interface StructFormErrors {
  name?: string;
  // 行の key → 理由
  rows: Record<number, string>;
  // 全体の理由（フィールドが 1 つもない）
  form?: string;
}

export const hasErrors = (e: StructFormErrors): boolean =>
  !!e.name || !!e.form || Object.keys(e.rows).length > 0;

// namedRows は、名前のある行（名前が空の行は、まだ書いていない行として保存しない）
export const namedRows = (rows: FieldRow[]): FieldRow[] =>
  rows.filter((r) => r.name.trim() !== "");

// validateStructForm は、型名と行を検査する。
// isNew なら型名も見る（登録済みの型は、型名を変えられない）
export function validateStructForm(
  name: string,
  rows: FieldRow[],
  options: { isNew: boolean; existingNames: string[]; scalarTypes: string[] },
): StructFormErrors {
  const errors: StructFormErrors = { rows: {} };
  if (options.isNew) {
    const n = name.trim();
    if (!n) errors.name = "型名を入れてください";
    else if (options.existingNames.includes(n)) {
      errors.name = `型「${n}」は、もうあります`;
    } else if (
      options.scalarTypes.includes(n) ||
      n === "STRING" ||
      n.startsWith("ARRAY[") ||
      n.startsWith("STRING[")
    ) {
      errors.name = `「${n}」は、型名に使えません`;
    }
  }
  const named = namedRows(rows);
  if (named.length === 0) {
    errors.form = "フィールドを 1 つ以上入れてください";
  }
  const seen = new Set<string>();
  for (const row of named) {
    const fieldName = row.name.trim();
    if (seen.has(fieldName)) {
      errors.rows[row.key] = `名前「${fieldName}」が重なっています`;
      continue;
    }
    seen.add(fieldName);
    if (
      row.type === "STRING" &&
      !(
        Number.isInteger(row.stringLength) &&
        row.stringLength >= 1 &&
        row.stringLength <= 256
      )
    ) {
      errors.rows[row.key] = "バイト長は 1〜256 にしてください";
      continue;
    }
    if (row.isArray) {
      const bounds = parseBounds(row.bounds);
      if (!bounds.ok) errors.rows[row.key] = bounds.error;
    }
  }
  return errors;
}

// buildFields は、名前のある行から、登録するフィールド（名前とデータ型）を作る。検査を通った行に使う
export function buildFields(
  rows: FieldRow[],
): { name: string; dataType: string }[] {
  return namedRows(rows).map((r) => ({
    name: r.name.trim(),
    dataType: rowDataType(r) ?? r.type,
  }));
}

export interface RowLayout {
  // 先頭からのワードのオフセットと、この行のワード数（名前のない行・型が読めない行は null）
  offset: number | null;
  words: number | null;
}

// layoutRows は、行ごとのオフセットとワード数、合計のワード数を計算する（名前のない行は数えない）
export function layoutRows(
  rows: FieldRow[],
  wordCountOf: (dataType: string) => number,
): { rows: Record<number, RowLayout>; total: number } {
  const out: Record<number, RowLayout> = {};
  let offset = 0;
  for (const row of rows) {
    const dataType = row.name.trim() === "" ? null : rowDataType(row);
    if (dataType === null) {
      out[row.key] = { offset: null, words: null };
      continue;
    }
    const words = wordCountOf(dataType);
    out[row.key] = { offset, words };
    offset += words;
  }
  return { rows: out, total: offset };
}

// moveRow は、index の行を delta（-1 で上、+1 で下）だけ動かす。端なら何もしない
export function moveRow(
  rows: FieldRow[],
  index: number,
  delta: number,
): FieldRow[] {
  const to = index + delta;
  if (index < 0 || index >= rows.length || to < 0 || to >= rows.length) {
    return rows;
  }
  const next = [...rows];
  const [row] = next.splice(index, 1);
  next.splice(to, 0, row);
  return next;
}

// duplicateRow は、index の行を写して、すぐ下に入れる（名前には、重ならない番号を付ける）
export function duplicateRow(
  rows: FieldRow[],
  index: number,
  key: number,
): FieldRow[] {
  const src = rows[index];
  if (!src) return rows;
  const next = [...rows];
  next.splice(index + 1, 0, {
    ...src,
    key,
    name: nextFieldName(
      src.name,
      rows.map((r) => r.name.trim()),
    ),
  });
  return next;
}

// nextFieldName は、name の末尾の番号を進めた、まだない名前を返す（"speed" → "speed2"、"in1" → "in2"）。名前が空なら空
export function nextFieldName(name: string, existing: string[]): string {
  const base = name.trim();
  if (!base) return "";
  const m = base.match(/^(.*?)(\d+)$/);
  const stem = m ? m[1] : base;
  let n = m ? parseInt(m[2], 10) + 1 : 2;
  while (existing.includes(`${stem}${n}`)) n++;
  return `${stem}${n}`;
}
