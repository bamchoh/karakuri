import { test, expect } from "@playwright/test";
import path from "path";
import {
  api,
  openApp,
  openClient,
  openRegisters,
  resetApp,
  selectClientTab,
} from "./helpers";

// README に載せるスクリーンショット（docs/screenshot.png）を撮り直す。
//
// ふだんのテストでは実行しない。撮り直すときだけ、環境変数 README_SCREENSHOT を付けて実行する:
//
//   $env:README_SCREENSHOT = '1'
//   task e2e -- readme-screenshot
//
// アプリの状態を作り直すので、ほかのテストと同じく、作業に使っている開発サーバーには流さない。

const OUTPUT = path.join("..", "docs", "screenshot.png");
const MODBUS_PORT = 15020;
const FINS_PORT = 19600;

test.skip(
  !process.env.README_SCREENSHOT,
  "README のスクリーンショットを撮り直すときだけ実行する（README_SCREENSHOT=1）",
);

test("README のスクリーンショットを撮る", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await openApp(page);
  await resetApp(page);

  // サーバー: Modbus TCP と FINS を起動し、OPC UA は止めたままにする
  const configure = async (protocolType: string, settings: object) => {
    await api(page, "AddServer", protocolType, "");
    const cfg = await api<{ variant: string; settings: object }>(
      page,
      "GetServerConfig",
      protocolType,
    );
    await api(page, "UpdateServerConfig", {
      protocolType,
      variant: cfg.variant,
      settings: { ...cfg.settings, ...settings },
    });
  };
  await configure("modbus-tcp", { tcpPort: MODBUS_PORT });
  await configure("fins", { port: FINS_PORT });
  await api(page, "AddServer", "opcua", "");

  // レジスタに、それらしい値を入れる
  const values = [
    1200, 1215, 1198, 1230, 1247, 1260, 1254, 1239, 1221, 1204, 350, 352, 349,
    355, 361, 358, 354, 351, 348, 346, 0, 1, 1, 0, 1, 0, 0, 1, 1, 1, 8191, 4095,
    2047, 1023, 511, 255, 127, 63, 31, 15,
  ];
  for (let i = 0; i < values.length; i++) {
    await api(
      page,
      "WriteWord",
      "modbus-tcp",
      "holdingRegisters",
      i,
      values[i],
    );
  }

  // クライアント: 自分の Modbus TCP サーバーにつなぐ接続先
  const client = await api<{ id: string }>(
    page,
    "AddClient",
    "modbus-tcp",
    "PLC-A",
  );
  await api(page, "UpdateClient", client.id, "PLC-A", {
    host: "127.0.0.1",
    port: MODBUS_PORT,
    unitId: 1,
    timeoutMs: 1000,
  });
  await api(page, "AddClient", "opcua", "検査装置");

  // ログは、ここからの出来事だけにする
  await api(page, "ClearAppLogs");
  await api(page, "StartServer", "modbus-tcp");
  await api(page, "StartServer", "fins");
  await api(
    page,
    "AddClientPollItem",
    client.id,
    "3",
    { address: 0, count: 10 },
    "",
  );
  await api(page, "StartClientPolling", client.id);

  // 接続先がつながって「ポーリング中」と出るのを待つ（一覧に状態を出すため、いったん接続先を開く）
  await openClient(page, "PLC-A");
  await selectClientTab(page, "ポーリング");
  await expect(
    page.locator(".client-tab-item", { hasText: "ポーリング中" }),
  ).toBeVisible();

  // Modbus TCP のレジスタを開き、ログの欄も開く
  await openRegisters(page, "一覧表示", "Modbus TCP");
  await expect(page.locator("td.matrix-cell").first()).toHaveText("1200");
  await page.locator(".app-log-toggle").click();
  await expect(page.locator(".app-log-entry")).not.toHaveCount(0);
  // 選択の枠やホバーの色が写らないよう、マウスを何もないところに置く
  await page.mouse.move(700, 620);
  await page.waitForTimeout(400);

  await page.screenshot({ path: OUTPUT });

  await api(page, "StopClientPolling", client.id);
  await resetApp(page);
});
