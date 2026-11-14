import { test, expect } from "@playwright/test";
import { api, openApp, resetApp, shot } from "./helpers";

// 画面の外枠の配置を確認する:
//   - 上のヘッダーはない。アプリの名前とバージョンは、ウィンドウのタイトルに出す
//   - プロジェクトを開く・保存は、左のアイコンの帯の下端にある
//   - HTTP API の URL とポートの変更は、下のログの見出しの行の右端にある

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1200, height: 700 });
  await openApp(page);
  await resetApp(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

test("ヘッダーはなく、タイトルにアプリの名前とバージョンが出る", async ({
  page,
}) => {
  await expect(page.locator(".app-header")).toHaveCount(0);
  await expect(page).toHaveTitle(/^Karakuri v\d+\.\d+\.\d+$/);

  // 中身は、画面のいちばん上から始まる
  const layout = (await page.locator(".main-layout").boundingBox())!;
  expect(layout.y).toBe(0);
  await shot(page, "app-shell");
});

test("プロジェクトを開く・保存は、左の帯の下端にある", async ({ page }) => {
  const sidebar = page.locator(".sidebar");
  const open = sidebar.getByRole("button", { name: "プロジェクトオープン" });
  const save = sidebar.getByRole("button", { name: "プロジェクト保存" });
  await expect(open).toBeVisible();
  await expect(save).toBeVisible();

  // ページの切り替えは上、プロジェクトの操作は下（帯の下端にそろう）
  const sidebarBox = (await sidebar.boundingBox())!;
  const pageButton = (await sidebar
    .getByRole("button", { name: "Scripts" })
    .boundingBox())!;
  const openBox = (await open.boundingBox())!;
  const saveBox = (await save.boundingBox())!;
  expect(openBox.y).toBeGreaterThan(pageButton.y + pageButton.height);
  expect(saveBox.y).toBeGreaterThan(openBox.y);
  expect(
    Math.abs(saveBox.y + saveBox.height - (sidebarBox.y + sidebarBox.height)),
  ).toBeLessThanOrEqual(1);
});

test("HTTP API の URL は下の帯の右端に出て、ポートを変更できる", async ({
  page,
}) => {
  const original = await api<number>(page, "GetHTTPAPIPort");
  const changed = original === 18765 ? 18766 : 18765;
  const header = page.locator(".app-log-header");
  const indicator = header.locator(".httpapi-indicator");
  await expect(indicator.locator(".httpapi-url")).toHaveText(
    `http://localhost:${original}/api`,
  );

  // 右端にある（ログを開くボタンより右）
  const toggleBox = (await header.locator(".app-log-toggle").boundingBox())!;
  const indicatorBox = (await indicator.boundingBox())!;
  expect(indicatorBox.x).toBeGreaterThanOrEqual(
    toggleBox.x + toggleBox.width - 1,
  );

  try {
    // 範囲外は、理由を出して保存しない
    await indicator
      .getByRole("button", { name: "HTTP API のポートを変更" })
      .click();
    const input = indicator.getByLabel("HTTP API のポート");
    await expect(input).toHaveValue(String(original));
    await input.fill("80");
    await input.press("Enter");
    await expect(indicator.locator(".httpapi-error")).toContainText(
      "1024〜65535",
    );
    expect(await api<number>(page, "GetHTTPAPIPort")).toBe(original);
    await shot(page, "app-shell-port-edit");

    // 変更すると、表示も変わる。ポートを触っても、ログの欄は開かない
    await input.fill(String(changed));
    await input.press("Enter");
    await expect(indicator.locator(".httpapi-url")).toHaveText(
      `http://localhost:${changed}/api`,
    );
    expect(await api<number>(page, "GetHTTPAPIPort")).toBe(changed);
    await expect(page.locator(".app-log-output")).toHaveCount(0);

    // Esc でやめられる
    await indicator
      .getByRole("button", { name: "HTTP API のポートを変更" })
      .click();
    await indicator.getByLabel("HTTP API のポート").press("Escape");
    await expect(indicator.locator(".httpapi-url")).toBeVisible();
  } finally {
    await api(page, "SetHTTPAPIPort", original);
  }
});
