import { test, expect, Page } from "@playwright/test";
import {
  addModbusServer,
  api,
  createVariable,
  escapeRegExp,
  openApp,
  resetApp,
  selectServerTab,
  shot,
} from "./helpers";

// 2 画面（左右に 2 つのページを並べる）の動きを確認する:
//   - 左の帯の「2 画面」で、通信とスクリプトが並ぶ。もう一度押すと、最後に使った欄のページだけになる
//   - 左の帯でページを選ぶと、最後に使った欄のページが替わる。もう出ているページは、その欄を選ぶだけ
//   - 境目をドラッグすると幅が変わる。並べ方は、開き直しても残る
//   - 片方でスクリプトを動かしながら、もう片方でレジスタの値が変わるのを見られる

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1500, height: 800 });
  await openApp(page);
  await resetApp(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

const pane = (page: Page, side: "left" | "right") =>
  page.locator(`.app-page[data-pane="${side}"]`);
const pageOf = (page: Page, name: "server" | "variables" | "scripts") =>
  page.locator(`.app-page[data-page="${name}"]`);
const splitButton = (page: Page) =>
  page.locator(".sidebar").getByRole("button", { name: "2 画面" });
const sidebarItem = (page: Page, title: string) =>
  page.locator(".sidebar").getByTitle(title, { exact: true });

test("2 画面にすると通信とスクリプトが並び、左の帯で最後に使った欄のページを替えられる", async ({
  page,
}) => {
  // はじめは 1 画面
  await expect(page.locator(".pane-splitter")).toHaveCount(0);
  await expect(splitButton(page)).toHaveAttribute("aria-pressed", "false");

  await splitButton(page).click();
  await expect(splitButton(page)).toHaveAttribute("aria-pressed", "true");
  await expect(pane(page, "left")).toHaveAttribute("data-page", "server");
  await expect(pane(page, "right")).toHaveAttribute("data-page", "scripts");
  await expect(pageOf(page, "variables")).toBeHidden();

  // 左が左に、右が右に出て、幅は半分ずつ
  const left = (await pane(page, "left").boundingBox())!;
  const right = (await pane(page, "right").boundingBox())!;
  expect(left.x).toBeLessThan(right.x);
  expect(Math.abs(left.width - right.width)).toBeLessThan(12);

  // 左の帯: 最後に使った欄（左）のページが選ばれ、もう片方のページにも印が付く
  await expect(sidebarItem(page, "Connections")).toHaveClass(/active/);
  await expect(sidebarItem(page, "Scripts")).toHaveClass(/shown/);

  // 出ていないページを選ぶと、最後に使った欄（左）のページが替わる
  await sidebarItem(page, "Variables").click();
  await expect(pane(page, "left")).toHaveAttribute("data-page", "variables");
  await expect(pane(page, "right")).toHaveAttribute("data-page", "scripts");

  // もう出ているページを選ぶと、その欄が最後に使った欄になるだけ（並びは変わらない）
  await sidebarItem(page, "Scripts").click();
  await expect(pane(page, "left")).toHaveAttribute("data-page", "variables");
  await expect(pane(page, "right")).toHaveClass(/app-page-focused/);
  await sidebarItem(page, "Connections").click();
  await expect(pane(page, "right")).toHaveAttribute("data-page", "server");

  // 欄の中を押しても、その欄が最後に使った欄になる
  await pane(page, "left").click({ position: { x: 200, y: 300 } });
  await expect(pane(page, "left")).toHaveClass(/app-page-focused/);
  await expect(sidebarItem(page, "Variables")).toHaveClass(/active/);

  // 1 画面に戻すと、最後に使った欄のページが残る。もう一度 2 画面にすると、前の右のページが出る
  await splitButton(page).click();
  await expect(page.locator(".pane-splitter")).toHaveCount(0);
  await expect(pageOf(page, "variables")).toBeVisible();
  await expect(pageOf(page, "server")).toBeHidden();
  await splitButton(page).click();
  await expect(pane(page, "left")).toHaveAttribute("data-page", "variables");
  await expect(pane(page, "right")).toHaveAttribute("data-page", "server");
});

test("境目をドラッグすると幅が変わり、並べ方は開き直しても残る", async ({
  page,
}) => {
  await splitButton(page).click();
  const splitter = page.locator(".pane-splitter");
  const main = (await page.locator(".app-main").boundingBox())!;
  const box = (await splitter.boundingBox())!;

  // 左から 30% のところまでドラッグする
  await page.mouse.move(box.x + box.width / 2, box.y + 200);
  await page.mouse.down();
  await page.mouse.move(main.x + main.width * 0.3, box.y + 200, { steps: 5 });
  await page.mouse.up();
  await expect(splitter).toHaveAttribute("aria-valuenow", "30");
  const left = (await pane(page, "left").boundingBox())!;
  expect(Math.abs(left.width - main.width * 0.3)).toBeLessThan(4);

  // 端まで引いても、どちらの欄も残る（20%〜80%）
  await page.mouse.move(main.x + main.width * 0.3, box.y + 200);
  await page.mouse.down();
  await page.mouse.move(main.x + main.width - 5, box.y + 200, { steps: 5 });
  await page.mouse.up();
  await expect(splitter).toHaveAttribute("aria-valuenow", "80");

  // 開き直しても、2 画面と幅が残る
  await openApp(page);
  await expect(pane(page, "left")).toHaveAttribute("data-page", "server");
  await expect(pane(page, "right")).toHaveAttribute("data-page", "scripts");
  await expect(page.locator(".pane-splitter")).toHaveAttribute(
    "aria-valuenow",
    "80",
  );

  // ダブルクリックで半分に戻る
  await page.locator(".pane-splitter").dblclick();
  await expect(page.locator(".pane-splitter")).toHaveAttribute(
    "aria-valuenow",
    "50",
  );
});

test("スクリプトを動かしながら、隣でレジスタの値が変わるのを見られる", async ({
  page,
}) => {
  await addModbusServer(page);
  const v = await createVariable(page, "Counter", "INT", 0);
  await api(page, "UpdateVariableMappings", v.id, [
    {
      protocolType: "modbus-tcp",
      memoryArea: "holdingRegisters",
      address: 0,
      endianness: "big",
    },
  ]);
  await api(page, "CreateScript", "count", "vars.Counter = 1234;", 100);
  await openApp(page);

  // 左に通信画面のレジスタ、右にスクリプト
  await splitButton(page).click();
  await selectServerTab(page, "レジスタ");
  const firstCell = pane(page, "left").locator("td.matrix-cell").first();
  await expect(firstCell).toHaveText("0");

  // 右の欄でスクリプトを 1 回実行すると、左のレジスタに出る（ページを移らない）
  const scripts = pane(page, "right");
  await scripts.getByText("count", { exact: true }).click();
  await scripts.getByRole("button", { name: "1 回実行" }).click();
  await expect(firstCell).toHaveText("1234");
  await expect(scripts.locator(".console-entry").last()).toContainText(
    "=> 1234",
  );
  await shot(page, "split-view");
});

test("通信画面に保存していない変更があると、隠れるときだけ確認する", async ({
  page,
}) => {
  await api(page, "AddServer", "modbus-tcp", "");
  await openApp(page);
  await splitButton(page).click();

  const port = pane(page, "left")
    .locator(".vscode-setting-item", {
      has: page.locator(".vscode-setting-label", {
        hasText: new RegExp("^" + escapeRegExp("ポート") + "$"),
      }),
    })
    .locator("input")
    .first();
  await port.fill("15999");

  // 通信画面が出たままなら、確認しない（右の欄のページを替える）
  await pane(page, "right").click({ position: { x: 300, y: 300 } });
  await sidebarItem(page, "Variables").click();
  await expect(pane(page, "right")).toHaveAttribute("data-page", "variables");
  await expect(page.getByText("保存せずに移動")).toHaveCount(0);
  await expect(port).toHaveValue("15999");

  // 通信画面が隠れるときは、確認する。やめれば、そのまま
  await splitButton(page).click();
  await expect(
    page.getByRole("button", { name: "保存せずに移動" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "キャンセル" }).click();
  await expect(pane(page, "left")).toHaveAttribute("data-page", "server");
  await expect(port).toHaveValue("15999");
});

test("ログの出どころを押すと、出ていないページは最後に使った欄に出る", async ({
  page,
}) => {
  await api(page, "AddServer", "modbus-tcp", "");
  await openApp(page);
  // 左に変数、右にスクリプト（通信画面は出ていない）
  await splitButton(page).click();
  await sidebarItem(page, "Variables").click();
  await expect(pane(page, "left")).toHaveAttribute("data-page", "variables");

  await pane(page, "right").click({ position: { x: 300, y: 300 } });
  await page.locator(".app-log-toggle").click();
  await page
    .locator(".app-log-link", { hasText: "[Modbus TCP]" })
    .first()
    .click();
  await expect(pane(page, "right")).toHaveAttribute("data-page", "server");
  await expect(pane(page, "left")).toHaveAttribute("data-page", "variables");
  await expect(
    pane(page, "right").locator(".server-config-pane-name"),
  ).toHaveText("Modbus TCP");
});
