import { test, expect, Page, Locator } from "@playwright/test";
import {
  addModbusServer,
  api,
  dialogByTitle,
  field,
  openApp,
  openRegisters,
  readWords,
  resetApp,
  shot,
  writeWord,
} from "./helpers";

// レジスタ画面の「モニタリング」の動きを確認する。
// 画面のコンポーネントを分割するときに、動きが変わっていないことを確かめるためのテスト。

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await addModbusServer(page);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

interface Item {
  id: string;
  order: number;
  protocolType: string;
  memoryArea: string;
  address: number;
  bitWidth: number;
  endianness: string;
  displayFormat: string;
}

// モニタリング項目を API で登録する（address は 0 始まり）
async function addItem(page: Page, address: number, extra: Partial<Item> = {}) {
  await api(page, "AddMonitoringItem", {
    id: "",
    order: 0,
    protocolType: "modbus-tcp",
    memoryArea: "holdingRegisters",
    address,
    bitWidth: 16,
    endianness: "big",
    displayFormat: "decimal",
    ...extra,
  });
}

const items = (page: Page) => api<Item[]>(page, "GetMonitoringItems");
const rows = (page: Page) => page.locator("table.monitoring-table tbody tr");
const value = (row: Locator) => row.locator("td.monitoring-value");
const addDialog = (page: Page) => dialogByTitle(page, "モニタリング項目を追加");
const writeDialog = (page: Page) => dialogByTitle(page, "レジスタ書き込み");
const controls = (page: Page) => page.locator(".monitoring-controls");

// 追加ダイアログを開き、最初のエリアが選ばれるまで待つ
// （開いた直後の一瞬はエリアが決まっておらず、その間に入力するとアドレスがずれる）
async function openAddDialog(page: Page) {
  await controls(page).getByRole("button", { name: "追加" }).click();
  await expect(field(addDialog(page), "メモリエリア:")).toHaveValue(
    "holdingRegisters",
  );
  await expect(field(addDialog(page), "開始アドレス:")).toHaveValue("1");
}

// 行の列（プロトコルの列がないとき）: 0 つまみ, 1 エリア, 2 アドレス, 3 ビット幅, 4 エンディアン, 5 表示形式, 6 現在値, 7 操作
const cell = (row: Locator, index: number) => row.locator("td").nth(index);

test.describe("モニタリング", () => {
  test("項目がないときは案内を表示し、ダイアログからまとめて追加できる", async ({
    page,
  }) => {
    await openRegisters(page, "モニタリング");
    await expect(page.locator(".monitoring-empty")).toContainText(
      "モニタリング項目がありません",
    );

    await controls(page).getByRole("button", { name: "追加" }).click();
    const dialog = addDialog(page);
    await expect(dialog).toBeVisible();
    // 最初のワードのエリアが選ばれ、アドレスは 1 始まりで表示する
    await expect(field(dialog, "メモリエリア:")).toHaveValue(
      "holdingRegisters",
    );
    await expect(field(dialog, "開始アドレス:")).toHaveValue("1");
    await expect(field(dialog, "個数:")).toHaveValue("1");
    // サーバーが 1 つのときは、プロトコルを選ばない
    await expect(field(dialog, "プロトコル:")).toHaveCount(0);
    await shot(page, "monitoring-add-dialog");

    // Esc とキャンセルでは追加しない
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await controls(page).getByRole("button", { name: "追加" }).click();
    await dialog.getByRole("button", { name: "キャンセル" }).click();
    await expect(dialog).toBeHidden();
    expect((await items(page)) || []).toHaveLength(0);

    // 開始アドレスと個数を指定すると、連続したアドレスで追加される
    await openAddDialog(page);
    await field(dialog, "開始アドレス:").fill("11");
    await field(dialog, "個数:").fill("3");
    await dialog.getByRole("button", { name: "追加" }).click();
    await expect(dialog).toBeHidden();
    await expect(rows(page)).toHaveCount(3);
    expect((await items(page)).map((i) => i.address)).toEqual([10, 11, 12]);
    await expect(cell(rows(page).nth(0), 2)).toHaveText("11");
    await expect(cell(rows(page).nth(2), 2)).toHaveText("13");
    await expect(cell(rows(page).nth(0), 1)).toContainText("保持レジスタ");

    // 32bit では 2 ワードずつ進める。ダイアログの設定が項目に入る
    await openAddDialog(page);
    await field(dialog, "開始アドレス:").fill("21");
    await field(dialog, "個数:").fill("2");
    await field(dialog, "ビット幅:").selectOption({ label: "32bit" });
    await field(dialog, "エンディアン:").selectOption("little");
    await field(dialog, "表示形式:").selectOption({ label: "16進数" });
    // Enter でも追加できる
    await page.waitForTimeout(150);
    await field(dialog, "個数:").press("Enter");
    await expect(dialog).toBeHidden();
    await expect(rows(page)).toHaveCount(5);
    const added = (await items(page)).slice(3);
    expect(added.map((i) => i.address)).toEqual([20, 22]);
    expect(added[0]).toMatchObject({
      bitWidth: 32,
      endianness: "little",
      displayFormat: "hex",
    });

    // ビットのエリアでは、ビット幅などを選ばない
    await openAddDialog(page);
    await field(dialog, "メモリエリア:").selectOption("coils");
    await expect(field(dialog, "ビット幅:")).toHaveCount(0);
    await expect(field(dialog, "表示形式:")).toHaveCount(0);
    await dialog.getByRole("button", { name: "追加" }).click();
    await expect(rows(page)).toHaveCount(6);
    await expect(cell(rows(page).nth(5), 3)).toHaveText("Bit");
    await expect(value(rows(page).nth(5))).toHaveText("OFF");
  });

  test("現在値を自動で更新し、自動更新を止めると「更新」で読み直す", async ({
    page,
  }) => {
    await addItem(page, 0);
    await openRegisters(page, "モニタリング");
    const row = rows(page).first();
    await expect(value(row)).toHaveText("0");

    await writeWord(page, 0, 123);
    await expect(value(row)).toHaveText("123");

    // 自動更新を止めると、値が変わっても表示は変わらない
    await controls(page).getByLabel("自動更新").uncheck();
    await page.waitForTimeout(300);
    await writeWord(page, 0, 456);
    await page.waitForTimeout(500);
    await expect(value(row)).toHaveText("123");
    await controls(page).getByRole("button", { name: "更新" }).click();
    await expect(value(row)).toHaveText("456");

    await controls(page).getByLabel("自動更新").check();
    await writeWord(page, 0, 789);
    await expect(value(row)).toHaveText("789");
  });

  test("行ごとにビット幅・エンディアン・表示形式を変えられる", async ({
    page,
  }) => {
    await writeWord(page, 0, 0x1234);
    await writeWord(page, 1, 0x5678);
    await addItem(page, 0);
    await openRegisters(page, "モニタリング");
    const row = rows(page).first();
    await expect(value(row)).toHaveText(String(0x1234));

    await cell(row, 3).locator("select").selectOption({ label: "32bit" });
    await expect(value(row)).toHaveText(String(0x12345678));
    await cell(row, 4).locator("select").selectOption("little");
    await expect(value(row)).toHaveText(String(0x56781234));
    await cell(row, 5).locator("select").selectOption({ label: "16進数" });
    await expect(value(row)).toHaveText("0x56781234");
    await cell(row, 5).locator("select").selectOption({ label: "2進数" });
    await expect(value(row)).toHaveText("01010110011110000001001000110100");
    await shot(page, "monitoring-list");

    // 変更は保存される
    expect((await items(page))[0]).toMatchObject({
      bitWidth: 32,
      endianness: "little",
      displayFormat: "binary",
    });
  });

  test("現在値をクリックして書き込める", async ({ page }) => {
    await addItem(page, 5);
    await addItem(page, 6, { bitWidth: 32, displayFormat: "hex" });
    await writeWord(page, 5, 100);
    await openRegisters(page, "モニタリング");
    await expect(value(rows(page).first())).toHaveText("100");

    // 16bit
    await value(rows(page).first()).click();
    const dialog = writeDialog(page);
    await expect(dialog).toBeVisible();
    await expect(
      dialog
        .locator(".dialog-row", { hasText: "アドレス:" })
        .locator(".dialog-value"),
    ).toHaveText("6");
    await expect(
      dialog
        .locator(".dialog-row", { hasText: "現在の値:" })
        .locator(".dialog-value"),
    ).toHaveText("100");
    const input = dialog.locator("input.dialog-input");
    await expect(input).toHaveValue("100");
    await shot(page, "monitoring-write-dialog");

    // 入力形式を変えると、入力中の値が変換される
    const format = field(dialog, "入力形式:");
    await expect(format).toHaveValue("decimal");
    await format.selectOption({ label: "16進数" });
    await expect(input).toHaveValue("0x0064");
    await input.fill("0x00FF");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 5)).toEqual([255]);
    await expect(value(rows(page).first())).toHaveText("255");

    // Enter で書き込める。キャンセルと Esc（入力欄で押す）では書き込まない
    await value(rows(page).first()).click();
    await page.waitForTimeout(150);
    await input.fill("300");
    await input.press("Enter");
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 5)).toEqual([300]);
    await value(rows(page).first()).click();
    await input.fill("1");
    await dialog.getByRole("button", { name: "キャンセル" }).click();
    await expect(dialog).toBeHidden();
    await value(rows(page).first()).click();
    await input.fill("1");
    await input.press("Escape");
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 5)).toEqual([300]);

    // 数値でない入力では書き込まず、ダイアログは開いたまま
    await value(rows(page).first()).click();
    await input.fill("abc");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "キャンセル" }).click();
    expect(await readWords(page, 5)).toEqual([300]);

    // 32bit の項目は、行の表示形式で開き、エンディアンに従って書き込む
    await value(rows(page).nth(1)).click();
    await expect(format).toHaveValue("hex");
    await expect(input).toHaveValue("0x00000000");
    await input.fill("0xDEADBEEF");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 6, 2)).toEqual([0xdead, 0xbeef]);
    await expect(value(rows(page).nth(1))).toHaveText("0xDEADBEEF");
  });

  test("書き込みダイアログを開くと、入力欄の値が選択された状態になる", async ({
    page,
  }) => {
    await addItem(page, 0);
    await writeWord(page, 0, 1234);
    await openRegisters(page, "モニタリング");
    await expect(value(rows(page).first())).toHaveText("1234");
    await value(rows(page).first()).click();
    const dialog = writeDialog(page);
    const input = dialog.locator("input.dialog-input");
    await expect(input).toBeFocused();

    // そのまま打つと、元の値を置き換える
    await page.waitForTimeout(150);
    await page.keyboard.type("56");
    await expect(input).toHaveValue("56");
    await page.keyboard.press("Enter");
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 0)).toEqual([56]);
  });

  test("ビットの項目は ON / OFF で表示し、文字で書き込む", async ({ page }) => {
    await addItem(page, 2, { memoryArea: "coils" });
    await openRegisters(page, "モニタリング");
    const row = rows(page).first();
    await expect(cell(row, 2)).toHaveText("3");
    await expect(cell(row, 3)).toHaveText("Bit");
    await expect(cell(row, 4)).toHaveText("-");
    await expect(cell(row, 5)).toHaveText("-");
    await expect(value(row)).toHaveText("OFF");

    await value(row).click();
    const dialog = writeDialog(page);
    const input = dialog.locator("input.dialog-input");
    await expect(field(dialog, "入力形式:")).toHaveCount(0);
    await expect(input).toHaveAttribute("placeholder", "0, 1, ON, OFF");
    await expect(input).toHaveValue("OFF");
    await input.fill("on");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(dialog).toBeHidden();
    await expect(value(row)).toHaveText("ON");
    expect(await api(page, "ReadBits", "modbus-tcp", "coils", 2, 1)).toEqual([
      true,
    ]);

    await value(row).click();
    await input.fill("0");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(value(row)).toHaveText("OFF");
    await value(row).click();
    await input.fill("1");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(value(row)).toHaveText("ON");
  });

  test("項目を削除でき、つまみをドラッグして並べ替えられる", async ({
    page,
  }) => {
    await addItem(page, 0);
    await addItem(page, 1);
    await addItem(page, 2);
    await openRegisters(page, "モニタリング");
    await expect(rows(page)).toHaveCount(3);
    const addresses = () =>
      rows(page).evaluateAll((trs) =>
        trs.map((tr) => tr.querySelectorAll("td")[2].textContent),
      );
    expect(await addresses()).toEqual(["1", "2", "3"]);

    // 先頭の行を 3 行目の位置までドラッグする
    const handle = rows(page).nth(0).locator(".monitoring-drag-handle");
    const from = (await handle.boundingBox())!;
    const to = (await rows(page)
      .nth(2)
      .locator(".monitoring-drag-handle")
      .boundingBox())!;
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
    await page.mouse.down();
    await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2 + 4, {
      steps: 12,
    });
    await page.mouse.up();
    await expect.poll(addresses).toEqual(["2", "3", "1"]);
    // 並び順は保存される
    await expect
      .poll(async () => (await items(page)).map((i) => i.address))
      .toEqual([1, 2, 0]);

    // 削除（ドロップのアニメーションが終わってから押す）
    await page.waitForTimeout(500);
    await rows(page).nth(1).getByRole("button", { name: "削除" }).click();
    await expect(rows(page)).toHaveCount(2);
    expect(await addresses()).toEqual(["2", "1"]);
    expect((await items(page)).map((i) => i.address)).toEqual([1, 0]);
  });

  test("サーバーが複数あっても、選んだサーバーの項目だけを出す", async ({
    page,
  }) => {
    await api(page, "AddServer", "modbus-rtu", "");
    await addItem(page, 0);
    await api(page, "AddMonitoringItem", {
      id: "",
      order: 0,
      protocolType: "modbus-rtu",
      memoryArea: "holdingRegisters",
      address: 4,
      bitWidth: 16,
      endianness: "big",
      displayFormat: "decimal",
    });
    await addItem(page, 2);

    // Modbus TCP: 自分の 2 項目だけ。プロトコルの列はない
    await openRegisters(page, "モニタリング");
    const headers = page.locator("table.monitoring-table thead th");
    await expect(headers).toHaveCount(8);
    await expect(rows(page)).toHaveCount(2);
    expect(
      await rows(page).locator("td:nth-child(3)").allTextContents(),
    ).toEqual(["1", "3"]);

    // 追加ダイアログにプロトコルの選択はなく、選んでいるサーバーに追加する
    await openAddDialog(page);
    const dialog = addDialog(page);
    await expect(field(dialog, "プロトコル:")).toHaveCount(0);
    await field(dialog, "開始アドレス:").fill("8");
    await dialog.getByRole("button", { name: "追加" }).click();
    await expect(rows(page)).toHaveCount(3);
    expect((await items(page)).map((i) => [i.protocolType, i.address])).toEqual(
      [
        ["modbus-tcp", 0],
        ["modbus-rtu", 4],
        ["modbus-tcp", 2],
        ["modbus-tcp", 7],
      ],
    );

    // Modbus RTU を選ぶと、モニタリングのタブのまま、その 1 項目だけになる
    await openRegisters(page, "モニタリング", "Modbus RTU");
    await expect(rows(page)).toHaveCount(1);
    await expect(cell(rows(page).first(), 2)).toHaveText("5");
    await shot(page, "monitoring-in-server");
  });
});
