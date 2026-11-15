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

test.describe("SLMP クライアントのランダム読み出し・書き込み", () => {
  test.beforeEach(async ({ page }) => {
    const client = await api<{ id: string }>(page, "AddClient", "slmp", "PLC");
    await api(page, "UpdateClient", client.id, "PLC", {
      host: "127.0.0.1",
      port: SLMP_PORT,
      transport: "tcp",
      frame: "4e",
      timeoutMs: 500,
    });
    await openClient(page, "PLC");
  });

  // コマンドのダイアログと、その中の表（"rows"）
  async function openDialog(page: Page, command: string) {
    await page.getByRole("button", { name: "新しいコマンド" }).click();
    const dialog = page.locator(".dialog").last();
    await expect(dialog).toBeVisible();
    await dialog
      .locator(".vscode-setting-item", {
        has: page.locator(".vscode-setting-label", { hasText: /^コマンド$/ }),
      })
      .locator("select")
      .selectOption({ label: command });
    const table = dialog.locator(".rows-table");
    await expect(table).toBeVisible();
    return { dialog, table };
  }

  // 表の i 行目のセル（列のラベルで選ぶ）
  const cell = (table: ReturnType<Page["locator"]>, i: number, label: string) =>
    table.locator("tbody tr").nth(i).getByLabel(label, { exact: true });

  test("0403: 行を足してデバイスを並べ、ワードとダブルワードを読む", async ({
    page,
  }) => {
    await api(page, "WriteWord", "slmp", "D", 100, 0x1234);
    await api(page, "WriteWord", "slmp", "D", 200, 0x1170); // D200〜D201 = 70000
    await api(page, "WriteWord", "slmp", "D", 201, 0x0001);
    await api(page, "WriteBit", "slmp", "M", 3, true);

    const { dialog, table } = await openDialog(page, "0403 ランダム読み出し");
    // 最初は 1 行
    await expect(table.locator("tbody tr")).toHaveCount(1);
    await cell(table, 0, "デバイス番号").fill("100");

    // 行を足すと、最後の行を写した行が増える
    await dialog.getByRole("button", { name: "行を追加" }).click();
    await expect(table.locator("tbody tr")).toHaveCount(2);
    await expect(cell(table, 1, "デバイス番号")).toHaveValue("100");
    await cell(table, 1, "デバイス番号").fill("200");
    await cell(table, 1, "単位").selectOption("dword");

    await dialog.getByRole("button", { name: "行を追加" }).click();
    await cell(table, 2, "デバイス").selectOption("M");
    await cell(table, 2, "デバイス番号").fill("0");
    await cell(table, 2, "単位").selectOption("word");
    await expect(dialog).toContainText("3 行 / 最大 192");
    await shot(page, "slmp-random-read-rows");

    await dialog.getByRole("button", { name: "送信", exact: true }).click();
    const result = dialog.getByTestId("client-dialog-result");
    await expect(result.locator(".client-status")).toHaveText("正常");
    await expect(result).toContainText(
      "0403 ランダム読み出し ワード 2 点・ダブルワード 1 点",
    );
    const values = result.locator(".client-poll-value");
    await expect(values).toHaveCount(3);
    await expect(values.nth(0)).toContainText("D100");
    await expect(values.nth(0)).toContainText("4660 (0x1234)");
    await expect(values.nth(1)).toContainText("M0 から 16 点");
    await expect(values.nth(1)).toContainText("8 (0x0008)");
    await expect(values.nth(2)).toContainText("D200（ダブルワード）");
    await expect(values.nth(2)).toContainText("70000 (0x00011170)");
    // ダブルワードの長い値も、欄からはみ出さない（切れずに全部見える）
    const overflow = await values
      .nth(2)
      .locator(".client-poll-value-text")
      .evaluate((el) => {
        const cell = el.parentElement!.getBoundingClientRect();
        const text = el.getBoundingClientRect();
        return text.right - cell.right;
      });
    expect(overflow).toBeLessThanOrEqual(1);

    // 行を消せる
    await table.getByRole("button", { name: "1 行目を削除" }).click();
    await expect(table.locator("tbody tr")).toHaveCount(2);
    await expect(cell(table, 0, "デバイス番号")).toHaveValue("200");
    await shot(page, "slmp-random-read-result");
  });

  test("1402: ワード・ダブルワードと、ビットを書く。混ぜると理由が出る", async ({
    page,
  }) => {
    const { dialog, table } = await openDialog(page, "1402 ランダム書き込み");
    await cell(table, 0, "デバイス番号").fill("10");
    await cell(table, 0, "値").fill("0x1234");
    await dialog.getByRole("button", { name: "行を追加" }).click();
    await cell(table, 1, "デバイス番号").fill("20");
    await cell(table, 1, "単位").selectOption("dword");
    await cell(table, 1, "値").fill("-2");
    await dialog.getByRole("button", { name: "送信", exact: true }).click();
    await expect(
      dialog.getByTestId("client-dialog-result").locator(".client-status"),
    ).toHaveText("正常");
    expect(await api(page, "ReadWords", "slmp", "D", 10, 1)).toEqual([0x1234]);
    expect(await api(page, "ReadWords", "slmp", "D", 20, 2)).toEqual([
      0xfffe, 0xffff,
    ]);

    // ビットとワードを混ぜると、理由を出して送れない
    await dialog.getByRole("button", { name: "行を追加" }).click();
    await cell(table, 2, "デバイス").selectOption("M");
    await cell(table, 2, "単位").selectOption("bit");
    await cell(table, 2, "値").fill("1");
    await expect(dialog).toContainText("同じコマンドで書けません");
    await expect(
      dialog.getByRole("button", { name: "送信", exact: true }),
    ).toBeDisabled();

    // ビットだけにすれば書ける
    await table.getByRole("button", { name: "1 行目を削除" }).click();
    await table.getByRole("button", { name: "1 行目を削除" }).click();
    await expect(table.locator("tbody tr")).toHaveCount(1);
    await dialog.getByRole("button", { name: "送信", exact: true }).click();
    await expect(
      dialog.getByTestId("client-dialog-result").locator(".client-status"),
    ).toHaveText("正常");
    expect(await api(page, "ReadBits", "slmp", "M", 20, 1)).toEqual([true]);
    await shot(page, "slmp-random-write");
  });
});

test.describe("SLMP クライアントの表（行を足せる項目）を、ポーリングと保存したコマンドで使う", () => {
  test.beforeEach(async ({ page }) => {
    const client = await api<{ id: string }>(page, "AddClient", "slmp", "PLC");
    await api(page, "UpdateClient", client.id, "PLC", {
      host: "127.0.0.1",
      port: SLMP_PORT,
      transport: "tcp",
      frame: "4e",
      timeoutMs: 500,
    });
    await openClient(page, "PLC");
  });

  test("0403 をポーリングの読み取り項目にすると、デバイスごとの値が周期的に出る", async ({
    page,
  }) => {
    await api(page, "WriteWord", "slmp", "D", 5, 7);
    await api(page, "WriteWord", "slmp", "W", 0x10, 0x1170);
    await api(page, "WriteWord", "slmp", "W", 0x11, 0x0001);
    await page.locator(".sub-tab-button", { hasText: "ポーリング" }).click();
    const view = page.locator(".client-polling");
    await expect(view).toBeVisible();

    await view
      .locator(".vscode-setting-item", {
        has: page.locator(".vscode-setting-label", { hasText: /^コマンド$/ }),
      })
      .locator("select")
      .selectOption({ label: "0403 ランダム読み出し" });
    const table = view.locator(".rows-table");
    await table.getByLabel("デバイス番号", { exact: true }).fill("5");
    await view.getByRole("button", { name: "行を追加" }).click();
    const second = table.locator("tbody tr").nth(1);
    await second.getByLabel("デバイス", { exact: true }).selectOption("W");
    await second.getByLabel("デバイス番号", { exact: true }).fill("10");
    await second.getByLabel("単位", { exact: true }).selectOption("dword");
    await view.getByRole("button", { name: "項目を追加" }).click();

    const item = page.getByTestId("client-poll-item").first();
    await expect(item).toContainText(
      "0403 ランダム読み出し ワード 1 点・ダブルワード 1 点（D5, W10）",
    );
    await view.getByLabel("周期（ms）").fill("100");
    await view.getByLabel("周期（ms）").press("Enter");
    await view.getByRole("button", { name: /開始/ }).click();
    const values = item.locator(".client-poll-value-text");
    await expect(values).toHaveText(["7", "70000 (0x00011170)"]);

    // サーバーの値を変えると、次の周期で変わる
    await api(page, "WriteWord", "slmp", "D", 5, 8);
    await expect(values.first()).toHaveText("8");
    await shot(page, "slmp-random-read-polling");
    await view.getByRole("button", { name: /停止/ }).click();
  });

  test("表のあるコマンドを保存して、編集で開くと行が戻る", async ({ page }) => {
    await page.getByRole("button", { name: "新しいコマンド" }).click();
    const dialog = page.locator(".dialog").last();
    await dialog
      .locator(".vscode-setting-item", {
        has: page.locator(".vscode-setting-label", { hasText: /^コマンド$/ }),
      })
      .locator("select")
      .selectOption({ label: "1402 ランダム書き込み" });
    const table = dialog.locator(".rows-table");
    await table.getByLabel("デバイス番号", { exact: true }).fill("30");
    await table.getByLabel("値", { exact: true }).fill("99");
    await dialog.getByRole("button", { name: "行を追加" }).click();
    const second = table.locator("tbody tr").nth(1);
    await second.getByLabel("デバイス番号", { exact: true }).fill("31");
    await second.getByLabel("値", { exact: true }).fill("100");
    await dialog.getByPlaceholder("例: 速度を読む").fill("2 か所に書く");
    await dialog.getByRole("button", { name: "保存して閉じる" }).click();
    await expect(dialog).toBeHidden();

    // 保存したコマンドを送ると、書ける
    const saved = page.locator("tr", { hasText: "2 か所に書く" });
    await saved.getByRole("button", { name: "送信" }).click();
    await expect
      .poll(() => api(page, "ReadWords", "slmp", "D", 30, 2))
      .toEqual([99, 100]);

    // 編集で開くと、2 行が戻る
    await saved.getByRole("button", { name: "編集" }).click();
    const edit = page.locator(".dialog").last();
    const rows = edit.locator(".rows-table tbody tr");
    await expect(rows).toHaveCount(2);
    await expect(
      rows.nth(1).getByLabel("デバイス番号", { exact: true }),
    ).toHaveValue("31");
    await expect(rows.nth(1).getByLabel("値", { exact: true })).toHaveValue(
      "100",
    );
    await shot(page, "slmp-random-write-saved-edit");
  });
});
