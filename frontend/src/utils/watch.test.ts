import { describe, expect, it } from "vitest";
import {
  filterCandidates,
  isWritableValue,
  parseVariableInput,
  variableNameOf,
  variablePathLabel,
  visibleRows,
} from "./watch";

describe("isWritableValue", () => {
  it("数値・真偽・文字列は書ける。構造体・配列は書けない", () => {
    expect(isWritableValue(1)).toBe(true);
    expect(isWritableValue(false)).toBe(true);
    expect(isWritableValue("T#1s")).toBe(true);
    expect(isWritableValue([1, 2])).toBe(false);
    expect(isWritableValue({ speed: 1 })).toBe(false);
    expect(isWritableValue(undefined)).toBe(false);
  });
});

describe("parseVariableInput", () => {
  it("数値は 10 進・16 進・2 進・小数を読む", () => {
    expect(parseVariableInput(" 12 ", 0)).toEqual({ ok: true, value: 12 });
    expect(parseVariableInput("0xFF", 0)).toEqual({ ok: true, value: 255 });
    expect(parseVariableInput("0b101", 0)).toEqual({ ok: true, value: 5 });
    expect(parseVariableInput("-1.5", 0)).toEqual({ ok: true, value: -1.5 });
    expect(parseVariableInput("abc", 0).ok).toBe(false);
    expect(parseVariableInput("", 0).ok).toBe(false);
  });
  it("真偽は TRUE / FALSE / ON / OFF / 1 / 0", () => {
    expect(parseVariableInput("true", false)).toEqual({
      ok: true,
      value: true,
    });
    expect(parseVariableInput("ON", false)).toEqual({ ok: true, value: true });
    expect(parseVariableInput("0", true)).toEqual({ ok: true, value: false });
    expect(parseVariableInput("yes", true).ok).toBe(false);
  });
  it("文字列は、そのまま渡す（LINT の 10 進の文字列・前後の空白も）", () => {
    expect(parseVariableInput(" a ", "x")).toEqual({ ok: true, value: " a " });
    expect(parseVariableInput("9223372036854775807", "1")).toEqual({
      ok: true,
      value: "9223372036854775807",
    });
  });
  it("構造体・配列は書けない", () => {
    expect(parseVariableInput("1", [1]).ok).toBe(false);
  });
});

describe("filterCandidates", () => {
  const list = [
    { key: "1", label: "Counter", detail: "INT" },
    { key: "2", label: "Motor1", detail: "MotorType" },
    { key: "3", label: "ns=2;s=Temp" },
  ];
  it("名前か補足に含まれるものに絞る", () => {
    expect(filterCandidates(list, "")).toEqual(list);
    expect(filterCandidates(list, "motor").map((c) => c.key)).toEqual(["2"]);
    expect(filterCandidates(list, "int").map((c) => c.key)).toEqual(["1"]);
    expect(filterCandidates(list, "TEMP").map((c) => c.key)).toEqual(["3"]);
    expect(filterCandidates(list, "zzz")).toEqual([]);
  });
});

describe("visibleRows", () => {
  // Line: [5]{speed, limits{[1],[2]}}, [6]{speed}
  const rows = [
    { path: "[5]", depth: 1, composite: true },
    { path: "[5].speed", depth: 2 },
    { path: "[5].limits", depth: 2, composite: true },
    { path: "[5].limits[1]", depth: 3 },
    { path: "[5].limits[2]", depth: 3 },
    { path: "[6]", depth: 1, composite: true },
    { path: "[6].speed", depth: 2 },
  ];
  const shown = (collapsed: string[]) =>
    visibleRows(rows, (r) => collapsed.includes(r.path)).map((r) => r.path);

  it("畳んでいなければ、全部出す", () => {
    expect(shown([])).toEqual(rows.map((r) => r.path));
  });
  it("畳んだ行の中身（孫も）を除く。畳んだ行そのものと、次の行は出す", () => {
    expect(shown(["[5]"])).toEqual(["[5]", "[6]", "[6].speed"]);
    expect(shown(["[5].limits"])).toEqual([
      "[5]",
      "[5].speed",
      "[5].limits",
      "[6]",
      "[6].speed",
    ]);
    expect(shown(["[5]", "[6]"])).toEqual(["[5]", "[6]"]);
  });
  it("中で畳んだ行があっても、外を畳めば全部隠れる", () => {
    expect(shown(["[5]", "[5].limits"])).toEqual(["[5]", "[6]", "[6].speed"]);
  });
});

describe("variablePathLabel / variableNameOf", () => {
  it("変数名とパスをつなぐ", () => {
    expect(variablePathLabel("Motor1", "")).toBe("Motor1");
    expect(variablePathLabel("Motor1", "speed")).toBe("Motor1.speed");
    expect(variablePathLabel("Values", "[1].x")).toBe("Values[1].x");
  });
  it("項目の名前から変数名を取り出す", () => {
    expect(variableNameOf("Line[6]", "[6]")).toBe("Line");
    expect(variableNameOf("Motor1.speed", "speed")).toBe("Motor1");
    expect(variableNameOf("Motor1", "")).toBe("Motor1");
  });
});
