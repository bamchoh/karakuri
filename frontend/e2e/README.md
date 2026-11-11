# 画面の確認用テスト（Playwright）

`task dev`（`wails dev`）が立てる開発用サーバー（`http://localhost:34115`）をブラウザで開き、画面を操作して確認します。ブラウザから開いても Go 側の API とプラグインにつながるので、アプリと同じ動作を確認できます。

## 準備（初回のみ）

```bash
cd frontend
npm install
npx playwright install chromium   # Playwright 用の Chromium（インストール済みなら何もしない）
```

Playwright のバージョンは固定しています（`package.json`）。バージョンを上げると、対応する Chromium のダウンロードが必要になります。

## 実行

1. 開発サーバーを起動する（別のターミナル）。設定フォルダは一時フォルダに向ける。

   ```powershell
   $env:KARAKURI_CONFIG_DIR = "$env:TEMP\karakuri-e2e-config"
   $env:KARAKURI_WEBVIEW2_PATH = 'D:\tools\WebView2-156'   # 必要な PC のみ（CLAUDE.md 参照）
   task dev
   ```

2. テストを実行する。

   ```bash
   task e2e                      # 全部
   task e2e -- node-publishing   # ファイル名で絞る
   task e2e -- --headed          # ブラウザを表示して実行
   ```

- スクリーンショットは `e2e/.screenshots/`、失敗時の記録は `e2e/.results/` に出ます（どちらも git には入れません）。
- 開発サーバーの URL を変える場合は、環境変数 `KARAKURI_DEV_URL` で指定します。

## 注意

- **テストはアプリの状態を消します。** 各テストの前後で、サーバーと変数をすべて削除します（`resetApp()`）。作業中のプロジェクトを開いたアプリに対して実行しないでください。
- テストは実際にポートを開きます（OPC UA: 48401、Modbus TCP: 15020）。
- アプリは 1 つなので、テストは並列に実行しません（`workers: 1`）。
- 確認できないもの: アプリのウィンドウ固有の挙動（WebView2 の描画、ネイティブのファイル選択ダイアログ）、UaExpert などの外部クライアントでの表示。

## 書き方

- データの準備と後片付けは Go 側の API を直接呼ぶ（`helpers.ts` の `api(page, "CreateVariable", ...)`）。確認したい操作だけを画面で行う。
- 見た目を残したいところで `shot(page, "名前")` を呼ぶ。
- `alert()` は `page.once("dialog", ...)` で受け取る（受け取らないとテストが止まる）。
