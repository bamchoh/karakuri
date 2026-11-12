import { test, expect, Page, Locator } from "@playwright/test";
import {
  addModbusServer,
  api,
  openApp,
  openRegisters,
  readWords,
  resetApp,
  shot,
  writeWord,
} from "./helpers";

// レジスタ画面の「一覧表示」の動きを確認する。
// 画面のコンポーネントを分割するときに、動きが変わっていないことを確かめるためのテスト。

test.beforeEach(async ({ page }) => {
  await openApp(page);
  await resetApp(page);
  await addModbusServer(page);
  await openRegisters(page, "一覧表示");
  // メモリエリアが読み込まれ、最初のワードのエリア（保持レジスタ）が選ばれるまで待つ
  await expect(control(page, "メモリエリア")).toHaveValue("holdingRegisters");
  await expect(cells(page)).toHaveCount(100);
});

test.afterEach(async ({ page }) => {
  await resetApp(page);
});

// 上の操作欄で、ラベルがちょうど label の入力欄
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
// 見出しがちょうど「書き込み」のダイアログ（モニタリングの「レジスタ書き込み」と区別する）
const writeDialog = (page: Page) =>
  page.locator(".dialog", {
    has: page.locator("h3", { hasText: /^書き込み$/ }),
  });

// ダイアログの「ラベル:」の行
function dialogRow(dialog: Locator, label: string) {
  return dialog.locator(".dialog-row", {
    has: dialog.page().locator("label", { hasText: label }),
  });
}

test.describe("レジスタの一覧表示", () => {
  test("値を表示し、セルをクリックして書き込める", async ({ page }) => {
    // アドレスは 1 始まりで表示する（Modbus のエリア）
    await expect(rowHeaders(page).first()).toHaveText("1");
    await expect(rowHeaders(page).nth(1)).toHaveText("11");
    await expect(colHeaders(page)).toHaveCount(10);
    await expect(page.locator(".register-navigation span")).toHaveText(
      "1 - 100",
    );

    // 外から書かれた値が自動で表示される
    await writeWord(page, 0, 1234);
    await expect(cells(page).first()).toHaveText("1234");

    // セルをクリックすると書き込みダイアログが開く
    await cells(page).nth(2).click();
    const dialog = writeDialog(page);
    await expect(dialog).toBeVisible();
    await expect(
      dialogRow(dialog, "アドレス:").locator(".dialog-value"),
    ).toHaveText("3");
    await expect(
      dialogRow(dialog, "現在の値:").locator(".dialog-value"),
    ).toHaveText("0");
    const input = dialog.locator("input.dialog-input");
    await expect(input).toHaveValue("0");
    await shot(page, "register-write-dialog");

    await input.fill("4321");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 2)).toEqual([4321]);
    await expect(cells(page).nth(2)).toHaveText("4321");
    await expect(cells(page).nth(2)).toHaveClass(/cell-selected/);

    // Enter でも書き込める
    await cells(page).nth(3).click();
    await expect(dialog).toBeVisible();
    await page.waitForTimeout(150);
    await input.fill("77");
    await input.press("Enter");
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 3)).toEqual([77]);

    // キャンセルと Esc（入力欄で押す）では書き込まない
    await cells(page).nth(4).click();
    await input.fill("9");
    await dialog.getByRole("button", { name: "キャンセル" }).click();
    await expect(dialog).toBeHidden();
    await cells(page).nth(4).click();
    await input.fill("9");
    await input.press("Escape");
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 4)).toEqual([0]);

    // 数値でない入力では書き込まず、ダイアログは開いたまま
    await cells(page).nth(4).click();
    await input.fill("abc");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(dialog).toBeVisible();
    expect(await readWords(page, 4)).toEqual([0]);
    await shot(page, "register-list");
  });

  test("書き込みダイアログを開くと、入力欄の値が選択された状態になる", async ({
    page,
  }) => {
    await writeWord(page, 0, 1234);
    await expect(cells(page).first()).toHaveText("1234");
    await cells(page).first().click();
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

  test("表示形式を変えられ、ダイアログの入力形式で値を変換できる", async ({
    page,
  }) => {
    await writeWord(page, 0, 255);
    await expect(cells(page).first()).toHaveText("255");

    await control(page, "表示形式").selectOption({ label: "16進数" });
    await expect(cells(page).first()).toHaveText("0x00FF");
    await control(page, "表示形式").selectOption({ label: "8進数" });
    await expect(cells(page).first()).toHaveText("0o000377");
    await control(page, "表示形式").selectOption({ label: "2進数" });
    await expect(cells(page).first()).toHaveText("0000000011111111");
    await control(page, "表示形式").selectOption({ label: "16進数" });

    // ダイアログは、一覧の表示形式で開く
    await cells(page).first().click();
    const dialog = writeDialog(page);
    const input = dialog.locator("input.dialog-input");
    const format = dialogRow(dialog, "入力形式:").locator("select");
    await expect(format).toHaveValue("hex");
    await expect(input).toHaveValue("0x00FF");
    await expect(
      dialogRow(dialog, "現在の値:").locator(".dialog-value"),
    ).toHaveText("0x00FF");

    // 入力形式を変えると、入力中の値が変換される
    await format.selectOption({ label: "2進数" });
    await expect(input).toHaveValue("0000000011111111");
    await format.selectOption({ label: "10進数" });
    await expect(input).toHaveValue("255");
    await format.selectOption({ label: "16進数" });
    await expect(input).toHaveValue("0x00FF");

    // 16 進は 0x を付けなくても読める
    await input.fill("1A");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 0)).toEqual([26]);
    await expect(cells(page).first()).toHaveText("0x001A");

    // モニタリングに切り替えて戻っても、表示形式はそのまま
    await page.locator(".sub-tab-button", { hasText: "モニタリング" }).click();
    await expect(page.locator(".monitoring-view")).toBeVisible();
    await page.locator(".sub-tab-button", { hasText: "一覧表示" }).click();
    await expect(control(page, "表示形式")).toHaveValue("hex");
    await expect(cells(page).first()).toHaveText("0x001A");
  });

  test("32bit・64bit でまとめて表示し、エンディアンに従って書き込める", async ({
    page,
  }) => {
    await writeWord(page, 0, 0x1234);
    await writeWord(page, 1, 0x5678);

    // 32bit: 2 ワードを 1 つの値として表示する
    await control(page, "ビット幅").selectOption({ label: "32bit" });
    await expect(colHeaders(page)).toHaveCount(5);
    await expect(colHeaders(page).first()).toHaveText("+1~+2");
    await expect(cells(page)).toHaveCount(50);
    await expect(cells(page).first()).toHaveText(String(0x12345678));

    await control(page, "エンディアン").selectOption({ label: "リトル(LE)" });
    await expect(cells(page).first()).toHaveText(String(0x56781234));
    await control(page, "表示形式").selectOption({ label: "16進数" });
    await expect(cells(page).first()).toHaveText("0x56781234");

    // リトルエンディアンでは、先頭のワードが下位
    await cells(page).first().click();
    const dialog = writeDialog(page);
    const input = dialog.locator("input.dialog-input");
    await expect(input).toHaveValue("0x56781234");
    await input.fill("0xDEADBEEF");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 0, 2)).toEqual([0xbeef, 0xdead]);

    // ビッグエンディアンでは、先頭のワードが上位
    await control(page, "エンディアン").selectOption({ label: "ビッグ(BE)" });
    await expect(cells(page).first()).toHaveText("0xBEEFDEAD");
    await cells(page).nth(1).click();
    await expect(
      dialogRow(dialog, "アドレス:").locator(".dialog-value"),
    ).toHaveText("3");
    await input.fill("0x00010002");
    // 入力形式を 10 進数に変えると、32bit の値として変換される
    await dialogRow(dialog, "入力形式:")
      .locator("select")
      .selectOption({ label: "10進数" });
    await expect(input).toHaveValue("65538");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 2, 2)).toEqual([1, 2]);

    // 64bit: 4 ワードを 1 つの値として表示する
    await control(page, "ビット幅").selectOption({ label: "64bit" });
    await expect(colHeaders(page)).toHaveCount(2);
    await expect(colHeaders(page).first()).toHaveText("+1~+4");
    await expect(cells(page)).toHaveCount(25);
    await expect(cells(page).first()).toHaveText("0xBEEFDEAD00010002");
    await shot(page, "register-list-64bit");

    await cells(page).nth(1).click();
    await expect(
      dialogRow(dialog, "アドレス:").locator(".dialog-value"),
    ).toHaveText("5");
    await input.fill("0x0011002200330044");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 4, 4)).toEqual([0x11, 0x22, 0x33, 0x44]);
  });

  test("ビットのエリアは ON / OFF で表示し、ボタンで書き込む", async ({
    page,
  }) => {
    await control(page, "メモリエリア").selectOption("coils");
    // ビットのエリアでは、表示形式・ビット幅・エンディアンを出さない
    await expect(control(page, "表示形式")).toHaveCount(0);
    await expect(control(page, "ビット幅")).toHaveCount(0);
    await expect(control(page, "エンディアン")).toHaveCount(0);
    await expect(cells(page).first()).toHaveText("OFF");

    await cells(page).nth(1).click();
    const dialog = writeDialog(page);
    await expect(
      dialogRow(dialog, "アドレス:").locator(".dialog-value"),
    ).toHaveText("2");
    await expect(
      dialogRow(dialog, "現在の値:").locator(".dialog-value"),
    ).toHaveText("OFF");
    await expect(dialog.locator("input.dialog-input")).toHaveCount(0);
    await expect(dialogRow(dialog, "入力形式:")).toHaveCount(0);
    await expect(dialog.getByRole("button", { name: "書き込み" })).toHaveCount(
      0,
    );
    await shot(page, "register-bit-dialog");

    await dialog.getByRole("button", { name: "ON", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect(cells(page).nth(1)).toHaveText("ON");
    await expect(cells(page).nth(1)).toHaveClass(/cell-on/);
    expect(await api(page, "ReadBits", "modbus-tcp", "coils", 1, 1)).toEqual([
      true,
    ]);

    await cells(page).nth(1).click();
    await dialog.getByRole("button", { name: "OFF", exact: true }).click();
    await expect(cells(page).nth(1)).toHaveText("OFF");
    await expect(cells(page).nth(1)).not.toHaveClass(/cell-on/);

    // ワードのエリアに戻すと、表示形式などがまた出る
    await control(page, "メモリエリア").selectOption("holdingRegisters");
    await expect(control(page, "表示形式")).toHaveCount(1);
    await expect(cells(page).first()).toHaveText("0");
  });

  test("開始アドレスと「前へ」「次へ」で表示範囲を変えられる", async ({
    page,
  }) => {
    const start = control(page, "開始アドレス");
    const nav = page.locator(".register-navigation");
    const prev = nav.getByRole("button", { name: "前へ" });
    const next = nav.getByRole("button", { name: "次へ" });
    await expect(start).toHaveValue("1");
    await expect(prev).toBeDisabled();

    await next.click();
    await expect(start).toHaveValue("101");
    await expect(rowHeaders(page).first()).toHaveText("101");
    await expect(nav.locator("span")).toHaveText("101 - 200");
    await prev.click();
    await expect(start).toHaveValue("1");

    // 開始アドレスを入力してフォーカスを外すと、その位置から表示する
    await writeWord(page, 250, 55);
    await start.fill("251");
    await start.blur();
    await expect(rowHeaders(page).first()).toHaveText("251");
    await expect(cells(page).first()).toHaveText("55");
    await expect(start).toHaveValue("251");

    // 表示中の先頭からの位置に書き込む
    await cells(page).nth(1).click();
    const dialog = writeDialog(page);
    await expect(
      dialogRow(dialog, "アドレス:").locator(".dialog-value"),
    ).toHaveText("252");
    await dialog.locator("input.dialog-input").fill("66");
    await dialog.getByRole("button", { name: "書き込み" }).click();
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 251)).toEqual([66]);

    // 数値でない入力は元に戻る
    await start.fill("abc");
    await start.blur();
    await expect(start).toHaveValue("251");
    await expect(rowHeaders(page).first()).toHaveText("251");

    // 最後のページでは「次へ」を押せない
    const areas = await api<{ id: string; size: number }[]>(
      page,
      "GetMemoryAreas",
      "modbus-tcp",
    );
    const size = areas.find((a) => a.id === "holdingRegisters")!.size;
    await start.fill(String(size - 100 + 1));
    await start.blur();
    await expect(rowHeaders(page).first()).toHaveText(String(size - 100 + 1));
    await expect(cells(page)).toHaveCount(100);
    await expect(next).toBeDisabled();
    await expect(prev).toBeEnabled();
  });

  test("矢印キーなどでセルを選び、Enter で書き込みダイアログを開ける", async ({
    page,
  }) => {
    const container = page.locator(".register-matrix-container");
    const selected = page.locator("td.matrix-cell.cell-selected");
    const selectedIndex = async () =>
      cells(page).evaluateAll((els) =>
        els.findIndex((el) => el.classList.contains("cell-selected")),
      );
    await container.focus();
    await expect(selected).toHaveCount(1);
    expect(await selectedIndex()).toBe(0);

    const steps: [string, number][] = [
      ["ArrowRight", 1],
      ["ArrowDown", 11],
      ["ArrowLeft", 10],
      ["ArrowUp", 0],
      ["ArrowUp", 0], // 端では動かない
      ["End", 99],
      ["ArrowDown", 99],
      ["Home", 0],
      ["PageDown", 50],
      ["PageUp", 0],
    ];
    for (const [key, want] of steps) {
      await page.keyboard.press(key);
      await expect.poll(selectedIndex, { message: key }).toBe(want);
    }

    // Enter で選択中のセルの書き込みダイアログが開く
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("Enter");
    const dialog = writeDialog(page);
    await expect(dialog).toBeVisible();
    await expect(
      dialogRow(dialog, "アドレス:").locator(".dialog-value"),
    ).toHaveText("3");
    // ダイアログが開いている間は、矢印キーで選択が動かない
    await page.keyboard.press("ArrowDown");
    expect(await selectedIndex()).toBe(2);

    // 書き込んで閉じると一覧にフォーカスが戻り、続けてキーで操作できる
    await page.waitForTimeout(150);
    const input = dialog.locator("input.dialog-input");
    await input.fill("5");
    await input.press("Enter");
    await expect(dialog).toBeHidden();
    expect(await readWords(page, 2)).toEqual([5]);
    await expect(container).toBeFocused();
    await page.keyboard.press("ArrowRight");
    await expect.poll(selectedIndex).toBe(3);

    // Esc で閉じても同じ
    await page.keyboard.press("Enter");
    await expect(dialog).toBeVisible();
    await input.press("Escape");
    await expect(dialog).toBeHidden();
    await expect(container).toBeFocused();
    await page.keyboard.press("ArrowLeft");
    await expect.poll(selectedIndex).toBe(2);
  });

  test("レジスタを持つサーバーが複数あるときだけ、プロトコルを選べる", async ({
    page,
  }) => {
    await expect(control(page, "プロトコル")).toHaveCount(0);

    // OPC UA はレジスタを持たないので、数に入らない
    await api(page, "AddServer", "opcua", "");
    await page.waitForTimeout(1500); // サーバーの一覧は 1 秒ごとに読み直す
    await expect(control(page, "プロトコル")).toHaveCount(0);

    await api(page, "AddServer", "modbus-rtu", "");
    const protocol = control(page, "プロトコル");
    await expect(protocol).toBeVisible();
    const names = await protocol.locator("option").allTextContents();
    expect([...names].sort()).toEqual(["Modbus RTU", "Modbus TCP"]);
    // 選んでいたプロトコルはそのまま
    await expect(protocol).toHaveValue("modbus-tcp");
    await writeWord(page, 0, 4242);
    await expect(cells(page).first()).toHaveText("4242");

    // 切り替えて戻すと、そのプロトコルのエリアを読み直す
    await protocol.selectOption("modbus-rtu");
    await protocol.selectOption("modbus-tcp");
    await expect(control(page, "メモリエリア")).toHaveValue("holdingRegisters");
    await expect(cells(page).first()).toHaveText("4242");
  });
});
