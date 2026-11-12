import { test, expect, Page } from "@playwright/test";
import {
  api,
  dialogByTitle,
  escapeRegExp,
  openApp,
  openServers,
  openVariables,
  resetApp,
  shot,
} from "./helpers";

// サーバー画面（サーバーの追加・設定・起動と停止・削除）の動きを確認する。
// 画面のコンポーネントを分割するときに、動きが変わっていないことを確かめるためのテスト。

const TCP_PORT = 15021;

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await openServers(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

// 左の一覧の、名前がちょうど name のサーバー
function serverTab(page: Page, name: string) {
  return page.locator(".server-tab-item", {
    has: page.locator(".server-tab-name", {
      hasText: new RegExp("^" + escapeRegExp(name) + "$"),
    }),
  });
}

// 右の設定で、ラベルがちょうど label の項目の入力欄
function setting(page: Page, label: string) {
  return page
    .locator(".vscode-setting-item", {
      has: page.locator(".vscode-setting-label", {
        hasText: new RegExp("^" + escapeRegExp(label) + "$"),
      }),
    })
    .locator("input, select")
    .first();
}

const pane = (page: Page) => page.locator(".server-config-pane");
const saveButton = (page: Page) =>
  page.locator(".server-config-pane-footer button");
// サーバーの追加ボタン（一覧にはクライアントの追加ボタンもある）
const addButton = (page: Page) =>
  page.getByRole("button", { name: "サーバーを追加" });

async function config(page: Page, protocolType: string) {
  return api<{ variant: string; settings: Record<string, any> }>(
    page,
    "GetServerConfig",
    protocolType,
  );
}

test.describe("サーバーの追加と削除", () => {
  test("サーバーがないときは案内を表示し、ダイアログから追加できる", async ({
    page,
  }) => {
    const empty = page.locator(".server-instance-empty");
    await expect(empty).toContainText(
      "サーバーもクライアントも登録されていません",
    );
    // 案内が折り返しても、枠からはみ出さない（狭いウィンドウでも）
    await page.setViewportSize({ width: 900, height: 700 });
    expect(
      await empty.evaluate((el) => el.scrollHeight <= el.clientHeight),
    ).toBe(true);
    await shot(page, "connections-empty");
    await page.setViewportSize({ width: 1400, height: 900 });
    await expect(page.locator(".server-tab-item")).toHaveCount(0);

    // 追加ダイアログには、追加できるプロトコルが並ぶ
    await addButton(page).click();
    const dialog = dialogByTitle(page, "サーバーを追加");
    await expect(dialog).toBeVisible();
    const protocols = await api<{ displayName: string }[]>(
      page,
      "GetAvailableProtocols",
    );
    await expect(dialog.locator("select").first().locator("option")).toHaveText(
      protocols.map((p) => p.displayName),
    );
    await shot(page, "server-add-dialog");

    // Esc とキャンセルでは追加しない
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await addButton(page).click();
    await dialog.getByRole("button", { name: "キャンセル" }).click();
    await expect(dialog).toBeHidden();
    await expect(page.locator(".server-tab-item")).toHaveCount(0);

    // プロトコルを選んで追加すると、一覧に出て選択される
    await addButton(page).click();
    await dialog
      .locator("select")
      .first()
      .selectOption({ label: "Modbus TCP" });
    await dialog.getByRole("button", { name: "追加" }).click();
    await expect(dialog).toBeHidden();
    await expect(serverTab(page, "Modbus TCP")).toHaveClass(/selected/);
    await expect(pane(page).locator(".server-config-pane-name")).toHaveText(
      "Modbus TCP",
    );
    await expect(pane(page).locator(".server-status-badge")).toHaveText(
      "Stopped",
    );
    const instances = await api<{ protocolType: string }[]>(
      page,
      "GetServerInstances",
    );
    expect(instances.map((i) => i.protocolType)).toEqual(["modbus-tcp"]);

    // 追加済みのプロトコルは、次の追加ダイアログに出ない
    await addButton(page).click();
    await expect(dialog.locator("select").first().locator("option")).toHaveText(
      protocols.map((p) => p.displayName).filter((n) => n !== "Modbus TCP"),
    );
    // Enter でも追加できる（先頭のプロトコルが選ばれている）
    await page.waitForTimeout(150);
    await page.keyboard.press("Enter");
    await expect(dialog).toBeHidden();
    await expect(page.locator(".server-tab-item")).toHaveCount(2);
  });

  test("すべてのプロトコルを追加すると、追加ボタンが押せなくなる", async ({
    page,
  }) => {
    const protocols = await api<{ type: string }[]>(
      page,
      "GetAvailableProtocols",
    );
    for (const p of protocols) await api(page, "AddServer", p.type, "");

    // API で追加したサーバーも、イベントで一覧に反映される
    await expect(page.locator(".server-tab-item")).toHaveCount(
      protocols.length,
    );
    await expect(addButton(page)).toBeDisabled();
    // 選択中のサーバーがなければ、先頭が選ばれる
    await expect(page.locator(".server-tab-item").first()).toHaveClass(
      /selected/,
    );
  });

  test("ごみ箱ボタンで削除でき、残ったサーバーの先頭が選ばれる", async ({
    page,
  }) => {
    await api(page, "AddServer", "modbus-tcp", "");
    await api(page, "AddServer", "opcua", "");
    await serverTab(page, "OPC UA").click();
    await expect(serverTab(page, "OPC UA")).toHaveClass(/selected/);

    await pane(page).locator(".btn-danger").click();
    await expect(serverTab(page, "OPC UA")).toHaveCount(0);
    await expect(serverTab(page, "Modbus TCP")).toHaveClass(/selected/);

    await pane(page).locator(".btn-danger").click();
    await expect(page.locator(".server-tab-item")).toHaveCount(0);
    await expect(page.locator(".server-instance-empty")).toContainText(
      "サーバーもクライアントも登録されていません",
    );
  });
});

test.describe("サーバーの設定", () => {
  test("設定を変更して保存でき、起動中は変更できない", async ({ page }) => {
    await api(page, "AddServer", "modbus-tcp", "");
    const port = setting(page, "ポート");
    await expect(port).toHaveValue("502");
    await expect(setting(page, "アドレス")).toHaveValue("0.0.0.0");
    await expect(saveButton(page)).toHaveText("保存");

    // 変更すると「保存 *」になる
    await port.fill(String(TCP_PORT));
    await expect(saveButton(page)).toHaveText("保存 *");
    await expect(saveButton(page)).toHaveClass(/btn-primary/);
    expect((await config(page, "modbus-tcp")).settings.tcpPort).toBe(502);
    await shot(page, "server-config-dirty");

    await saveButton(page).click();
    await expect(saveButton(page)).toHaveText("保存");
    expect((await config(page, "modbus-tcp")).settings.tcpPort).toBe(TCP_PORT);

    // 一覧の再生ボタンで起動する
    const tab = serverTab(page, "Modbus TCP");
    await tab.getByTitle("開始").click();
    await expect(pane(page).locator(".server-status-badge")).toHaveText(
      "Running",
    );
    await expect(tab.locator(".server-status-dot")).toHaveClass(/running/);
    await expect(port).toBeDisabled();
    await expect(saveButton(page)).toBeDisabled();
    await expect(pane(page).locator(".btn-danger")).toBeDisabled();
    await shot(page, "server-running");

    // 停止すると、また変更できる
    await tab.getByTitle("停止").click();
    await expect(pane(page).locator(".server-status-badge")).toHaveText(
      "Stopped",
    );
    await expect(tab.locator(".server-status-dot")).toHaveClass(/stopped/);
    await expect(port).toBeEnabled();
    await expect(pane(page).locator(".btn-danger")).toBeEnabled();
  });

  test("カテゴリごとに項目を表示し、保存に失敗するとエラーを表示する", async ({
    page,
  }) => {
    await api(page, "AddServer", "opcua", "");
    await expect(page.locator(".vscode-setting-category")).toHaveText([
      "ネットワーク",
      "アプリケーション情報",
    ]);
    const name = setting(page, "アプリケーション名 (ApplicationName)");
    await expect(name).toHaveValue("Karakuri PLC Simulator");

    // 必須の項目を空にして保存すると、エラーが出て設定は変わらない
    await name.fill("");
    await saveButton(page).click();
    await expect(pane(page).locator(".error-message")).toBeVisible();
    expect((await config(page, "opcua")).settings.application_name).toBe(
      "Karakuri PLC Simulator",
    );
    await shot(page, "server-config-error");

    // 直して保存するとエラーが消える
    await name.fill("My PLC");
    await saveButton(page).click();
    await expect(pane(page).locator(".error-message")).toHaveCount(0);
    await expect(saveButton(page)).toHaveText("保存");
    expect((await config(page, "opcua")).settings.application_name).toBe(
      "My PLC",
    );
  });

  test("選択肢・シリアルポートの項目を表示し、変更を保存できる", async ({
    page,
  }) => {
    await api(page, "AddServer", "modbus-rtu", "");
    await expect(page.locator(".vscode-setting-category")).toHaveText([
      "基本設定",
      "フレーム設定",
    ]);

    // 選択肢の値は、数値なら数値として保存される
    const baud = setting(page, "ボーレート");
    await expect(baud).toHaveValue("115200");
    await baud.selectOption("9600");
    const parity = setting(page, "パリティ");
    await parity.selectOption({ label: "Even" });
    await expect(saveButton(page)).toHaveText("保存 *");

    // シリアルポートは一覧から選ぶ。見つからないポートが設定されていれば「(未検出)」と出す
    const serial = setting(page, "シリアルポート");
    const ports = await api<string[]>(page, "GetSerialPorts");
    if (!(ports || []).includes("COM1")) {
      await expect(serial.locator("option", { hasText: "COM1" })).toHaveText(
        "COM1 (未検出)",
      );
    }
    await expect(page.getByTitle("ポート一覧を更新")).toBeVisible();

    await saveButton(page).click();
    await expect(saveButton(page)).toHaveText("保存");
    const saved = (await config(page, "modbus-rtu")).settings;
    expect(saved.baudRate).toBe(9600);
    expect(saved.parity).toBe("E");
  });

  test("UnitID ごとの応答を切り替えられる（UnitID のないプロトコルには出ない）", async ({
    page,
  }) => {
    await api(page, "AddServer", "modbus-tcp", "");
    await api(page, "AddServer", "opcua", "");
    await serverTab(page, "Modbus TCP").click();
    await setting(page, "ポート").fill(String(TCP_PORT));
    await saveButton(page).click();
    await expect(saveButton(page)).toHaveText("保存");
    await serverTab(page, "Modbus TCP").getByTitle("開始").click();
    await expect(pane(page).locator(".server-status-badge")).toHaveText(
      "Running",
    );

    const toggles = page.locator(".unit-id-toggle");
    await expect(toggles).toHaveCount(247);
    const unit5 = toggles.nth(4).locator("input");
    await expect(toggles.nth(4)).toHaveText("5");
    await expect(unit5).toBeChecked();

    // 切り替えは、起動中でも、保存ボタンを押さなくてもすぐ反映される
    await unit5.click();
    await expect(unit5).not.toBeChecked();
    await expect
      .poll(() => api<number[]>(page, "GetDisabledUnitIDs", "modbus-tcp"))
      .toEqual([5]);
    await expect(saveButton(page)).toHaveText("保存");
    await shot(page, "server-unit-id");

    // 別のサーバーを見て戻っても、切り替えた状態が表示される
    await serverTab(page, "OPC UA").click();
    await expect(pane(page).locator(".server-config-pane-name")).toHaveText(
      "OPC UA",
    );
    await expect(page.locator(".unit-id-section")).toHaveCount(0);
    await serverTab(page, "Modbus TCP").click();
    await expect(unit5).not.toBeChecked();
    await expect(toggles.nth(5).locator("input")).toBeChecked();

    await unit5.click();
    await expect(unit5).toBeChecked();
    await expect
      .poll(
        async () =>
          (
            (await api<number[]>(page, "GetDisabledUnitIDs", "modbus-tcp")) ||
            []
          ).length,
      )
      .toBe(0);
  });
});

test.describe("保存していない変更の確認", () => {
  test.beforeEach(async ({ page }) => {
    await api(page, "AddServer", "modbus-tcp", "");
    await api(page, "AddServer", "opcua", "");
    await serverTab(page, "Modbus TCP").click();
    await expect(setting(page, "ポート")).toHaveValue("502");
    await setting(page, "ポート").fill(String(TCP_PORT));
    await expect(saveButton(page)).toHaveText("保存 *");
  });

  const confirm = (page: Page) =>
    dialogByTitle(page, "設定が保存されていません");

  test("別のサーバーに切り替えるとき: キャンセル・保存せずに切り替え", async ({
    page,
  }) => {
    await serverTab(page, "OPC UA").click();
    await expect(confirm(page)).toBeVisible();
    await shot(page, "server-unsaved-confirm");

    // キャンセルすると、変更したまま元のサーバーに残る
    await confirm(page).getByRole("button", { name: "キャンセル" }).click();
    await expect(confirm(page)).toBeHidden();
    await expect(serverTab(page, "Modbus TCP")).toHaveClass(/selected/);
    await expect(setting(page, "ポート")).toHaveValue(String(TCP_PORT));
    await expect(saveButton(page)).toHaveText("保存 *");

    // 保存せずに切り替えると、変更は捨てられる
    await serverTab(page, "OPC UA").click();
    await confirm(page)
      .getByRole("button", { name: "保存せずに切り替え" })
      .click();
    await expect(serverTab(page, "OPC UA")).toHaveClass(/selected/);
    expect((await config(page, "modbus-tcp")).settings.tcpPort).toBe(502);

    // 戻ると元の値で、確認は出ない
    await serverTab(page, "Modbus TCP").click();
    await expect(confirm(page)).toBeHidden();
    await expect(setting(page, "ポート")).toHaveValue("502");
  });

  test("別のサーバーに切り替えるとき: 保存してから切り替え", async ({
    page,
  }) => {
    await serverTab(page, "OPC UA").click();
    await confirm(page)
      .getByRole("button", { name: "保存してから切り替え" })
      .click();
    await expect(serverTab(page, "OPC UA")).toHaveClass(/selected/);
    expect((await config(page, "modbus-tcp")).settings.tcpPort).toBe(TCP_PORT);
  });

  test("起動するとき: 保存してから開始・保存せずに開始", async ({ page }) => {
    const tab = serverTab(page, "Modbus TCP");
    await tab.getByTitle("開始").click();
    await expect(
      confirm(page).getByRole("button", { name: "保存せずに開始" }),
    ).toBeVisible();
    await confirm(page)
      .getByRole("button", { name: "保存してから開始" })
      .click();
    await expect(pane(page).locator(".server-status-badge")).toHaveText(
      "Running",
    );
    expect((await config(page, "modbus-tcp")).settings.tcpPort).toBe(TCP_PORT);

    await tab.getByTitle("停止").click();
    await expect(pane(page).locator(".server-status-badge")).toHaveText(
      "Stopped",
    );

    // 選択していないサーバーの起動では確認しない（変更はそのまま残る）
    await setting(page, "アドレス").fill("127.0.0.1");
    await expect(saveButton(page)).toHaveText("保存 *");
    await serverTab(page, "OPC UA").getByTitle("開始").click();
    await expect(confirm(page)).toBeHidden();
    await expect(
      serverTab(page, "OPC UA").locator(".server-status-dot"),
    ).toHaveClass(/running/);
    await expect(saveButton(page)).toHaveText("保存 *");
    await serverTab(page, "OPC UA").getByTitle("停止").click();

    // 保存せずに開始すると、変更は捨てて元の設定で起動する
    await tab.getByTitle("開始").click();
    await confirm(page).getByRole("button", { name: "保存せずに開始" }).click();
    await expect(pane(page).locator(".server-status-badge")).toHaveText(
      "Running",
    );
    expect((await config(page, "modbus-tcp")).settings.tcpAddress).toBe(
      "0.0.0.0",
    );
  });

  test("別の画面に移るとき: キャンセル・保存してから移動・保存せずに移動", async ({
    page,
  }) => {
    await page.getByTitle("Variables").click();
    await expect(confirm(page)).toBeVisible();
    await confirm(page).getByRole("button", { name: "キャンセル" }).click();
    await expect(page.locator(".server-vertical-tabs")).toBeVisible();
    await expect(setting(page, "ポート")).toHaveValue(String(TCP_PORT));

    // 保存してから移動
    await page.getByTitle("Variables").click();
    await confirm(page)
      .getByRole("button", { name: /保存して/ })
      .click();
    await expect(page.locator(".variable-toolbar")).toBeVisible();
    expect((await config(page, "modbus-tcp")).settings.tcpPort).toBe(TCP_PORT);

    // 保存せずに移動
    await openServers(page);
    await serverTab(page, "Modbus TCP").click();
    await setting(page, "ポート").fill("15099");
    await expect(saveButton(page)).toHaveText("保存 *");
    await page.getByTitle("Variables").click();
    await confirm(page)
      .getByRole("button", { name: /保存せず/ })
      .click();
    await expect(page.locator(".variable-toolbar")).toBeVisible();
    expect((await config(page, "modbus-tcp")).settings.tcpPort).toBe(TCP_PORT);

    // 変更がなければ確認なしで移動できる
    await openServers(page);
    await openVariables(page);
  });
});
