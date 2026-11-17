import { test, expect, Locator, Page } from "@playwright/test";
import {
  addModbusServer,
  api,
  createVariable,
  dialogByTitle,
  field,
  getVariable,
  nextDialogMessage,
  openApp,
  openVariables,
  resetApp,
  shot,
  variableRow,
} from "./helpers";

// 変数画面の各ダイアログの動き。ダイアログを別コンポーネントに分ける前後で変わらないことを確かめる。

const MOTOR_TYPE = {
  name: "Motor",
  fields: [
    { name: "speed", dataType: "INT", offset: 0 },
    { name: "on", dataType: "BOOL", offset: 0 },
  ],
  wordCount: 0,
};

// ダイアログは、開いてから 100ms 以内の Enter を「ダイアログを開いた Enter」とみなして無視する。
// 人の操作ではそれより後になるので、Enter で確定するテストでは開いたあとに少し待つ
async function waitAfterOpen(page: Page) {
  await page.waitForTimeout(150);
}

async function openContextMenu(page: Page, name: string) {
  await variableRow(page, name).click({ button: "right" });
  const menu = page.locator(".context-menu");
  await expect(menu).toBeVisible();
  return menu;
}

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await openVariables(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

test.describe("変数追加ダイアログ", () => {
  test("スカラー型を初期値つきで追加し、フォームが初期状態に戻る", async ({
    page,
  }) => {
    await page.getByRole("button", { name: "変数を追加" }).click();
    const dialog = dialogByTitle(page, "変数を追加");
    await field(dialog, "変数名:").fill("Counter");
    await field(dialog, "データ型:").selectOption("DINT");
    await field(dialog, "初期値:").fill("42");
    await shot(page, "add-scalar");
    await dialog.getByRole("button", { name: "追加" }).click();
    await expect(dialog).toBeHidden();

    const row = variableRow(page, "Counter");
    await expect(row.locator("td.var-type")).toHaveText("DINT");
    await expect(row.locator("td.var-value")).toHaveText("42");

    await page.getByRole("button", { name: "変数を追加" }).click();
    await expect(field(dialog, "変数名:")).toHaveValue("");
    await expect(field(dialog, "データ型:")).toHaveValue("INT");
    await expect(field(dialog, "初期値:")).toHaveValue("");
  });

  test("キャンセルしても入力は残り、Esc でも閉じる", async ({ page }) => {
    await page.getByRole("button", { name: "変数を追加" }).click();
    const dialog = dialogByTitle(page, "変数を追加");
    await field(dialog, "変数名:").fill("Keep");
    await dialog.getByRole("button", { name: "キャンセル" }).click();
    await expect(dialog).toBeHidden();

    await page.getByRole("button", { name: "変数を追加" }).click();
    await expect(field(dialog, "変数名:")).toHaveValue("Keep");
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    expect(await getVariable(page, "Keep")).toBeUndefined();
  });

  // issue #25: 以前は、入力の直後（約 100ms 以内）の Enter が無視されていた
  test("Enter で確定でき、変数名が空なら何もしない", async ({ page }) => {
    await page.getByRole("button", { name: "変数を追加" }).click();
    const dialog = dialogByTitle(page, "変数を追加");
    await dialog.getByRole("button", { name: "追加" }).click();
    await expect(dialog).toBeVisible();

    // 入力の直後の Enter でも確定する
    await waitAfterOpen(page);
    await field(dialog, "変数名:").fill("ByEnter");
    await field(dialog, "変数名:").press("Enter");
    await expect(dialog).toBeHidden();
    await expect(variableRow(page, "ByEnter")).toBeVisible();
  });

  test("STRING はバイト長つきの型になる", async ({ page }) => {
    await page.getByRole("button", { name: "変数を追加" }).click();
    const dialog = dialogByTitle(page, "変数を追加");
    await field(dialog, "変数名:").fill("Label");
    await field(dialog, "データ型:").selectOption("STRING");
    await field(dialog, "バイト長:").fill("8");
    await field(dialog, "初期値:").fill("abc");
    await dialog.getByRole("button", { name: "追加" }).click();

    const row = variableRow(page, "Label");
    await expect(row.locator("td.var-type")).toHaveText("STRING[8]");
    await expect(row.locator("td.var-value")).toHaveText('"abc"');
  });

  test("多次元配列を追加できる", async ({ page }) => {
    await page.getByRole("button", { name: "変数を追加" }).click();
    const dialog = dialogByTitle(page, "変数を追加");
    await field(dialog, "変数名:").fill("Grid");
    await field(dialog, "型カテゴリ:").selectOption("array");
    await field(dialog, "要素型:").selectOption("REAL");
    await field(dialog, "次元数:").selectOption("2");
    await field(dialog, "開始インデックス(1次元目):").fill("1");
    await field(dialog, "終了インデックス(1次元目):").fill("2");
    await field(dialog, "開始インデックス(2次元目):").fill("0");
    await field(dialog, "終了インデックス(2次元目):").fill("1");
    await shot(page, "add-array");
    await dialog.getByRole("button", { name: "追加" }).click();

    const row = variableRow(page, "Grid");
    await expect(row.locator("td.var-type")).toHaveText(
      "ARRAY[1..2, 0..1] OF REAL",
    );
    await expect(row.locator("td.var-value")).toHaveText("(2 要素)");
    expect((await getVariable(page, "Grid")).value).toEqual([
      [0, 0],
      [0, 0],
    ]);
  });

  test("構造体型は、型が登録されているときだけ選べる", async ({ page }) => {
    await page.getByRole("button", { name: "変数を追加" }).click();
    let dialog = dialogByTitle(page, "変数を追加");
    await expect(field(dialog, "型カテゴリ:").locator("option")).toHaveText([
      "スカラー型",
      "配列型",
    ]);
    await dialog.getByRole("button", { name: "キャンセル" }).click();

    await api(page, "RegisterStructType", MOTOR_TYPE);
    await page.reload();
    await openApp(page);
    await openVariables(page);

    await page.getByRole("button", { name: "変数を追加" }).click();
    dialog = dialogByTitle(page, "変数を追加");
    await field(dialog, "変数名:").fill("M1");
    await field(dialog, "型カテゴリ:").selectOption("struct");
    await expect(field(dialog, "構造体型:")).toHaveValue("Motor");
    await dialog.getByRole("button", { name: "追加" }).click();

    await expect(variableRow(page, "M1").locator("td.var-type")).toHaveText(
      "Motor",
    );
    expect((await getVariable(page, "M1")).value).toEqual({
      speed: 0,
      on: false,
    });
  });
});

test.describe("構造体型管理ダイアログ", () => {
  // 右の表の、フィールドの行と、その中の入力欄
  const rows = (dialog: Locator) => dialog.getByTestId("struct-field-row");
  const nameOf = (dialog: Locator, n: number) =>
    dialog.getByLabel(`フィールド ${n} の名前`);
  const typeOf = (dialog: Locator, n: number) =>
    dialog.getByLabel(`フィールド ${n} の型`);
  const arrayOf = (dialog: Locator, n: number) =>
    dialog.getByLabel(`フィールド ${n} を配列にする`);
  const boundsOf = (dialog: Locator, n: number) =>
    dialog.getByLabel(`フィールド ${n} の配列の範囲`);
  const typeItem = (dialog: Locator, name: string) =>
    dialog.getByTestId("struct-type-list").locator(".struct-type-item", {
      has: dialog.page().locator(".struct-type-name", {
        hasText: new RegExp(`^${name}$`),
      }),
    });
  const openDialog = async (page: Page) => {
    await page.getByRole("button", { name: "構造体型管理" }).click();
    const dialog = dialogByTitle(page, "構造体型管理");
    await expect(dialog).toBeVisible();
    return dialog;
  };

  test("フィールドを 1 行ずつ入れて登録し、編集・削除できる", async ({
    page,
  }) => {
    const dialog = await openDialog(page);
    const typeName = dialog.getByLabel("型名");
    const save = dialog.getByRole("button", { name: "型を登録" });

    // 何も入れていない間は、登録できない
    await expect(save).toBeDisabled();
    await expect(rows(dialog)).toHaveCount(1);

    // 型名だけでは登録できず、理由が出る
    await typeName.fill("Pump");
    await save.click();
    await expect(dialog.locator(".struct-error")).toHaveText(
      "フィールドを 1 つ以上入れてください",
    );

    // 1 行目: STRING[10]。名前の欄で Enter を押すと、次の行ができてそこに移る
    await nameOf(dialog, 1).fill("label");
    await typeOf(dialog, 1).selectOption("STRING");
    await dialog.getByLabel("フィールド 1 のバイト長").fill("10");
    await nameOf(dialog, 1).press("Enter");
    await expect(rows(dialog)).toHaveCount(2);
    await expect(nameOf(dialog, 2)).toBeFocused();

    // 2 行目: DINT の配列。範囲は 1 つの欄に書く（個数だけでもよい）
    await page.keyboard.type("hist");
    await typeOf(dialog, 2).selectOption("DINT");
    await arrayOf(dialog, 2).check();
    await expect(boundsOf(dialog, 2)).toHaveValue("0..9");
    await boundsOf(dialog, 2).fill("5");

    // 位置とワード数、合計が出る（STRING[10] は 5 ワード、DINT 5 個は 10 ワード）
    await expect(rows(dialog).nth(1).locator(".col-num")).toHaveText([
      "5",
      "10",
    ]);
    await expect(dialog.getByTestId("struct-total")).toHaveText(
      "合計 15 ワード",
    );

    // 範囲が読めないと、その行に理由を出して登録しない
    await boundsOf(dialog, 2).fill("9..0");
    await save.click();
    await expect(
      rows(dialog).nth(1).locator(".struct-row-error"),
    ).toBeVisible();
    expect(await api<unknown[]>(page, "GetStructTypes")).toEqual([]);
    await boundsOf(dialog, 2).fill("0..9");
    await shot(page, "struct-new");
    await save.click();

    // 登録すると、左の一覧に出て、その型を選んだ状態になる
    await expect(typeItem(dialog, "Pump")).toHaveClass(/selected/);
    await expect(typeItem(dialog, "Pump")).toContainText("25W");
    await expect(typeName).toHaveValue("Pump");
    await expect(typeName).toBeDisabled();
    expect(
      (await api<any[]>(page, "GetStructTypes"))[0].fields.map(
        (f: any) => `${f.name}:${f.dataType}`,
      ),
    ).toEqual(["label:STRING[10]", "hist:ARRAY[0..9] OF DINT"]);

    // 編集: 2 次元にして保存する。変えるまでは「保存」「元に戻す」は押せない
    const update = dialog.getByRole("button", { name: "保存" });
    const revert = dialog.getByRole("button", { name: "元に戻す" });
    await expect(update).toBeDisabled();
    await expect(revert).toBeDisabled();
    await boundsOf(dialog, 2).fill("0..9, 0..4");
    await shot(page, "struct-edit");
    await update.click();
    await expect(typeItem(dialog, "Pump")).toContainText("105W");

    // 元に戻す: 保存していない変更を捨てる
    await nameOf(dialog, 1).fill("renamed");
    await revert.click();
    await expect(nameOf(dialog, 1)).toHaveValue("label");

    // 削除は確認してから
    const confirm = nextDialogMessage(page);
    await dialog.getByRole("button", { name: "この型を削除" }).click();
    expect(await confirm).toContain('構造体型 "Pump" を削除しますか');
    await expect(typeItem(dialog, "Pump")).toHaveCount(0);
    expect(await api<unknown[]>(page, "GetStructTypes")).toEqual([]);

    await dialog.getByRole("button", { name: "閉じる" }).click();
    await expect(dialog).toBeHidden();
  });

  test("行を写す・並べ替える・消す", async ({ page }) => {
    const dialog = await openDialog(page);
    await dialog.getByLabel("型名").fill("IO");
    await nameOf(dialog, 1).fill("in1");
    await typeOf(dialog, 1).selectOption("BOOL");

    // 写すと、すぐ下に、番号を進めた名前で入る（型も同じ）
    await dialog.getByLabel("フィールド 1 を写す").click();
    await expect(nameOf(dialog, 2)).toHaveValue("in2");
    await expect(typeOf(dialog, 2)).toHaveValue("BOOL");
    await expect(nameOf(dialog, 2)).toBeFocused();

    await dialog
      .getByRole("button", { name: "フィールド", exact: true })
      .click();
    await nameOf(dialog, 3).fill("count");

    // 下へ・上へ（ボタンと Alt + 矢印）
    await dialog.getByLabel("フィールド 1 を下へ").click();
    await expect(nameOf(dialog, 1)).toHaveValue("in2");
    await expect(nameOf(dialog, 2)).toHaveValue("in1");
    await nameOf(dialog, 3).press("Alt+ArrowUp");
    await expect(nameOf(dialog, 2)).toHaveValue("count");
    await expect(nameOf(dialog, 2)).toBeFocused();
    await expect(dialog.getByLabel("フィールド 1 を上へ")).toBeDisabled();
    await expect(dialog.getByLabel("フィールド 3 を下へ")).toBeDisabled();

    // 消す
    await dialog.getByLabel("フィールド 1 を消す").click();
    await expect(rows(dialog)).toHaveCount(2);

    // Ctrl + Enter で保存
    await nameOf(dialog, 1).press("Control+Enter");
    await expect(typeItem(dialog, "IO")).toHaveClass(/selected/);
    expect(
      (await api<any[]>(page, "GetStructTypes"))[0].fields.map(
        (f: any) => `${f.name}:${f.dataType}`,
      ),
    ).toEqual(["count:INT", "in1:BOOL"]);
  });

  test("フィールドの型に他の構造体型を選べるが、自分と、自分を使っている型は選べない", async ({
    page,
  }) => {
    await api(page, "RegisterStructType", MOTOR_TYPE);
    await page.reload();
    await openApp(page);
    await openVariables(page);

    // 型があるときは、先頭の型を選んだ状態で開く
    const dialog = await openDialog(page);
    await expect(typeItem(dialog, "Motor")).toHaveClass(/selected/);
    await expect(nameOf(dialog, 1)).toHaveValue("speed");
    await expect(nameOf(dialog, 2)).toHaveValue("on");

    // 新しい型 Line: Motor と、Motor の 2 次元配列
    await dialog.getByRole("button", { name: "新しい型" }).click();
    await dialog.getByLabel("型名").fill("Line");
    await nameOf(dialog, 1).fill("main");
    await typeOf(dialog, 1).selectOption("Motor");
    await nameOf(dialog, 1).press("Enter");
    await nameOf(dialog, 2).fill("spares");
    await typeOf(dialog, 2).selectOption("Motor");
    await arrayOf(dialog, 2).check();
    await boundsOf(dialog, 2).fill("0..9, 0..4");
    await dialog.getByRole("button", { name: "型を登録" }).click();
    await expect(typeItem(dialog, "Line")).toHaveClass(/selected/);
    expect(
      (await api<any[]>(page, "GetStructTypes"))
        .find((t: any) => t.name === "Line")
        .fields.map((f: any) => `${f.name}:${f.dataType}`),
    ).toEqual(["main:Motor", "spares:ARRAY[0..9, 0..4] OF Motor"]);

    // Line のフィールドには、Line 自身は選べない
    const structOptions = (n: number) =>
      typeOf(dialog, n).locator('optgroup[label="構造体"] option');
    await expect(structOptions(1)).toHaveText(["Motor"]);

    // Motor のフィールドには、Motor を使っている Line も選べない（循環するため）
    await typeItem(dialog, "Motor").click();
    await expect(nameOf(dialog, 1)).toHaveValue("speed");
    await expect(structOptions(1)).toHaveCount(0);
  });

  test("変数が使っている型も変えられ、名前の同じフィールドの値は残る", async ({
    page,
  }) => {
    await api(page, "RegisterStructType", MOTOR_TYPE);
    await createVariable(page, "M1", "Motor", { speed: 120, on: true });
    await page.reload();
    await openApp(page);
    await openVariables(page);

    const dialog = await openDialog(page);
    // speed を DINT に広げ、limit を足す
    await typeOf(dialog, 1).selectOption("DINT");
    await dialog
      .getByRole("button", { name: "フィールド", exact: true })
      .click();
    await nameOf(dialog, 3).fill("limit");
    await dialog.getByRole("button", { name: "保存" }).click();
    await expect(typeItem(dialog, "Motor")).toContainText("4W");
    await expect(dialog.locator(".struct-error")).toHaveCount(0);

    expect((await getVariable(page, "M1")).value).toEqual({
      speed: 120,
      on: true,
      limit: 0,
    });

    // 使っている型は、削除できない（理由をダイアログの中に出す）
    const confirm = nextDialogMessage(page);
    await dialog.getByRole("button", { name: "この型を削除" }).click();
    await confirm;
    await expect(dialog.locator(".struct-error")).toContainText("M1");
    await expect(typeItem(dialog, "Motor")).toBeVisible();
  });

  test("保存していない変更があると、閉じる・型を選び直すときに確かめる", async ({
    page,
  }) => {
    await api(page, "RegisterStructType", MOTOR_TYPE);
    await page.reload();
    await openApp(page);
    await openVariables(page);

    // 変更がなければ、確かめずに閉じる（Esc でも）
    let dialog = await openDialog(page);
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();

    // 変更があれば確かめる。やめれば、そのまま
    dialog = await openDialog(page);
    await nameOf(dialog, 1).fill("velocity");
    page.once("dialog", (d) => d.dismiss());
    await dialog.getByRole("button", { name: "新しい型" }).click();
    await expect(nameOf(dialog, 1)).toHaveValue("velocity");
    page.once("dialog", (d) => d.dismiss());
    await dialog.getByRole("button", { name: "閉じる" }).click();
    await expect(dialog).toBeVisible();

    // 捨てると閉じ、開き直すと元の定義
    const confirm = nextDialogMessage(page);
    await dialog.getByRole("button", { name: "閉じる" }).click();
    expect(await confirm).toContain("保存していない変更");
    await expect(dialog).toBeHidden();
    dialog = await openDialog(page);
    await expect(nameOf(dialog, 1)).toHaveValue("speed");
  });
});

test.describe("変数の編集（名前・データ型）ダイアログ", () => {
  test("右クリックメニューから開き、現在の定義が入っている", async ({
    page,
  }) => {
    await createVariable(page, "Label", "STRING[8]", "abc");
    await createVariable(page, "Grid", "ARRAY[1..2, 0..1] OF REAL", [
      [0, 0],
      [0, 0],
    ]);

    await (
      await openContextMenu(page, "Label")
    )
      .getByRole("button", { name: "編集" })
      .click();
    const dialog = dialogByTitle(page, "変数を編集");
    await expect(field(dialog, "変数名:")).toHaveValue("Label");
    await expect(field(dialog, "型カテゴリ:")).toHaveValue("scalar");
    await expect(field(dialog, "データ型:")).toHaveValue("STRING");
    await expect(field(dialog, "バイト長:")).toHaveValue("8");
    await dialog.getByRole("button", { name: "キャンセル" }).click();
    await expect(dialog).toBeHidden();

    await (
      await openContextMenu(page, "Grid")
    )
      .getByRole("button", { name: "編集" })
      .click();
    await expect(field(dialog, "型カテゴリ:")).toHaveValue("array");
    await expect(field(dialog, "要素型:")).toHaveValue("REAL");
    await expect(field(dialog, "次元数:")).toHaveValue("2");
    await expect(field(dialog, "開始インデックス(1次元目):")).toHaveValue("1");
    await expect(field(dialog, "終了インデックス(2次元目):")).toHaveValue("1");
    await shot(page, "meta-edit-array");
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
  });

  test("構造体型・STRING の配列も、現在の定義がフォームに入る", async ({
    page,
  }) => {
    await api(page, "RegisterStructType", MOTOR_TYPE);
    await page.reload();
    await openApp(page);
    await openVariables(page);
    await createVariable(page, "M1", "Motor", { speed: 0, on: false });
    await createVariable(page, "Names", "ARRAY[1..2] OF STRING[6]", ["", ""]);
    const dialog = dialogByTitle(page, "変数を編集");

    await (
      await openContextMenu(page, "M1")
    )
      .getByRole("button", { name: "編集" })
      .click();
    await expect(field(dialog, "型カテゴリ:")).toHaveValue("struct");
    await expect(field(dialog, "構造体型:")).toHaveValue("Motor");
    await page.keyboard.press("Escape");

    await (
      await openContextMenu(page, "Names")
    )
      .getByRole("button", { name: "編集" })
      .click();
    await expect(field(dialog, "型カテゴリ:")).toHaveValue("array");
    await expect(field(dialog, "要素型:")).toHaveValue("STRING");
    await expect(field(dialog, "バイト長:")).toHaveValue("6");
    await expect(field(dialog, "開始インデックス(1次元目):")).toHaveValue("1");
    await expect(field(dialog, "終了インデックス(1次元目):")).toHaveValue("2");
    // そのまま保存しても、データ型は変わらない
    await dialog.getByRole("button", { name: "保存" }).click();
    await expect(dialog).toBeHidden();
    await expect(variableRow(page, "Names").locator("td.var-type")).toHaveText(
      "ARRAY[1..2] OF STRING[6]",
    );
  });

  test("名前とデータ型を変更できる（値は初期値に戻る）", async ({ page }) => {
    await createVariable(page, "Old", "INT", 7);
    await (
      await openContextMenu(page, "Old")
    )
      .getByRole("button", { name: "編集" })
      .click();
    const dialog = dialogByTitle(page, "変数を編集");
    await field(dialog, "変数名:").fill("New");
    await field(dialog, "型カテゴリ:").selectOption("array");
    await field(dialog, "要素型:").selectOption("BOOL");
    await field(dialog, "終了インデックス(1次元目):").fill("2");
    await dialog.getByRole("button", { name: "保存" }).click();
    await expect(dialog).toBeHidden();

    await expect(variableRow(page, "New").locator("td.var-type")).toHaveText(
      "ARRAY[0..2] OF BOOL",
    );
    expect((await getVariable(page, "New")).value).toEqual([
      false,
      false,
      false,
    ]);
    expect(await getVariable(page, "Old")).toBeUndefined();
  });

  test("右クリックメニューから削除できる（確認あり）", async ({ page }) => {
    await createVariable(page, "Gone");
    const menu = await openContextMenu(page, "Gone");
    const confirm = nextDialogMessage(page);
    await menu.getByRole("button", { name: "削除" }).click();
    expect(await confirm).toContain('変数 "Gone" を削除しますか');
    await expect(variableRow(page, "Gone")).toHaveCount(0);
  });
});

test.describe("値を編集ダイアログ", () => {
  test("数値は 16 進でも入力でき、Enter で確定できる", async ({ page }) => {
    await createVariable(page, "Counter", "INT", 1);
    await variableRow(page, "Counter").locator("td.var-value").click();
    const dialog = dialogByTitle(page, "値を編集");
    await expect(dialog.locator(".dialog-value").nth(0)).toHaveText("Counter");
    await expect(dialog.locator(".dialog-value").nth(1)).toHaveText("INT");
    const input = dialog.locator("input");
    await expect(input).toBeFocused();
    await shot(page, "value-edit-scalar");
    await waitAfterOpen(page);
    await input.fill("0x10");
    await input.press("Enter");
    await expect(dialog).toBeHidden();
    await expect(
      variableRow(page, "Counter").locator("td.var-value"),
    ).toHaveText("16");
  });

  test("BOOL・REAL・STRING を編集できる", async ({ page }) => {
    await createVariable(page, "Flag", "BOOL", false);
    await createVariable(page, "Temp", "REAL", 0);
    await createVariable(page, "Label", "STRING[4]", "");
    const dialog = dialogByTitle(page, "値を編集");

    await variableRow(page, "Flag").locator("td.var-value").click();
    await dialog.locator("select").selectOption("true");
    await dialog.getByRole("button", { name: "更新" }).click();
    await expect(variableRow(page, "Flag").locator("td.var-value")).toHaveText(
      "TRUE",
    );

    await variableRow(page, "Temp").locator("td.var-value").click();
    await dialog.locator("input").fill("1.5");
    await dialog.getByRole("button", { name: "更新" }).click();
    await expect(variableRow(page, "Temp").locator("td.var-value")).toHaveText(
      "1.5",
    );

    // STRING[4] は 4 文字までしか入らない
    await variableRow(page, "Label").locator("td.var-value").click();
    await dialog.locator("input").fill("abcdef");
    await expect(dialog.locator("input")).toHaveValue("abcd");
    await dialog.getByRole("button", { name: "更新" }).click();
    await expect(variableRow(page, "Label").locator("td.var-value")).toHaveText(
      '"abcd"',
    );
  });

  test("構造体のフィールドと配列の要素を、行ごとに編集できる", async ({
    page,
  }) => {
    await api(page, "RegisterStructType", MOTOR_TYPE);
    await page.reload();
    await openApp(page);
    await openVariables(page);
    await createVariable(page, "M1", "Motor", { speed: 1, on: false });
    await createVariable(page, "Values", "ARRAY[0..2] OF DINT", [1, 2, 3]);
    const dialog = dialogByTitle(page, "値を編集");

    // 構造体: 見出し行をクリックして展開し、フィールドの行を編集
    await variableRow(page, "M1").locator("td.var-name").click();
    const speedRow = variableRow(page, "M1.speed");
    await expect(speedRow.locator("td.var-value")).toHaveText("1");
    await speedRow.locator("td.var-value").click();
    await expect(dialog.locator(".dialog-value").nth(1)).toHaveText("INT");
    await dialog.locator("input").fill("250");
    await dialog.getByRole("button", { name: "更新" }).click();
    await expect(speedRow.locator("td.var-value")).toHaveText("250");
    expect((await getVariable(page, "M1")).value).toEqual({
      speed: 250,
      on: false,
    });

    // 配列: 要素の行を編集
    await variableRow(page, "Values").locator("td.var-name").click();
    await shot(page, "rows-expanded");
    const elem = variableRow(page, "Values[1]").locator("td.var-value");
    await expect(elem).toHaveText("2");
    await elem.click();
    await dialog.locator("input").fill("20");
    await dialog.getByRole("button", { name: "更新" }).click();
    expect((await getVariable(page, "Values")).value).toEqual([1, 20, 3]);
  });

  // issue #24: 以前は、開始インデックスの分だけずれた要素に書き込まれていた
  test("開始インデックスが 0 でない配列でも、編集した行の要素に書き込む", async ({
    page,
  }) => {
    await api(page, "RegisterStructType", {
      name: "Rec",
      fields: [{ name: "hist", dataType: "ARRAY[2..3] OF INT", offset: 0 }],
      wordCount: 0,
    });
    await page.reload();
    await openApp(page);
    await openVariables(page);
    await createVariable(page, "Values", "ARRAY[1..3] OF DINT", [1, 2, 3]);
    await createVariable(page, "Grid", "ARRAY[1..2, 5..6] OF INT", [
      [1, 2],
      [3, 4],
    ]);
    await createVariable(page, "Recs", "ARRAY[10..11] OF Rec", [
      { hist: [1, 2] },
      { hist: [3, 4] },
    ]);
    const dialog = dialogByTitle(page, "値を編集");
    const edit = async (rowName: string, value: string) => {
      const cell = variableRow(page, rowName).locator("td.var-value");
      await cell.click();
      await expect(dialog.locator(".dialog-value").nth(0)).toHaveText(rowName);
      await dialog.locator("input").fill(value);
      await dialog.getByRole("button", { name: "更新" }).click();
      await expect(dialog).toBeHidden();
      await expect(cell).toHaveText(value);
    };
    const expand = (rowName: string) =>
      variableRow(page, rowName).locator("td.var-name").click();

    // 1 次元: 最後の要素（Values[3]）
    await expand("Values");
    await edit("Values[3]", "30");
    expect((await getVariable(page, "Values")).value).toEqual([1, 2, 30]);

    // 2 次元: Grid[2][5] は 2 行目の先頭
    await expand("Grid");
    await expand("Grid[2]");
    await edit("Grid[2][5]", "50");
    expect((await getVariable(page, "Grid")).value).toEqual([
      [1, 2],
      [50, 4],
    ]);

    // 構造体の配列の中の配列: Recs[11].hist[3] は最後の要素
    await expand("Recs");
    await expand("Recs[11]");
    await expand("Recs[11].hist");
    await edit("Recs[11].hist[3]", "99");
    expect((await getVariable(page, "Recs")).value).toEqual([
      { hist: [1, 2] },
      { hist: [3, 99] },
    ]);
  });

  test("見出し行からは変数全体を編集でき、キャンセルと Esc では変わらない", async ({
    page,
  }) => {
    await api(page, "RegisterStructType", MOTOR_TYPE);
    await page.reload();
    await openApp(page);
    await openVariables(page);
    await createVariable(page, "Motors", "ARRAY[0..1] OF Motor", [
      { speed: 1, on: false },
      { speed: 2, on: false },
    ]);
    const dialog = dialogByTitle(page, "値を編集");
    const valueCell = variableRow(page, "Motors").locator("td.var-value");

    await valueCell.click();
    await expect(dialog.locator(".dialog-value").nth(1)).toHaveText(
      "ARRAY[0..1] OF Motor",
    );
    // 構造体の配列は要素ごとに折りたたまれている。2 つ目を開いて speed を変える
    await dialog
      .getByText("[1]")
      .locator("xpath=..")
      .locator("div")
      .first()
      .click();
    await shot(page, "value-edit-struct-array");
    const inputs = dialog.locator("input");
    await expect(inputs).toHaveCount(1);
    await inputs.fill("99");
    await dialog.getByRole("button", { name: "キャンセル" }).click();
    await expect(dialog).toBeHidden();
    expect((await getVariable(page, "Motors")).value[1].speed).toBe(2);

    await valueCell.click();
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();

    await valueCell.click();
    await dialog
      .getByText("[1]")
      .locator("xpath=..")
      .locator("div")
      .first()
      .click();
    await dialog.locator("input").fill("99");
    await dialog.getByRole("button", { name: "更新" }).click();
    await expect(dialog).toBeHidden();
    expect((await getVariable(page, "Motors")).value).toEqual([
      { speed: 1, on: false },
      { speed: 99, on: false },
    ]);
  });
});

test.describe("マッピング設定ダイアログ（レジスタ型）", () => {
  test("マッピングを追加・保存・削除でき、アドレスは 1 オリジンで表示する", async ({
    page,
  }) => {
    await addModbusServer(page);
    await createVariable(page, "Speed");
    await page.reload();
    await openApp(page);
    await openVariables(page);

    const cell = variableRow(page, "Speed").locator("td.var-mapping");
    await cell.click();
    const dialog = dialogByTitle(page, "マッピング設定: Speed");
    await dialog.getByRole("button", { name: "+ マッピング追加" }).click();
    const selects = dialog.locator("select");
    await expect(selects.nth(0)).toHaveValue("modbus-tcp");
    // 内部アドレス 0 は 1 と表示する
    const address = dialog.locator('input[type="number"]');
    await expect(address).toHaveValue("1");
    await selects.nth(1).selectOption("holdingRegisters");
    await address.fill("5");
    await selects.nth(2).selectOption("little");
    await shot(page, "mapping-register");
    await dialog.getByRole("button", { name: "保存" }).click();
    await expect(dialog).toBeHidden();
    await expect(cell).toHaveText("modbus-tcp:4:5");
    expect((await getVariable(page, "Speed")).mappings).toEqual([
      {
        protocolType: "modbus-tcp",
        memoryArea: "holdingRegisters",
        address: 4,
        endianness: "little",
      },
    ]);

    // 開き直すと保存した内容が入っている。X で削除して保存
    await cell.click();
    await expect(dialog.locator('input[type="number"]')).toHaveValue("5");
    await dialog.getByRole("button", { name: "X", exact: true }).click();
    await dialog.getByRole("button", { name: "保存" }).click();
    await expect(cell).toHaveText("-");
  });

  test("他の変数と重なるアドレスは警告し、一覧にも印が出る", async ({
    page,
  }) => {
    await addModbusServer(page);
    const a = await createVariable(page, "A", "DINT");
    await createVariable(page, "B");
    await api(page, "UpdateVariableMappings", a.id, [
      {
        protocolType: "modbus-tcp",
        memoryArea: "holdingRegisters",
        address: 10,
        endianness: "big",
      },
    ]);
    await page.reload();
    await openApp(page);
    await openVariables(page);

    await variableRow(page, "B").locator("td.var-mapping").click();
    const dialog = dialogByTitle(page, "マッピング設定: B");
    await dialog.getByRole("button", { name: "+ マッピング追加" }).click();
    await dialog.locator("select").nth(1).selectOption("holdingRegisters");
    // A は DINT（2 ワード）なので、内部アドレス 10〜11 = 表示 11〜12 を使っている
    await dialog.locator('input[type="number"]').fill("12");
    await expect(dialog.locator(".mapping-conflict-warning")).toHaveText(
      "⚠ 以下の変数と重複しています: A",
    );
    await shot(page, "mapping-conflict");
    await dialog.locator('input[type="number"]').fill("13");
    await expect(dialog.locator(".mapping-conflict-warning")).toHaveCount(0);
    await dialog.locator('input[type="number"]').fill("12");
    await dialog.getByRole("button", { name: "保存" }).click();

    await expect(
      variableRow(page, "B").locator(".mapping-conflict-icon"),
    ).toHaveAttribute("title", "以下の変数と重複: A");
    await expect(
      variableRow(page, "A").locator(".mapping-conflict-icon"),
    ).toHaveAttribute("title", "以下の変数と重複: B");
  });
});

test.describe("一括マッピング編集ダイアログ（レジスタ型）", () => {
  test("アドレスをまとめて設定でき、重複は警告し、空欄はマッピングを消す", async ({
    page,
  }) => {
    await addModbusServer(page);
    const a = await createVariable(page, "A", "DINT");
    await createVariable(page, "B");
    await api(page, "UpdateVariableMappings", a.id, [
      {
        protocolType: "modbus-tcp",
        memoryArea: "holdingRegisters",
        address: 0,
        endianness: "big",
      },
    ]);
    await page.reload();
    await openApp(page);
    await openVariables(page);

    await page.getByRole("button", { name: "一括マッピング編集" }).click();
    const dialog = dialogByTitle(page, "一括マッピング編集");
    const row = (name: string) =>
      dialog.locator("tbody tr", {
        has: page.locator("td", { hasText: new RegExp(`^${name}$`) }),
      });
    const addr = (name: string) => row(name).locator('input[type="number"]');
    // 既存のマッピングが入っている（内部 0 → 表示 1）
    await expect(addr("A")).toHaveValue("1");
    await expect(row("A").locator("select").first()).toHaveValue(
      "holdingRegisters",
    );
    await expect(addr("B")).toHaveValue("");

    // B を A（2 ワード）と重なるアドレスにする
    await row("B").locator("select").first().selectOption("holdingRegisters");
    await addr("B").fill("2");
    await expect(
      dialog.getByText("⚠ アドレスが重複している変数が 2 件あります"),
    ).toBeVisible();
    await shot(page, "bulk-register-conflict");
    await addr("B").fill("3");
    await expect(dialog.getByText(/アドレスが重複している変数/)).toHaveCount(0);

    // A は空欄にしてマッピングを消す
    await addr("A").fill("");
    await dialog.getByRole("button", { name: "一括保存" }).click();
    await expect(dialog).toBeHidden();

    expect((await getVariable(page, "A")).mappings ?? []).toEqual([]);
    expect((await getVariable(page, "B")).mappings).toEqual([
      {
        protocolType: "modbus-tcp",
        memoryArea: "holdingRegisters",
        address: 2,
        endianness: "big",
      },
    ]);
    await expect(variableRow(page, "B").locator("td.var-mapping")).toHaveText(
      "modbus-tcp:4:3",
    );
  });

  test("サーバーがなければ押せず、キャンセルと Esc では保存しない", async ({
    page,
  }) => {
    await expect(
      page.getByRole("button", { name: "一括マッピング編集" }),
    ).toBeDisabled();

    await addModbusServer(page);
    await createVariable(page, "A");
    await page.reload();
    await openApp(page);
    await openVariables(page);

    const dialog = dialogByTitle(page, "一括マッピング編集");
    for (const close of ["button", "Escape"] as const) {
      await page.getByRole("button", { name: "一括マッピング編集" }).click();
      await dialog.locator('tbody input[type="number"]').fill("7");
      if (close === "button") {
        await dialog.getByRole("button", { name: "キャンセル" }).click();
      } else {
        await page.keyboard.press("Escape");
      }
      await expect(dialog).toBeHidden();
    }
    expect((await getVariable(page, "A")).mappings ?? []).toEqual([]);
  });
});

test.describe("行のキーボード操作", () => {
  test("Insert・F2・M・Enter でダイアログが開き、閉じると行にフォーカスが戻る", async ({
    page,
  }) => {
    await addModbusServer(page);
    await createVariable(page, "Counter", "INT", 1);
    await page.reload();
    await openApp(page);
    await openVariables(page);
    const row = variableRow(page, "Counter");

    await row.focus();
    await page.keyboard.press("Insert");
    await expect(dialogByTitle(page, "変数を追加")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(dialogByTitle(page, "変数を追加")).toBeHidden();

    await row.focus();
    await page.keyboard.press("F2");
    const meta = dialogByTitle(page, "変数を編集");
    await expect(field(meta, "変数名:")).toHaveValue("Counter");
    await page.keyboard.press("Escape");
    await expect(meta).toBeHidden();

    await row.focus();
    await page.keyboard.press("m");
    await expect(dialogByTitle(page, "マッピング設定: Counter")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(dialogByTitle(page, "マッピング設定: Counter")).toBeHidden();

    // Enter で値の編集。確定すると行にフォーカスが戻り、続けてキー操作できる
    await row.focus();
    await page.keyboard.press("Enter");
    const edit = dialogByTitle(page, "値を編集");
    await expect(edit.locator("input")).toBeFocused();
    await waitAfterOpen(page);
    await edit.locator("input").fill("5");
    await page.keyboard.press("Enter");
    await expect(edit).toBeHidden();
    await expect(row.locator("td.var-value")).toHaveText("5");
    await expect(row).toBeFocused();

    // Delete で削除（確認あり）
    const confirm = nextDialogMessage(page);
    await page.keyboard.press("Delete");
    expect(await confirm).toContain('変数 "Counter" を削除しますか');
    await expect(row).toHaveCount(0);
  });
});
