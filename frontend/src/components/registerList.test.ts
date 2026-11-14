import { describe, expect, it } from "vitest";
import {
  buildRows,
  COLUMNS,
  columnsPerRow,
  formatMultiWord,
  formatRegisterValue,
  moveSelection,
  PAGE_SIZE,
  WORD_BIT_COLUMNS,
  WORD_BIT_PAGE_SIZE,
} from "./registerList";

const numbers = (n: number) => Array.from({ length: n }, (_, i) => i);

describe("columnsPerRow", () => {
  it("1 行に並べるセルの数（32bit / 64bit は、複数ワードを 1 つのセルにまとめる）", () => {
    expect(columnsPerRow(1)).toBe(10);
    expect(columnsPerRow(2)).toBe(5);
    expect(columnsPerRow(4)).toBe(2);
    // 「ワード.ビット」のエリアは、1 行に 1 ワード分の 16 ビット
    expect(columnsPerRow(1, WORD_BIT_COLUMNS)).toBe(16);
    // どんなに幅が広くても、1 行に 1 つは置く
    expect(columnsPerRow(100)).toBe(1);
  });

  it("定数の関係", () => {
    expect(PAGE_SIZE).toBe(COLUMNS * 10);
    expect(WORD_BIT_PAGE_SIZE).toBe(WORD_BIT_COLUMNS * 10);
  });
});

describe("buildRows", () => {
  it("16bit: 10 個ずつの行にする。行の先頭のアドレスは、開始アドレスから", () => {
    const rows = buildRows(numbers(25), 100, 1);
    expect(rows.map((r) => r.rowStart)).toEqual([100, 110, 120]);
    expect(rows.map((r) => r.cells.length)).toEqual([10, 10, 5]);
    expect(rows[1].cells[3]).toEqual({ index: 13, value: 13, wordSpan: 1 });
  });

  it("32bit: 2 ワードを 1 つのセルにして、1 行に 5 個", () => {
    const rows = buildRows(numbers(20), 0, 2);
    expect(rows.map((r) => r.rowStart)).toEqual([0, 10]);
    expect(rows[0].cells.map((c) => c.index)).toEqual([0, 2, 4, 6, 8]);
    expect(rows[0].cells.every((c) => c.wordSpan === 2)).toBe(true);
  });

  it("64bit: 4 ワードを 1 つのセルにして、1 行に 2 個（1 行は 8 ワード）", () => {
    const rows = buildRows(numbers(16), 0, 4);
    expect(rows.map((r) => r.rowStart)).toEqual([0, 8]);
    expect(rows[0].cells.map((c) => c.index)).toEqual([0, 4]);
  });

  it("「ワード.ビット」のエリア: 16 個ずつの行にする", () => {
    const bits = numbers(40).map((i) => i % 2 === 0);
    const rows = buildRows(bits, 1600, 1, WORD_BIT_COLUMNS);
    expect(rows.map((r) => r.rowStart)).toEqual([1600, 1616, 1632]);
    expect(rows.map((r) => r.cells.length)).toEqual([16, 16, 8]);
  });

  it("値がなければ行もない", () => {
    expect(buildRows([], 0, 1)).toEqual([]);
  });
});

describe("moveSelection", () => {
  const max = 99;

  it("矢印キーで上下左右に動く（端では止まる）", () => {
    expect(moveSelection("ArrowRight", 0, max)).toBe(1);
    expect(moveSelection("ArrowLeft", 0, max)).toBe(0);
    expect(moveSelection("ArrowDown", 0, max)).toBe(10);
    expect(moveSelection("ArrowUp", 15, max)).toBe(5);
    expect(moveSelection("ArrowUp", 5, max)).toBe(0);
    expect(moveSelection("ArrowDown", 95, max)).toBe(99);
    expect(moveSelection("ArrowRight", 99, max)).toBe(99);
  });

  it("Home / End / PageUp / PageDown", () => {
    expect(moveSelection("Home", 42, max)).toBe(0);
    expect(moveSelection("End", 42, max)).toBe(99);
    expect(moveSelection("PageDown", 0, max)).toBe(50);
    expect(moveSelection("PageUp", 60, max)).toBe(10);
    expect(moveSelection("PageDown", 80, max)).toBe(99);
    expect(moveSelection("PageUp", 20, max)).toBe(0);
  });

  it("「ワード.ビット」のエリアでは、上下が 16 個ずつ", () => {
    expect(moveSelection("ArrowDown", 5, 159, WORD_BIT_COLUMNS)).toBe(21);
    expect(moveSelection("ArrowUp", 21, 159, WORD_BIT_COLUMNS)).toBe(5);
    expect(moveSelection("PageDown", 0, 159, WORD_BIT_COLUMNS)).toBe(80);
  });

  it("移動のキーでなければ null", () => {
    expect(moveSelection("a", 0, max)).toBeNull();
    expect(moveSelection("Enter", 0, max)).toBeNull();
  });
});

describe("値の書式", () => {
  it("ビットは ON / OFF", () => {
    expect(formatRegisterValue([true], true, 0, "decimal", 16, "big")).toBe(
      "ON",
    );
    expect(formatRegisterValue([false], false, 0, "hex", 16, "big")).toBe(
      "OFF",
    );
  });

  it("16bit は 1 ワード", () => {
    expect(formatRegisterValue([26], 26, 0, "hex", 16, "big")).toBe("0x001A");
    expect(formatRegisterValue([26], 26, 0, "decimal", 16, "big")).toBe("26");
  });

  it("32bit は、エンディアンに従って 2 ワードをまとめる", () => {
    const values = [0x1234, 0x5678];
    expect(formatRegisterValue(values, values[0], 0, "hex", 32, "big")).toBe(
      "0x12345678",
    );
    expect(formatRegisterValue(values, values[0], 0, "hex", 32, "little")).toBe(
      "0x56781234",
    );
  });

  it("ワードがそろっていなければ ---", () => {
    expect(formatMultiWord([1], 0, "decimal", 32, "big")).toBe("---");
    expect(formatMultiWord([1, 2, 3], 0, "decimal", 64, "big")).toBe("---");
    // ビットの値が混ざっていても ---
    expect(formatMultiWord([1, true], 0, "decimal", 32, "big")).toBe("---");
  });
});
