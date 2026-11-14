import { test, expect, Page } from "@playwright/test";
import {
  addModbusServer,
  api,
  createVariable,
  dialogByTitle,
  getVariable,
  openApp,
  openRegisters,
  openVariables,
  resetApp,
  shot,
  variableRow,
} from "./helpers";

// レジスタのアドレスの範囲を画面に出し、範囲外の入力を止めることを確認する。
// Modbus（1 始まり・65536 個）と、FINS（0 始まり・「ワード.ビット」）の両方で見る。

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await addModbusServer(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

// レジスタの一覧の上の操作欄で、ラベルがちょうど label の入力欄がある枠
function group(page: Page, label: string) {
  return page.locator(".register-controls .form-group", {
    has: page.locator("label", { hasText: new RegExp("^" + label + "$") }),
  });
}

// ダイアログの「ラベル:」の行の入力欄
function row(dialog: ReturnType<typeof dialogByTitle>, label: string) {
  return dialog
    .locator(".dialog-row", {
      has: dialog.page().locator("label", {
        hasText: new RegExp("^" + label + ":$"),
      }),
    })
    .locator("input, select");
}

async function openMapping(page: Page, name: string) {
  await variableRow(page, name).click({ button: "right" });
  await page.locator(".context-menu").getByText("マッピング").click();
  const dialog = dialogByTitle(page, /マッピング設定/);
  await expect(dialog).toBeVisible();
  return dialog;
}

test("レジスタの一覧: エリアの範囲を出し、範囲外の開始アドレスは最後のページに合わせる", async ({
  page,
}) => {
  await openRegisters(page, "一覧表示");
  const start = group(page, "開始アドレス");
  // Modbus は 1 始まりで表示する
  await expect(start.locator(".address-range-hint")).toHaveText(
    "範囲: 1〜65536",
  );

  await start.locator("input").fill("70000");
  await start.locator("input").blur();
  await expect(start.locator("input")).toHaveValue("65437");
  await expect(page.locator("td.row-header").first()).toHaveText("65437");
  await expect(page.locator("td.matrix-cell")).toHaveCount(100);
  await expect(page.getByRole("button", { name: "次へ" })).toBeDisabled();
  await shot(page, "register-range-list");

  // エリアを変えると、そのエリアの範囲になる（FINS: 0 始まり。1bit でも、開始アドレスはワードの番号）
  await api(page, "AddServer", "fins", "");
  await openRegisters(page, "一覧表示", "FINS");
  const finsStart = group(page, "開始アドレス");
  await expect(finsStart.locator(".address-range-hint")).toHaveText(
    "範囲: 0〜6143",
  );
  await group(page, "ビット幅").locator("select").selectOption("1");
  await expect(finsStart.locator(".address-range-hint")).toHaveText(
    "範囲: 0〜6143",
  );
  await finsStart.locator("input").fill("9999");
  await finsStart.locator("input").blur();
  // 最後のページは、10 ワード分（6134〜6143）
  await expect(finsStart.locator("input")).toHaveValue("6134");
  await group(page, "メモリエリア").locator("select").selectOption("WR");
  await expect(finsStart.locator(".address-range-hint")).toHaveText(
    "範囲: 0〜511",
  );
});

test("モニタリングの追加: 範囲を出し、最後の項目まで収まらなければ追加できない", async ({
  page,
}) => {
  await openRegisters(page, "モニタリング");
  await page
    .locator(".monitoring-controls")
    .getByRole("button", { name: "追加" })
    .click();
  const dialog = dialogByTitle(page, "モニタリング項目を追加");
  const hint = dialog.locator(".address-range-hint");
  const add = dialog.getByRole("button", { name: "追加" });
  await expect(row(dialog, "開始アドレス")).toHaveValue("1");
  await expect(hint).toHaveText("範囲: 1〜65536");
  await expect(add).toBeEnabled();

  // 範囲外
  await row(dialog, "開始アドレス").fill("65537");
  await expect(hint).toHaveClass(/error/);
  await expect(hint).toContainText("アドレスが範囲外です（範囲: 1〜65536）");
  await expect(row(dialog, "開始アドレス")).toHaveClass(/address-invalid/);
  await expect(add).toBeDisabled();
  await shot(page, "register-range-monitoring");

  // 最後のアドレスは 1 個なら収まるが、2 個は収まらない
  await row(dialog, "開始アドレス").fill("65536");
  await expect(hint).not.toHaveClass(/error/);
  await expect(add).toBeEnabled();
  await row(dialog, "個数").fill("2");
  await expect(hint).toContainText("末尾が範囲を超えます");
  await expect(add).toBeDisabled();
  // 32bit（2 ワード）も、1 個で 2 ワード使うので収まらない
  await row(dialog, "個数").fill("1");
  await expect(add).toBeEnabled();
  await row(dialog, "ビット幅").selectOption("32");
  await expect(hint).toContainText("末尾が範囲を超えます");
  await expect(add).toBeDisabled();
  // Enter でも追加されない
  await page.waitForTimeout(150);
  await row(dialog, "個数").press("Enter");
  await expect(dialog).toBeVisible();
  expect(await api<unknown[]>(page, "GetMonitoringItems")).toHaveLength(0);

  await row(dialog, "開始アドレス").fill("65535");
  await expect(add).toBeEnabled();
  await add.click();
  await expect(dialog).toBeHidden();
  expect(await api<unknown[]>(page, "GetMonitoringItems")).toHaveLength(1);
});

test("マッピング設定: 変数が占める大きさの分、範囲に収まらなければ保存できない", async ({
  page,
}) => {
  await createVariable(page, "Big", "DINT", 0); // 2 ワード
  await openVariables(page);
  const dialog = await openMapping(page, "Big");
  await dialog.getByRole("button", { name: "+ マッピング追加" }).click();
  await dialog
    .locator("select.mapping-col")
    .nth(1)
    .selectOption("holdingRegisters");
  const address = dialog.locator("input.mapping-col");
  const hint = dialog.locator(".address-range-hint");
  const save = dialog.getByRole("button", { name: "保存" });
  await expect(hint).toHaveText("範囲: 1〜65536");
  await expect(save).toBeEnabled();

  // 最後のアドレスには、2 ワードの変数は収まらない
  await address.fill("65536");
  await expect(hint).toHaveClass(/error/);
  await expect(hint).toContainText("末尾が範囲を超えます（65536 から 2 個");
  await expect(address).toHaveClass(/address-invalid/);
  await expect(save).toBeDisabled();
  await shot(page, "register-range-mapping");

  await address.fill("65535");
  await expect(hint).toHaveText("範囲: 1〜65536");
  await expect(save).toBeEnabled();
  await save.click();
  await expect(dialog).toBeHidden();
  expect((await getVariable(page, "Big")).mappings[0].address).toBe(65534);
});

test("マッピング設定（FINS）: BOOL は「ワード.ビット」の範囲で検査する", async ({
  page,
}) => {
  await api(page, "RemoveServer", "modbus-tcp").catch(async () => {
    await api(page, "StopServer", "modbus-tcp");
    await api(page, "RemoveServer", "modbus-tcp");
  });
  await api(page, "AddServer", "fins", "");
  await createVariable(page, "Run", "BOOL", false);
  await createVariable(page, "Speed", "INT", 0);
  await openVariables(page);

  let dialog = await openMapping(page, "Run");
  await dialog.getByRole("button", { name: "+ マッピング追加" }).click();
  const address = dialog.locator("input.mapping-col");
  const hint = dialog.locator(".address-range-hint");
  const save = dialog.getByRole("button", { name: "保存" });
  // 最初のエリアは CIO（6144 ワード）。BOOL は、ビットの範囲
  await expect(hint).toHaveText("範囲: 0.00〜6143.15");
  await address.fill("6143.15");
  await expect(save).toBeEnabled();
  await address.fill("6144.00");
  await expect(hint).toContainText(
    "アドレスが範囲外です（範囲: 0.00〜6143.15）",
  );
  await expect(save).toBeDisabled();
  // エリアを WR に変えると、WR の範囲で検査する
  await dialog.locator("select.mapping-col").nth(1).selectOption("WR");
  await expect(hint).toContainText("範囲: 0.00〜511.15");
  await dialog.getByRole("button", { name: "キャンセル" }).click();

  // INT は、ワードの範囲
  dialog = await openMapping(page, "Speed");
  await dialog.getByRole("button", { name: "+ マッピング追加" }).click();
  await expect(dialog.locator(".address-range-hint")).toHaveText(
    "範囲: 0〜6143",
  );
});

test("一括マッピング編集: 範囲外の行があると、件数を出して保存を止める", async ({
  page,
}) => {
  await createVariable(page, "A", "INT", 0);
  await createVariable(page, "B", "DINT", 0);
  await openVariables(page);
  await page.getByRole("button", { name: "一括マッピング編集" }).click();
  const dialog = dialogByTitle(page, /一括マッピング/);
  await expect(dialog).toBeVisible();
  const input = (name: string) =>
    dialog
      .locator("tbody tr", { hasText: new RegExp("^" + name) })
      .locator("input.bulk-address-input");
  const save = dialog.getByRole("button", { name: "一括保存" });
  const banner = dialog.locator(".bulk-range-banner");

  // 最初に選ばれているのはコイル（ビットのエリア）なので、保持レジスタにする
  for (const name of ["A", "B"]) {
    await dialog
      .locator("tbody tr", { hasText: new RegExp("^" + name) })
      .locator("select")
      .first()
      .selectOption("holdingRegisters");
  }

  // 入力欄の案内に、範囲が出る
  await expect(input("A")).toHaveAttribute("placeholder", /1〜65536/);
  await expect(banner).toHaveCount(0);

  await input("A").fill("65536"); // INT（1 ワード）は収まる
  await input("B").fill("65536"); // DINT（2 ワード）は収まらない
  await expect(input("A")).not.toHaveClass(/address-invalid/);
  await expect(input("B")).toHaveClass(/address-invalid/);
  await expect(input("B")).toHaveAttribute("title", /末尾が範囲を超えます/);
  await expect(banner).toContainText("アドレスが範囲外の変数が 1 件あります");
  await expect(save).toBeDisabled();
  await shot(page, "register-range-bulk");

  await input("B").fill("100");
  await expect(banner).toHaveCount(0);
  await expect(save).toBeEnabled();
  // 空欄（マッピングなし）は、検査しない
  await input("A").fill("");
  await expect(save).toBeEnabled();
});
