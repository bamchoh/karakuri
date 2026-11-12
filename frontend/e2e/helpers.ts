import { Locator, Page, expect } from "@playwright/test";
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

// テストで作ったサーバー・変数・構造体型・モニタリング項目・スクリプトをすべて消して、アプリを初期状態に戻す
export async function resetApp(page: Page) {
  const vars = await api<{ id: string }[]>(page, "GetVariables");
  for (const v of vars || []) await api(page, "DeleteVariable", v.id);
  const types = await api<{ name: string }[]>(page, "GetStructTypes");
  for (const t of types || []) await api(page, "DeleteStructType", t.name);
  const servers = await api<{ protocolType: string }[]>(
    page,
    "GetServerInstances",
  );
  for (const s of servers || []) {
    await api(page, "StopServer", s.protocolType).catch(() => {});
    await api(page, "RemoveServer", s.protocolType);
  }
  await api(page, "ClearMonitoringItems");
  const scripts = await api<{ id: string }[]>(page, "GetScripts");
  for (const s of scripts || []) await api(page, "DeleteScript", s.id);
  await api(page, "ClearConsoleLogs");
}

// ===== サーバー画面・レジスタ画面の操作 =====

export async function openServers(page: Page) {
  await page.getByTitle("Server", { exact: true }).click();
  await expect(page.locator(".server-vertical-tabs")).toBeVisible();
}

// レジスタ画面を開く。tab を渡すとサブタブ（一覧表示 / モニタリング）も切り替える
export async function openRegisters(
  page: Page,
  tab?: "一覧表示" | "モニタリング",
) {
  await page.getByTitle("Registers").click();
  await expect(page.locator(".sub-tab-nav")).toBeVisible();
  if (tab) {
    await page.locator(".sub-tab-button", { hasText: tab }).click();
  }
}

// 保持レジスタ（0 始まりのアドレス）を読む・書く
export async function readWords(page: Page, address: number, count = 1) {
  return api<number[]>(
    page,
    "ReadWords",
    "modbus-tcp",
    "holdingRegisters",
    address,
    count,
  );
}

export async function writeWord(page: Page, address: number, value: number) {
  await api(page, "WriteWord", "modbus-tcp", "holdingRegisters", address, value);
}

// ===== 変数画面の操作 =====

export const MODBUS_PORT = 15020;

// 正規表現の特別な文字を、文字そのものとして扱えるようにする
export function escapeRegExp(s: string) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export async function openVariables(page: Page) {
  await page.getByTitle("Variables").click();
  await expect(page.locator(".variable-toolbar")).toBeVisible();
}

// 見出し（h3）でダイアログを特定する
export function dialogByTitle(page: Page, title: string | RegExp) {
  return page.locator(".dialog", {
    has: page.locator("h3", { hasText: title }),
  });
}

// 「ラベル: 入力欄」の行（.dialog-row）から入力欄を取る。ラベルは完全一致
export function field(scope: Locator, label: string) {
  return scope
    .locator(".dialog-row", {
      has: scope.page().locator("label", {
        hasText: new RegExp("^" + escapeRegExp(label) + "$"),
      }),
    })
    .locator("input, select")
    .first();
}

// 変数一覧で、名前の列がちょうど name の行
export function variableRow(page: Page, name: string) {
  return page.locator(".variable-view > table.variable-table > tbody > tr", {
    has: page.locator("td.var-name", {
      hasText: new RegExp("^\\s*(▶|▼)?\\s*" + escapeRegExp(name) + "$"),
    }),
  });
}

export async function createVariable(
  page: Page,
  name: string,
  dataType = "INT",
  value: unknown = 0,
) {
  return api<{ id: string; name: string }>(
    page,
    "CreateVariable",
    name,
    dataType,
    value,
  );
}

export async function getVariable(page: Page, name: string) {
  const vars = await api<any[]>(page, "GetVariables");
  return (vars || []).find((v) => v.name === name);
}

// Modbus TCP サーバーを追加して起動する（マッピングのメモリエリアを使えるようにする）
export async function addModbusServer(page: Page) {
  await api(page, "AddServer", "modbus-tcp", "");
  const cfg = await api<{ variant: string; settings: Record<string, unknown> }>(
    page,
    "GetServerConfig",
    "modbus-tcp",
  );
  const portKey = Object.keys(cfg.settings).find((k) => /port/i.test(k))!;
  await api(page, "UpdateServerConfig", {
    protocolType: "modbus-tcp",
    variant: cfg.variant,
    settings: { ...cfg.settings, [portKey]: MODBUS_PORT },
  });
  await api(page, "StartServer", "modbus-tcp");
}

// 次に出る alert / confirm を受け取って OK し、メッセージを返す
export function nextDialogMessage(page: Page): Promise<string> {
  return new Promise<string>((resolve) =>
    page.once("dialog", async (d) => {
      resolve(d.message());
      await d.accept();
    }),
  );
}
