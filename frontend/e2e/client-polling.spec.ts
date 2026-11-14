import { test, expect, Page, Locator } from "@playwright/test";
import {
  api,
  openApp,
  resetApp,
  shot,
  addModbusServer,
  writeWord,
  escapeRegExp,
  MODBUS_PORT,
  openClient,
  addClientFromDialog,
} from "./helpers";

// クライアント画面のポーリング（周期的な読み取り）を確認する。
// 接続先は、テストで起動した Karakuri 自身の Modbus TCP サーバー（127.0.0.1:MODBUS_PORT）。

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await addModbusServer(page);
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

const pollingView = (page: Page) => page.locator(".client-polling");
const items = (page: Page) => page.getByTestId("client-poll-item");
const startStop = (page: Page) =>
  pollingView(page).getByRole("button", { name: /開始|停止/ });

// 接続先を API で追加し、ポーリングのタブを開く
async function openPolling(page: Page, settings: Record<string, unknown> = {}) {
  const c = await api<{ id: string }>(page, "AddClient", "modbus-tcp", "自分");
  await api(page, "UpdateClient", c.id, "自分", {
    host: "127.0.0.1",
    port: MODBUS_PORT,
    unitId: 1,
    timeoutMs: 200,
    ...settings,
  });
  await openClient(page, "自分");
  await page.locator(".sub-tab-button", { hasText: "ポーリング" }).click();
  await expect(pollingView(page)).toBeVisible();
  return c;
}

test("読み取り項目を追加してポーリングすると、値が周期的に更新される", async ({
  page,
}) => {
  await writeWord(page, 10, 100);
  await writeWord(page, 11, 0xffff);
  const c = await openPolling(page);
  await expect(page.getByText("読み取り項目がありません")).toBeVisible();
  // 項目がなければ開始できない
  await expect(startStop(page)).toBeDisabled();

  // 読み取りのコマンドだけ選べる
  const options = await setting(pollingView(page), "コマンド")
    .locator("option")
    .allTextContents();
  expect(options.every((o) => o.includes("読み取り"))).toBe(true);

  await setting(pollingView(page), "コマンド").selectOption({
    label: "FC03 保持レジスタの読み取り",
  });
  await setting(pollingView(page), "アドレス").fill("10");
  await setting(pollingView(page), "個数").fill("2");
  await pollingView(page).getByRole("button", { name: "項目を追加" }).click();
  await expect(items(page)).toHaveCount(1);
  await expect(items(page).first()).toContainText("未送信");
  await expect(items(page).first()).toContainText(
    "FC03 保持レジスタの読み取り アドレス 10 から 2 個",
  );

  await pollingView(page).getByLabel("周期（ms）").fill("100");
  await pollingView(page).getByLabel("周期（ms）").press("Enter");
  await startStop(page).click();
  await expect(startStop(page)).toHaveText(/停止/);
  await expect(page.locator(".server-tab-item.selected")).toContainText(
    "ポーリング中",
  );

  const values = items(page).first().locator(".client-poll-value-text");
  await expect(values).toHaveText(["100", "65535"]);
  await expect(items(page).first().locator(".client-status")).toHaveText(
    "正常",
  );

  // サーバーのレジスタを変えると、次の周期で表示が変わる
  await writeWord(page, 10, 123);
  await expect(values).toHaveText(["123", "65535"]);

  // 表示形式を変える（保存される）
  await items(page).first().getByLabel("表示形式").selectOption({
    label: "10進数（符号付き）",
  });
  await expect(values).toHaveText(["123", "-1"]);
  await items(page).first().getByLabel("表示形式").selectOption({
    label: "16進数",
  });
  await expect(values).toHaveText(["0x007B", "0xFFFF"]);
  await shot(page, "client-polling");

  const p = await api<any>(page, "GetClientPolling", c.id);
  expect(p.intervalMs).toBe(100);
  expect(p.items[0].format).toBe("hex");

  await startStop(page).click();
  await expect(startStop(page)).toHaveText(/開始/);
  const stopped = (await api<any>(page, "GetClientPolling", c.id)).cycles;
  await page.waitForTimeout(400);
  expect((await api<any>(page, "GetClientPolling", c.id)).cycles).toBe(stopped);
});

test("読めなくなったら最後の値を薄く残し、後ろの項目はその周期は送らない", async ({
  page,
}) => {
  await writeWord(page, 0, 7);
  const c = await openPolling(page);
  await api(page, "AddClientPollItem", c.id, "3", { address: 0, count: 1 }, "");
  await api(page, "AddClientPollItem", c.id, "3", { address: 1, count: 1 }, "");
  await api(page, "SetClientPollInterval", c.id, 100);
  // ほかから周期を変えても、欄が追う
  await expect(pollingView(page).getByLabel("周期（ms）")).toHaveValue("100");
  await startStop(page).click();

  await expect(
    items(page).first().locator(".client-poll-value-text"),
  ).toHaveText("7");

  // 応答しない UnitID にする
  await api(page, "SetUnitIDEnabled", "modbus-tcp", 1, false);
  await expect(items(page).first().locator(".client-status")).toHaveText(
    "タイムアウト",
  );
  await expect(items(page).first().locator(".client-poll-values")).toHaveClass(
    /stale/,
  );
  await expect(
    items(page).first().locator(".client-poll-value-text"),
  ).toHaveText("7");
  await expect(items(page).first()).toContainText("に読めた値");
  await expect(items(page).nth(1).locator(".client-status")).toHaveText(
    "送っていない",
  );
  await shot(page, "client-polling-timeout");

  // 応答するように戻すと、また読める
  await api(page, "SetUnitIDEnabled", "modbus-tcp", 1, true);
  await expect(items(page).first().locator(".client-status")).toHaveText(
    "正常",
  );
  await expect(
    items(page).first().locator(".client-poll-values"),
  ).not.toHaveClass(/stale/);
});

test("読み取り項目を削除できる。タブを切り替えても選んだタブを保つ", async ({
  page,
}) => {
  const c = await openPolling(page);
  await api(page, "AddClientPollItem", c.id, "1", { address: 0, count: 8 }, "");
  await api(page, "AddClientPollItem", c.id, "4", { address: 0, count: 1 }, "");
  await expect(items(page)).toHaveCount(2);

  await items(page).first().getByLabel("読み取り項目を削除").click();
  await expect(items(page)).toHaveCount(1);
  await expect(items(page).first()).toContainText("FC04");

  // 別の接続先を選んでも、ポーリングのタブのまま
  await addClientFromDialog(page);
  await expect(page.locator(".client-tab-item")).toHaveCount(2);
  await expect(pollingView(page)).toBeVisible();
});
