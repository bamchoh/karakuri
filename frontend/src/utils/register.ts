import type { DisplayFormat, BitWidth, Endianness } from "../types/register";
import {
  DISPLAY_FORMATS,
  BIT_WIDTHS,
  ENDIANNESS_OPTIONS,
} from "../constants/register";

// ビット幅に応じたワード数を取得
export const getWordCount = (bitWidth: BitWidth): number => {
  return BIT_WIDTHS.find((b) => b.value === bitWidth)?.wordCount ?? 1;
};

// 複数ワードを結合して数値に変換
export const combineWords = (
  words: number[],
  endianness: Endianness,
): bigint => {
  if (words.length === 0) return BigInt(0);
  const orderedWords = endianness === "little" ? words : [...words].reverse();
  let result = BigInt(0);
  for (let i = orderedWords.length - 1; i >= 0; i--) {
    result = (result << BigInt(16)) | BigInt(orderedWords[i] & 0xffff);
  }
  return result;
};

// bigintを指定形式でフォーマット
export const formatBigInt = (
  value: bigint,
  format: DisplayFormat,
  bitWidth: BitWidth,
): string => {
  const absValue = value < 0 ? -value : value;
  switch (format) {
    case "hex": {
      const hexDigits = bitWidth / 4;
      return (
        "0x" + absValue.toString(16).toUpperCase().padStart(hexDigits, "0")
      );
    }
    case "octal":
      return "0o" + absValue.toString(8);
    case "binary":
      return absValue.toString(2).padStart(bitWidth, "0");
    default:
      return value.toString();
  }
};

// 16bit値をフォーマット
export const formatSingleWord = (
  value: number,
  format: DisplayFormat,
): string => {
  switch (format) {
    case "hex":
      return "0x" + value.toString(16).toUpperCase().padStart(4, "0");
    case "octal":
      return "0o" + value.toString(8).padStart(6, "0");
    case "binary":
      return value.toString(2).padStart(16, "0");
    default:
      return value.toString();
  }
};

// bigintを複数ワードに分解
export const splitToWords = (
  value: bigint,
  wordCount: number,
  endianness: Endianness,
): number[] => {
  const words: number[] = [];
  let remaining = value < 0 ? -value : value;
  const mask = BigInt(0xffff);
  for (let i = 0; i < wordCount; i++) {
    words.push(Number(remaining & mask));
    remaining = remaining >> BigInt(16);
  }
  return endianness === "little" ? words : words.reverse();
};

// 文字列入力をbigintにパース
export const parseBigIntInput = (
  input: string,
  format: DisplayFormat,
): bigint => {
  const trimmed = input.trim();
  switch (format) {
    case "hex": {
      const hexStr = trimmed.replace(/^0x/i, "");
      return BigInt("0x" + hexStr);
    }
    case "octal": {
      const octStr = trimmed.replace(/^0o/i, "");
      return BigInt("0o" + octStr);
    }
    case "binary": {
      const binStr = trimmed.replace(/^0b/i, "");
      return BigInt("0b" + binStr);
    }
    default:
      return BigInt(trimmed);
  }
};

// 入力値をパース（16bit用）
export const parseInputValue = (
  input: string,
  format: DisplayFormat,
): number => {
  const trimmed = input.trim();
  switch (format) {
    case "hex": {
      const hexStr = trimmed.replace(/^0x/i, "");
      return parseInt(hexStr, 16);
    }
    case "octal": {
      const octStr = trimmed.replace(/^0o/i, "");
      return parseInt(octStr, 8);
    }
    case "binary": {
      const binStr = trimmed.replace(/^0b/i, "");
      return parseInt(binStr, 2);
    }
    default:
      return parseInt(trimmed, 10);
  }
};
