import { describe, expect, it } from "vitest";
import {
  FieldRow,
  buildFields,
  duplicateRow,
  emptyRow,
  hasErrors,
  layoutRows,
  moveRow,
  nextFieldName,
  parseBounds,
  rowDataType,
  rowFromField,
  selectableStructs,
  usesType,
  validateStructForm,
} from "./structForm";

const row = (key: number, patch: Partial<FieldRow>): FieldRow => ({
  ...emptyRow(key),
  ...patch,
});

describe("parseBounds", () => {
  it("範囲を読む（3 次元まで）", () => {
    expect(parseBounds("0..9")).toEqual({
      ok: true,
      dims: [{ lower: 0, upper: 9 }],
    });
    expect(parseBounds(" 1 .. 3 ,0..4")).toEqual({
      ok: true,
      dims: [
        { lower: 1, upper: 3 },
        { lower: 0, upper: 4 },
      ],
    });
    expect(parseBounds("-2..2")).toEqual({
      ok: true,
      dims: [{ lower: -2, upper: 2 }],
    });
  });
  it("個数だけ書くと、0 からの範囲にする", () => {
    expect(parseBounds("10")).toEqual({
      ok: true,
      dims: [{ lower: 0, upper: 9 }],
    });
    expect(parseBounds("0").ok).toBe(false);
  });
  it("読めないものは理由を返す", () => {
    expect(parseBounds("").ok).toBe(false);
    expect(parseBounds("a..b").ok).toBe(false);
    expect(parseBounds("5..1").ok).toBe(false);
    expect(parseBounds("0..1,0..1,0..1,0..1").ok).toBe(false);
    expect(parseBounds("0..1,").ok).toBe(false);
  });
});

describe("rowDataType / rowFromField", () => {
  it("行からデータ型の文字列を作る", () => {
    expect(rowDataType(row(1, { type: "INT" }))).toBe("INT");
    expect(rowDataType(row(1, { type: "STRING", stringLength: 10 }))).toBe(
      "STRING[10]",
    );
    expect(
      rowDataType(row(1, { type: "DINT", isArray: true, bounds: "10" })),
    ).toBe("ARRAY[0..9] OF DINT");
    expect(
      rowDataType(
        row(1, {
          type: "STRING",
          stringLength: 4,
          isArray: true,
          bounds: "1..2, 0..3",
        }),
      ),
    ).toBe("ARRAY[1..2, 0..3] OF STRING[4]");
    expect(rowDataType(row(1, { isArray: true, bounds: "x" }))).toBeNull();
  });
  it("データ型の文字列を行にして、元に戻せる", () => {
    for (const dataType of [
      "INT",
      "STRING[10]",
      "Motor",
      "ARRAY[0..9] OF DINT",
      "ARRAY[1..2, 0..3] OF STRING[4]",
      "ARRAY[1..5] OF Motor",
    ]) {
      const r = rowFromField(7, "f", dataType);
      expect(r.key).toBe(7);
      expect(r.name).toBe("f");
      expect(rowDataType(r)).toBe(dataType);
    }
  });
  it("古い書き方の配列も読む（新しい書き方に直る）", () => {
    expect(rowDataType(rowFromField(1, "f", "ARRAY[INT;10]"))).toBe(
      "ARRAY[0..9] OF INT",
    );
    expect(rowDataType(rowFromField(1, "f", "ARRAY[ARRAY[INT;5];3]"))).toBe(
      "ARRAY[0..2, 0..4] OF INT",
    );
  });
});

describe("usesType / selectableStructs", () => {
  const types = [
    { name: "Inner", fields: [{ name: "x", dataType: "INT" }] },
    { name: "Mid", fields: [{ name: "i", dataType: "ARRAY[0..1] OF Inner" }] },
    { name: "Outer", fields: [{ name: "m", dataType: "Mid" }] },
    { name: "Other", fields: [{ name: "x", dataType: "INT" }] },
  ];
  it("入れ子・配列の要素として使っているかを見る", () => {
    expect(usesType("Outer", "Inner", types)).toBe(true);
    expect(usesType("Mid", "Inner", types)).toBe(true);
    expect(usesType("Inner", "Outer", types)).toBe(false);
    expect(usesType("Other", "Inner", types)).toBe(false);
  });
  it("自分と、自分を使っている型は選べない（循環するため）", () => {
    expect(selectableStructs("Inner", types).map((t) => t.name)).toEqual([
      "Other",
    ]);
    expect(selectableStructs("Outer", types).map((t) => t.name)).toEqual([
      "Inner",
      "Mid",
      "Other",
    ]);
    // 新しい型は、どれでも選べる
    expect(selectableStructs("", types)).toHaveLength(4);
  });
});

describe("validateStructForm", () => {
  const options = {
    isNew: true,
    existingNames: ["Motor"],
    scalarTypes: ["INT", "BOOL"],
  };
  it("正しければ、理由は 1 つもない", () => {
    const e = validateStructForm(
      "Pump",
      [row(1, { name: "a" }), row(2, { name: "" })],
      options,
    );
    expect(hasErrors(e)).toBe(false);
  });
  it("型名: 空・もうある・予約の名前", () => {
    const rows = [row(1, { name: "a" })];
    expect(validateStructForm(" ", rows, options).name).toBeTruthy();
    expect(validateStructForm("Motor", rows, options).name).toBeTruthy();
    expect(validateStructForm("INT", rows, options).name).toBeTruthy();
    expect(validateStructForm("STRING", rows, options).name).toBeTruthy();
    // 登録済みの型の編集では、型名は見ない
    expect(
      validateStructForm("Motor", rows, { ...options, isNew: false }).name,
    ).toBeUndefined();
  });
  it("フィールド: 1 つもない・名前が重なる・範囲とバイト長", () => {
    expect(validateStructForm("P", [row(1, {})], options).form).toBeTruthy();
    const e = validateStructForm(
      "P",
      [
        row(1, { name: "a" }),
        row(2, { name: " a " }),
        row(3, { name: "b", isArray: true, bounds: "9..0" }),
        row(4, { name: "c", type: "STRING", stringLength: 0 }),
        // 名前のない行は、範囲がおかしくても見ない（保存しないため）
        row(5, { name: "", isArray: true, bounds: "x" }),
      ],
      options,
    );
    expect(Object.keys(e.rows).map(Number)).toEqual([2, 3, 4]);
  });
});

describe("buildFields / layoutRows", () => {
  const rows = [
    row(1, { name: " label ", type: "STRING", stringLength: 10 }),
    row(2, { name: "" }),
    row(3, { name: "hist", type: "DINT", isArray: true, bounds: "0..9" }),
    row(4, { name: "bad", isArray: true, bounds: "x" }),
    row(5, { name: "speed" }),
  ];
  it("名前のある行から、登録するフィールドを作る", () => {
    expect(buildFields(rows.filter((r) => r.key !== 4))).toEqual([
      { name: "label", dataType: "STRING[10]" },
      { name: "hist", dataType: "ARRAY[0..9] OF DINT" },
      { name: "speed", dataType: "INT" },
    ]);
  });
  it("オフセットとワード数を計算する（名前のない行・読めない行は数えない）", () => {
    const words: Record<string, number> = {
      "STRING[10]": 5,
      "ARRAY[0..9] OF DINT": 20,
      INT: 1,
    };
    const layout = layoutRows(rows, (dt) => words[dt]);
    expect(layout.rows[1]).toEqual({ offset: 0, words: 5 });
    expect(layout.rows[2]).toEqual({ offset: null, words: null });
    expect(layout.rows[3]).toEqual({ offset: 5, words: 20 });
    expect(layout.rows[4]).toEqual({ offset: null, words: null });
    expect(layout.rows[5]).toEqual({ offset: 25, words: 1 });
    expect(layout.total).toBe(26);
  });
});

describe("moveRow / duplicateRow / nextFieldName", () => {
  const rows = [
    row(1, { name: "a" }),
    row(2, { name: "in1", type: "BOOL" }),
    row(3, { name: "c" }),
  ];
  const keys = (rs: FieldRow[]) => rs.map((r) => r.key);
  it("行を上下に動かす。端では何もしない", () => {
    expect(keys(moveRow(rows, 0, 1))).toEqual([2, 1, 3]);
    expect(keys(moveRow(rows, 2, -1))).toEqual([1, 3, 2]);
    expect(moveRow(rows, 0, -1)).toBe(rows);
    expect(moveRow(rows, 2, 1)).toBe(rows);
  });
  it("行を写して、すぐ下に入れる（名前は番号を進める）", () => {
    const next = duplicateRow(rows, 1, 9);
    expect(keys(next)).toEqual([1, 2, 9, 3]);
    expect(next[2]).toMatchObject({ name: "in2", type: "BOOL" });
  });
  it("まだない名前にする", () => {
    expect(nextFieldName("speed", ["speed"])).toBe("speed2");
    expect(nextFieldName("in1", ["in1", "in2"])).toBe("in3");
    expect(nextFieldName("", [])).toBe("");
  });
});
