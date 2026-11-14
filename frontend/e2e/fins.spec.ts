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

  test("レジスタの一覧: ワードのエリアは 10 列、ビットのエリアは 1 行に 1 ワード（16 列）", async ({
    page,
  }) => {
    await api(page, "WriteWord", "fins", "DM", 100, 0x1234);
    await api(page, "WriteWord", "fins", "CIO", 100, 0x0021);
    await openRegisters(page, "一覧表示");

    // 最初のワードのエリア（CIO）が選ばれる。アドレスは 0 始まり
    const area = control(page, "メモリエリア");
    await expect(area).toHaveValue("CIO");
    await expect(area.locator("option")).toHaveCount(10);
    await expect(rowHeaders(page).first()).toHaveText("0");
    await expect(colHeaders(page)).toHaveCount(10);
    await shot(page, "fins-register-cio");

    // ビットのエリア: 列はビットの番号（.00〜.15）、行はワードの番号
    await area.selectOption("CIO_BIT");
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

    await variableRow(page, "Run").click({ button: "right" });
    await page.locator(".context-menu").getByText("マッピング").click();
    const dialog = dialogByTitle(page, /マッピング設定/);
    await expect(dialog).toBeVisible();
    const address = dialog.locator("input.mapping-col");
    await expect(address).toHaveValue("100.05");
    await shot(page, "fins-mapping-dialog");

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
    await row.locator("select").first().selectOption("WR_BIT");
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

  test("クライアント: 既定のプロトコルは最初に登録したもの。コマンドの項目は条件で出し分ける", async ({
    page,
  }) => {
    await openServers(page);
    // プロトコルは登録した順に並ぶ（名前順ではない）
    await expect(page.getByLabel("追加する接続先のプロトコル")).toHaveValue(
      "modbus-tcp",
    );

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
