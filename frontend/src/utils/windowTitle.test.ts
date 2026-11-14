import { describe, expect, it } from "vitest";
import { fileNameOf, windowTitle } from "./windowTitle";

describe("fileNameOf", () => {
  it("Windows のパスから、ファイル名を取り出す", () => {
    expect(fileNameOf("C:\\work\\plc\\line1.json")).toBe("line1.json");
  });

  it("/ で区切ったパスから、ファイル名を取り出す", () => {
    expect(fileNameOf("/home/user/line1.json")).toBe("line1.json");
  });

  it("区切りのない名前は、そのまま返す", () => {
    expect(fileNameOf("line1.json")).toBe("line1.json");
  });

  it("空なら空を返す", () => {
    expect(fileNameOf("")).toBe("");
  });

  it("区切りで終わるパスは、最後のフォルダ名を返す", () => {
    expect(fileNameOf("C:\\work\\plc\\")).toBe("plc");
  });
});

describe("windowTitle", () => {
  it("プロジェクトを開いていなければ、アプリの名前とバージョンだけ", () => {
    expect(windowTitle("v0.0.43", "")).toBe("Karakuri v0.0.43");
  });

  it("プロジェクトを開いていれば、ファイル名を先に出す", () => {
    expect(windowTitle("v0.0.43", "C:\\work\\line1.json")).toBe(
      "line1.json - Karakuri v0.0.43",
    );
  });
});
