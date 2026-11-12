import { test, expect, Page } from "@playwright/test";
import { api, MODBUS_PORT, openApp, resetApp, shot } from "./helpers";

// issue #49: UnitID の応答設定が、サーバーの起動前は反映されず、起動し直すと消えていた

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

const disabled = (page: Page) =>
  api<number[]>(page, "GetDisabledUnitIDs", "modbus-tcp").then(
    (ids) => ids || [],
  );

async function addStoppedModbusServer(page: Page) {
  await api(page, "AddServer", "modbus-tcp", "");
  const cfg = await api<{ variant: string; settings: Record<string, unknown> }>(
    page,
    "GetServerConfig",
    "modbus-tcp",
  );
  await api(page, "UpdateServerConfig", {
    protocolType: "modbus-tcp",
    variant: cfg.variant,
    settings: { ...cfg.settings, tcpPort: MODBUS_PORT },
  });
}

test("起動前に切り替えた UnitID の設定が残り、起動・起動し直しのあとも有効", async ({
  page,
}) => {
  await addStoppedModbusServer(page);
  await api(page, "AddServer", "opcua", "");
  await page.getByTitle("Server", { exact: true }).click();
  const tab = (name: string) =>
    page.locator(".server-tab-item", { hasText: name });
  await tab("Modbus TCP").click();

  // 一度も起動していないサーバーで、UnitID 5 の応答をオフにする
  const toggles = page.locator(".unit-id-toggle");
  await expect(toggles).toHaveCount(247);
  const unit5 = toggles.nth(4).locator("input");
  await expect(unit5).toBeChecked();
  await unit5.click();
  await expect(unit5).not.toBeChecked();
  await expect.poll(() => disabled(page)).toEqual([5]);

  // 別のサーバーを見て戻っても、オフのまま
  await tab("OPC UA").click();
  await expect(page.locator(".unit-id-section")).toHaveCount(0);
  await tab("Modbus TCP").click();
  await expect(unit5).not.toBeChecked();
  await shot(page, "unit-id-before-start");

  // 起動しても、停止して起動し直しても、オフのまま
  await api(page, "StartServer", "modbus-tcp");
  expect(await disabled(page)).toEqual([5]);
  await api(page, "SetUnitIDEnabled", "modbus-tcp", 9, false);
  await api(page, "StopServer", "modbus-tcp");
  expect(await disabled(page)).toEqual([5, 9]);
  await api(page, "StartServer", "modbus-tcp");
  expect(await disabled(page)).toEqual([5, 9]);

  // 画面にも反映されている
  await tab("OPC UA").click();
  await tab("Modbus TCP").click();
  await expect(unit5).not.toBeChecked();
  await expect(toggles.nth(8).locator("input")).not.toBeChecked();
  await expect(toggles.nth(5).locator("input")).toBeChecked();
});

test("オフにした UnitID には、起動前に設定しても、起動し直しても応答しない", async ({
  page,
}) => {
  const net = await import("net");
  // Modbus TCP で保持レジスタを 1 つ読み、応答の機能コードを返す（通常は 0x03）。
  // 応答がなければ null（例外応答も返らないことを確かめる）
  const functionCode = (unitId: number) =>
    new Promise<number | null>((resolve, reject) => {
      const socket = net.connect(MODBUS_PORT, "127.0.0.1", () => {
        socket.write(Buffer.from([0, 1, 0, 0, 0, 6, unitId, 3, 0, 0, 0, 1]));
      });
      socket.once("data", (data) => {
        socket.destroy();
        resolve(data[7]);
      });
      socket.once("error", reject);
      socket.setTimeout(700, () => {
        socket.destroy();
        resolve(null);
      });
    });

  // 起動前に UnitID 2 をオフにしてから起動する
  await addStoppedModbusServer(page);
  await api(page, "SetUnitIDEnabled", "modbus-tcp", 2, false);
  await api(page, "StartServer", "modbus-tcp");
  expect(await functionCode(1)).toBe(0x03);
  expect(await functionCode(2)).toBeNull();

  await api(page, "StopServer", "modbus-tcp");
  await api(page, "StartServer", "modbus-tcp");
  expect(await functionCode(1)).toBe(0x03);
  expect(await functionCode(2)).toBeNull();

  // オンに戻すと応答する
  await api(page, "SetUnitIDEnabled", "modbus-tcp", 2, true);
  expect(await functionCode(2)).toBe(0x03);
});

test("プロジェクトの保存と読み込みで、UnitID の設定が引き継がれる", async ({
  page,
  request,
}) => {
  // ファイルの選択ダイアログを使わないよう、REST API でプロジェクトを取り出して読み込む
  const port = await api<number>(page, "GetHTTPAPIPort");
  const base = `http://localhost:${port}/api/project`;

  await addStoppedModbusServer(page);
  await api(page, "SetDisabledUnitIDs", "modbus-tcp", [3, 4]);
  const exported = await request.get(`${base}/export`);
  expect(exported.ok()).toBe(true);
  const project = await exported.json();

  // 読み込むとサーバーは作り直される（起動していない状態）。その状態でも設定が入る
  await api(page, "SetDisabledUnitIDs", "modbus-tcp", []);
  const imported = await request.post(`${base}/import`, { data: project });
  expect(imported.ok()).toBe(true);
  expect(await disabled(page)).toEqual([3, 4]);

  await api(page, "StartServer", "modbus-tcp");
  expect(await disabled(page)).toEqual([3, 4]);
});
