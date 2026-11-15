import { describe, expect, it } from "vitest";
import {
  addRow,
  canAddRow,
  removeRow,
  rowDefaults,
  rowsOf,
  updateCell,
} from "./rows";

const columns = [
  { name: "device", default: "D" },
  { name: "address", default: "0" },
  { name: "unit", default: "word" },
];

describe("rowsOf", () => {
  it("行の並びなら、足りない列を既定値で埋めて写す", () => {
    const value = [{ device: "M", address: "10" }];
    const rows = rowsOf(value, undefined, columns);
    expect(rows).toEqual([{ device: "M", address: "10", unit: "word" }]);
    expect(rows[0]).not.toBe(value[0]);
  });

  it("値がなければ項目の既定値、それもなければ既定値の 1 行", () => {
    const fieldDefault = [{ device: "W", address: "1F", unit: "dword" }];
    expect(rowsOf(undefined, fieldDefault, columns)).toEqual(fieldDefault);
    expect(rowsOf(undefined, fieldDefault, columns)[0]).not.toBe(
      fieldDefault[0],
    );
    expect(rowsOf("D100", undefined, columns)).toEqual([rowDefaults(columns)]);
  });

  it("行ではない値は読み飛ばす", () => {
    expect(rowsOf([null, "x", { device: "X" }], undefined, columns)).toEqual([
      { device: "X", address: "0", unit: "word" },
    ]);
  });
});

describe("行の操作", () => {
  const rows = [
    { device: "D", address: "100", unit: "word" },
    { device: "M", address: "5", unit: "dword" },
  ];

  it("addRow は最後の行を写して足す（行がなければ既定値）", () => {
    const added = addRow(rows, columns);
    expect(added).toHaveLength(3);
    expect(added[2]).toEqual(rows[1]);
    expect(added[2]).not.toBe(rows[1]);
    expect(addRow([], columns)).toEqual([rowDefaults(columns)]);
    expect(rows).toHaveLength(2); // 元の配列は変えない
  });

  it("removeRow は指定した行を消す", () => {
    expect(removeRow(rows, 0)).toEqual([rows[1]]);
    expect(rows).toHaveLength(2);
  });

  it("updateCell は指定した行の列だけを変える", () => {
    const updated = updateCell(rows, 1, "address", "6");
    expect(updated[1]).toEqual({ device: "M", address: "6", unit: "dword" });
    expect(updated[0]).toBe(rows[0]);
    expect(rows[1].address).toBe("5");
  });

  it("canAddRow は上限まで足せる", () => {
    expect(canAddRow(rows, 3)).toBe(true);
    expect(canAddRow(rows, 2)).toBe(false);
    expect(canAddRow(rows, undefined)).toBe(true);
    expect(canAddRow(rows, null)).toBe(true);
  });
});
