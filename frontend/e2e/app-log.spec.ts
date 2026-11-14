import { test, expect, Page } from "@playwright/test";
import {
  addModbusServer,
  api,
  MODBUS_PORT,
  openApp,
  openServers,
  openVariables,
  resetApp,
  shot,
} from "./helpers";

// アプリ全体のログの欄（どのページでも画面の下に出る）を確認する。
// サーバーの起動・停止などの出来事が出て、閉じている間のエラーは見出しで分かる。

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

const panel = (page: Page) => page.locator(".app-log");
const toggle = (page: Page) => page.locator(".app-log-toggle");
const entries = (page: Page) => page.locator(".app-log-entry");
const entry = (page: Page, text: string) =>
  page.locator(".app-log-entry", { hasText: text });

test("サーバーの出来事がログに出て、どのページからも開ける", async ({
  page,
}) => {
  // 最初は閉じていて、見出しだけが出ている
  await expect(panel(page)).toBeVisible();
  await expect(page.locator(".app-log-output")).toHaveCount(0);
  await expect(toggle(page)).toContainText("ログ");
  await expect(toggle(page)).toContainText("0 件");

  // サーバーを追加して起動する（API から）。閉じている間は、最後の 1 件が見出しに出る
  await addModbusServer(page);
  await expect(page.locator(".app-log-last")).toContainText(
    "[Modbus TCP] サーバーを起動しました",
  );
  await expect(page.locator(".app-log-badge")).toHaveCount(0);

  // 開くと、追加・設定の変更・起動の順に並ぶ
  await toggle(page).click();
  await expect(page.locator(".app-log-output")).toBeVisible();
  await expect(entries(page)).toHaveCount(3);
  await expect(entries(page).nth(0)).toContainText("サーバーを追加しました");
  await expect(entries(page).nth(1)).toContainText("設定を変更しました");
  await expect(entries(page).nth(2)).toContainText("サーバーを起動しました");
  await expect(entries(page).nth(2).locator(".app-log-source")).toHaveText(
    "[Modbus TCP]",
  );
  await expect(entries(page).nth(2).locator(".app-log-level")).toHaveText(
    "情報",
  );

  // 開いている間に起きた出来事は、その場で足される
  await api(page, "StopServer", "modbus-tcp");
  await expect(entry(page, "サーバーを停止しました")).toHaveCount(1);
  await shot(page, "app-log-open");

  // ほかのページに移っても、開いたまま同じ内容が見える
  await openVariables(page);
  await expect(entries(page)).toHaveCount(4);
  await page.getByTitle("Scripts").click();
  await expect(entries(page)).toHaveCount(4);
  await openServers(page);

  // 全部消す
  await page.getByTitle("ログをすべて消す").click();
  await expect(entries(page)).toHaveCount(0);
  await expect(page.locator(".app-log-output")).toContainText(
    "ログはありません",
  );
  expect(await api<unknown[]>(page, "GetAppLogs")).toHaveLength(0);

  // 閉じる
  await toggle(page).click();
  await expect(page.locator(".app-log-output")).toHaveCount(0);
});

test("閉じている間に起きたエラーは、見出しに件数と内容が出る", async ({
  page,
}) => {
  await addModbusServer(page);
  // Modbus TCP が使っているポートで FINS を起動しようとすると、失敗する
  await api(page, "AddServer", "fins", "");
  const cfg = await api<{ variant: string; settings: Record<string, unknown> }>(
    page,
    "GetServerConfig",
    "fins",
  );
  await api(page, "UpdateServerConfig", {
    protocolType: "fins",
    variant: cfg.variant,
    // 待ち受けるアドレスは、Modbus TCP と同じ既定（0.0.0.0）のままにする
    settings: { ...cfg.settings, port: MODBUS_PORT },
  });
  await api(page, "StartServer", "fins").catch(() => {});

  await expect(page.locator(".app-log-badge.error")).toHaveText("エラー 1");
  await expect(page.locator(".app-log-last")).toHaveClass(/error/);
  await expect(page.locator(".app-log-last")).toContainText(
    "[FINS] サーバーの起動に失敗しました",
  );
  await shot(page, "app-log-closed-error");

  // 開くと件数の表示は消え、エラーの行は色が変わる
  await toggle(page).click();
  await expect(page.locator(".app-log-badge")).toHaveCount(0);
  const failed = entry(page, "サーバーの起動に失敗しました");
  await expect(failed).toHaveClass(/error/);
  await expect(failed.locator(".app-log-level")).toHaveText("エラー");
  await shot(page, "app-log-error");

  // 開いている間に起きたエラーは、件数に数えない（見えているので）
  await api(page, "StartServer", "fins").catch(() => {});
  await expect(entry(page, "サーバーの起動に失敗しました")).toHaveCount(2);
  await toggle(page).click();
  await expect(page.locator(".app-log-badge")).toHaveCount(0);
});

test("ページを開き直しても、それまでのログが読める", async ({ page }) => {
  await addModbusServer(page);
  await page.reload();
  await openApp(page);
  await toggle(page).click();
  await expect(entry(page, "サーバーを起動しました")).toHaveCount(1);
});
