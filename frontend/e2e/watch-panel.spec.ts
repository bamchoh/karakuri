import { test, expect, Page } from "@playwright/test";
import {
  MODBUS_PORT,
  addModbusServer,
  api,
  createVariable,
  dialogByTitle,
  openApp,
  resetApp,
  shot,
} from "./helpers";

// 下の帯の「モニタリング」を確認する:
//   - 登録したレジスタ・変数・接続先の値（サブスクリプション・ポーリング）が、どのページにいても見える
//   - 値を押すと、レジスタと変数（数値・真偽・文字列）に書ける
//   - 変数・接続先を消すと、その項目も消える

const OPCUA_PORT = 48402;

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await openApp(page);
  await resetApp(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

const watchTab = (page: Page) => page.locator(".app-log-tab");
const rows = (page: Page) => page.getByTestId("watch-row");
const row = (page: Page, label: string) =>
  rows(page).filter({
    has: page.locator(".watch-label", { hasText: new RegExp(`^${label}$`) }),
  });
// 中身の行（構造体のフィールド・配列の要素・接続先の値の 1 つずつ）
const children = (page: Page) => page.getByTestId("watch-child");
const child = (page: Page, label: string) =>
  children(page).filter({
    has: page.locator(".watch-label", {
      hasText: new RegExp(`^${label.replace(/[[\]]/g, "\\$&")}$`),
    }),
  });
const value = (page: Page, label: string) =>
  row(page, label).locator(".watch-value");

test("レジスタと変数を足すと、どのページでも値が見え、押すと書ける", async ({
  page,
}) => {
  await addModbusServer(page);
  await createVariable(page, "Counter", "INT", 5);
  await createVariable(page, "Flag", "BOOL", false);
  await createVariable(page, "Values", "ARRAY[1..3] OF INT");
  await api(page, "WriteWord", "modbus-tcp", "holdingRegisters", 0, 77);
  await openApp(page);

  // 開くと、はじめは項目がない
  await watchTab(page).click();
  await expect(watchTab(page)).toHaveAttribute("aria-expanded", "true");
  await expect(page.locator(".watch-empty")).toBeVisible();

  // レジスタを足す（サーバーの「モニタリング」のタブと同じダイアログ）
  await page.locator(".watch-toolbar").getByText("レジスタ").click();
  const addDialog = page.locator(".dialog");
  await addDialog.getByRole("button", { name: /追加|保存/ }).click();
  await expect(rows(page)).toHaveCount(1);
  await expect(rows(page).first().locator(".watch-source")).toHaveText(
    "Modbus TCP",
  );
  await expect(rows(page).first().locator(".watch-value")).toHaveText("77");

  // 変数を足す（絞り込んで、チェックで選ぶ）
  await page.locator(".watch-toolbar").getByText("変数").click();
  const pick = dialogByTitle(page, "変数をモニタリングに追加");
  await expect(pick.locator(".watch-pick-item")).toHaveCount(3);
  await pick.getByLabel("絞り込み").fill("a");
  await expect(pick.locator(".watch-pick-item")).toHaveCount(2);
  await pick.getByLabel("絞り込み").fill("");
  for (const name of ["Counter", "Flag", "Values"]) {
    await pick.locator(".watch-pick-item", { hasText: name }).click();
  }
  await pick.getByRole("button", { name: "追加（3）" }).click();
  await expect(pick).toBeHidden();
  await expect(rows(page)).toHaveCount(4);
  await expect(value(page, "Counter")).toHaveText("5");
  await expect(value(page, "Flag")).toHaveText("FALSE");
  // 配列は、要素を 1 つずつの行にして、字下げして出す（添字は宣言どおり 1 から）
  await expect(value(page, "Values")).toHaveText("");
  await expect(children(page).locator(".watch-label")).toHaveText([
    "[1]",
    "[2]",
    "[3]",
  ]);
  await expect(children(page).locator(".watch-value")).toHaveText([
    "0",
    "0",
    "0",
  ]);

  // ページを移っても出たまま。ほかから変わった値を追う
  await page.getByTitle("Scripts").click();
  await api(page, "RunScriptOnce", "vars.Counter = 123; vars.Values[2] = 9;");
  await expect(value(page, "Counter")).toHaveText("123");
  await expect(child(page, "[2]").locator(".watch-value")).toHaveText("9");
  await shot(page, "watch-panel");

  // 変数の値を押すと書ける。型に合わない値は、理由を出して閉じない
  await value(page, "Counter").click();
  const write = dialogByTitle(page, "変数の書き込み");
  await write.locator("input").fill("70000");
  await write.getByRole("button", { name: "書き込み" }).click();
  await expect(write.locator(".watch-write-error")).toBeVisible();
  await write.locator("input").fill("0x10");
  await write.locator("input").press("Enter");
  await expect(write).toBeHidden();
  await expect(value(page, "Counter")).toHaveText("16");

  // 真偽はボタンで書く
  await value(page, "Flag").click();
  await dialogByTitle(page, "変数の書き込み")
    .getByRole("button", { name: "TRUE" })
    .click();
  await expect(value(page, "Flag")).toHaveText("TRUE");

  // 配列の要素の行を押すと、その要素に書ける
  await child(page, "[3]").locator(".watch-value").click();
  const elemWrite = dialogByTitle(page, "変数の書き込み");
  await expect(elemWrite.locator(".dialog-value").first()).toHaveText(
    "Values[3]",
  );
  await elemWrite.locator("input").fill("33");
  await elemWrite.getByRole("button", { name: "書き込み" }).click();
  await expect(child(page, "[3]").locator(".watch-value")).toHaveText("33");

  // 中身は畳める。欄を閉じて開いても、畳んだまま
  await row(page, "Values").getByLabel("中身を畳む").click();
  await expect(children(page)).toHaveCount(0);
  await watchTab(page).click();
  await watchTab(page).click();
  await expect(rows(page)).toHaveCount(4);
  await expect(children(page)).toHaveCount(0);
  await row(page, "Values").getByLabel("中身を開く").click();
  await expect(children(page)).toHaveCount(3);

  // レジスタの値も、押すと書ける
  await rows(page).first().locator(".watch-value").click();
  const regWrite = dialogByTitle(page, "レジスタ書き込み");
  await regWrite.locator("input").fill("88");
  await regWrite.getByRole("button", { name: "書き込み" }).click();
  await expect(rows(page).first().locator(".watch-value")).toHaveText("88");

  // 外すと消える。変数を消すと、その項目も消える
  await row(page, "Flag").getByLabel("モニタリングから外す").click();
  await expect(rows(page)).toHaveCount(3);
  const vars = await api<{ id: string; name: string }[]>(page, "GetVariables");
  await api(page, "DeleteVariable", vars.find((v) => v.name === "Values")!.id);
  await expect(rows(page)).toHaveCount(2);

  // サーバーの「モニタリング」のタブには、そのサーバーのレジスタだけが出る
  await page.getByTitle("Connections", { exact: true }).click();
  await page
    .locator(".server-sub-tabs .sub-tab-button", { hasText: /^モニタリング$/ })
    .click();
  await expect(page.locator(".monitoring-table tbody tr")).toHaveCount(1);

  // ログのタブに切り替えられ、もう一度押すと閉じる
  await page.locator(".app-log-toggle").click();
  await expect(page.locator(".app-log-output")).toBeVisible();
  await expect(page.locator(".watch-panel")).toHaveCount(0);
  await watchTab(page).click();
  await expect(page.locator(".watch-panel")).toBeVisible();
  await watchTab(page).click();
  await expect(page.locator(".watch-panel")).toHaveCount(0);
});

test("接続先のサブスクリプションとポーリングの値を足せる", async ({ page }) => {
  // Karakuri 自身の OPC UA サーバーと Modbus TCP サーバーに、接続先からつなぐ
  await addModbusServer(page);
  await api(page, "WriteWord", "modbus-tcp", "holdingRegisters", 0, 7);
  await api(page, "AddServer", "opcua", "");
  const cfg = await api<{ settings: Record<string, unknown> }>(
    page,
    "GetServerConfig",
    "opcua",
  );
  await api(page, "UpdateServerConfig", {
    protocolType: "opcua",
    variant: "",
    settings: { ...cfg.settings, host: "127.0.0.1", port: OPCUA_PORT },
  });
  const v = await createVariable(page, "Temp", "INT", 21);
  await api(page, "UpdateVariableNodePublishing", v.id, "opcua", {
    protocolType: "opcua",
    enabled: true,
    accessMode: "readwrite",
  });
  await api(page, "StartServer", "opcua");
  const addrs = await api<Record<string, { address: string }>>(
    page,
    "GetNodeAddresses",
    "opcua",
    [v.id],
  );
  const nodeId = Object.values(addrs)[0].address;

  const ua = await api<{ id: string }>(page, "AddClient", "opcua", "UA");
  await api(page, "UpdateClient", ua.id, "UA", {
    endpointUrl: `opc.tcp://127.0.0.1:${OPCUA_PORT}`,
  });
  await api(page, "AddClientSubscriptionItems", ua.id, { nodeId });
  await api(page, "SetClientSubscriptionInterval", ua.id, 100);

  const mb = await api<{ id: string }>(page, "AddClient", "modbus-tcp", "MB");
  await api(page, "UpdateClient", mb.id, "MB", {
    host: "127.0.0.1",
    port: MODBUS_PORT,
    unitId: 1,
    timeoutMs: 500,
  });
  await api(
    page,
    "AddClientPollItem",
    mb.id,
    "3",
    { address: 0, count: 2 },
    "",
  );
  await api(page, "SetClientPollInterval", mb.id, 100);
  await openApp(page);

  await watchTab(page).click();
  await page.locator(".watch-toolbar").getByText("接続先の値").click();
  const pick = dialogByTitle(page, "接続先の値をモニタリングに追加");
  await expect(pick.locator(".watch-pick-item")).toHaveCount(2);
  await expect(
    pick.locator(".watch-pick-item", { hasText: "UA（サブスクリプション）" }),
  ).toContainText(nodeId);
  for (const item of await pick.locator(".watch-pick-item").all()) {
    await item.click();
  }
  await pick.getByRole("button", { name: "追加（2）" }).click();
  await expect(rows(page)).toHaveCount(2);

  // 止まっている間は、値がなく、理由が出る
  const uaRow = rows(page).filter({ hasText: "接続先: UA" });
  const mbRow = rows(page).filter({ hasText: "接続先: MB" });
  await expect(uaRow).toHaveClass(/watch-stale/);
  await expect(uaRow.locator(".watch-note")).toHaveText("監視していません");
  await expect(mbRow.locator(".watch-note")).toHaveText(
    "ポーリングしていません",
  );

  // 監視・ポーリングを始めると、値が出て、変化を追う
  await api(page, "StartClientSubscription", ua.id);
  await api(page, "StartClientPolling", mb.id);
  await expect(uaRow).toHaveClass(/watch-ok/);
  await expect(uaRow.locator(".watch-value")).toContainText("21");
  // 値をいくつも返す項目（2 個の読み取り）は、1 つずつの行にする
  await expect(children(page)).toHaveCount(2);
  await expect(children(page).first().locator(".watch-value")).toContainText(
    "7",
  );
  await api(page, "UpdateVariableValue", v.id, 22);
  await api(page, "WriteWord", "modbus-tcp", "holdingRegisters", 0, 8);
  await expect(uaRow.locator(".watch-value")).toContainText("22");
  await expect(children(page).first().locator(".watch-value")).toContainText(
    "8",
  );
  await expect(mbRow.locator(".watch-value")).toHaveText("");
  // 接続先の値は、ここからは書けない
  await expect(uaRow.locator(".watch-value")).not.toHaveClass(/writable/);
  await shot(page, "watch-panel-clients");

  // 止めても、最後の値は薄く残る
  await api(page, "StopClientSubscription", ua.id);
  await expect(uaRow).toHaveClass(/watch-stale/);
  await expect(uaRow.locator(".watch-value")).toContainText("22");

  // 接続先を消すと、その項目も消える
  await api(page, "StopClientPolling", mb.id);
  await api(page, "RemoveClient", mb.id);
  await expect(rows(page)).toHaveCount(1);
});

test("構造体は、フィールドを定義の順に 1 行ずつ出し、入れ子は畳める", async ({
  page,
}) => {
  await api(page, "RegisterStructType", {
    name: "Motor",
    fields: [
      { name: "speed", dataType: "INT", offset: 0 },
      { name: "on", dataType: "BOOL", offset: 0 },
      { name: "limits", dataType: "ARRAY[1..2] OF INT", offset: 0 },
    ],
    wordCount: 0,
  });
  const m = await createVariable(page, "Motor1", "Motor");
  await api(page, "AddMonitoringItem", {
    id: "",
    order: 0,
    protocolType: "",
    memoryArea: "",
    address: 0,
    bitWidth: 0,
    endianness: "",
    displayFormat: "",
    kind: "variable",
    variableId: m.id,
  });
  await openApp(page);
  await watchTab(page).click();

  await expect(children(page).locator(".watch-label")).toHaveText([
    "speed",
    "on",
    "limits",
    "[1]",
    "[2]",
  ]);
  await expect(children(page).locator(".watch-value")).toHaveText([
    "0",
    "FALSE",
    "",
    "0",
    "0",
  ]);
  // 入れ子は、1 段深く字下げする
  const pad = async (label: string) =>
    parseFloat(
      await child(page, label)
        .locator(".watch-label")
        .evaluate((el) => getComputedStyle(el).paddingLeft),
    );
  expect(await pad("[1]")).toBeGreaterThan(await pad("limits"));

  // フィールドに書ける（名前は、変数名からのパス）
  await child(page, "speed").locator(".watch-value").click();
  const write = dialogByTitle(page, "変数の書き込み");
  await expect(write.locator(".dialog-value").first()).toHaveText(
    "Motor1.speed",
  );
  await write.locator("input").fill("1500");
  await write.getByRole("button", { name: "書き込み" }).click();
  await expect(child(page, "speed").locator(".watch-value")).toHaveText("1500");
  await child(page, "on").locator(".watch-value").click();
  await dialogByTitle(page, "変数の書き込み")
    .getByRole("button", { name: "TRUE" })
    .click();
  await expect(child(page, "on").locator(".watch-value")).toHaveText("TRUE");
  await shot(page, "watch-panel-struct");

  // 入れ子の配列だけを畳める
  await child(page, "limits").getByLabel("中身を畳む").click();
  await expect(children(page).locator(".watch-label")).toHaveText([
    "speed",
    "on",
    "limits",
  ]);
});
