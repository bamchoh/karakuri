import { test, expect, Page, Locator } from "@playwright/test";
import {
  api,
  openApp,
  resetApp,
  shot,
  addModbusServer,
  readWords,
  escapeRegExp,
  openClient,
  MODBUS_PORT,
} from "./helpers";

// クライアントの保存したコマンド: 「新しいコマンド」のダイアログで組み立て、名前を付けて保存しておき、あとから送る。
// 接続先は、テストで起動した Karakuri 自身の Modbus TCP サーバー（127.0.0.1:MODBUS_PORT）。

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await addModbusServer(page);
  const c = await api<{ id: string }>(page, "AddClient", "modbus-tcp", "自分");
  await api(page, "UpdateClient", c.id, "自分", {
    host: "127.0.0.1",
    port: MODBUS_PORT,
    unitId: 1,
    timeoutMs: 500,
  });
  await openClient(page, "自分");
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

function setting(scope: Locator, label: string) {
  return scope
    .locator(".vscode-setting-item", {
      has: scope.page().locator(".vscode-setting-label", {
        hasText: new RegExp("^" + escapeRegExp(label) + "$"),
      }),
    })
    .locator("input, select")
    .first();
}

const savedArea = (page: Page) => page.locator(".client-saved-commands-area");
const dialog = (page: Page) => page.locator(".client-command-dialog");
const savedRows = (page: Page) => page.getByTestId("client-saved-command");
const savedRow = (page: Page, name: string) =>
  savedRows(page).filter({
    has: page.locator("td.client-saved-name", {
      hasText: new RegExp("^" + escapeRegExp(name) + "$"),
    }),
  });
const button = (page: Page, name: string) =>
  dialog(page).getByRole("button", { name, exact: true });

test("新しいコマンドのダイアログで組み立て、試しに送ってから保存し、一覧から送れる", async ({
  page,
}) => {
  await expect(savedArea(page)).toContainText("保存したコマンドはありません");

  await page.getByRole("button", { name: "新しいコマンド" }).click();
  await expect(dialog(page).locator("h3")).toHaveText("新しいコマンド");
  await setting(dialog(page), "コマンド").selectOption({
    label: "FC06 保持レジスタ 1 点の書き込み",
  });
  await setting(dialog(page), "アドレス").fill("3");
  await setting(dialog(page), "値").fill("0x1234");

  // 「送信」はダイアログを開いたまま送り、結果をダイアログに出す（保存はしない）
  await button(page, "送信").click();
  const dialogResult = page.getByTestId("client-dialog-result");
  await expect(dialogResult.locator(".client-status")).toHaveText("正常");
  expect(await readWords(page, 3)).toEqual([0x1234]);
  await expect(dialog(page)).toBeVisible();
  await expect(savedRows(page)).toHaveCount(0);
  await shot(page, "client-command-dialog");

  // 名前を付けて保存すると、ダイアログが閉じて一覧に並ぶ
  await dialog(page).getByLabel("保存する名前").fill("速度を書く");
  await button(page, "保存して閉じる").click();
  await expect(dialog(page)).toBeHidden();
  await expect(savedRow(page, "速度を書く")).toContainText("06 00 03 12 34");

  // 名前が空なら、内容の要約が名前になる
  await page.getByRole("button", { name: "新しいコマンド" }).click();
  await setting(dialog(page), "コマンド").selectOption({
    label: "FC03 保持レジスタの読み取り",
  });
  await setting(dialog(page), "アドレス").fill("3");
  await button(page, "保存して閉じる").click();
  await expect(savedRows(page)).toHaveCount(2);
  await expect(savedRows(page).nth(1)).toContainText(
    "FC03 保持レジスタの読み取り アドレス 3 から 1 個",
  );
  await expect(page.getByText("保存したコマンド（2）")).toBeVisible();

  // 一覧から送ると、結果と履歴にコマンドの名前が付く
  await savedRow(page, "FC03 保持レジスタの読み取り アドレス 3 から 1 個")
    .getByLabel("送信")
    .click();
  const result = page.getByTestId("client-result");
  await expect(result.locator(".client-status")).toHaveText("正常");
  await expect(result).toContainText("4660 (0x1234)");
  await expect(page.locator(".client-history tbody tr").first()).toContainText(
    "FC03 保持レジスタの読み取り アドレス 3 から 1 個",
  );
  await shot(page, "client-saved-commands");
});

test("保存したコマンドを編集ダイアログで直して上書きできる。閉じると変わらず、削除もできる", async ({
  page,
}) => {
  const c = (await api<any[]>(page, "GetClients"))[0];
  await api(page, "AddClientSavedCommand", c.id, {
    name: "先頭を読む",
    mode: "build",
    commandId: "3",
    params: { address: 0, count: 1 },
    request: "",
  });
  await api(page, "AddClientSavedCommand", c.id, {
    name: "生の PDU",
    mode: "hex",
    commandId: "",
    params: {},
    request: "03 00 05 00 01",
  });
  // 画面を開き直して読み込む
  await page.getByTitle("Variables").click();
  await openClient(page, "自分");
  await expect(savedRows(page)).toHaveCount(2);

  // 組み立てたコマンドを編集すると、組み立ての欄に入った状態で開く
  await savedRow(page, "先頭を読む").getByLabel("編集").click();
  await expect(dialog(page).locator("h3")).toHaveText(
    "コマンドを編集: 先頭を読む",
  );
  await expect(setting(dialog(page), "コマンド")).toHaveValue("3");
  await expect(dialog(page).getByLabel("保存する名前")).toHaveValue(
    "先頭を読む",
  );
  await setting(dialog(page), "個数").fill("4");
  await dialog(page).getByLabel("保存する名前").fill("先頭 4 個");
  await button(page, "保存して閉じる").click();
  await expect(savedRow(page, "先頭 4 個")).toContainText("03 00 00 00 04");
  await expect(savedRows(page)).toHaveCount(2);

  // 16 進で保存したコマンドは、16 進の欄に入った状態で開く。変えても「閉じる」なら保存しない
  await savedRow(page, "生の PDU").getByLabel("編集").click();
  const hex = dialog(page).getByLabel("送るリクエスト（16 進）");
  await expect(hex).toHaveValue("03 00 05 00 01");
  await hex.fill("03 00 06 00 01");
  await button(page, "閉じる").click();
  await expect(savedRow(page, "生の PDU")).toContainText("03 00 05 00 01");

  // Esc でも閉じる
  await savedRow(page, "生の PDU").getByLabel("編集").click();
  await page.keyboard.press("Escape");
  await expect(dialog(page)).toBeHidden();

  // 読めない 16 進は、ダイアログの中にエラーを出し、閉じない
  await savedRow(page, "生の PDU").getByLabel("編集").click();
  await hex.fill("0G");
  await button(page, "保存して閉じる").click();
  await expect(dialog(page).locator(".config-error")).toContainText("16 進");
  await expect(dialog(page)).toBeVisible();
  await button(page, "閉じる").click();

  // 削除する
  await savedRow(page, "生の PDU").getByLabel("削除").click();
  await expect(savedRows(page)).toHaveCount(1);
  await savedRow(page, "先頭 4 個").getByLabel("削除").click();
  await expect(savedArea(page)).toContainText("保存したコマンドはありません");
});
