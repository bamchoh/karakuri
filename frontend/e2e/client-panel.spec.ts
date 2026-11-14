import { test, expect, Page, Locator } from "@playwright/test";
import {
  api,
  openApp,
  resetApp,
  shot,
  addModbusServer,
  readWords,
  writeWord,
  escapeRegExp,
  MODBUS_PORT,
  openClient,
  addClientFromDialog,
  selectClientTab,
  dialogByTitle,
} from "./helpers";

// クライアント画面の動きを確認する。
// 接続先は、テストで起動した Karakuri 自身の Modbus TCP サーバー（127.0.0.1:MODBUS_PORT）。

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await addModbusServer(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

async function openClients(page: Page) {
  await page.getByTitle("Connections", { exact: true }).click();
  await expect(page.locator(".server-vertical-tabs")).toBeVisible();
}

// 「ラベル」の設定項目（.vscode-setting-item）の入力欄
function setting(scope: Page | Locator, label: string) {
  const page = "page" in scope ? scope.page() : scope;
  return scope
    .locator(".vscode-setting-item", {
      has: page.locator(".vscode-setting-label", {
        hasText: new RegExp("^" + escapeRegExp(label) + "$"),
      }),
    })
    .locator("input, select")
    .first();
}

// コマンドのダイアログ（組み立て・16 進の入力・送信）
const composer = (page: Page) => page.locator(".client-command-dialog");
const preview = (page: Page) => page.getByTestId("client-preview");
const result = (page: Page) => page.getByTestId("client-result");
const sendButton = (page: Page) =>
  composer(page).getByRole("button", { name: "送信", exact: true });

// 「新しいコマンド」でダイアログを開く
async function openCommandDialog(page: Page) {
  await page.getByRole("button", { name: "新しいコマンド" }).click();
  await expect(composer(page)).toBeVisible();
}

// 接続先を API で追加し（127.0.0.1:MODBUS_PORT）、画面で選ぶ
async function addClient(page: Page, settings: Record<string, unknown> = {}) {
  const c = await api<{ id: string; name: string }>(
    page,
    "AddClient",
    "modbus-tcp",
    "自分",
  );
  await api(page, "UpdateClient", c.id, "自分", {
    host: "127.0.0.1",
    port: MODBUS_PORT,
    unitId: 1,
    timeoutMs: 500,
    ...settings,
  });
  await openClient(page, "自分");
  return c;
}

test("接続先を画面から追加して、設定を保存できる", async ({ page }) => {
  await openClients(page);
  await expect(page.getByText("接続先が登録されていません")).toBeVisible();

  // 「＋」を押すと、サーバーの追加と同じ形のダイアログが開く（一覧の見出しにプルダウンはない）
  await expect(page.locator(".server-panel-header select")).toHaveCount(0);
  await page.getByTitle("接続先を追加").click();
  const addDialog = dialogByTitle(page, "接続先を追加");
  await expect(addDialog).toBeVisible();
  // プロトコルは登録した順に並び、最初のもの（Modbus TCP）が選ばれている
  await expect(addDialog.locator("select")).toHaveValue("modbus-tcp");
  await expect(addDialog.locator("select option").first()).toHaveText(
    "Modbus TCP",
  );
  await shot(page, "client-add-dialog");

  // Esc とキャンセルでは追加しない
  await page.keyboard.press("Escape");
  await expect(addDialog).toBeHidden();
  await page.getByTitle("接続先を追加").click();
  await addDialog.getByRole("button", { name: "キャンセル" }).click();
  await expect(addDialog).toBeHidden();
  await expect(page.locator(".client-tab-item")).toHaveCount(0);

  // 名前を入れて Enter で追加できる
  await page.getByTitle("接続先を追加").click();
  await addDialog.locator("input").fill("名前つき");
  await page.waitForTimeout(150); // FocusTrap は、開いた直後の Enter を無視する
  await addDialog.locator("input").press("Enter");
  await expect(addDialog).toBeHidden();
  await expect(page.locator(".server-tab-item.selected")).toContainText(
    "名前つき",
  );
  await api(
    page,
    "RemoveClient",
    (await api<{ id: string }[]>(page, "GetClients"))[0].id,
  );
  await expect(page.locator(".client-tab-item")).toHaveCount(0);

  // 名前を空のまま追加すると、名前が付く
  await addClientFromDialog(page);
  await expect(page.locator(".server-tab-item.selected")).toContainText(
    "接続先",
  );
  await expect(setting(page, "ホスト")).toHaveValue("127.0.0.1");
  await expect(setting(page, "ポート")).toHaveValue("502");

  await setting(page, "名前").fill("PLC-A");
  await setting(page, "ポート").fill(String(MODBUS_PORT));
  await expect(page.getByText("保存していない変更があります")).toBeVisible();
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByText("保存していない変更があります")).toBeHidden();
  await expect(page.locator(".server-tab-item.selected")).toContainText(
    "PLC-A",
  );

  const clients = await api<any[]>(page, "GetClients");
  expect(clients).toHaveLength(1);
  expect(clients[0].name).toBe("PLC-A");
  expect(clients[0].settings.port).toBe(MODBUS_PORT);
});

test("ファンクションコードで組み立てて書き込み・読み取りができる", async ({
  page,
}) => {
  await addClient(page);
  await openCommandDialog(page);

  // FC16: 組み立てたリクエストがその場で 16 進で出る
  await setting(composer(page), "コマンド").selectOption({
    label: "FC16 保持レジスタの複数書き込み",
  });
  await setting(composer(page), "アドレス").fill("5");
  await setting(composer(page), "値").fill("11, 0x16");
  await expect(preview(page)).toHaveText("10 00 05 00 02 04 00 0B 00 16");
  await sendButton(page).click();
  await expect(result(page).locator(".client-status")).toHaveText("正常");
  expect(await readWords(page, 5, 2)).toEqual([11, 22]);
  await expect(page.locator(".server-status-badge")).toHaveText("接続中");

  // FC03: 読み取った値が一覧に出る
  await writeWord(page, 7, 1234);
  await setting(composer(page), "コマンド").selectOption({
    label: "FC03 保持レジスタの読み取り",
  });
  await setting(composer(page), "アドレス").fill("5");
  await setting(composer(page), "個数").fill("3");
  await expect(preview(page)).toHaveText("03 00 05 00 03");
  await sendButton(page).click();
  await expect(result(page)).toContainText("FC03 保持レジスタの読み取り");
  const values = result(page).locator(".client-values tr");
  await expect(values).toHaveCount(3);
  await expect(values.nth(0)).toContainText("11 (0x000B)");
  await expect(values.nth(2)).toContainText("1234 (0x04D2)");
  // MBAP ヘッダー付きの送受信バイト列
  await expect(result(page).locator(".client-bytes")).toContainText(
    "00 00 00 06 01 03 00 05 00 03",
  );
  await expect(page.locator(".client-history tbody tr")).toHaveCount(2);
  await shot(page, "client-read");
});

test("項目の誤りは組み立ての欄に出て、送信できない", async ({ page }) => {
  await addClient(page);
  await openCommandDialog(page);
  await setting(composer(page), "コマンド").selectOption({
    label: "FC03 保持レジスタの読み取り",
  });
  await setting(composer(page), "個数").fill("200");
  await expect(page.locator(".client-build-error")).toContainText("個数");
  await expect(sendButton(page)).toBeDisabled();
});

test("16 進で入れたリクエストを送れる。組み立てた内容を写して変えられる", async ({
  page,
}) => {
  await addClient(page);
  await openCommandDialog(page);
  await writeWord(page, 2, 0xabcd);

  await setting(composer(page), "コマンド").selectOption({
    label: "FC03 保持レジスタの読み取り",
  });
  await setting(composer(page), "アドレス").fill("1");
  await composer(page).getByRole("button", { name: "16 進で編集" }).click();
  const hex = composer(page).getByLabel("送るリクエスト（16 進）");
  await expect(hex).toHaveValue("03 00 01 00 01");

  await hex.fill("03 00 02 00 01");
  await hex.press("Enter");
  await expect(result(page).locator(".client-values")).toContainText(
    "43981 (0xABCD)",
  );

  // サーバーが対応しないファンクションコードは例外応答
  await hex.fill("2B 0E 01 00");
  await sendButton(page).click();
  await expect(result(page).locator(".client-status")).toHaveText("例外応答");

  // 読めない 16 進は送る前にエラー
  await hex.fill("0G");
  await sendButton(page).click();
  await expect(page.locator(".config-error")).toContainText("16 進");
  await shot(page, "client-hex");
});

test("応答しない UnitID はタイムアウトになり、履歴から送り直せる", async ({
  page,
}) => {
  await api(page, "SetUnitIDEnabled", "modbus-tcp", 2, false);
  await addClient(page, { unitId: 2, timeoutMs: 200 });
  await openCommandDialog(page);

  await sendButton(page).click();
  await expect(result(page).locator(".client-status")).toHaveText(
    "タイムアウト",
  );
  await expect(result(page)).toContainText(
    "200 ms 以内に応答がありませんでした",
  );
  await expect(page.locator(".server-status-badge")).toHaveText("未接続");

  // UnitID を応答するようにして、ダイアログを閉じ、履歴の行から送り直す
  await page.keyboard.press("Escape");
  await expect(composer(page)).toBeHidden();
  await api(page, "SetUnitIDEnabled", "modbus-tcp", 2, true);
  await page
    .locator(".client-history tbody tr")
    .first()
    .getByLabel("もう一度送る")
    .click();
  await expect(page.locator(".client-history tbody tr")).toHaveCount(2);
  await expect(result(page).locator(".client-status")).toHaveText("正常");

  // 古い履歴の行を選ぶと、その結果を表示する
  await page.locator(".client-history tbody tr").nth(1).click();
  await expect(result(page).locator(".client-status")).toHaveText(
    "タイムアウト",
  );
  await shot(page, "client-timeout");
});

test("サーバーとクライアントが 1 つの一覧に並び、保存していない接続先の設定は切り替える前に確認する", async ({
  page,
}) => {
  const c = await addClient(page);
  // サーバーとクライアントが同じ画面に並ぶ
  await expect(
    page.locator(".server-tab-item:not(.client-tab-item)"),
  ).toHaveCount(1);
  await expect(page.locator(".client-tab-item")).toHaveCount(1);
  await expect(page.getByText("CLIENTS", { exact: true })).toBeVisible();

  // 接続先の設定を変えたまま、サーバーへ切り替えようとすると確認する
  await selectClientTab(page, "設定");
  await setting(page, "ポート").fill("1502");
  await page
    .locator(".server-tab-item", { hasText: "Modbus TCP" })
    .first()
    .click();
  const confirm = dialogByTitle(page, "設定が保存されていません");
  await expect(confirm).toBeVisible();
  await shot(page, "connections-unsaved-confirm");
  await confirm.getByRole("button", { name: "キャンセル" }).click();
  await expect(page.locator(".client-tab-item")).toHaveClass(/selected/);
  await expect(setting(page, "ポート")).toHaveValue("1502");

  // 保存してから切り替えると、接続先の設定が保存される
  await page
    .locator(".server-tab-item", { hasText: "Modbus TCP" })
    .first()
    .click();
  await confirm.getByRole("button", { name: "保存してから切り替え" }).click();
  await expect(page.locator(".server-config-pane-name")).toHaveText(
    "Modbus TCP",
  );
  const clients = await api<any[]>(page, "GetClients");
  expect(clients.find((x) => x.id === c.id).settings.port).toBe(1502);
});

test("右側はタブ（設定 / コマンド / ポーリング）で切り替え、接続先は見出しに 1 行で出る", async ({
  page,
}) => {
  await addClient(page);
  const tabs = page.locator(".client-sub-tabs .sub-tab-button");
  // サブスクリプションは、対応するプロトコル（OPC UA）の接続先にだけ出る
  await expect(tabs).toHaveText(["設定", "コマンド", "ポーリング"]);

  // 既にある接続先を選んだときは、コマンドのタブ。設定の入力欄は出ていない
  await expect(tabs.nth(1)).toHaveClass(/active/);
  await expect(
    page.getByRole("button", { name: "新しいコマンド" }),
  ).toBeVisible();
  await expect(
    page.locator(".vscode-setting-label", { hasText: "ホスト" }),
  ).toHaveCount(0);

  // どのタブでも、見出しに接続先が 1 行で出る
  const summary = page.locator(".client-settings-summary");
  await expect(summary).toContainText(
    `ホスト: 127.0.0.1 / ポート: ${MODBUS_PORT}`,
  );
  await shot(page, "client-tab-command");

  // 設定のタブ: 入力欄が出て、コマンドの一覧は出ない
  await selectClientTab(page, "設定");
  await expect(setting(page, "ホスト")).toHaveValue("127.0.0.1");
  await expect(
    page.getByRole("button", { name: "新しいコマンド" }),
  ).toHaveCount(0);
  await shot(page, "client-tab-settings");

  // 設定を変えたままほかのタブに移っても、変更は消えない。設定のタブに印が付き、要約も入力中の内容になる
  await setting(page, "ポート").fill("1502");
  await selectClientTab(page, "ポーリング");
  await expect(tabs.nth(0)).toHaveText("設定 *");
  await expect(summary).toContainText("ポート: 1502");
  await selectClientTab(page, "設定");
  await expect(setting(page, "ポート")).toHaveValue("1502");
  await page.getByRole("button", { name: "元に戻す" }).click();
  await expect(tabs.nth(0)).toHaveText("設定");
  await expect(setting(page, "ポート")).toHaveValue(String(MODBUS_PORT));

  // 選んでいるタブは、別の画面に移って戻ると、コマンドに戻る（通信画面を作り直すため）
  await page.getByTitle("Variables").click();
  await openClient(page, "自分");
  await expect(tabs.nth(1)).toHaveClass(/active/);
});

test("接続先を追加した直後は、設定のタブが開く", async ({ page }) => {
  await addClient(page);
  await expect(
    page.locator(".client-sub-tabs .sub-tab-button.active"),
  ).toHaveText("コマンド");
  await addClientFromDialog(page, { name: "新しい接続先" });
  await expect(page.locator(".server-config-pane-name")).toHaveText(
    "新しい接続先",
  );
  await expect(
    page.locator(".client-sub-tabs .sub-tab-button.active"),
  ).toHaveText("設定");
  await expect(setting(page, "ホスト")).toBeVisible();
});
