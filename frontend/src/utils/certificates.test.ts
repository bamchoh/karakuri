import { describe, expect, it } from "vitest";
import { formatDate, groupCertificates, shortThumbprint } from "./certificates";

const cert = (thumbprint: string, status: string) => ({ thumbprint, status });

describe("groupCertificates", () => {
  it("自分・信頼・拒否に、もとの順のまま分ける", () => {
    const g = groupCertificates([
      cert("A", "own"),
      cert("B", "rejected"),
      cert("C", "trusted"),
      cert("D", "rejected"),
      cert("E", "unknown"),
    ]);
    expect(g.own.map((c) => c.thumbprint)).toEqual(["A"]);
    expect(g.trusted.map((c) => c.thumbprint)).toEqual(["C"]);
    expect(g.rejected.map((c) => c.thumbprint)).toEqual(["B", "D"]);
  });
});

describe("formatDate", () => {
  it("日付にする", () => {
    expect(formatDate("2031-11-16T12:00:00Z")).toMatch(/^2031-11-1[67]$/);
  });
  it("読めなければそのまま", () => {
    expect(formatDate("いつか")).toBe("いつか");
  });
});

describe("shortThumbprint", () => {
  it("4 文字ずつ区切って、先頭 4 組", () => {
    expect(shortThumbprint("0123456789ABCDEF0123456789ABCDEF01234567")).toBe(
      "0123 4567 89AB CDEF …",
    );
    expect(shortThumbprint("ABCD")).toBe("ABCD");
  });
});
