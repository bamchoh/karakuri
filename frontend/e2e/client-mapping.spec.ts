import { test, expect, Page } from "@playwright/test";
import {
  api,
  openApp,
  resetApp,
  shot,
  addModbusServer,
  readWords,
  writeWord,
  openVariables,
  variableRow,
  dialogByTitle,
  createVariable,
  getVariable,
  MODBUS_PORT,
  openClient,
} from "./helpers";

// 変数を、クライアントの接続先（テストで起動した Karakuri 自身の Modbus TCP サーバー）のアドレスにマッピングして同期する。

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await addModbusServer(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

async function addClient(page: Page) {
  const c = await api<{ id: string }>(page, "AddClient", "modbus-tcp", "自分");
  await api(page, "UpdateClient", c.id, "自分", {
    host: "127.0.0.1",
    port: MODBUS_PORT,
    unitId: 1,
    timeoutMs: 150,
  });
  await api(page, "SetClientPollInterval", c.id, 100);
  await api(page, "SetClientFailThreshold", c.id, 2);
  return c;
}

const badges = (page: Page, name: string) =>
  variableRow(page, name).locator(".device-status-badge");

test("マッピング設定で接続先を選べて、ポーリング中は変数と同期する", async ({
  page,
}) => {
  const c = await addClient(page);
  await writeWord(page, 9, 4321); // 1 始まりの表示で 10
  await createVariable(page, "Remote", "INT", 0);
  await page.reload();
  await openApp(page);
  await openVariables(page);

  // マッピング設定で接続先を選ぶ
  const cell = variableRow(page, "Remote").locator("td.var-mapping");
  await cell.click();
  const dialog = dialogByTitle(page, "マッピング設定: Remote");
  await dialog.getByRole("button", { name: "+ マッピング追加" }).click();
  const selects = dialog.locator("select");
  await selects.nth(0).selectOption({ label: "接続先: 自分（Modbus TCP）" });
  await selects.nth(1).selectOption("holdingRegisters");
  await dialog.locator('input[type="number"]').fill("10");
  await dialog.getByRole("button", { name: "保存" }).click();
  await expect(dialog).toBeHidden();
  // 一覧には ID ではなく接続先の名前で出る
  await expect(cell).toHaveText("自分:4:10");
  expect((await getVariable(page, "Remote")).mappings[0]).toEqual({
    protocolType: `client:${c.id}`,
    memoryArea: "holdingRegisters",
    address: 9,
    endianness: "big",
  });

  // ポーリングを始めると、機器の値が変数に入る
  await api(page, "StartClientPolling", c.id);
  const value = variableRow(page, "Remote").locator("td.var-value");
  await expect(value).toContainText("4321");
  await expect(badges(page, "Remote")).toHaveCount(0);

  // 変数を変えると、機器に書く
  const v = await getVariable(page, "Remote");
  await api(page, "UpdateVariableValue", v.id, -5);
  await expect.poll(() => readWords(page, 9)).toEqual([65531]);

  // 機器の側で変わると、変数に入る
  await writeWord(page, 9, 77);
  await expect(value).toContainText("77");
  await shot(page, "client-mapping");
});

test("読めなくなったら「通信エラー」、書けなければ「書き込み未反映」と出し、つながったら戻る", async ({
  page,
}) => {
  const c = await addClient(page);
  await writeWord(page, 0, 10);
  const v = await createVariable(page, "Remote", "INT", 0);
  await api(page, "UpdateVariableMappings", v.id, [
    {
      protocolType: `client:${c.id}`,
      memoryArea: "holdingRegisters",
      address: 0,
      endianness: "big",
    },
  ]);
  await api(page, "StartClientPolling", c.id);
  await openVariables(page);
  const value = variableRow(page, "Remote").locator("td.var-value");
  await expect(value).toContainText("10");

  // 応答しない UnitID にする
  await api(page, "SetUnitIDEnabled", "modbus-tcp", 1, false);
  await expect(badges(page, "Remote")).toHaveText(["通信エラー"]);
  await expect(value).toContainText("10"); // 値は最後に読めたまま

  await api(page, "UpdateVariableValue", v.id, 99);
  await expect(badges(page, "Remote")).toHaveText([
    "通信エラー",
    "書き込み未反映",
  ]);
  await shot(page, "client-mapping-error");

  // 接続先の画面に、状態のまとめが出る
  await openClient(page, "自分");
  await page.locator(".sub-tab-button", { hasText: "ポーリング" }).click();
  const summary = page.getByTestId("client-mapping-summary");
  await expect(summary).toContainText("通信エラー 1");
  await expect(summary).toContainText("書き込み未反映 1");
  await expect(page.getByLabel("通信エラーにするまでの失敗回数")).toHaveValue(
    "2",
  );
  await shot(page, "client-mapping-summary");

  // 応答するようにすると、未反映の値を書き、表示が消える
  await api(page, "SetUnitIDEnabled", "modbus-tcp", 1, true);
  await expect.poll(() => readWords(page, 0)).toEqual([99]);
  await expect(summary).not.toContainText("通信エラー");
  await openVariables(page);
  await expect(badges(page, "Remote")).toHaveCount(0);
});

test("接続先を削除すると、その接続先へのマッピングが外れる", async ({
  page,
}) => {
  const c = await addClient(page);
  const v = await createVariable(page, "Remote", "INT", 0);
  await api(page, "UpdateVariableMappings", v.id, [
    {
      protocolType: `client:${c.id}`,
      memoryArea: "coils",
      address: 0,
      endianness: "big",
    },
  ]);
  await openVariables(page);
  // ビットのエリアに INT は使えない
  await expect(badges(page, "Remote")).toHaveText(["マッピング不可"]);

  await api(page, "RemoveClient", c.id);
  await expect(
    variableRow(page, "Remote").locator("td.var-mapping"),
  ).toHaveText("-");
});
