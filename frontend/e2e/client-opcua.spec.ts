import { test, expect, Page, Locator } from "@playwright/test";
import {
  api,
  openApp,
  resetApp,
  shot,
  escapeRegExp,
  openClient,
  openServers,
} from "./helpers";

// OPC UA クライアントの画面の動きを確認する。
// 接続先は、テストで起動した Karakuri 自身の OPC UA サーバー（127.0.0.1:OPCUA_PORT）の公開変数。

// OPC UA の待ち受けポート。普段使う 4840 とぶつからない値にする
const OPCUA_PORT = 48401;
const ENDPOINT = `opc.tcp://127.0.0.1:${OPCUA_PORT}`;

let nodeId = ""; // 公開変数 Counter の NodeId（ns= の番号は起動のたびに決まる）
let counterId = "";

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);

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
  const v = await api<{ id: string }>(
    page,
    "CreateVariable",
    "Counter",
    "INT",
    5,
  );
  counterId = v.id;
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
  nodeId = Object.values(addrs)[0].address;
  expect(nodeId).toMatch(/^ns=\d+;s=Counter$/);
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

const composer = (page: Page) => page.locator(".client-composer");
const preview = (page: Page) => page.getByTestId("client-preview");
const result = (page: Page) => page.getByTestId("client-result");
const sendButton = (page: Page) =>
  composer(page).getByRole("button", { name: "送信" });
const valueRow = (page: Page, label: string) =>
  result(page).locator(".client-values tr", {
    has: page.locator("th", {
      hasText: new RegExp("^" + escapeRegExp(label) + "$"),
    }),
  });

// 接続先を API で追加し、画面で選ぶ
async function addClient(page: Page) {
  const c = await api<{ id: string }>(page, "AddClient", "opcua", "UA");
  await api(page, "UpdateClient", c.id, "UA", {
    endpointUrl: ENDPOINT,
    timeoutMs: 3000,
  });
  await openClient(page, "UA");
  return c;
}

async function variableValue(page: Page) {
  const vars = await api<{ id: string; value: unknown }[]>(
    page,
    "GetVariables",
  );
  return vars.find((v) => v.id === counterId)?.value;
}

test("プロトコルを選んで接続先を追加し、設定を保存できる（パスワードは隠す）", async ({
  page,
}) => {
  await openServers(page);
  await page.getByLabel("追加する接続先のプロトコル").selectOption("opcua");
  await page.getByTitle("接続先を追加").click();
  await expect(page.locator(".server-tab-item.selected")).toContainText(
    "接続先 1",
  );
  await expect(
    page.locator(".server-config-pane-title .server-instance-variant"),
  ).toContainText("クライアント（OPC UA）");
  await expect(setting(page, "エンドポイント URL")).toHaveValue(
    "opc.tcp://127.0.0.1:4840",
  );
  await expect(setting(page, "セキュリティポリシー")).toHaveValue("None");
  await expect(setting(page, "パスワード")).toHaveAttribute("type", "password");

  await setting(page, "エンドポイント URL").fill(ENDPOINT);
  await setting(page, "ユーザー名").fill("operator");
  await setting(page, "パスワード").fill("secret-pass");
  await shot(page, "client-opcua-settings");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByText("保存していない変更があります")).toBeHidden();

  const clients = await api<any[]>(page, "GetClients");
  expect(clients).toHaveLength(1);
  expect(clients[0].protocolType).toBe("opcua");
  expect(clients[0].settings.endpointUrl).toBe(ENDPOINT);
  expect(clients[0].settings.password).toBe("secret-pass");

  // 設定の欄を閉じたときの要約に、パスワードは出さない
  await page.locator(".client-settings-toggle").click();
  const summary = page.locator(".client-settings-summary");
  await expect(summary).toContainText(ENDPOINT);
  await expect(summary).toContainText("ユーザー名: operator");
  await expect(summary).not.toContainText("secret-pass");
  await expect(summary).not.toContainText("パスワード");

  // 形の違う URL は保存できない
  await page.locator(".client-settings-toggle").click();
  await setting(page, "エンドポイント URL").fill("127.0.0.1:4840");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator(".config-error")).toContainText("opc.tcp://");
});

test("Read・Write・Browse を組み立てて送れる", async ({ page }) => {
  await addClient(page);

  // Read: 公開変数の値と型が出る
  await setting(composer(page), "NodeId").fill(nodeId);
  await expect(preview(page)).not.toHaveText("");
  await sendButton(page).click();
  await expect(result(page).locator(".client-status")).toHaveText("正常");
  await expect(result(page)).toContainText(`Read ${nodeId} の Value`);
  await expect(valueRow(page, "Value（Int16）")).toContainText("5");
  await expect(page.locator(".server-status-badge")).toHaveText("接続中");
  await shot(page, "client-opcua-read");

  // Write: 変数に入る
  await setting(composer(page), "コマンド").selectOption({
    label: "Write（ノードの値の書き込み）",
  });
  await setting(composer(page), "NodeId").fill(nodeId);
  await setting(composer(page), "値").fill("-42");
  await sendButton(page).click();
  await expect(result(page)).toContainText(`Write ${nodeId} = -42（Int16）`);
  await expect(result(page).locator(".client-status")).toHaveText("正常");
  await expect.poll(() => variableValue(page)).toBe(-42);

  // 型が違う書き込みは、サーバーが断る（例外応答）。接続はそのまま
  await setting(composer(page), "データ型").selectOption("String");
  await sendButton(page).click();
  await expect(result(page).locator(".client-status")).toHaveText("例外応答");
  await expect(result(page).locator(".client-result-error")).toContainText(
    "BadTypeMismatch",
  );
  await expect(page.locator(".server-status-badge")).toHaveText("接続中");

  // 型に入らない値は、組み立ての欄にエラーが出て送れない
  await setting(composer(page), "データ型").selectOption("Int16");
  await setting(composer(page), "値").fill("70000");
  await expect(composer(page).locator(".client-build-error")).toContainText(
    "Int16",
  );
  await expect(sendButton(page)).toBeDisabled();

  // Browse: 公開変数のフォルダの下に変数が出る
  await setting(composer(page), "コマンド").selectOption({
    label: "Browse（子ノードの一覧）",
  });
  await setting(composer(page), "NodeId").fill(
    nodeId.replace(";s=Counter", ";s=PLCVariables"),
  );
  await sendButton(page).click();
  await expect(result(page).locator(".client-status")).toHaveText("正常");
  await expect(valueRow(page, "Counter")).toContainText(
    `${nodeId}（Variable）`,
  );
  await shot(page, "client-opcua-browse");
});

test("16 進で入れる欄に OPC UA の説明が出て、組み立てた内容を送れる", async ({
  page,
}) => {
  await addClient(page);
  await setting(composer(page), "NodeId").fill(nodeId);
  await expect(preview(page)).not.toHaveText("");
  await composer(page).getByRole("button", { name: "16 進で編集" }).click();
  await expect(
    composer(page).locator(".vscode-setting-description"),
  ).toContainText("サービスのリクエスト本体");
  await sendButton(page).click();
  await expect(result(page).locator(".client-status")).toHaveText("正常");
  await expect(valueRow(page, "Value（Int16）")).toContainText("5");

  // リクエストとして読めないバイト列は、送らずにエラーにする
  await composer(page).getByLabel("送るリクエスト（16 進）").fill("01 02 03");
  await sendButton(page).click();
  await expect(result(page).locator(".client-status")).toHaveText("エラー");
  await expect(result(page)).toContainText("解釈できないリクエスト");
});

test("ポーリングで値を追う（表示形式の選択は出さない）", async ({ page }) => {
  const c = await addClient(page);
  await api(page, "SetClientPollInterval", c.id, 100);
  await page.locator(".sub-tab-button", { hasText: "ポーリング" }).click();
  const view = page.locator(".client-polling");
  await expect(view).toBeVisible();

  // 読み取りのコマンドだけ選べる
  await expect(
    view.getByLabel("読み取りのコマンド").locator("option"),
  ).toHaveText(["Read（ノードの読み取り）"]);
  await setting(view, "NodeId").fill(nodeId);
  await view.getByRole("button", { name: "項目を追加" }).click();
  const item = page.getByTestId("client-poll-item");
  await expect(item).toHaveCount(1);
  await expect(item).toContainText(`Read ${nodeId} の Value`);
  // 値は型付きなので、10 進・16 進の表示形式は選ばせない
  await expect(item.getByLabel("表示形式")).toHaveCount(0);

  await view.getByRole("button", { name: "開始" }).click();
  await expect(item.locator(".client-status")).toHaveText("正常");
  await expect(item.locator(".client-poll-value-text")).toHaveText("5");
  await api(page, "UpdateVariableValue", counterId, 123);
  await expect(item.locator(".client-poll-value-text")).toHaveText("123");
  await shot(page, "client-opcua-polling");
  await view.getByRole("button", { name: "停止" }).click();
});
