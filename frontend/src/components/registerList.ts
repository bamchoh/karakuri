// レジスタの一覧表示（RegisterListView）で使う、state に依存しない関数
import {
  combineWords,
  formatBigInt,
  formatSingleWord,
  getWordCount,
} from "../utils/register";
import type { BitWidth, DisplayFormat, Endianness } from "../types/register";

// 1 行に並べるワード数と、1 画面に表示するレジスタ数
export const COLUMNS = 10;
export const PAGE_SIZE = 100;

// レジスタの値（ビットのエリアは boolean、ワードのエリアは number）
export type RegisterValue = boolean | number;

export interface RegisterRow {
  // 行の先頭のアドレス（0 始まり）
  rowStart: number;
  // index は表示中の値の配列での位置、wordSpan はセルがまとめるワード数
  cells: { index: number; value: RegisterValue; wordSpan: number }[];
}

// 1 行に並べるセルの数（32bit / 64bit では、複数ワードを 1 つのセルにまとめる）
export function columnsPerRow(wordCount: number): number {
  return Math.max(1, Math.floor(COLUMNS / wordCount));
}

// 表示中の値を、表の行に分ける
export function buildRows(
  values: RegisterValue[],
  startAddress: number,
  wordCount: number,
): RegisterRow[] {
  const columns = columnsPerRow(wordCount);
  const step = columns * wordCount;
  const rows: RegisterRow[] = [];

  for (let i = 0; i < values.length; i += step) {
    const cells: RegisterRow["cells"] = [];
    for (let j = 0; j < columns; j++) {
      const idx = i + j * wordCount;
      if (idx < values.length) {
        cells.push({ index: idx, value: values[idx], wordSpan: wordCount });
      }
    }
    rows.push({ rowStart: startAddress + i, cells });
  }
  return rows;
}

// キー操作による選択セルの移動先を返す。移動のキーでなければ null
export function moveSelection(
  key: string,
  selectedIndex: number,
  maxIndex: number,
): number | null {
  switch (key) {
    case "ArrowUp":
      return Math.max(0, selectedIndex - COLUMNS);
    case "ArrowDown":
      return Math.min(maxIndex, selectedIndex + COLUMNS);
    case "ArrowLeft":
      return Math.max(0, selectedIndex - 1);
    case "ArrowRight":
      return Math.min(maxIndex, selectedIndex + 1);
    case "Home":
      return 0;
    case "End":
      return maxIndex;
    case "PageUp":
      return Math.max(0, selectedIndex - COLUMNS * 5);
    case "PageDown":
      return Math.min(maxIndex, selectedIndex + COLUMNS * 5);
    default:
      return null;
  }
}

// index から始まる複数ワードを 1 つの値として文字列にする。値がそろっていなければ "---"
export function formatMultiWord(
  values: RegisterValue[],
  startIndex: number,
  format: DisplayFormat,
  width: BitWidth,
  endian: Endianness,
): string {
  const wordCount = getWordCount(width);
  const words: number[] = [];
  for (let i = 0; i < wordCount; i++) {
    const val = values[startIndex + i];
    if (val === undefined || typeof val === "boolean") {
      return "---";
    }
    words.push(val);
  }
  return formatBigInt(combineWords(words, endian), format, width);
}

// セルの値を文字列にする（ビットは ON / OFF、16bit は 1 ワード、32bit / 64bit は複数ワードをまとめる）
export function formatRegisterValue(
  values: RegisterValue[],
  value: RegisterValue,
  index: number,
  format: DisplayFormat,
  width: BitWidth,
  endian: Endianness,
): string {
  if (typeof value === "boolean") {
    return value ? "ON" : "OFF";
  }
  if (width === 16) {
    return formatSingleWord(value, format);
  }
  return formatMultiWord(values, index, format, width, endian);
}
