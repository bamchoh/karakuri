import { test, expect, Page } from "@playwright/test";
import {
  api,
  openApp,
  resetApp,
  shot,
  openClient,
  openServers,
} from "./helpers";

// OPC UA の暗号化とユーザー認証・証明書のタブ。
// Karakuri 自身の OPC UA サーバー（暗号化・「信頼したものだけ」・ユーザー名）に、Karakuri の接続先（「信頼したものだけ」）からつなぎ、
// 両方の証明書のタブで相手の証明書を信頼すると、つながるようになることを確かめる。
//
// 証明書は、開発サーバーの exe と同じフォルダの pki / pki-client に置かれる。テストの前後に、相手の証明書を消す（自分の証明書は残す）

const OPCUA_PORT = 48402;
const ENDPOINT = `opc.tcp://127.0.0.1:${OPCUA_PORT}`;

interface CertList {
  dir: string;
  certificates: { thumbprint: string; status: string }[];
}

async function clearPeerCertificates(page: Page) {
  for (const kind of ["server", "client"]) {
    const list = await api<CertList>(page, "GetCertificates", kind, "opcua");
    for (const c of list.certificates) {
      if (c.status !== "own") {
        await api(page, "DeleteCertificate", kind, "opcua", c.thumbprint);
      }
    }
  }
}

let clientId = "";
let request = "";

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await clearPeerCertificates(page);

  await api(page, "AddServer", "opcua", "");
  const cfg = await api<{ settings: Record<string, unknown> }>(
    page,
    "GetServerConfig",
    "opcua",
  );
  await api(page, "UpdateServerConfig", {
    protocolType: "opcua",
    variant: "",
    settings: {
      ...cfg.settings,
      host: "127.0.0.1",
      port: OPCUA_PORT,
      securityEndpoints: [
        { policy: "None", mode: "SignAndEncrypt" },
        { policy: "Basic256Sha256", mode: "SignAndEncrypt" },
      ],
      trustClients: "trusted",
      allowAnonymous: "no",
      users: [{ userName: "operator", password: "secret" }],
    },
  });
  await api(page, "StartServer", "opcua");

  const c = await api<{ id: string }>(page, "AddClient", "opcua", "UA");
  clientId = c.id;
  await api(page, "UpdateClient", clientId, "UA", {
    endpointUrl: ENDPOINT,
    securityPolicy: "Basic256Sha256",
    securityMode: "SignAndEncrypt",
    trustServers: "trusted",
    userName: "operator",
    password: "secret",
    timeoutMs: 5000,
  });
  request = await api<string>(page, "BuildClientRequest", "opcua", "read", {
    nodeId: "i=2258",
    attribute: "Value",
  });
  // 通信画面は、サーバーを追加した時点の設定を読んでいるので、開き直して、API で変えた設定を読ませる
  await openApp(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
  await clearPeerCertificates(page);
});

async function send(page: Page) {
  const r = await api<{ status: string; error: string }>(
    page,
    "SendClientRequest",
    clientId,
    request,
  );
  await api(page, "DisconnectClient", clientId);
  return r;
}

// 証明書のタブの「拒否した証明書」の 1 行目の「信頼する」を押し、信頼した証明書に移ったことを確かめる
async function trustFirstRejected(page: Page) {
  const view = page.locator(".certificates-view");
  await expect(
    view.locator(".vscode-setting-category", {
      hasText: "拒否した証明書（1）",
    }),
  ).toBeVisible();
  await view.getByRole("button", { name: /を信頼する$/ }).click();
  await expect(
    view.locator(".vscode-setting-category", {
      hasText: "信頼した証明書（1）",
    }),
  ).toBeVisible();
  await expect(
    view.locator(".vscode-setting-category", {
      hasText: "拒否した証明書（0）",
    }),
  ).toBeVisible();
}

test("サーバーの設定に、エンドポイントとユーザーの表が出る（パスワードは隠す）", async ({
  page,
}) => {
  await openServers(page);
  const list = page.locator(".vscode-settings-list");
  const item = (label: string) =>
    list.locator(".vscode-setting-item", {
      has: page.locator(".vscode-setting-label", {
        hasText: new RegExp("^" + label + "$"),
      }),
    });
  const endpoints = item("エンドポイント").locator(".rows-table tbody tr");
  await expect(endpoints).toHaveCount(2);
  await expect(endpoints.nth(1).locator("select").first()).toHaveValue(
    "Basic256Sha256",
  );
  const users = item("ユーザー").locator(".rows-table tbody tr");
  await expect(users).toHaveCount(1);
  await expect(users.locator('input[type="password"]')).toHaveValue("secret");
  // 起動中は変えられない
  await expect(users.locator('input[type="password"]')).toBeDisabled();
  await item("エンドポイント").scrollIntoViewIfNeeded();
  await shot(page, "opcua-security-settings");
});

test("両方の証明書のタブで相手を信頼すると、暗号化してつながる", async ({
  page,
}) => {
  // 1. 接続先が、サーバーの証明書を知らないので、つながない
  expect((await send(page)).status).toBe("error");

  await openClient(page, "UA");
  await page
    .locator(".client-sub-tabs .sub-tab-button", { hasText: /^証明書$/ })
    .click();
  await expect(page.locator(".certificates-view")).toContainText(
    "OPC UA の全部の接続先で共通です。",
  );
  await trustFirstRejected(page);
  await shot(page, "opcua-security-client-certs");

  // 2. 次は、サーバーが接続先の証明書を知らないので、断る
  expect((await send(page)).status).toBe("error");

  await openServers(page);
  await page
    .locator(".server-tab-item:not(.client-tab-item)", { hasText: "OPC UA" })
    .click();
  await expect(page.locator(".server-config-pane-name")).toHaveText("OPC UA");
  await page
    .locator(".server-sub-tabs .sub-tab-button", { hasText: /^証明書$/ })
    .click();
  // サーバーは起動したので、自分の証明書がある
  await expect(
    page.locator(".vscode-setting-category", {
      hasText: "このアプリの証明書（1）",
    }),
  ).toBeVisible();
  await trustFirstRejected(page);
  await shot(page, "opcua-security-server-certs");

  // 3. 両方が信頼したので、つながる
  const r = await send(page);
  expect(r.status, r.error).toBe("ok");
});
