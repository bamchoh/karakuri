import { test, expect, Page, Locator } from "@playwright/test";
import {
  api,
  openApp,
  resetApp,
  shot,
  escapeRegExp,
  openClient,
  addClientFromDialog,
  selectClientTab,
} from "./helpers";

// Modbus RTU のクライアント（マスター）の画面を確認する。
// 実際の COM ポートで送る確認は、2 つのポートをクロスでつなぎ、テストを流す側で環境変数
// KARAKURI_SERIAL_PAIR（"COM4,COM6" の形。サーバー側, クライアント側）を設定したときだけ動く。

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

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

const composer = (page: Page) => page.locator(".client-command-dialog");
const result = (page: Page) => page.getByTestId("client-result");

// 「新しいコマンド」でダイアログを開き、FC03 を組み立てて送る
async function sendReadHolding(page: Page, address: number, count: number) {
  await selectClientTab(page, "コマンド");
  await page.getByRole("button", { name: "新しいコマンド" }).click();
  await expect(composer(page)).toBeVisible();
  await setting(composer(page), "コマンド").selectOption({
    label: "FC03 保持レジスタの読み取り",
  });
  await setting(composer(page), "アドレス").fill(String(address));
  await setting(composer(page), "個数").fill(String(count));
  await composer(page)
    .getByRole("button", { name: "送信", exact: true })
    .click();
}

test("Modbus RTU の接続先を追加し、PC のシリアルポートから選んで保存できる", async ({
  page,
}) => {
  const ports = (await api<string[]>(page, "GetSerialPorts")) || [];
  await page.getByTitle("Connections", { exact: true }).click();
  await addClientFromDialog(page, { protocol: "modbus-rtu", name: "RTU" });

  // 追加した直後は設定のタブ。シリアルの項目と UnitID・タイムアウトが出る
  const portSelect = setting(page, "シリアルポート");
  await expect(portSelect).toBeVisible();
  await expect(setting(page, "ボーレート")).toHaveValue("115200");
  await expect(setting(page, "パリティ")).toHaveValue("N");
  await expect(setting(page, "UnitID")).toHaveValue("1");
  await expect(setting(page, "タイムアウト（ms）")).toHaveValue("1000");

  // ポートの選択肢は、PC のシリアルポート（サーバーの設定と同じ一覧）
  const options = await portSelect.locator("option").allTextContents();
  for (const p of ports) expect(options).toContain(p);
  await shot(page, "client-rtu-settings");

  test.skip(ports.length === 0, "この PC にシリアルポートがない");
  await portSelect.selectOption(ports[0]);
  await setting(page, "UnitID").fill("17");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByText("保存していない変更があります")).toBeHidden();

  const clients = await api<any[]>(page, "GetClients");
  expect(clients[0].settings.serialPort).toBe(ports[0]);
  expect(clients[0].settings.unitId).toBe(17);
  // 見出しに、どこへ送るのかが出る
  await expect(page.locator(".server-config-pane")).toContainText(
    `シリアルポート: ${ports[0]}`,
  );
});

test("開けないシリアルポートに送ると、理由が結果に出る", async ({ page }) => {
  const c = await api<{ id: string }>(page, "AddClient", "modbus-rtu", "RTU");
  await api(page, "UpdateClient", c.id, "RTU", {
    serialPort: "COM199",
    unitId: 1,
    timeoutMs: 300,
  });
  await openClient(page, "RTU");
  await sendReadHolding(page, 0, 1);
  await expect(result(page).locator(".client-status")).not.toHaveText("正常");
  await expect(result(page)).toContainText("COM199 を開けませんでした");
});

const serialPair = (process.env.KARAKURI_SERIAL_PAIR || "")
  .split(",")
  .map((s) => s.trim())
  .filter(Boolean);

test("実機: Karakuri の Modbus RTU サーバーから読み取れる", async ({ page }) => {
  test.skip(
    serialPair.length !== 2,
    "KARAKURI_SERIAL_PAIR（例: COM4,COM6）が設定されていない",
  );
  const [serverPort, clientPort] = serialPair;

  await api(page, "AddServer", "modbus-rtu", "");
  const cfg = await api<any>(page, "GetServerConfig", "modbus-rtu");
  cfg.settings.serialPort = serverPort;
  await api(page, "UpdateServerConfig", cfg);
  await api(page, "StartServer", "modbus-rtu");
  await api(page, "WriteWord", "modbus-rtu", "holdingRegisters", 5, 1234);

  const c = await api<{ id: string }>(page, "AddClient", "modbus-rtu", "RTU");
  await api(page, "UpdateClient", c.id, "RTU", {
    serialPort: clientPort,
    baudRate: 115200,
    unitId: 1,
    timeoutMs: 500,
  });
  await openClient(page, "RTU");
  await sendReadHolding(page, 4, 2);

  await expect(result(page).locator(".client-status")).toHaveText("正常");
  const values = result(page).locator(".client-values tr");
  await expect(values).toHaveCount(2);
  await expect(values.nth(1)).toContainText("1234 (0x04D2)");
  // UnitID と CRC 付きの送受信バイト列
  await expect(result(page).locator(".client-bytes")).toContainText(
    "01 03 00 04 00 02",
  );
  await shot(page, "client-rtu-read");
});
