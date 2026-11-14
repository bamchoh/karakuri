import { describe, expect, it } from "vitest";
import {
  combineWords,
  formatBigInt,
  formatSingleWord,
  getWordCount,
  parseBigIntInput,
  parseInputValue,
  splitToWords,
} from "./register";

describe("getWordCount", () => {
  it("ビット幅ごとのワード数", () => {
    expect(getWordCount(16)).toBe(1);
    expect(getWordCount(32)).toBe(2);
    expect(getWordCount(64)).toBe(4);
  });
});

describe("combineWords / splitToWords", () => {
  it("ビッグエンディアンは、先頭のワードが上位", () => {
    expect(combineWords([0x1234, 0x5678], "big")).toBe(0x12345678n);
    expect(splitToWords(0x12345678n, 2, "big")).toEqual([0x1234, 0x5678]);
  });

  it("リトルエンディアンは、先頭のワードが下位", () => {
    expect(combineWords([0x1234, 0x5678], "little")).toBe(0x56781234n);
    expect(splitToWords(0x12345678n, 2, "little")).toEqual([0x5678, 0x1234]);
  });

  it("64 ビット（4 ワード）でも桁が落ちない", () => {
    const words = [0xffff, 0xfffe, 0x0001, 0x8000];
    expect(combineWords(words, "big")).toBe(0xfffffffe00018000n);
    expect(splitToWords(0xfffffffe00018000n, 4, "big")).toEqual(words);
    expect(splitToWords(0xfffffffe00018000n, 4, "little")).toEqual(
      [...words].reverse(),
    );
  });

  it("ワードがなければ 0。足りない上位のワードは 0 で埋める", () => {
    expect(combineWords([], "big")).toBe(0n);
    expect(splitToWords(0x12n, 2, "big")).toEqual([0, 0x12]);
    expect(splitToWords(0x12n, 2, "little")).toEqual([0x12, 0]);
  });

  it("分けて結ぶと、もとに戻る", () => {
    for (const endian of ["big", "little"] as const) {
      for (const value of [0n, 1n, 0xffffn, 0x10000n, 0xdeadbeefn]) {
        expect(combineWords(splitToWords(value, 2, endian), endian)).toBe(
          value,
        );
      }
    }
  });
});

describe("値の書式", () => {
  it("formatSingleWord", () => {
    expect(formatSingleWord(26, "decimal")).toBe("26");
    expect(formatSingleWord(26, "hex")).toBe("0x001A");
    expect(formatSingleWord(8, "octal")).toBe("0o000010");
    expect(formatSingleWord(5, "binary")).toBe("0000000000000101");
    expect(formatSingleWord(0xffff, "hex")).toBe("0xFFFF");
  });

  it("formatBigInt は、ビット幅に合わせて桁をそろえる", () => {
    expect(formatBigInt(255n, "decimal", 32)).toBe("255");
    expect(formatBigInt(255n, "hex", 32)).toBe("0x000000FF");
    expect(formatBigInt(0x12345678n, "hex", 32)).toBe("0x12345678");
    expect(formatBigInt(255n, "hex", 64)).toBe("0x00000000000000FF");
    expect(formatBigInt(8n, "octal", 32)).toBe("0o10");
    expect(formatBigInt(5n, "binary", 32)).toBe("101".padStart(32, "0"));
  });
});

describe("入力の読み取り", () => {
  it("parseInputValue（16 ビット）", () => {
    expect(parseInputValue("42", "decimal")).toBe(42);
    expect(parseInputValue(" 42 ", "decimal")).toBe(42);
    // 接頭辞は、あってもなくてもよい
    expect(parseInputValue("0x1A", "hex")).toBe(26);
    expect(parseInputValue("1a", "hex")).toBe(26);
    expect(parseInputValue("0o17", "octal")).toBe(15);
    expect(parseInputValue("17", "octal")).toBe(15);
    expect(parseInputValue("0b101", "binary")).toBe(5);
    expect(parseInputValue("101", "binary")).toBe(5);
    expect(parseInputValue("abc", "decimal")).toBeNaN();
    expect(parseInputValue("", "hex")).toBeNaN();
  });

  it("parseBigIntInput（32 / 64 ビット）", () => {
    expect(parseBigIntInput("42", "decimal")).toBe(42n);
    expect(parseBigIntInput("0x1A", "hex")).toBe(26n);
    expect(parseBigIntInput("1A", "hex")).toBe(26n);
    expect(parseBigIntInput("0o17", "octal")).toBe(15n);
    expect(parseBigIntInput("0b101", "binary")).toBe(5n);
    expect(parseBigIntInput("18446744073709551615", "decimal")).toBe(
      0xffffffffffffffffn,
    );
    // 読めない入力は例外（呼び出し側が受け取って、入力の誤りとして表示する）
    expect(() => parseBigIntInput("abc", "decimal")).toThrow();
    expect(() => parseBigIntInput("0xZZ", "hex")).toThrow();
  });

  it("書式にしたものを読むと、もとに戻る", () => {
    for (const format of ["decimal", "hex", "octal", "binary"] as const) {
      for (const value of [0, 1, 26, 0x8000, 0xffff]) {
        expect(parseInputValue(formatSingleWord(value, format), format)).toBe(
          value,
        );
      }
    }
  });
});
