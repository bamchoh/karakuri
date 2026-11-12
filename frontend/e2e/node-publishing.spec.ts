import { test, expect, Page } from "@playwright/test";
import { api, openApp, resetApp, shot } from "./helpers";

// OPC UA の待ち受けポート。普段使う 4840 とぶつからない値にする
const OPCUA_PORT = 48401;
const MODBUS_PORT = 15020;

async function addOpcuaServer(page: Page) {
  await api(page, "AddServer", "opcua", "");
  const cfg = await api<{ settings: Record<string, unknown> }>(
    page,
    "GetServerConfig",
    "opcua",
  );
  await api(page, "UpdateServerConfig", {
    protocolType: "opcua",
    variant: "",
    settings: { ...cfg.settings, host: "127.0.0.1", port: OPCUA_PORT },
  });
}

async function createVariable(page: Page, name: string, dataType = "INT") {
  return api<{ id: string }>(page, "CreateVariable", name, dataType, 0);
}

async function openVariables(page: Page) {
  await page.getByTitle("Variables").click();
}

// 変数一覧で、変数名の行の「マッピング」セル
function mappingCell(page: Page, name: string) {
  return page
    .locator("tr", { has: page.getByText(name, { exact: true }) })
    .locator("td.var-mapping");
}

async function openMappingDialog(page: Page, name: string) {
  await mappingCell(page, name).click();
  const dialog = page.locator(".dialog", {
    hasText: `マッピング設定: ${name}`,
  });
  await expect(dialog).toBeVisible();
  return dialog;
}

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

test("NodeID は停止中 ns=???、起動中は実際の番号で、変数名を使う", async ({
  page,
}) => {
  await addOpcuaServer(page);
  await createVariable(page, "Motor_1");
  await openVariables(page);

  // 停止中
  let dialog = await openMappingDialog(page, "Motor_1");
  await dialog.getByLabel("公開する").check();
  await expect(dialog.getByText("ns=???;s=Motor_1")).toBeVisible();
  await shot(page, "nodeid-stopped");
  await dialog.getByRole("button", { name: "保存" }).click();
  await expect(dialog).toBeHidden();
  await expect(mappingCell(page, "Motor_1")).toContainText("opcua(R/W)");

  // 起動中
  await api(page, "StartServer", "opcua");
  dialog = await openMappingDialog(page, "Motor_1");
  await expect(dialog.getByText(/^ns=\d+;s=Motor_1$/)).toBeVisible();
  await shot(page, "nodeid-running");
});

test("NodeID が重なる変数名は、理由を表示して保存を止める", async ({
  page,
}) => {
  await addOpcuaServer(page);
  await createVariable(page, "Bad.Name");
  await openVariables(page);

  const dialog = await openMappingDialog(page, "Bad.Name");
  await dialog.getByLabel("公開する").check();
  const error = dialog.locator(".node-address-error");
  await expect(error).toContainText("OPC UA で公開できません");
  await shot(page, "nodeid-error");

  // 保存するとアラートを出して、ダイアログは閉じない
  const alertText = new Promise<string>((resolve) =>
    page.once("dialog", async (d) => {
      resolve(d.message());
      await d.accept();
    }),
  );
  await dialog.getByRole("button", { name: "保存" }).click();
  expect(await alertText).toContain("OPC UA で公開できません");
  await expect(dialog).toBeVisible();

  // バックエンドにも保存されていない
  const vars = await api<
    { name: string; nodePublishings?: { enabled: boolean }[] }[]
  >(page, "GetVariables");
  const bad = vars.find((v) => v.name === "Bad.Name")!;
  expect((bad.nodePublishings || []).some((p) => p.enabled)).toBe(false);
});

test("一括マッピング編集でも NodeID と理由を表示し、保存を止める", async ({
  page,
}) => {
  await addOpcuaServer(page);
  await createVariable(page, "Motor_1");
  await createVariable(page, "Bad.Name");
  await openVariables(page);

  await page.getByRole("button", { name: "一括マッピング編集" }).click();
  const dialog = page.locator(".dialog", { hasText: "一括マッピング編集" });
  await expect(dialog).toBeVisible();

  const row = (name: string) =>
    dialog.locator("tr", { has: page.getByText(name, { exact: true }) });
  await row("Motor_1").getByRole("checkbox").check();
  await row("Bad.Name").getByRole("checkbox").check();
  await expect(row("Motor_1")).toContainText("ns=???;s=Motor_1");
  await expect(row("Bad.Name").locator(".node-address-error")).toContainText(
    "OPC UA で公開できません",
  );
  await shot(page, "bulk-nodeid");

  const alertText = new Promise<string>((resolve) =>
    page.once("dialog", async (d) => {
      resolve(d.message());
      await d.accept();
    }),
  );
  await dialog.getByRole("button", { name: "保存" }).click();
  expect(await alertText).toContain("Bad.Name");
  await expect(dialog).toBeVisible();

  // 1 件でも公開できない変数があれば、他の変数も保存しない
  const vars = await api<
    { name: string; nodePublishings?: { enabled: boolean }[] }[]
  >(page, "GetVariables");
  for (const v of vars) {
    expect((v.nodePublishings || []).some((p) => p.enabled)).toBe(false);
  }
});

test("Modbus のマッピング列は、プロトコルの実装が返すエリアの略称で表示する", async ({
  page,
}) => {
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

  const v = await createVariable(page, "Speed");
  await api(page, "UpdateVariableMappings", v.id, [
    {
      protocolType: "modbus-tcp",
      memoryArea: "holdingRegisters",
      address: 0,
      endianness: "big",
    },
  ]);

  await openVariables(page);
  // 保持レジスタ（4x）の先頭。アドレスは 1 オリジンで表示する
  await expect(mappingCell(page, "Speed")).toContainText("modbus-tcp:4:1");
  await shot(page, "modbus-shortname");
});
