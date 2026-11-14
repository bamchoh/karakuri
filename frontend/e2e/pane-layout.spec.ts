import { test, expect, Page, Locator } from "@playwright/test";
import {
  addModbusServer,
  api,
  openApp,
  openClient,
  openRegisters,
  openServers,
  resetApp,
  selectClientTab,
  shot,
} from "./helpers";

// 通信画面の右側の配置を確認する:
//   - 見出し・タブ・保存の欄は動かず、その間の内容だけがスクロールする
//   - レジスタの一覧の操作欄で、入力欄の高さがそろっている

test.beforeEach(async ({ page }) => {
  // 内容が画面に収まらない高さにする
  await page.setViewportSize({ width: 1200, height: 520 });
  await openApp(page);
  await resetApp(page);
  await addModbusServer(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

const pane = (page: Page) => page.locator(".server-config-pane");
const scroller = (page: Page) =>
  page.locator(".server-config-pane > .vscode-settings-list");

// 要素を、いちばん下までスクロールする
async function scrollToBottom(target: Locator) {
  await target.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
}

// スクロールできる（内容が、見えている高さより長い）
async function expectScrollable(target: Locator) {
  const { scrollHeight, clientHeight } = await target.evaluate((el) => ({
    scrollHeight: el.scrollHeight,
    clientHeight: el.clientHeight,
  }));
  expect(scrollHeight).toBeGreaterThan(clientHeight);
}

test("サーバー: 設定をスクロールしても、見出し・タブ・保存の欄は動かない", async ({
  page,
}) => {
  await openServers(page);
  const header = pane(page).locator(".server-config-pane-header");
  const tabs = pane(page).locator(".server-sub-tabs");
  const footer = pane(page).locator(".server-config-pane-footer");
  await expect(tabs).toBeVisible();
  const before = {
    header: await header.boundingBox(),
    tabs: await tabs.boundingBox(),
    footer: await footer.boundingBox(),
  };

  // UnitID の応答設定があるので、設定は画面に収まらない
  await expectScrollable(scroller(page));
  await scrollToBottom(scroller(page));
  expect(await scroller(page).evaluate((el) => el.scrollTop)).toBeGreaterThan(
    0,
  );

  // スクロールしても、見出し・タブ・保存の欄は同じ位置に見えている
  await expect(header).toBeInViewport({ ratio: 1 });
  await expect(tabs).toBeInViewport({ ratio: 1 });
  await expect(footer).toBeInViewport({ ratio: 1 });
  expect(await header.boundingBox()).toEqual(before.header);
  expect(await tabs.boundingBox()).toEqual(before.tabs);
  expect(await footer.boundingBox()).toEqual(before.footer);
  // 右側の枠そのものは、スクロールしない
  expect(
    await page
      .locator(".server-tab-content")
      .evaluate((el) => el.scrollHeight <= el.clientHeight + 1),
  ).toBe(true);
  await shot(page, "pane-layout-server-scrolled");
});

test("クライアント: 設定のタブにも下に固定した保存の欄があり、どのタブでもタブは動かない", async ({
  page,
}) => {
  const client = await api<{ id: string }>(
    page,
    "AddClient",
    "modbus-tcp",
    "PLC",
  );
  await openClient(page, "PLC");
  const header = pane(page).locator(".server-config-pane-header");
  const tabs = pane(page).locator(".client-sub-tabs");
  const footer = pane(page).locator(".server-config-pane-footer");

  // 保存の欄は、設定のタブにだけ出る
  await expect(footer).toHaveCount(0);
  await selectClientTab(page, "設定");
  await expect(footer).toBeVisible();
  const save = footer.getByRole("button", { name: "保存", exact: true });
  const revert = footer.getByRole("button", { name: "元に戻す" });
  // 変更がなくても「保存」は出ている。「元に戻す」は、変更があるときだけ押せる
  await expect(save).toBeVisible();
  await expect(revert).toBeDisabled();
  await expect(footer.locator(".client-note")).toHaveCount(0);

  const before = {
    tabs: await tabs.boundingBox(),
    footer: await footer.boundingBox(),
  };
  await expectScrollable(scroller(page));
  await scrollToBottom(scroller(page));
  await expect(header).toBeInViewport({ ratio: 1 });
  await expect(tabs).toBeInViewport({ ratio: 1 });
  await expect(footer).toBeInViewport({ ratio: 1 });
  expect(await tabs.boundingBox()).toEqual(before.tabs);
  expect(await footer.boundingBox()).toEqual(before.footer);

  // 変更すると、案内が出て「元に戻す」が押せる。保存できる
  await pane(page).getByLabel("名前").fill("PLC-2");
  await expect(footer.locator(".client-note")).toContainText(
    "保存していない変更があります",
  );
  await expect(revert).toBeEnabled();
  await shot(page, "pane-layout-client-settings");
  await save.click();
  await expect(revert).toBeDisabled();
  const clients = await api<{ id: string; name: string }[]>(page, "GetClients");
  expect(clients.find((c) => c.id === client.id)?.name).toBe("PLC-2");

  // コマンドのタブ: 履歴が長くなってスクロールしても、タブは動かない
  for (let i = 0; i < 12; i++) {
    await api(page, "SendClientRequest", client.id, "03 00 00 00 01");
  }
  await selectClientTab(page, "コマンド");
  await expect(footer).toHaveCount(0);
  await page.reload();
  await openApp(page);
  await openClient(page, "PLC-2");
  await expectScrollable(scroller(page));
  await scrollToBottom(scroller(page));
  await expect(tabs).toBeInViewport({ ratio: 1 });
  await expect(header).toBeInViewport({ ratio: 1 });
});

test("レジスタの一覧: 操作欄の入力欄の高さがそろっていて、範囲はその下に出る", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1400, height: 900 });
  await openRegisters(page, "一覧表示");
  const controls = page.locator(".register-controls");
  const inputs = controls.locator(".form-group > input, .form-group > select");
  await expect(inputs).toHaveCount(5);

  // 5 つの欄（メモリエリア・開始アドレス・表示形式・ビット幅・エンディアン）の上端と下端がそろっている
  const boxes = await inputs.evaluateAll((els) =>
    els.map((el) => {
      const r = el.getBoundingClientRect();
      return { top: Math.round(r.top), bottom: Math.round(r.bottom) };
    }),
  );
  for (const box of boxes) {
    expect(Math.abs(box.top - boxes[0].top)).toBeLessThanOrEqual(1);
    expect(Math.abs(box.bottom - boxes[0].bottom)).toBeLessThanOrEqual(1);
  }
  // ラベルの上端もそろっている
  const labels = await controls
    .locator(".form-group > label")
    .evaluateAll((els) =>
      els.map((el) => Math.round(el.getBoundingClientRect().top)),
    );
  for (const top of labels) {
    expect(Math.abs(top - labels[0])).toBeLessThanOrEqual(1);
  }

  // 範囲は、開始アドレスの入力欄のすぐ下、操作欄の枠の中に出る
  const hint = controls.locator(".address-range-hint");
  await expect(hint).toHaveText("範囲: 1〜65536");
  const hintBox = (await hint.boundingBox())!;
  const controlsBox = (await controls.boundingBox())!;
  expect(hintBox.y).toBeGreaterThanOrEqual(boxes[1].bottom);
  expect(hintBox.y + hintBox.height).toBeLessThanOrEqual(
    controlsBox.y + controlsBox.height,
  );
  await shot(page, "pane-layout-register-controls");
});
