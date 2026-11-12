import type { DisplayFormat, BitWidth, Endianness } from "../types/register";

export const DISPLAY_FORMATS: { value: DisplayFormat; label: string }[] = [
  { value: "decimal", label: "10進数" },
  { value: "hex", label: "16進数" },
  { value: "octal", label: "8進数" },
  { value: "binary", label: "2進数" },
];

export const BIT_WIDTHS: {
  value: BitWidth;
  label: string;
  wordCount: number;
}[] = [
  { value: 16, label: "16bit", wordCount: 1 },
  { value: 32, label: "32bit", wordCount: 2 },
  { value: 64, label: "64bit", wordCount: 4 },
];

export const ENDIANNESS_OPTIONS: { value: Endianness; label: string }[] = [
  { value: "big", label: "BE" },
  { value: "little", label: "LE" },
];

// エンディアンの選択肢（幅に余裕のある場所で使う、長い表記）
export const ENDIANNESS_LONG_OPTIONS: { value: Endianness; label: string }[] = [
  { value: "big", label: "ビッグ(BE)" },
  { value: "little", label: "リトル(LE)" },
];
