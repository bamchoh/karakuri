import { test, expect, Page } from "@playwright/test";
import {
  api,
  createVariable,
  dialogByTitle,
  getVariable,
  openApp,
  openClient,
  openRegisters,
  openServers,
  openVariables,
  resetApp,
  shot,
  variableRow,
} from "./helpers";

// SLMP（三菱電機 MELSEC）のサーバーとクライアントを、画面から確認する。
// Karakuri 自身の SLMP サーバーに、SLMP クライアントからつなぐ。
// X・Y・B・W などのデバイスは、番号を 16 進で表示・入力する（X1F は "1F"）。

const SLMP_PORT = 15007;

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await api(page, "AddServer", "slmp", "");
  const cfg = await api<{ variant: string; settings: Record<string, unknown> }>(
    page,
    "GetServerConfig",
    "slmp",
  );
  await api(page, "UpdateServerConfig", {
    protocolType: "slmp",
    variant: cfg.variant,
    settings: { ...cfg.settings, host: "127.0.0.1", port: SLMP_PORT },
  });
  await api(page, "StartServer", "slmp");
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

// レジスタ画面の上の操作欄で、ラベルがちょうど label の入力欄
function control(page: Page, label: string) {
  return page
    .locator(".register-controls .form-group", {
      has: page.locator("label", { hasText: new RegExp("^" + label + "$") }),
    })
    .locator("input, select");
}

const cells = (page: Page) => page.locator("td.matrix-cell");
const rowHeaders = (page: Page) => page.locator("td.row-header");
const colHeaders = (page: Page) => page.locator("th.col-header");

test.describe("SLMP", () => {
  test("サーバーの設定を表示する", async ({ page }) => {
    await openServers(page);
    await expect(
      page.locator(".server-tab-item", { hasText: "SLMP" }),
    ).toBeVisible();
    await expect(page.getByText("通信方式").first()).toBeVisible();
    // レジスタを持つので、タブが出る
    await expect(page.locator(".server-sub-tabs .sub-tab-button")).toHaveText([
      "設定",
      "レジスタ",
      "モニタリング",
    ]);
    await shot(page, "slmp-server-config");
  });

  test("レジスタの一覧: 10 進のデバイスは 10 列、16 進のデバイスは 16 列で、番号を 16 進で表示する", async ({
    page,
  }) => {
    await api(page, "WriteWord", "slmp", "D", 105, 1234);
    await api(page, "WriteBit", "slmp", "X", 0x1f, true);
    await api(page, "WriteWord", "slmp", "W", 0x2a, 0xbeef);
    await openRegisters(page, "一覧表示");

    // 最初のデバイスは D（10 進）。大きさは、SLMP で指せる最大
    const area = control(page, "メモリエリア");
    const start = control(page, "開始アドレス");
    const hint = page.locator(".register-controls .address-range-hint");
    await expect(area).toHaveValue("D");
    await expect(area.locator("option")).toHaveCount(28);
    await expect(hint).toHaveText("範囲: 0〜16777215");
    await expect(colHeaders(page)).toHaveCount(10);
    await expect(rowHeaders(page).nth(1)).toHaveText("10");
    await start.fill("100");
    await start.blur();
    await expect(cells(page).nth(5)).toHaveText("1234");
    await shot(page, "slmp-register-d");

    // X（16 進のビットデバイス）: 16 列で、行の先頭は 0・10・20…、列は +0〜+F
    await area.selectOption("X");
    await expect(hint).toHaveText("範囲: 0〜FFFFFF");
    await expect(colHeaders(page)).toHaveCount(16);
    await expect(colHeaders(page).first()).toHaveText("+0");
    await expect(colHeaders(page).last()).toHaveText("+F");
    await start.fill("0");
    await start.blur();
    await expect(rowHeaders(page).nth(0)).toHaveText("0");
    await expect(rowHeaders(page).nth(1)).toHaveText("10");
    await expect(rowHeaders(page).nth(9)).toHaveText("90");
    await expect(cells(page)).toHaveCount(160);
    // X1F は、2 行目の最後
    await expect(cells(page).nth(0x1f)).toHaveText("ON");
    await expect(cells(page).nth(0x1e)).toHaveText("OFF");
    await expect(page.locator(".register-navigation span")).toHaveText(
      "0 - 9F",
    );
    await shot(page, "slmp-register-x");

    // セルをクリックすると、16 進の番号で書き込みダイアログが開く。OFF にする
    await cells(page).nth(0x1f).click();
    const dialog = page.locator(".dialog", {
      has: page.locator("h3", { hasText: /^書き込み$/ }),
    });
    await expect(dialog).toContainText("1F");
    await shot(page, "slmp-register-write-dialog");
    await dialog.getByRole("button", { name: "OFF" }).click();
    await expect(dialog).toBeHidden();
    await expect(cells(page).nth(0x1f)).toHaveText("OFF");

    // 開始アドレスは 16 進で入れる。次のページも 16 進
    await start.fill("1A0");
    await start.blur();
    await expect(rowHeaders(page).first()).toHaveText("1A0");
    await page.getByRole("button", { name: "次へ" }).click();
    await expect(start).toHaveValue("240");
    // 範囲外を入れたら、最後のページに合わせる
    await start.fill("FFFFFFFF");
    await start.blur();
    await expect(start).toHaveValue("FFFF60");
    await expect(page.getByRole("button", { name: "次へ" })).toBeDisabled();

    // W（16 進のワードデバイス）
    await area.selectOption("W");
    await start.fill("20");
    await start.blur();
    await control(page, "表示形式").selectOption("hex");
    await expect(colHeaders(page)).toHaveCount(16);
    await expect(cells(page).nth(0x0a)).toHaveText("0xBEEF");
  });

  test("モニタリング: 16 進のデバイスの番号を 16 進で入れて、16 進で表示する", async ({
    page,
  }) => {
    await api(page, "WriteBit", "slmp", "Y", 0x2b, true);
    await openRegisters(page, "モニタリング");
    await page
      .locator(".monitoring-controls")
      .getByRole("button", { name: "追加" })
      .click();
    const dialog = dialogByTitle(page, "モニタリング項目を追加");
    const row = (label: string) =>
      dialog
        .locator(".dialog-row", {
          has: page.locator("label", {
            hasText: new RegExp("^" + label + ":$"),
          }),
        })
        .locator("input, select");
    await row("メモリエリア").selectOption("Y");
    await row("開始アドレス").fill("2A");
    await row("個数").fill("2");
    await shot(page, "slmp-monitoring-add");
    await dialog.getByRole("button", { name: "追加" }).click();
    await expect(dialog).toBeHidden();

    const rows = page.locator(".monitoring-table tbody tr");
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(0)).toContainText("2A");
    await expect(rows.nth(1)).toContainText("2B");
    await expect(rows.nth(1)).toContainText("ON");
    const items = await api<{ memoryArea: string; address: number }[]>(
      page,
      "GetMonitoringItems",
    );
    expect(items.map((i) => [i.memoryArea, i.address])).toEqual([
      ["Y", 0x2a],
      ["Y", 0x2b],
    ]);
  });

  test("変数のマッピング: 16 進のデバイスは番号を 16 進で表示・入力し、範囲も 16 進で出す", async ({
    page,
  }) => {
    const speed = await createVariable(page, "Speed", "INT", 12);
    const run = await createVariable(page, "Run", "BOOL", true);
    await api(page, "UpdateVariableMappings", speed.id, [
      {
        protocolType: "slmp",
        memoryArea: "D",
        address: 100,
        endianness: "little",
      },
    ]);
    await api(page, "UpdateVariableMappings", run.id, [
      {
        protocolType: "slmp",
        memoryArea: "X",
        address: 0x1f,
        endianness: "little",
      },
    ]);
    await openVariables(page);
    await expect(variableRow(page, "Speed")).toContainText("slmp:D:100");
    await expect(variableRow(page, "Run")).toContainText("slmp:X:1F");
    await shot(page, "slmp-variable-list");

    const dialog = dialogByTitle(page, /マッピング設定/);
    const areaSelect = dialog.locator("select.mapping-col").nth(1);
    const address = dialog.locator("input.mapping-col");
    const hint = dialog.locator(".address-range-hint");
    const save = dialog.getByRole("button", { name: "保存" });

    await variableRow(page, "Run").click({ button: "right" });
    await page.locator(".context-menu").getByText("マッピング").click();
    await expect(dialog).toBeVisible();
    await expect(areaSelect).toHaveValue("X");
    await expect(address).toHaveValue("1F");
    await expect(hint).toHaveText("範囲: 0〜FFFFFF");
    await shot(page, "slmp-mapping-dialog");

    // 範囲外は保存できない。最後の番号は保存できる
    await address.fill("1000000");
    await expect(hint).toContainText("アドレスが範囲外です（範囲: 0〜FFFFFF）");
    await expect(save).toBeDisabled();
    await address.fill("ffffff");
    await expect(save).toBeEnabled();

    // 16 進で入れ直して保存する。10 進のデバイス（M）に変えると、番号は 10 進になる
    await address.fill("2c");
    await save.click();
    await expect(dialog).toBeHidden();
    await expect(variableRow(page, "Run")).toContainText("slmp:X:2C");
    expect((await getVariable(page, "Run")).mappings[0]).toMatchObject({
      memoryArea: "X",
      address: 0x2c,
    });
    // サーバーのメモリに書くと、変数に反映される
    await api(page, "WriteBit", "slmp", "X", 0x2c, false);
    await expect(variableRow(page, "Run")).toContainText("FALSE");

    await variableRow(page, "Run").click({ button: "right" });
    await page.locator(".context-menu").getByText("マッピング").click();
    await areaSelect.selectOption("M");
    await expect(address).toHaveValue("44");
    await expect(hint).toHaveText("範囲: 0〜16777215");
    await dialog.getByRole("button", { name: "キャンセル" }).click();
  });

  test("一括マッピング編集でも、16 進のデバイスは 16 進で入れられる", async ({
    page,
  }) => {
    await createVariable(page, "Run", "BOOL", false);
    await createVariable(page, "Count", "INT", 0);
    await openVariables(page);
    await page.getByRole("button", { name: "一括マッピング編集" }).click();
    const dialog = dialogByTitle(page, /一括マッピング/);
    await expect(dialog).toBeVisible();
    const run = dialog.locator("tbody tr", { hasText: "Run" });
    await run.locator("select").first().selectOption("B");
    await run.locator("input.bulk-address-input").fill("a0");
    const count = dialog.locator("tbody tr", { hasText: "Count" });
    await count.locator("select").first().selectOption("W");
    await count.locator("input.bulk-address-input").fill("1FF");
    await shot(page, "slmp-bulk-mapping");
    await dialog.getByRole("button", { name: "保存" }).click();
    await expect(dialog).toBeHidden();
    expect((await getVariable(page, "Run")).mappings).toEqual([
      expect.objectContaining({ memoryArea: "B", address: 0xa0 }),
    ]);
    expect((await getVariable(page, "Count")).mappings).toEqual([
      expect.objectContaining({ memoryArea: "W", address: 0x1ff }),
    ]);
    await expect(variableRow(page, "Run")).toContainText("slmp:B:A0");
    await expect(variableRow(page, "Count")).toContainText("slmp:W:1FF");
  });

  test("クライアント: デバイス番号をデバイスの書き方で入れて、読み出す", async ({
    page,
  }) => {
    await openServers(page);
    await api(page, "WriteWord", "slmp", "D", 100, 0x1234);
    await api(page, "WriteBit", "slmp", "X", 0x1f, true);
    const client = await api<{ id: string }>(page, "AddClient", "slmp", "PLC");
    await api(page, "UpdateClient", client.id, "PLC", {
      host: "127.0.0.1",
      port: SLMP_PORT,
      transport: "tcp",
      frame: "4e",
      timeoutMs: 500,
    });
    await openClient(page, "PLC");

    await page.getByRole("button", { name: "新しいコマンド" }).click();
    const dialog = page.locator(".dialog").last();
    await expect(dialog).toBeVisible();
    const item = (label: string) =>
      dialog.locator(".vscode-setting-item", {
        has: page.locator(".vscode-setting-label", {
          hasText: new RegExp("^" + label + "$"),
        }),
      });

    // D100 を読む
    await item("デバイス番号").locator("input").fill("100");
    await shot(page, "slmp-client-command-dialog");
    await dialog.getByRole("button", { name: "送信", exact: true }).click();
    await expect(dialog.getByText("D100").first()).toBeVisible();
    await expect(dialog).toContainText("4660 (0x1234)");

    // X1F から 2 点を読む（16 進のデバイスは、番号を 16 進で入れる）
    await item("デバイス").locator("select").selectOption("X");
    await item("デバイス番号").locator("input").fill("1F");
    await item("点数").locator("input").fill("2");
    await dialog.getByRole("button", { name: "送信", exact: true }).click();
    await expect(dialog.getByText("X1F").first()).toBeVisible();
    await expect(dialog.getByText("X20").first()).toBeVisible();
    await expect(dialog).toContainText("ON");
    await shot(page, "slmp-client-command-sent");

    // 10 進のデバイスに 16 進を入れると、理由を出して、送信を押せなくする
    await item("デバイス").locator("select").selectOption("D");
    await expect(dialog).toContainText("10 進として読めません");
    await expect(
      dialog.getByRole("button", { name: "送信", exact: true }),
    ).toBeDisabled();
  });
});
