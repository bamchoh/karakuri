import { describe, expect, it } from "vitest";
import {
  addressPlaceholder,
  addressRangeError,
  addressRangeText,
  bitViewAreaOf,
  convertAddress,
  formatAddress,
  isBitDataType,
  isWordBitArea,
  mappingAreaFor,
  mappingRange,
  mappingWidthLabel,
  parseAddress,
  RangeArea,
  selectableAreas,
  selectedAreaId,
} from "./address";

// テストで使うエリア（MemoryAreaDTO のうち、アドレスの変換と範囲の検査に使う属性）
type TestArea = RangeArea & { id: string };

// Modbus の保持レジスタ（1 始まり）
const holding: TestArea = {
  id: "holdingRegisters",
  oneOrigin: true,
  size: 65536,
};
// Modbus のコイル（1 始まりのビットのエリア。ワードのエリアとは別のメモリ）
const coils: TestArea = {
  id: "coils",
  oneOrigin: true,
  isBit: true,
  size: 65536,
};
// FINS の CIO（0 始まり）と、同じメモリをビットごとに見るエリア
const cio: TestArea = { id: "CIO", size: 6144 };
const cioBit: TestArea = {
  id: "CIO_BIT",
  bitsOf: "CIO",
  isBit: true,
  size: 6144 * 16,
};
// FINS のタイマ（ビットごとに見るエリアがない）と、アップフラグ（別のメモリのビットのエリア）
const timer: TestArea = { id: "T", size: 4096 };
const timerFlag: TestArea = { id: "T_FLAG", isBit: true, size: 4096 };
const finsAreas = [cio, timer, cioBit, timerFlag];

describe("formatAddress / parseAddress", () => {
  it("0 始まりのエリアは、そのままの番号", () => {
    expect(formatAddress(cio, 0)).toBe("0");
    expect(formatAddress(cio, 100)).toBe("100");
    expect(parseAddress(cio, "100")).toBe(100);
    expect(parseAddress(cio, " 100 ")).toBe(100);
  });

  it("1 始まりのエリアは、表示が 1 多い", () => {
    expect(formatAddress(holding, 0)).toBe("1");
    expect(formatAddress(holding, 65535)).toBe("65536");
    expect(parseAddress(holding, "1")).toBe(0);
    expect(parseAddress(holding, "65536")).toBe(65535);
    // 1 始まりのエリアに 0 はない
    expect(parseAddress(holding, "0")).toBeNull();
  });

  it("「ワード.ビット」のエリアは、ワード × 16 + ビット", () => {
    expect(formatAddress(cioBit, 0)).toBe("0.00");
    expect(formatAddress(cioBit, 100 * 16 + 5)).toBe("100.05");
    expect(formatAddress(cioBit, 100 * 16 + 15)).toBe("100.15");
    expect(parseAddress(cioBit, "100.05")).toBe(1605);
    // ビットの頭の 0 は省ける。ビットを省くと 0 番
    expect(parseAddress(cioBit, "100.5")).toBe(1605);
    expect(parseAddress(cioBit, "100")).toBe(1600);
  });

  it("読めない入力は null", () => {
    for (const text of ["", "abc", "-1", "1.5", "0x10", "1 2"]) {
      expect(parseAddress(cio, text)).toBeNull();
    }
    for (const text of [
      "",
      "100.",
      ".5",
      "100.16",
      "100.005",
      "1.2.3",
      "a.b",
    ]) {
      expect(parseAddress(cioBit, text)).toBeNull();
    }
  });

  it("エリアが分からないときは、0 始まりとして扱う", () => {
    expect(formatAddress(undefined, 7)).toBe("7");
    expect(parseAddress(null, "7")).toBe(7);
  });

  it("表示と入力は、行って帰って同じになる", () => {
    for (const area of [cio, holding, cioBit]) {
      for (const address of [0, 1, 15, 16, 17, 1605, 6143]) {
        expect(parseAddress(area, formatAddress(area, address))).toBe(address);
      }
    }
  });
});

describe("エリアの選択肢", () => {
  it("isWordBitArea は、bitsOf のあるエリアだけ true", () => {
    expect(isWordBitArea(cioBit)).toBe(true);
    expect(isWordBitArea(cio)).toBe(false);
    expect(isWordBitArea(timerFlag)).toBe(false);
    expect(isWordBitArea(undefined)).toBe(false);
    expect(addressPlaceholder(cioBit)).toBe("100.05");
    expect(addressPlaceholder(cio)).toBe("");
  });

  it("selectableAreas は、ビットごとに見るエリアを除く（別のメモリのビットのエリアは残す）", () => {
    expect(selectableAreas(finsAreas).map((a) => a.id)).toEqual([
      "CIO",
      "T",
      "T_FLAG",
    ]);
    expect(selectableAreas([holding, coils]).map((a) => a.id)).toEqual([
      "holdingRegisters",
      "coils",
    ]);
  });

  it("bitViewAreaOf は、ワードのエリアをビットごとに見るエリアを返す", () => {
    expect(bitViewAreaOf(finsAreas, "CIO")?.id).toBe("CIO_BIT");
    expect(bitViewAreaOf(finsAreas, "T")).toBeUndefined();
    expect(bitViewAreaOf(finsAreas, "")).toBeUndefined();
    expect(bitViewAreaOf([holding, coils], "holdingRegisters")).toBeUndefined();
  });

  it("selectedAreaId は、ビットごとに見るエリアなら、もとのワードのエリア", () => {
    expect(selectedAreaId(finsAreas, "CIO_BIT")).toBe("CIO");
    expect(selectedAreaId(finsAreas, "CIO")).toBe("CIO");
    expect(selectedAreaId(finsAreas, "T_FLAG")).toBe("T_FLAG");
    expect(selectedAreaId(finsAreas, "なし")).toBe("なし");
  });
});

describe("mappingAreaFor（変数の型に合ったエリア）", () => {
  it("BOOL は、ビットごとに見るエリアがあればそれ", () => {
    expect(mappingAreaFor(finsAreas, "CIO", true)?.id).toBe("CIO_BIT");
    expect(mappingAreaFor(finsAreas, "CIO_BIT", true)?.id).toBe("CIO_BIT");
    // ビットごとに見るエリアがなければ、選んだエリアのまま
    expect(mappingAreaFor(finsAreas, "T", true)?.id).toBe("T");
    expect(mappingAreaFor(finsAreas, "T_FLAG", true)?.id).toBe("T_FLAG");
  });

  it("BOOL 以外は、ワードのエリア", () => {
    expect(mappingAreaFor(finsAreas, "CIO", false)?.id).toBe("CIO");
    expect(mappingAreaFor(finsAreas, "CIO_BIT", false)?.id).toBe("CIO");
    expect(mappingAreaFor(finsAreas, "T_FLAG", false)?.id).toBe("T_FLAG");
  });

  it("ないエリアは undefined", () => {
    expect(mappingAreaFor(finsAreas, "なし", true)).toBeUndefined();
  });
});

describe("convertAddress（エリアを変えても、同じワードを指す）", () => {
  it("ワード ↔ 「ワード.ビット」で 16 倍・16 分の 1", () => {
    expect(convertAddress(cio, cioBit, 100)).toBe(1600);
    expect(convertAddress(cioBit, cio, 1605)).toBe(100);
  });

  it("同じ書き方どうしなら変えない", () => {
    expect(convertAddress(cio, timer, 100)).toBe(100);
    expect(convertAddress(cioBit, cioBit, 1605)).toBe(1605);
    expect(convertAddress(undefined, cio, 5)).toBe(5);
  });
});

describe("mappingWidthLabel", () => {
  it("型で決まる大きさを返す", () => {
    expect(isBitDataType("BOOL")).toBe(true);
    expect(isBitDataType("INT")).toBe(false);
    expect(mappingWidthLabel("BOOL", 1)).toBe("1bit");
    expect(mappingWidthLabel("INT", 1)).toBe("16bit");
    expect(mappingWidthLabel("DINT", 2)).toBe("32bit");
    expect(mappingWidthLabel("LINT", 4)).toBe("64bit");
    expect(mappingWidthLabel("STRING[20]", 10)).toBe("10 ワード");
    expect(mappingWidthLabel("Motor", 3)).toBe("3 ワード");
  });
});

describe("アドレスの範囲", () => {
  it("addressRangeText は、画面の書き方で範囲を返す", () => {
    expect(addressRangeText(holding)).toBe("1〜65536");
    expect(addressRangeText(cio)).toBe("0〜6143");
    expect(addressRangeText(cioBit)).toBe("0.00〜6143.15");
    // 大きさが分からないエリアは、範囲を出さない
    expect(addressRangeText({ size: 0 })).toBe("");
    expect(addressRangeText(undefined)).toBe("");
  });

  it("addressRangeError は、収まれば null", () => {
    expect(addressRangeError(cio, 0)).toBeNull();
    expect(addressRangeError(cio, 6143)).toBeNull();
    expect(addressRangeError(cio, 6142, 2)).toBeNull();
    expect(addressRangeError(cioBit, 6143 * 16 + 15)).toBeNull();
  });

  it("先頭が範囲外なら、そのことを伝える", () => {
    expect(addressRangeError(cio, 6144)).toBe(
      "アドレスが範囲外です（範囲: 0〜6143）",
    );
    expect(addressRangeError(holding, 65536)).toBe(
      "アドレスが範囲外です（範囲: 1〜65536）",
    );
    expect(addressRangeError(cio, -1)).toContain("範囲外");
  });

  it("末尾だけが超えるなら、何個必要かを伝える", () => {
    expect(addressRangeError(cio, 6143, 2)).toBe(
      "末尾が範囲を超えます（6143 から 2 個。範囲: 0〜6143）",
    );
    // 表示は、エリアの書き方（1 始まり）
    expect(addressRangeError(holding, 65535, 2)).toBe(
      "末尾が範囲を超えます（65536 から 2 個。範囲: 1〜65536）",
    );
  });

  it("大きさが分からないエリアは、検査しない", () => {
    expect(addressRangeError({ size: 0 }, 99999)).toBeNull();
    expect(addressRangeError(undefined, 99999)).toBeNull();
  });
});

describe("mappingRange（マッピングの範囲の検査の対象）", () => {
  it("BOOL は 1 ビット", () => {
    expect(mappingRange(cioBit, "BOOL", 1)).toEqual({ area: cioBit, count: 1 });
    expect(mappingRange(coils, "BOOL", 1)).toEqual({ area: coils, count: 1 });
  });

  it("BOOL をワードのエリアに直接マッピングするときは、ビットの番号として見る", () => {
    const { area, count } = mappingRange(holding, "BOOL", 1);
    expect(count).toBe(1);
    expect(area?.size).toBe(65536 * 16);
    expect(addressRangeError(area, 65536 * 16 - 1, count)).toBeNull();
    expect(addressRangeError(area, 65536 * 16, count)).not.toBeNull();
  });

  it("BOOL 以外は、ワードのエリアならワード数", () => {
    expect(mappingRange(holding, "DINT", 2)).toEqual({
      area: holding,
      count: 2,
    });
    // ワード数が分からない（0）ときは 1 として扱う
    expect(mappingRange(holding, "?", 0).count).toBe(1);
  });

  it("BOOL 以外をビットのエリアに置くときは、ワード数 × 16 ビット", () => {
    expect(mappingRange(coils, "INT", 1).count).toBe(16);
    expect(mappingRange(coils, "DINT", 2).count).toBe(32);
  });

  it("エリアが分からなければ、検査の対象もない", () => {
    expect(mappingRange(undefined, "INT", 1)).toEqual({
      area: undefined,
      count: 1,
    });
  });
});
