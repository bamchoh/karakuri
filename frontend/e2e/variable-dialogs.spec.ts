import { test, expect, Page } from "@playwright/test";
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

// FocusTrap は、描画から 100ms 以内の Enter を「ダイアログを開いた Enter」とみなして無視する。
// 入力の直後は再描画で判定がやり直されるため、Enter の前に少し待つ
async function waitForEnterReady(page: Page) {
  await page.waitForTimeout(200);
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

  test("Enter で確定でき、変数名が空なら何もしない", async ({ page }) => {
    await page.getByRole("button", { name: "変数を追加" }).click();
    const dialog = dialogByTitle(page, "変数を追加");
    await dialog.getByRole("button", { name: "追加" }).click();
    await expect(dialog).toBeVisible();

    await field(dialog, "変数名:").fill("ByEnter");
    await waitForEnterReady(page);
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
    await expect(
      field(dialog, "型カテゴリ:").locator("option"),
    ).toHaveText(["スカラー型", "配列型"]);
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
  test("型を登録・編集・削除できる", async ({ page }) => {
    await page.getByRole("button", { name: "構造体型管理" }).click();
    const dialog = dialogByTitle(page, "構造体型管理");
    await expect(dialog.getByText("新規構造体型")).toBeVisible();

    // フィールドがない状態では登録できない
    await field(dialog, "型名:").fill("Pump");
    let alert = nextDialogMessage(page);
    await dialog.getByRole("button", { name: "型を登録" }).click();
    expect(await alert).toContain("少なくとも1つのフィールド");

    // 1 つ目: STRING[10]、2 つ目: DINT の配列
    const card = (i: number) =>
      dialog.locator(".dialog-row", {
        has: page.locator("label", { hasText: /^名前:$/ }),
      }).nth(i).locator("xpath=..");
    await field(card(0), "名前:").fill("label");
    await field(card(0), "データ型:").selectOption("STRING");
    await field(card(0), "バイト長:").fill("10");
    await dialog.getByRole("button", { name: "+ フィールド追加" }).click();
    await field(card(1), "名前:").fill("hist");
    await field(card(1), "型カテゴリ:").selectOption("array");
    await field(card(1), "要素型:").selectOption("DINT");
    await shot(page, "struct-new");
    await dialog.getByRole("button", { name: "型を登録" }).click();

    const listRow = dialog.locator("table.variable-table tbody tr", {
      hasText: "Pump",
    });
    await expect(listRow).toContainText(
      "label:STRING[10], hist:ARRAY[0..9] OF DINT",
    );
    // 登録後はフォームが初期状態に戻る
    await expect(field(dialog, "型名:")).toHaveValue("");

    // 編集: 既存の定義がフォームに入り、型名は変更できない
    await listRow.getByRole("button", { name: "編集" }).click();
    await expect(dialog.getByText("構造体型を編集")).toBeVisible();
    await expect(field(dialog, "型名:")).toHaveValue("Pump");
    await expect(field(dialog, "型名:")).toBeDisabled();
    await expect(field(card(0), "名前:")).toHaveValue("label");
    await expect(field(card(0), "データ型:")).toHaveValue("STRING");
    await expect(field(card(0), "バイト長:")).toHaveValue("10");
    await expect(field(card(1), "型カテゴリ:")).toHaveValue("array");
    await expect(field(card(1), "要素型:")).toHaveValue("DINT");
    await field(card(1), "次元数:").selectOption("2");
    await shot(page, "struct-edit");
    await dialog.getByRole("button", { name: "更新" }).click();
    await expect(listRow).toContainText("hist:ARRAY[0..9, 0..4] OF DINT");

    // 削除は確認してから
    const confirm = nextDialogMessage(page);
    await listRow.getByRole("button", { name: "削除" }).click();
    expect(await confirm).toContain('構造体型 "Pump" を削除しますか');
    await expect(listRow).toHaveCount(0);
    expect(await api<unknown[]>(page, "GetStructTypes")).toEqual([]);

    await dialog.getByRole("button", { name: "閉じる" }).click();
    await expect(dialog).toBeHidden();
  });

  test("閉じると入力は消える（Esc でも同じ）", async ({ page }) => {
    for (const close of ["button", "Escape"] as const) {
      await page.getByRole("button", { name: "構造体型管理" }).click();
      const dialog = dialogByTitle(page, "構造体型管理");
      await field(dialog, "型名:").fill("Temp");
      if (close === "button") {
        await dialog.getByRole("button", { name: "閉じる" }).click();
      } else {
        await page.keyboard.press("Escape");
      }
      await expect(dialog).toBeHidden();
      await page.getByRole("button", { name: "構造体型管理" }).click();
      await expect(field(dialog, "型名:")).toHaveValue("");
      await dialog.getByRole("button", { name: "閉じる" }).click();
    }
  });
});

test.describe("変数の編集（名前・データ型）ダイアログ", () => {
  test("右クリックメニューから開き、現在の定義が入っている", async ({ page }) => {
    await createVariable(page, "Label", "STRING[8]", "abc");
    await createVariable(page, "Grid", "ARRAY[1..2, 0..1] OF REAL", [
      [0, 0],
      [0, 0],
    ]);

    await (await openContextMenu(page, "Label"))
      .getByRole("button", { name: "編集" })
      .click();
    const dialog = dialogByTitle(page, "変数を編集");
    await expect(field(dialog, "変数名:")).toHaveValue("Label");
    await expect(field(dialog, "型カテゴリ:")).toHaveValue("scalar");
    await expect(field(dialog, "データ型:")).toHaveValue("STRING");
    await expect(field(dialog, "バイト長:")).toHaveValue("8");
    await dialog.getByRole("button", { name: "キャンセル" }).click();
    await expect(dialog).toBeHidden();

    await (await openContextMenu(page, "Grid"))
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

  test("名前とデータ型を変更できる（値は初期値に戻る）", async ({ page }) => {
    await createVariable(page, "Old", "INT", 7);
    await (await openContextMenu(page, "Old"))
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
    await input.fill("0x10");
    await shot(page, "value-edit-scalar");
    await waitForEnterReady(page);
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
    await expect(
      variableRow(page, "Label").locator("td.var-value"),
    ).toHaveText('"abcd"');
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

    // 配列: 要素の行を編集。
    // 開始インデックスが 0 でない配列は、別の要素に書き込まれる不具合があるため使わない（issue #24）
    await variableRow(page, "Values").locator("td.var-name").click();
    await shot(page, "rows-expanded");
    const elem = variableRow(page, "Values[1]").locator("td.var-value");
    await expect(elem).toHaveText("2");
    await elem.click();
    await dialog.locator("input").fill("20");
    await dialog.getByRole("button", { name: "更新" }).click();
    expect((await getVariable(page, "Values")).value).toEqual([1, 20, 3]);
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
    await dialog.getByText("[1]").locator("xpath=..").locator("div").first().click();
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
    await dialog.getByText("[1]").locator("xpath=..").locator("div").first().click();
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
    await edit.locator("input").fill("5");
    await waitForEnterReady(page);
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
