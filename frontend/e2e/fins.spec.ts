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

// FINS（オムロン）のサーバーとクライアントを、画面から確認する。
// Karakuri 自身の FINS サーバーに、FINS クライアントからつなぐ。
// FINS のビットのエリア（CIO など）は、ワードのエリアと同じメモリを指し、アドレスを「ワード.ビット」（100.05）で表示する。

const FINS_PORT = 19600;

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await api(page, "AddServer", "fins", "");
  const cfg = await api<{ variant: string; settings: Record<string, unknown> }>(
    page,
    "GetServerConfig",
    "fins",
  );
  await api(page, "UpdateServerConfig", {
    protocolType: "fins",
    variant: cfg.variant,
    settings: { ...cfg.settings, host: "127.0.0.1", port: FINS_PORT },
  });
  await api(page, "StartServer", "fins");
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

test.describe("FINS", () => {
  test("サーバーの設定を表示する", async ({ page }) => {
    await openServers(page);
    await expect(
      page.locator(".server-tab-item", { hasText: "FINS" }),
    ).toBeVisible();
    await expect(page.getByText("ノードアドレス").first()).toBeVisible();
    await shot(page, "fins-server-config");
  });

  test("レジスタの一覧: ビット幅を 1bit にすると、1 行に 1 ワード（16 列）のビットで表示する", async ({
    page,
  }) => {
    await api(page, "WriteWord", "fins", "DM", 100, 0x1234);
    await api(page, "WriteWord", "fins", "CIO", 100, 0x0021);
    await openRegisters(page, "一覧表示");

    // 最初のワードのエリア（CIO）が選ばれる。アドレスは 0 始まり
    const area = control(page, "メモリエリア");
    await expect(area).toHaveValue("CIO");
    // ワード 32（CIO・WR・HR・AR・DM・T・C・EM0〜EM18）+ アップフラグ 2。
    // ワードのエリアと同じメモリを見るビットのエリア（CIO のビットなど）は、選択肢に出さない
    await expect(area.locator("option")).toHaveCount(34);
    await expect(area.locator("option", { hasText: "ビット" })).toHaveCount(0);
    await expect(rowHeaders(page).first()).toHaveText("0");
    await expect(colHeaders(page)).toHaveCount(10);
    await shot(page, "fins-register-cio");

    // ビット幅を 1bit にする: 列はビットの番号（.00〜.15）、行はワードの番号。表示形式とエンディアンは出ない
    const bitWidth = control(page, "ビット幅");
    await expect(bitWidth.locator("option")).toHaveText([
      "1bit",
      "16bit",
      "32bit",
      "64bit",
    ]);
    await bitWidth.selectOption("1");
    await expect(control(page, "表示形式")).toHaveCount(0);
    await expect(control(page, "エンディアン")).toHaveCount(0);
    await expect(colHeaders(page)).toHaveCount(16);
    await expect(colHeaders(page).first()).toHaveText(".00");
    await expect(colHeaders(page).last()).toHaveText(".15");
    await expect(cells(page)).toHaveCount(160);
    await expect(rowHeaders(page).nth(1)).toHaveText("1");

    // 開始アドレスはワードの番号で入れる
    await control(page, "開始アドレス").fill("100");
    await control(page, "開始アドレス").blur();
    await expect(rowHeaders(page).first()).toHaveText("100");
    // CIO 100 = 0x0021 なので、100.00 と 100.05 が ON
    await expect(cells(page).nth(0)).toHaveText("ON");
    await expect(cells(page).nth(5)).toHaveText("ON");
    await expect(cells(page).nth(1)).toHaveText("OFF");
    await shot(page, "fins-register-cio-bit");

    // セルをクリックすると、「ワード.ビット」のアドレスで書き込みダイアログが開く
    await cells(page)
      .nth(16 + 3)
      .click();
    const dialog = page.locator(".dialog", {
      has: page.locator("h3", { hasText: /^書き込み$/ }),
    });
    await expect(dialog).toContainText("101.03");
    await shot(page, "fins-register-write-dialog");
    await dialog.getByRole("button", { name: "ON", exact: true }).click();
    await expect(cells(page).nth(16 + 3)).toHaveText("ON");
    // ビットで書いた値は、ワードのエリアからも読める
    expect(await api(page, "ReadWords", "fins", "CIO", 101, 1)).toEqual([8]);

    // 次のページは 10 ワード先
    await page.getByRole("button", { name: "次へ" }).click();
    await expect(rowHeaders(page).first()).toHaveText("110");

    // 16bit に戻すと、同じワードが先頭に来る
    await bitWidth.selectOption("16");
    await expect(colHeaders(page)).toHaveCount(10);
    await expect(rowHeaders(page).first()).toHaveText("110");
    await expect(control(page, "開始アドレス")).toHaveValue("110");

    // 1bit のまま別のエリアに変えても 1bit のまま（DM と EM にも 1bit がある）。
    // 1bit がないエリア（タイマの現在値）では 16bit に戻る
    await bitWidth.selectOption("1");
    await area.selectOption("WR");
    await expect(bitWidth).toHaveValue("1");
    await expect(colHeaders(page)).toHaveCount(16);
    await area.selectOption("DM");
    await expect(bitWidth).toHaveValue("1");
    await area.selectOption("EM18");
    await expect(bitWidth).toHaveValue("1");
    await expect(colHeaders(page)).toHaveCount(16);
    await area.selectOption("T");
    await expect(bitWidth).toHaveValue("16");
    await expect(bitWidth.locator("option")).toHaveCount(3);
    await expect(colHeaders(page)).toHaveCount(10);
    await expect(rowHeaders(page).first()).toHaveText("110");
  });

  test("モニタリング: ビット幅を 1bit にして、「ワード.ビット」のアドレスで追加できる", async ({
    page,
  }) => {
    await api(page, "WriteWord", "fins", "CIO", 100, 0x0020);
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
    await expect(row("メモリエリア")).toHaveValue("CIO");
    await expect(row("メモリエリア").locator("option")).toHaveCount(34);

    // 100 を入れてから 1bit にすると、同じワードの先頭のビットになる
    await row("開始アドレス").fill("100");
    await row("ビット幅").selectOption("1");
    await expect(row("開始アドレス")).toHaveValue("100.00");
    await expect(row("エンディアン")).toHaveCount(0);
    await row("開始アドレス").fill("100.04");
    await row("個数").fill("2");
    await shot(page, "fins-monitoring-add-1bit");
    await dialog.getByRole("button", { name: "追加" }).click();
    await expect(dialog).toBeHidden();

    // 100.04 と 100.05 の 2 行ができ、100.05 が ON
    const rows = page.locator(".monitoring-table tbody tr");
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(0)).toContainText("100.04");
    await expect(rows.nth(1)).toContainText("100.05");
    await expect(rows.nth(1)).toContainText("ON");
    await shot(page, "fins-monitoring-bits");
    const items = await api<{ memoryArea: string; address: number }[]>(
      page,
      "GetMonitoringItems",
    );
    expect(items.map((i) => [i.memoryArea, i.address])).toEqual([
      ["CIO_BIT", 100 * 16 + 4],
      ["CIO_BIT", 100 * 16 + 5],
    ]);
  });

  test("タイマ・カウンタと EM のバンク: 現在値はワード、アップフラグは番号ごとのビットで表示する", async ({
    page,
  }) => {
    await api(page, "WriteWord", "fins", "T", 5, 123);
    await api(page, "WriteBit", "fins", "C_FLAG", 7, true);
    await api(page, "WriteWord", "fins", "EM18", 3, 0x1818);
    await openRegisters(page, "一覧表示");
    const area = control(page, "メモリエリア");

    await area.selectOption("T");
    await expect(cells(page).nth(5)).toHaveText("123");

    // アップフラグは、ワードの中のビットではないので、10 列・番号のまま
    await area.selectOption("C_FLAG");
    await expect(colHeaders(page)).toHaveCount(10);
    await expect(cells(page).nth(7)).toHaveText("ON");
    await expect(cells(page).nth(6)).toHaveText("OFF");
    await shot(page, "fins-register-counter-flag");

    await area.selectOption("EM18");
    await control(page, "表示形式").selectOption("hex");
    await expect(cells(page).nth(3)).toHaveText("0x1818");
  });

  test("変数のマッピング: ビットのエリアのアドレスを「ワード.ビット」で表示・入力する", async ({
    page,
  }) => {
    const speed = await createVariable(page, "Speed", "INT", 12);
    const run = await createVariable(page, "Run", "BOOL", true);
    await api(page, "UpdateVariableMappings", speed.id, [
      {
        protocolType: "fins",
        memoryArea: "DM",
        address: 100,
        endianness: "big",
      },
    ]);
    await api(page, "UpdateVariableMappings", run.id, [
      {
        protocolType: "fins",
        memoryArea: "CIO_BIT",
        address: 100 * 16 + 5,
        endianness: "big",
      },
    ]);
    await openVariables(page);
    await expect(variableRow(page, "Speed")).toContainText("fins:DM:100");
    await expect(variableRow(page, "Run")).toContainText("fins:CIO:100.05");
    await shot(page, "fins-variable-list");

    const dialog = dialogByTitle(page, /マッピング設定/);
    const areaSelect = dialog.locator("select.mapping-col").nth(1);
    const width = dialog.locator(".mapping-width");
    const address = dialog.locator("input.mapping-col");

    // INT の変数: ビット幅は 16bit、アドレスはワードの番号
    await variableRow(page, "Speed").click({ button: "right" });
    await page.locator(".context-menu").getByText("マッピング").click();
    await expect(dialog).toBeVisible();
    await expect(areaSelect).toHaveValue("DM");
    // ビットごとに見るエリアは、選択肢に出ない
    await expect(areaSelect.locator("option")).toHaveCount(34);
    await expect(width).toHaveText("16bit");
    await expect(address).toHaveValue("100");
    await dialog.getByRole("button", { name: "キャンセル" }).click();

    // BOOL の変数: ビット幅は 1bit、エリアは「CIO」のまま、アドレスは「ワード.ビット」
    await variableRow(page, "Run").click({ button: "right" });
    await page.locator(".context-menu").getByText("マッピング").click();
    await expect(dialog).toBeVisible();
    await expect(areaSelect).toHaveValue("CIO");
    await expect(width).toHaveText("1bit");
    await expect(address).toHaveValue("100.05");
    await shot(page, "fins-mapping-dialog");

    // エリアを DM に変えても、同じ「ワード.ビット」のまま。保存すると DM のビットになる
    await areaSelect.selectOption("DM");
    await expect(address).toHaveValue("100.05");
    await dialog.getByRole("button", { name: "保存" }).click();
    await expect(dialog).toBeHidden();
    await expect(variableRow(page, "Run")).toContainText("fins:DM:100.05");
    expect((await getVariable(page, "Run")).mappings[0]).toMatchObject({
      memoryArea: "DM_BIT",
      address: 100 * 16 + 5,
    });
    // DM 100 には INT の Speed もある。同じメモリなので、ワードで書くと両方に反映される
    await api(page, "WriteWord", "fins", "DM", 100, 0x0020);
    await expect(variableRow(page, "Run")).toContainText("TRUE");
    await expect(variableRow(page, "Speed")).toContainText("32");

    // CIO に戻す
    await variableRow(page, "Run").click({ button: "right" });
    await page.locator(".context-menu").getByText("マッピング").click();
    await expect(dialog).toBeVisible();
    await areaSelect.selectOption("CIO");

    // 「ワード.ビット」で入れ直して保存する（ビットの頭の 0 は省ける）
    await address.fill("101.3");
    await dialog.getByRole("button", { name: "保存" }).click();
    await expect(dialog).toBeHidden();
    await expect(variableRow(page, "Run")).toContainText("fins:CIO:101.03");
    expect((await getVariable(page, "Run")).mappings[0].address).toBe(
      101 * 16 + 3,
    );

    // 同じメモリなので、ワードで書くと BOOL の変数に反映される
    await api(page, "WriteWord", "fins", "CIO", 101, 0);
    await expect(variableRow(page, "Run")).toContainText("FALSE");
  });

  test("一括マッピング編集でも「ワード.ビット」で入れられる", async ({
    page,
  }) => {
    await createVariable(page, "Run", "BOOL", false);
    await openVariables(page);
    await page.getByRole("button", { name: "一括マッピング編集" }).click();
    const dialog = dialogByTitle(page, /一括マッピング/);
    await expect(dialog).toBeVisible();
    const row = dialog.locator("tbody tr", { hasText: "Run" });
    // BOOL の変数は、ビット幅が 1bit。エリアは「WR」を選ぶ（ビットごとに見るエリアは選択肢にない）
    await expect(row).toContainText("1bit");
    await expect(row.locator("select").first().locator("option")).toHaveCount(
      34,
    );
    await row.locator("select").first().selectOption("WR");
    await row.locator("input.bulk-address-input").fill("10.15");
    await shot(page, "fins-bulk-mapping");
    await dialog.getByRole("button", { name: "保存" }).click();
    await expect(dialog).toBeHidden();
    const v = await getVariable(page, "Run");
    expect(v.mappings).toEqual([
      expect.objectContaining({
        protocolType: "fins",
        memoryArea: "WR_BIT",
        address: 10 * 16 + 15,
      }),
    ]);
    await expect(variableRow(page, "Run")).toContainText("fins:WR:10.15");
  });

  test("クライアント: コマンドの項目は条件で出し分ける", async ({ page }) => {
    await openServers(page);
    await api(page, "WriteWord", "fins", "DM", 100, 0x1234);
    const client = await api<{ id: string }>(page, "AddClient", "fins", "PLC");
    await api(page, "UpdateClient", client.id, "PLC", {
      host: "127.0.0.1",
      port: FINS_PORT,
      transport: "udp",
      timeoutMs: 500,
    });
    await openClient(page, "PLC");
    await page.getByText("接続先の設定").click();

    await page.getByRole("button", { name: "新しいコマンド" }).click();
    const dialog = page.locator(".dialog").last();
    await expect(dialog).toBeVisible();
    const label = (text: string) =>
      dialog.locator(".vscode-setting-label", {
        hasText: new RegExp("^" + text + "$"),
      });

    // 「ビット」の欄は、単位がビットのときだけ出る
    await expect(label("アドレス")).toBeVisible();
    await expect(label("ビット")).toHaveCount(0);
    await shot(page, "fins-client-command-dialog");
    await dialog.locator("select").nth(2).selectOption("bit");
    await expect(label("ビット")).toBeVisible();
    await shot(page, "fins-client-command-dialog-bit");
    await dialog.locator("select").nth(2).selectOption("word");
    await expect(label("ビット")).toHaveCount(0);

    await dialog.getByRole("spinbutton").first().fill("100");
    await dialog.getByRole("button", { name: "送信", exact: true }).click();
    await expect(dialog.getByText("D100").first()).toBeVisible();
    await expect(dialog).toContainText("4660 (0x1234)");
    await shot(page, "fins-client-command-sent");
  });
});
