import { Page, expect } from "@playwright/test";
import path from "path";

// スクリーンショットの保存先（git には入れない）。frontend フォルダで実行する前提
const SHOT_DIR = path.join("e2e", ".screenshots");

export async function shot(page: Page, name: string) {
  await page.screenshot({ path: path.join(SHOT_DIR, `${name}.png`) });
}

// Go 側の API（window.go.main.App.*）を呼ぶ。データの準備と後片付けに使う
export async function api<T = unknown>(
  page: Page,
  method: string,
  ...args: unknown[]
): Promise<T> {
  return page.evaluate(
    ([m, a]) => (window as any).go.main.App[m as string](...(a as unknown[])),
    [method, args] as const,
  ) as Promise<T>;
}

// アプリを開き、Go 側の API が使えるようになるまで待つ
export async function openApp(page: Page) {
  await page.goto("/");
  await page.waitForFunction(() => !!(window as any).go?.main?.App);
  await expect(page.locator("#root")).not.toBeEmpty();
}

// テストで作ったサーバーと変数をすべて消して、アプリを初期状態に戻す
export async function resetApp(page: Page) {
  const vars = await api<{ id: string }[]>(page, "GetVariables");
  for (const v of vars || []) await api(page, "DeleteVariable", v.id);
  const servers = await api<{ protocolType: string }[]>(
    page,
    "GetServerInstances",
  );
  for (const s of servers || []) {
    await api(page, "StopServer", s.protocolType).catch(() => {});
    await api(page, "RemoveServer", s.protocolType);
  }
}
