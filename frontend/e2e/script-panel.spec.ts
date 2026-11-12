import { test, expect, Page } from "@playwright/test";
import { api, openApp, resetApp, shot } from "./helpers";

// スクリプト画面の動きを確認する。
// スクリプトの結果とエラーは、コンソール（画面下）に出る。

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await page.getByTitle("Scripts").click();
  await expect(page.locator(".console-section")).toBeVisible();
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

interface Script {
  id: string;
  name: string;
  code: string;
  intervalMs: number;
  isRunning: boolean;
}

const getScripts = (page: Page) => api<Script[]>(page, "GetScripts");

const createScript = (page: Page, name: string, code: string, ms = 1000) =>
  api<Script>(page, "CreateScript", name, code, ms);

// 一覧の行
const row = (page: Page, name: string) =>
  page.locator(".server-tab-item", {
    has: page.locator(".server-tab-name", { hasText: new RegExp(`^${name}$`) }),
  });

// 一覧の行の開始・停止ボタン（行にマウスを乗せると出る）
async function toggle(page: Page, name: string, title: "開始" | "停止") {
  await row(page, name).hover();
  await row(page, name).getByTitle(title).click();
}

const editor = (page: Page) => page.locator(".script-panel");
const consoleEntries = (page: Page) => page.locator(".console-entry");
const errorEntries = (page: Page) => page.locator(".console-entry.error");

test("追加したスクリプトが選ばれ、名前・周期・コードを保存できる", async ({
  page,
}) => {
  await expect(page.getByText("スクリプトがありません")).toBeVisible();

  await page.getByTitle("スクリプトを追加").click();
  await expect(row(page, "新しいスクリプト")).toHaveClass(/selected/);
  await expect(editor(page).getByLabel("名前")).toHaveValue("新しいスクリプト");
  await expect(editor(page).getByLabel("実行周期")).toHaveValue("1000");

  await editor(page).getByLabel("名前").fill("カウンター");
  await editor(page).getByLabel("実行周期").selectOption({ label: "500ms" });
  await editor(page).getByLabel("コード").fill("console.log('tick');");
  await editor(page).getByRole("button", { name: "保存" }).click();

  await expect(row(page, "カウンター")).toBeVisible();
  await expect(row(page, "カウンター").locator(".script-interval")).toHaveText(
    "500ms",
  );
  const [saved] = await getScripts(page);
  expect(saved).toMatchObject({
    name: "カウンター",
    code: "console.log('tick');",
    intervalMs: 500,
  });

  // 2 つ目を追加すると「新しいスクリプト」の名前で選ばれる。1 つ目に戻ると、保存した内容が入っている
  await page.getByTitle("スクリプトを追加").click();
  await expect(row(page, "新しいスクリプト")).toHaveClass(/selected/);
  await row(page, "カウンター").click();
  await expect(editor(page).getByLabel("名前")).toHaveValue("カウンター");
  await expect(editor(page).getByLabel("実行周期")).toHaveValue("500");
  await expect(editor(page).getByLabel("コード")).toHaveValue(
    "console.log('tick');",
  );

  // 周期を変えずに保存しても、周期は変わらない
  await editor(page).getByLabel("名前").fill("カウンター2");
  await editor(page).getByRole("button", { name: "保存" }).click();
  await expect(row(page, "カウンター2")).toBeVisible();
  const scripts = await getScripts(page);
  expect(scripts.find((s) => s.name === "カウンター2")?.intervalMs).toBe(500);
  await shot(page, "script-01-editor");
});

test("1 回実行は、入力中のコードを保存せずに実行し、結果とエラーをコンソールに出す", async ({
  page,
}) => {
  await createScript(page, "試し", "// 保存済みのコード");
  await row(page, "試し").click();

  await editor(page).getByLabel("コード").fill('console.log("hello"); 1 + 2');
  await editor(page).getByRole("button", { name: "1 回実行" }).click();

  await expect(consoleEntries(page)).toHaveCount(2);
  await expect(consoleEntries(page).nth(0)).toContainText("[1 回実行]");
  await expect(consoleEntries(page).nth(0)).toContainText("hello");
  await expect(consoleEntries(page).nth(1)).toContainText("=> 3");
  await expect(errorEntries(page)).toHaveCount(0);

  // エラーは [ERROR] として出る
  await editor(page).getByLabel("コード").fill('throw new Error("boom")');
  await editor(page).getByRole("button", { name: "1 回実行" }).click();
  await expect(errorEntries(page)).toHaveCount(1);
  await expect(errorEntries(page)).toContainText("[ERROR]");
  await expect(errorEntries(page)).toContainText("boom");
  await shot(page, "script-02-run-once");

  // 保存はされず、周期実行も始まらない
  const [s] = await getScripts(page);
  expect(s.code).toBe("// 保存済みのコード");
  expect(s.isRunning).toBe(false);

  // クリアで消える
  await page
    .locator(".console-header")
    .getByRole("button", { name: "クリア" })
    .click();
  await expect(page.locator(".console-empty")).toBeVisible();
  expect(await api<unknown[]>(page, "GetConsoleLogs")).toHaveLength(0);
});

test("周期実行のエラーはコンソールに出る。同じエラーが続く間は 1 回だけ", async ({
  page,
}) => {
  await createScript(page, "失敗", 'throw new Error("毎回失敗")', 20);
  await expect(row(page, "失敗")).toBeVisible();

  await toggle(page, "失敗", "開始");
  await expect(row(page, "失敗").locator(".server-status-dot")).toHaveClass(
    /running/,
  );
  await expect(errorEntries(page)).toHaveCount(1);
  await expect(errorEntries(page)).toContainText("[失敗]");
  await expect(errorEntries(page)).toContainText("毎回失敗");

  // 20ms 周期で何十回も失敗しているが、出るのは 1 件だけ。実行は続いている
  await page.waitForTimeout(600);
  await expect(errorEntries(page)).toHaveCount(1);
  expect((await getScripts(page))[0].isRunning).toBe(true);
  await shot(page, "script-03-runtime-error");

  // 実行中は削除できない
  await row(page, "失敗").click();
  await expect(
    editor(page).getByRole("button", { name: "削除" }),
  ).toBeDisabled();

  await toggle(page, "失敗", "停止");
  await expect(row(page, "失敗").locator(".server-status-dot")).toHaveClass(
    /stopped/,
  );
  await expect(
    editor(page).getByRole("button", { name: "削除" }),
  ).toBeEnabled();
});

test("コンパイルできないスクリプトは開始できず、エラーが表示される", async ({
  page,
}) => {
  await createScript(page, "構文エラー", "invalid syntax {{{");
  await createScript(page, "正常", "1 + 1");
  await expect(row(page, "構文エラー")).toBeVisible();

  await toggle(page, "構文エラー", "開始");
  await expect(page.locator(".error-message")).toBeVisible();
  await expect(errorEntries(page)).toHaveCount(1);
  await expect(errorEntries(page)).toContainText("[構文エラー]");
  expect((await getScripts(page))[0].isRunning).toBe(false);
  await shot(page, "script-04-compile-error");

  // 次の操作が成功すると、操作のエラーの表示は消える（コンソールの記録は残る）
  await toggle(page, "正常", "開始");
  await expect(page.locator(".error-message")).toHaveCount(0);
  await expect(errorEntries(page)).toHaveCount(1);
  await toggle(page, "正常", "停止");
});

test("削除すると、同じ位置のスクリプトが選ばれる", async ({ page }) => {
  await createScript(page, "A", "1");
  await createScript(page, "B", "2");
  await createScript(page, "C", "3");
  await row(page, "B").click();

  // 確認でキャンセルすると消えない
  page.once("dialog", (d) => d.dismiss());
  await editor(page).getByRole("button", { name: "削除" }).click();
  await expect(row(page, "B")).toBeVisible();

  page.once("dialog", (d) => d.accept());
  await editor(page).getByRole("button", { name: "削除" }).click();
  await expect(row(page, "B")).toHaveCount(0);
  await expect(row(page, "C")).toHaveClass(/selected/);
  await expect(editor(page).getByLabel("名前")).toHaveValue("C");
  expect((await getScripts(page)).map((s) => s.name)).toEqual(["A", "C"]);
});
