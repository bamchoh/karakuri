import { test, expect } from "@playwright/test";
import { api, openApp, shot } from "./helpers";

test("アプリが開き、Go 側の API とつながる", async ({ page }) => {
  await openApp(page);
  const protocols = await api<{ type: string }[]>(
    page,
    "GetAvailableProtocols",
  );
  expect(protocols.map((p) => p.type)).toContain("opcua");
  await shot(page, "smoke");
});
