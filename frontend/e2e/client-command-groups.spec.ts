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
  nextDialogMessage,
  MODBUS_PORT,
} from "./helpers";

// クライアントのコマンドグループ: よく使うコマンドを名前を付けてグループに保存し、グループを選んで送る。
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

const groupsArea = (page: Page) => page.locator(".client-command-groups");
const composer = (page: Page) => page.locator(".client-composer");
const savedRows = (page: Page) => page.getByTestId("client-saved-command");
const savedRow = (page: Page, name: string) =>
  savedRows(page).filter({
    has: page.locator("td.client-saved-name", {
      hasText: new RegExp("^" + escapeRegExp(name) + "$"),
    }),
  });
const result = (page: Page) => page.getByTestId("client-result");

test("グループを作り、組み立てたコマンドを保存して、グループから送れる", async ({
  page,
}) => {
  await expect(groupsArea(page)).toContainText("コマンドグループはありません");
  // グループがないうちは保存できない
  await expect(
    composer(page).getByRole("button", { name: "グループに保存" }),
  ).toBeDisabled();

  // グループを追加すると、すぐに名前を変えられる
  await groupsArea(page).getByLabel("グループを追加").click();
  const groupName = groupsArea(page).getByLabel("グループの名前", {
    exact: true,
  });
  await expect(groupName).toHaveValue("グループ 1");
  await groupName.fill("モーター");
  await groupName.press("Enter");
  await expect(groupsArea(page).getByLabel("コマンドグループ")).toContainText(
    "モーター（0）",
  );

  // 組み立てたコマンドを名前を付けて保存する
  await setting(composer(page), "コマンド").selectOption({
    label: "FC06 保持レジスタ 1 点の書き込み",
  });
  await setting(composer(page), "アドレス").fill("3");
  await setting(composer(page), "値").fill("0x1234");
  await composer(page).getByLabel("保存する名前").fill("速度を書く");
  await composer(page)
    .getByRole("button", { name: "「モーター」に保存" })
    .click();
  await expect(savedRow(page, "速度を書く")).toContainText("06 00 03 12 34");

  // 名前が空なら、内容の要約が名前になる
  await setting(composer(page), "コマンド").selectOption({
    label: "FC03 保持レジスタの読み取り",
  });
  await setting(composer(page), "アドレス").fill("3");
  await composer(page).getByLabel("保存する名前").fill("");
  await composer(page)
    .getByRole("button", { name: "「モーター」に保存" })
    .click();
  await expect(savedRows(page)).toHaveCount(2);
  await expect(savedRows(page).nth(1)).toContainText(
    "FC03 保持レジスタの読み取り アドレス 3 から 1 個",
  );
  await expect(groupsArea(page).getByLabel("コマンドグループ")).toContainText(
    "モーター（2）",
  );

  // グループから送ると、履歴に「グループ名 / コマンド名」が付く
  await savedRow(page, "速度を書く").getByLabel("送信").click();
  await expect(result(page).locator(".client-status")).toHaveText("正常");
  await expect(result(page)).toContainText("モーター / 速度を書く");
  expect(await readWords(page, 3)).toEqual([0x1234]);
  await expect(page.locator(".client-history tbody tr").first()).toContainText(
    "モーター / 速度を書く",
  );
  await shot(page, "client-command-groups");
});

test("保存したコマンドを読み込んで変え、上書き保存できる。名前の変更と削除もできる", async ({
  page,
}) => {
  const c = (await api<any[]>(page, "GetClients"))[0];
  const g = await api<{ id: string }>(
    page,
    "AddClientCommandGroup",
    c.id,
    "読み取り",
  );
  await api(page, "AddClientSavedCommand", c.id, g.id, {
    name: "先頭を読む",
    mode: "build",
    commandId: "3",
    params: { address: 0, count: 1 },
    request: "",
  });
  await api(page, "AddClientSavedCommand", c.id, g.id, {
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

  // 組み立てたコマンドを読み込むと、組み立て欄に入る
  await savedRow(page, "先頭を読む").getByLabel("組み立て欄に読み込む").click();
  await expect(setting(composer(page), "コマンド")).toHaveValue("3");
  await expect(composer(page).getByLabel("保存する名前")).toHaveValue(
    "先頭を読む",
  );
  await setting(composer(page), "個数").fill("4");
  await composer(page)
    .getByRole("button", { name: "「先頭を読む」を上書き保存" })
    .click();
  await expect(savedRow(page, "先頭を読む")).toContainText("03 00 00 00 04");
  await expect(savedRows(page)).toHaveCount(2);

  // 16 進で保存したコマンドは、16 進の欄に入る
  await savedRow(page, "生の PDU").getByLabel("組み立て欄に読み込む").click();
  await expect(
    composer(page).getByLabel("送るリクエスト（16 進）"),
  ).toHaveValue("03 00 05 00 01");

  // 名前を変える（Esc で取り消し、Enter で決定）
  await savedRow(page, "生の PDU").getByLabel("名前を変える").click();
  const nameInput = groupsArea(page).getByLabel("コマンドの名前", {
    exact: true,
  });
  await nameInput.fill("取り消す");
  await nameInput.press("Escape");
  await expect(savedRow(page, "生の PDU")).toHaveCount(1);
  await savedRow(page, "生の PDU").getByLabel("名前を変える").click();
  await nameInput.fill("アドレス 5");
  await nameInput.press("Enter");
  await expect(savedRow(page, "アドレス 5")).toHaveCount(1);

  // コマンドを削除する
  await savedRow(page, "アドレス 5").getByLabel("削除").click();
  await expect(savedRows(page)).toHaveCount(1);

  // コマンドの入ったグループを削除するときは確認する
  const msg = nextDialogMessage(page);
  await groupsArea(page).getByLabel("グループを削除").click();
  expect(await msg).toContain(
    "グループ「読み取り」と、保存した 1 個のコマンド",
  );
  await expect(groupsArea(page)).toContainText("コマンドグループはありません");
});
