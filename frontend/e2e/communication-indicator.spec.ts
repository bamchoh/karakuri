import { test, expect } from "@playwright/test";
import net from "net";
import { addModbusServer, MODBUS_PORT, openApp, resetApp, shot } from "./helpers";

// Modbus TCP で保持レジスタを 1 つ読む（FC 03）。応答を受け取るまで待つ
function readHoldingRegister(unitId: number): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const socket = net.connect(MODBUS_PORT, "127.0.0.1", () => {
      // MBAP ヘッダー（トランザクション ID・プロトコル ID・長さ・ユニット ID）+ FC 03・アドレス 0・個数 1
      socket.write(Buffer.from([0, 1, 0, 0, 0, 6, unitId, 3, 0, 0, 0, 1]));
    });
    socket.once("data", (data) => {
      socket.destroy();
      resolve(data);
    });
    socket.once("error", reject);
    socket.setTimeout(3000, () => {
      socket.destroy();
      reject(new Error("Modbus の応答がありません"));
    });
  });
}

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

// issue #17: プラグイン化のあと、通信イベントがホストに届かずランプが点かなくなっていた
test("Modbus の通信があると RX / TX のランプが点き、しばらくすると消える", async ({
  page,
}) => {
  await addModbusServer(page);
  const rx = page.locator(".indicator-lamp").nth(0);
  const tx = page.locator(".indicator-lamp.tx");
  await expect(rx).not.toHaveClass(/active/);
  await expect(tx).not.toHaveClass(/active/);

  const response = await readHoldingRegister(1);
  // 応答は FC 03・2 バイト（レジスタ 1 つ分）
  expect([...response.subarray(7, 9)]).toEqual([3, 2]);

  await expect(rx).toHaveClass(/active/);
  await expect(tx).toHaveClass(/active/);
  await shot(page, "communication-indicator");

  // 通信が止まると消える（点灯は 300ms）
  await expect(rx).not.toHaveClass(/active/);
  await expect(tx).not.toHaveClass(/active/);

  // サーバーを止めて起動し直しても届く
  await page.evaluate(() => (window as any).go.main.App.StopServer("modbus-tcp"));
  await page.evaluate(() => (window as any).go.main.App.StartServer("modbus-tcp"));
  await readHoldingRegister(1);
  await expect(rx).toHaveClass(/active/);
});
