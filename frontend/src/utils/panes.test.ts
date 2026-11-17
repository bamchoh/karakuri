import { describe, expect, it } from "vitest";
import {
  DEFAULT_LAYOUT,
  PaneLayout,
  clampRatio,
  focusPane,
  focusedPage,
  hiddenBy,
  parseLayout,
  showPage,
  toggleSplit,
} from "./panes";

const single: PaneLayout = {
  left: "server",
  right: null,
  focused: "left",
  ratio: 0.5,
};
const split: PaneLayout = {
  left: "scripts",
  right: "server",
  focused: "left",
  ratio: 0.4,
};

describe("showPage", () => {
  it("1 画面では、ページを替える", () => {
    expect(showPage(single, "scripts")).toEqual({ ...single, left: "scripts" });
  });
  it("2 画面では、最後に使った欄のページを替える", () => {
    expect(showPage(split, "variables")).toEqual({
      ...split,
      left: "variables",
    });
    const right = focusPane(split, "right");
    expect(showPage(right, "variables")).toEqual({
      ...right,
      right: "variables",
    });
  });
  it("もう出ているページは、その欄を最後に使った欄にするだけ（両方の欄には出さない）", () => {
    expect(showPage(split, "server")).toEqual({ ...split, focused: "right" });
    expect(showPage(split, "scripts")).toBe(split);
  });
});

describe("toggleSplit", () => {
  it("2 画面にすると、通信とスクリプトを組にする", () => {
    expect(toggleSplit(single)).toMatchObject({
      left: "server",
      right: "scripts",
    });
    expect(toggleSplit({ ...single, left: "scripts" })).toMatchObject({
      left: "scripts",
      right: "server",
    });
    expect(toggleSplit({ ...single, left: "variables" })).toMatchObject({
      left: "variables",
      right: "scripts",
    });
  });
  it("前に右に出していたページがあれば、それを出す（左と同じなら使わない）", () => {
    expect(toggleSplit(single, "variables").right).toBe("variables");
    expect(toggleSplit(single, "server").right).toBe("scripts");
  });
  it("1 画面に戻すと、最後に使った欄のページが残る", () => {
    expect(toggleSplit(split)).toEqual({
      left: "scripts",
      right: null,
      focused: "left",
      ratio: 0.4,
    });
    expect(toggleSplit(focusPane(split, "right"))).toMatchObject({
      left: "server",
      right: null,
      focused: "left",
    });
  });
});

describe("focusPane / focusedPage", () => {
  it("1 画面には右の欄がない", () => {
    expect(focusPane(single, "right")).toBe(single);
    expect(focusedPage({ ...single, focused: "right" })).toBe("server");
  });
  it("2 画面では、欄のページを返す", () => {
    expect(focusedPage(split)).toBe("scripts");
    expect(focusedPage(focusPane(split, "right"))).toBe("server");
  });
});

describe("hiddenBy", () => {
  it("並べ方を変えると隠れるページ", () => {
    expect(hiddenBy(single, showPage(single, "scripts"))).toEqual(["server"]);
    expect(hiddenBy(single, toggleSplit(single))).toEqual([]);
    expect(hiddenBy(split, toggleSplit(split))).toEqual(["server"]);
    expect(hiddenBy(split, showPage(split, "server"))).toEqual([]);
  });
});

describe("clampRatio", () => {
  it("どちらの欄も狭くなりすぎない範囲に収める", () => {
    expect(clampRatio(0.05)).toBe(0.2);
    expect(clampRatio(0.95)).toBe(0.8);
    expect(clampRatio(0.3)).toBe(0.3);
    expect(clampRatio(NaN)).toBe(0.5);
    expect(clampRatio("x")).toBe(0.5);
  });
});

describe("parseLayout", () => {
  it("保存した並べ方を読む", () => {
    expect(parseLayout(JSON.stringify(split))).toEqual(split);
  });
  it("読めない・おかしいところは、既定で補う", () => {
    expect(parseLayout(null)).toEqual(DEFAULT_LAYOUT);
    expect(parseLayout("{")).toEqual(DEFAULT_LAYOUT);
    expect(parseLayout("3")).toEqual(DEFAULT_LAYOUT);
    expect(
      parseLayout(
        JSON.stringify({
          left: "scripts",
          right: "scripts",
          focused: "right",
          ratio: 9,
        }),
      ),
    ).toEqual({ left: "scripts", right: null, focused: "left", ratio: 0.8 });
    expect(parseLayout(JSON.stringify({ left: "nope" })).left).toBe("server");
  });
});
