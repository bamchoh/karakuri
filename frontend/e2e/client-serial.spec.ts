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

// Modbus RTU / Modbus ASCII のクライアント（マスター）の画面を確認する。
// 画面は RTU と ASCII で同じ（接続先の設定の項目はプロトコルの定義から作る）なので、画面の操作は RTU で確かめ、
// ASCII は既定の設定と、実機で読めることを確かめる。
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

test("Modbus ASCII の接続先は、サーバーと同じ 9600bps・7E1 が既定", async ({
  page,
}) => {
  await page.getByTitle("Connections", { exact: true }).click();
  await addClientFromDialog(page, { protocol: "modbus-ascii", name: "ASCII" });
  await expect(setting(page, "シリアルポート")).toBeVisible();
  await expect(setting(page, "ボーレート")).toHaveValue("9600");
  await expect(setting(page, "データビット")).toHaveValue("7");
  await expect(setting(page, "パリティ")).toHaveValue("E");
  await expect(page.locator(".server-config-pane")).toContainText(
    "クライアント（Modbus ASCII）",
  );
});

// 実機で、Karakuri のサーバーから読む。送ったバイト列の先頭（UnitID から。ASCII は ':' から文字のまま）も確かめる
const hardwareCases = [
  { protocol: "modbus-rtu", name: "RTU", sentHead: "01 03 00 04 00 02" },
  {
    protocol: "modbus-ascii",
    name: "ASCII",
    // ":010300040002" の文字コード
    sentHead: "3A 30 31 30 33 30 30 30 34 30 30 30 32",
  },
];

for (const hc of hardwareCases) {
  test(`実機: Karakuri の ${hc.protocol} サーバーから読み取れる`, async ({
    page,
  }) => {
    test.skip(
      serialPair.length !== 2,
      "KARAKURI_SERIAL_PAIR（例: COM4,COM6）が設定されていない",
    );
    const [serverPort, clientPort] = serialPair;

    await api(page, "AddServer", hc.protocol, "");
    const cfg = await api<any>(page, "GetServerConfig", hc.protocol);
    cfg.settings.serialPort = serverPort;
    await api(page, "UpdateServerConfig", cfg);
    await api(page, "StartServer", hc.protocol);
    await api(page, "WriteWord", hc.protocol, "holdingRegisters", 5, 1234);

    // ボーレートなどは、クライアントの既定（サーバーの既定と同じ）のまま
    const c = await api<{ id: string }>(
      page,
      "AddClient",
      hc.protocol,
      hc.name,
    );
    await api(page, "UpdateClient", c.id, hc.name, {
      serialPort: clientPort,
      unitId: 1,
      timeoutMs: 1000,
    });
    await openClient(page, hc.name);
    await sendReadHolding(page, 4, 2);

    await expect(result(page).locator(".client-status")).toHaveText("正常");
    const values = result(page).locator(".client-values tr");
    await expect(values).toHaveCount(2);
    await expect(values.nth(1)).toContainText("1234 (0x04D2)");
    await expect(result(page).locator(".client-bytes")).toContainText(
      hc.sentHead,
    );
    await shot(page, `client-${hc.name.toLowerCase()}-read`);
  });
}

test("FINS（Host Link）の接続先は、9600bps・7E2・号機番号 0 が既定", async ({
  page,
}) => {
  await page.getByTitle("Connections", { exact: true }).click();
  await addClientFromDialog(page, {
    protocol: "fins-hostlink",
    name: "HostLink",
  });
  await expect(setting(page, "シリアルポート")).toBeVisible();
  await expect(setting(page, "ボーレート")).toHaveValue("9600");
  await expect(setting(page, "データビット")).toHaveValue("7");
  await expect(setting(page, "ストップビット")).toHaveValue("2");
  await expect(setting(page, "パリティ")).toHaveValue("E");
  await expect(setting(page, "号機番号")).toHaveValue("0");
  await shot(page, "client-hostlink-settings");
});

test("実機: Karakuri の FINS（Host Link）サーバーから読み取れる", async ({
  page,
}) => {
  test.skip(
    serialPair.length !== 2,
    "KARAKURI_SERIAL_PAIR（例: COM4,COM6）が設定されていない",
  );
  const [serverPort, clientPort] = serialPair;

  await api(page, "AddServer", "fins-hostlink", "");
  const cfg = await api<any>(page, "GetServerConfig", "fins-hostlink");
  cfg.settings.serialPort = serverPort;
  cfg.settings.unit = 7;
  await api(page, "UpdateServerConfig", cfg);
  await api(page, "StartServer", "fins-hostlink");
  await api(page, "WriteWord", "fins-hostlink", "DM", 101, 1234);

  const c = await api<{ id: string }>(
    page,
    "AddClient",
    "fins-hostlink",
    "HostLink",
  );
  await api(page, "UpdateClient", c.id, "HostLink", {
    serialPort: clientPort,
    unit: 7,
    timeoutMs: 1000,
  });
  await openClient(page, "HostLink");
  await selectClientTab(page, "コマンド");
  await page.getByRole("button", { name: "新しいコマンド" }).click();
  await expect(composer(page)).toBeVisible();
  await setting(composer(page), "コマンド").selectOption({
    label: "0101 メモリエリアの読み出し",
  });
  await setting(composer(page), "アドレス").fill("100");
  await setting(composer(page), "個数").fill("2");
  await composer(page)
    .getByRole("button", { name: "送信", exact: true })
    .click();

  await expect(result(page).locator(".client-status")).toHaveText("正常");
  const values = result(page).locator(".client-values tr");
  await expect(values).toHaveCount(2);
  await expect(values.nth(1)).toContainText("1234 (0x04D2)");
  // Host Link のフレームの先頭 "@07FA0"（文字コードで 40 30 37 46 41 30）
  await expect(result(page).locator(".client-bytes")).toContainText(
    "40 30 37 46 41 30",
  );
  await shot(page, "client-hostlink-read");
});

test("MC プロトコル（シリアル）の接続先は、形式 4・サムチェックありが既定", async ({
  page,
}) => {
  await page.getByTitle("Connections", { exact: true }).click();
  await addClientFromDialog(page, { protocol: "mc-serial", name: "MC" });
  await expect(setting(page, "シリアルポート")).toBeVisible();
  await expect(setting(page, "形式")).toHaveValue("4");
  await expect(setting(page, "サムチェック")).toHaveValue("on");
  await expect(setting(page, "局番")).toHaveValue("0");
  // 形式を 5 にして保存できる
  await setting(page, "形式").selectOption("5");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByText("保存していない変更があります")).toBeHidden();
  const clients = await api<any[]>(page, "GetClients");
  expect(clients[0].settings.format).toBe(5);
  await shot(page, "client-mc-settings");
});

// 実機で、Karakuri の MC プロトコル（シリアル）のサーバーから読む。形式 4 は送ったものが ENQ "F8" で始まる
for (const mc of [
  { format: 4, sentHead: "05 46 38" },
  { format: 5, sentHead: "10 02" },
]) {
  test(`実機: Karakuri の MC プロトコル（シリアル、形式 ${mc.format}）サーバーから読み取れる`, async ({
    page,
  }) => {
    test.skip(
      serialPair.length !== 2,
      "KARAKURI_SERIAL_PAIR（例: COM4,COM6）が設定されていない",
    );
    const [serverPort, clientPort] = serialPair;

    await api(page, "AddServer", "mc-serial", "");
    const cfg = await api<any>(page, "GetServerConfig", "mc-serial");
    cfg.settings.serialPort = serverPort;
    cfg.settings.format = mc.format;
    cfg.settings.station = 2;
    await api(page, "UpdateServerConfig", cfg);
    await api(page, "StartServer", "mc-serial");
    await api(page, "WriteWord", "mc-serial", "D", 101, 1234);

    const c = await api<{ id: string }>(page, "AddClient", "mc-serial", "MC");
    await api(page, "UpdateClient", c.id, "MC", {
      serialPort: clientPort,
      format: mc.format,
      station: 2,
      timeoutMs: 1000,
    });
    await openClient(page, "MC");
    await selectClientTab(page, "コマンド");
    await page.getByRole("button", { name: "新しいコマンド" }).click();
    await expect(composer(page)).toBeVisible();
    await setting(composer(page), "コマンド").selectOption({
      label: "0401 一括読み出し",
    });
    await setting(composer(page), "デバイス番号").fill("100");
    await setting(composer(page), "点数").fill("2");
    await composer(page)
      .getByRole("button", { name: "送信", exact: true })
      .click();

    await expect(result(page).locator(".client-status")).toHaveText("正常");
    const values = result(page).locator(".client-values tr");
    await expect(values).toHaveCount(2);
    await expect(values.nth(1)).toContainText("1234 (0x04D2)");
    await expect(result(page).locator(".client-bytes")).toContainText(
      mc.sentHead,
    );
    await shot(page, `client-mc-f${mc.format}-read`);
  });
}
