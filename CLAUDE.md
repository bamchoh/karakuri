# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Rules

- Always respond in Japanese (日本語で応答すること)

## Project Overview

このプロジェクトは PLC シミュレーター です。Modbus（TCP/RTU/ASCII）と OPC UA をサポートしています。

### 主な機能

- **マルチプロトコル対応**: Modbus TCP / Modbus RTU / Modbus ASCII / OPC UA を個別のサーバーとして起動可能
- **複数サーバー同時実行**: 異なるプロトコル（例: Modbus TCP + OPC UA）を同時に起動可能。各サーバーは独立したメモリ空間を持つ
- **変数管理**: IEC 61131-3準拠のデータ型（スカラー、配列、構造体、STRING[n]）をサポート。OPC UA でノードとして公開可能
- **スクリプト機能**: JavaScriptで周期処理を記述。const/let対応（IIFE wrapping）、結果とエラーはコンソールに表示
- **レジスタ操作**: GUIからメモリエリアの値を直接操作可能
- **モニタリング**: 任意のレジスタをリアルタイム監視・書き込み（ドラッグ&ドロップ並べ替え対応）
- **プロトコルマッピング**: 変数を複数プロトコルのメモリアドレスにマッピング可能
- **REST HTTP API**: アプリをネットワーク経由で外部から操作可能（デフォルトポート 8765）。サーバー管理・レジスタ読み書き・変数管理・プロジェクトエクスポート/インポートに対応
- **クライアント機能**: Karakuri から外部の機器（Modbus TCP サーバー・OPC UA サーバー）につないで、コマンドを送る。コマンド（Modbus はファンクションコード、OPC UA は Read / Write / Browse）を選んで組み立てる方式と、16 進（Modbus は PDU、OPC UA はサービスのリクエスト本体）で直接入れる方式がある。送受信のバイト列・応答の解釈・履歴を表示する。読み取り項目を周期的に読むポーリングもできる。OPC UA は、サブスクリプション（値が変わったときに機器の側から知らせが届く）にも対応する。変数を接続先のアドレスにマッピングすると、ポーリング中に変数と同期する（通信エラー・書き込み未反映の状態を表示。Modbus TCP だけ）

## Build and Development Commands

```bash
# 開発モード（画面はホットリロード。Go 側を変更したら起動し直す）
task dev

# プロダクションビルド（build/bin に exe を 1 つ作る）
task build

# 全テスト実行
task test

# 画面の確認用テスト（Playwright）。先に task dev で開発サーバーを起動しておく
task e2e

# ビルド成果物を削除
task clean

# Goバインディングの再生成
wails generate module

# フロントエンドのみビルド
cd frontend && npm run build

# 画面のコードを整形する（Prettier）。検査だけなら npm run format:check
cd frontend && npm run format

# Goコードのビルド確認
go build ./...
```

タスクランナーは [go-task](https://taskfile.dev/) を使用。`Taskfile.yaml` に定義。

### CI（GitHub Actions）

`.github/workflows/ci.yml` が、Pull Request と main への push のたびに実行する。

- **ビルドとテスト（Windows）**: 画面のコードの整形の検査（`npm run format:check`）、画面の型検査とビルド（`npm run build`）、`go vet ./...`、`go test ./...`
- **整形と競合検出（Linux）**: `gofmt -l .`、`go test -race ./internal/...`
  - 開発 PC には C コンパイラがなく `go test -race` を実行できないので、データ競合は CI で見つける
  - Windows の実行環境は改行を CRLF に変えるため、`gofmt` の検査は Linux で行う
  - アプリ本体（`main.go` / `app.go`）は Wails が Linux 用のライブラリを必要とするので、Linux では `internal` だけを対象にする
- **画面のコード（`frontend/`）は Prettier で整形する**。画面のコードを変更したら `npm run format` を実行する（整形されていないと CI が落ちる）
  - 設定は `frontend/.prettierrc.json`（改行コードは、ファイルのものをそのまま使う）。自動生成される `wailsjs/` などは `frontend/.prettierignore` で対象から外している
- 画面の確認用テスト（Playwright）は対象外（開発サーバーと WebView2 が要る）。画面を変更したら手元で `task e2e` を実行する

### リリース

`.github/workflows/release.yml` が、`v` で始まるタグの push で Windows 用の exe をビルドし、GitHub のリリースを**下書き**で作る（`Karakuri_<バージョン>.zip` に `Karakuri.exe` と `LICENSE`）。

1. `frontend/src/App.tsx` の `APP_VERSION` を新しいバージョンにして、main にマージする
2. main でタグを打って push する: `git tag v0.0.39 && git push origin v0.0.39`
3. Actions の実行が終わったら、GitHub のリリースの画面で下書きの内容と zip を確かめて、公開する

- タグの名前が `APP_VERSION` と違うと、ワークフローは失敗する（画面に表示するバージョンと、リリースのバージョンがずれないようにするため）
- ワークフローのファイルを変更する Pull Request でも実行される（ビルドまで。リリースは作らない）。できた zip は実行結果のページの Artifacts から取り出せる

### 画面の確認用テスト（Playwright）

`frontend/e2e/` に、ブラウザで画面を操作して確認するテストがある。`task dev` が立てる開発用サーバー（`http://localhost:34115`）に接続するため、Go 側の API とプロトコルの実装を含めた動作を確認できる。手順と注意は `frontend/e2e/README.md` を参照。

- 画面ごとのテスト: 通信画面（サーバー）`server-panel.spec.ts`、変数画面 `variable-dialogs.spec.ts` / `node-publishing.spec.ts`、レジスタ画面 `register-panel.spec.ts` / `monitoring.spec.ts`、スクリプト画面 `script-panel.spec.ts`、通信画面（クライアント）`client-panel.spec.ts` / `client-polling.spec.ts` / `client-mapping.spec.ts` / `client-saved-commands.spec.ts` / `client-opcua.spec.ts`
- 画面を変更したら、該当するテストを実行する（なければ追加する）。`shot()` で撮ったスクリーンショット（`frontend/e2e/.screenshots/`）で見た目も確認する
- データの準備は `api(page, "メソッド名", ...)` で Go 側の API を直接呼び、確認したい操作だけを画面で行う
- **テストはアプリのサーバー・変数・モニタリング項目・スクリプトをすべて削除する。** 開発サーバーは `KARAKURI_CONFIG_DIR` を一時フォルダに向けて起動する
- Playwright のバージョンは固定（インストール済みの Chromium と対応させるため）

### フロントエンドビルドターゲット

`frontend/vite.config.ts` に `build.target` を明示的に指定している:

```typescript
build: {
  target: ['chrome87', 'edge88', 'firefox78', 'safari14'],
}
```

**理由**: LINT/ULINT の変数ビュー編集に JavaScript `BigInt` を使用しているため、Wails のデフォルトターゲット（safari13 を含む）ではビルドエラーになる。
safari14 以上が BigInt リテラル（`n` サフィックス）をサポートする最低要件。

### WebView2 ランタイムの指定（`KARAKURI_WEBVIEW2_PATH`）

環境変数 `KARAKURI_WEBVIEW2_PATH` に WebView2 の Fixed Version ランタイムのフォルダ（`msedgewebview2.exe` があるフォルダ）を指定すると、インストール済みの Evergreen ランタイムの代わりにそれを使う（`main.go` の `windows.Options.WebviewBrowserPath`）。未設定なら従来どおり。

```powershell
# task dev / Karakuri.exe をこのランタイムで起動する
$env:KARAKURI_WEBVIEW2_PATH = 'D:\tools\WebView2-156'
task dev

# エクスプローラーから起動する場合はユーザー環境変数に登録する
setx KARAKURI_WEBVIEW2_PATH D:\tools\WebView2-156
```

**理由**: 2026-11 時点の開発 PC では、WebView2 / Edge 157.0.4341.46 のネットワークサービス（`network.mojom.NetworkService`）が起動直後に毎回クラッシュする（`msedge.dll`、例外 `0xc0000409`）。画面ファイルの応答が失われ、ウィンドウが真っ暗になる・CSS が当たらないなどの症状が不定期に出る。Fixed Version 156.0.4314.58 では再現しない。

- 症状: ウィンドウは開くが中身が描画されない。ログに `WebVie2wProcess failed with kind 4`（ユーティリティプロセスの終了）が 2 回出る
- 確認方法: Windows のイベントログ（Application, ID 1000）で `msedgewebview2.exe` / `msedge.exe` のクラッシュを確認する
- Fixed Version ランタイムは https://developer.microsoft.com/microsoft-edge/webview2/ の「Fixed Version」から入手する
- Wails v2 は WebView2 に任意のブラウザ引数を渡す手段がなく、`WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` やレジストリのポリシーも反映されないため、ランタイムの差し替えで回避している

## Architecture

Wails v2をベースに、フロントエンドはVite + React + TypeScript、バックエンドはGoで構成。

### ディレクトリ構造（DDD + マルチプロトコル）

プロトコル（Modbus / OPC UA）の実装はアプリに組み込まれている（1 つの exe）。以前はプロトコルごとに別の exe（プラグイン）にして gRPC で通信していたが、やめた（issue #58）。

```
app.go / main.go      # Wails のエントリー。起動時に protocols.Factories() を PLCService に登録する
internal/
├── domain/           # ドメイン層
│   ├── protocol/     # プロトコル共通インターフェース（ServerFactory, DataStore, ConfigField等）
│   │   └── protocoltest/  # テスト用のフェイク（Factory / Server / DataStore）。ホスト側のテストで共通に使う
│   ├── script/       # スクリプトエンティティ
│   └── variable/     # 変数・構造体型・NodePublishing
├── application/      # アプリケーション層（プロトコル固有の値を持たない）
│   ├── plc_service.go            # メインサービス本体（構造体・初期化・ファクトリーの登録・サーバーの追加/削除・終了処理・イベント）
│   ├── plc_service_instance.go   # サーバーの組み立て（attachServer / detachSync）
│   ├── plc_service_servers.go    # サーバーの起動/停止・設定・プロトコルスキーマ・ノードアドレス・UnitID
│   ├── plc_service_memory.go     # 汎用メモリ操作
│   ├── plc_service_scripts.go    # スクリプト管理・コンソールログ
│   ├── plc_service_project.go    # プロジェクト Export/Import
│   ├── plc_service_monitoring.go # モニタリング項目と設定ファイル
│   ├── plc_service_variables.go  # 変数・マッピング・ノード公開設定・構造体型
│   ├── app_events.go             # Wails イベントの送信（WailsAppStateEmitter）と変数変更の間引き
│   └── dto.go                    # DTO定義（ProtocolSchemaDTO, MonitoringItemDTO等）
├── protocols/        # プロトコルの実装（プロトコル固有のコードはすべてここ）
│   ├── protocols.go      # Factories(): 組み込むプロトコルの一覧、ClientFactories(): 組み込むクライアントの一覧
│   ├── integration_test.go # アプリと同じ組み立てで、実際のクライアントからつないで確認するテスト
│   ├── modbus/           # Modbus TCP / RTU / ASCII
│   │   ├── factory.go      # TCPFactory / RTUFactory / ASCIIFactory、ModbusConfig、ModbusServer、プロトコルタイプの定数
│   │   ├── server.go       # Server（トランスポートの起動・停止）
│   │   ├── *_transport.go  # TCP / RTU / ASCII のトランスポート（TCP は、オフにした UnitID 宛てのリクエストを捨てる中継を持つ）
│   │   ├── datastore.go    # ModbusDataStore（4 つのメモリエリア）
│   │   ├── datastore_adapter.go # リクエスト → DataStore の読み書き
│   │   ├── client.go       # TCPClientFactory / TCPClient（Modbus TCP クライアント。リクエストの組み立て・送受信・応答の解釈）
│   │   ├── client_mapping.go # 変数マッピング用のエリアと読み書きのリクエスト（MappableClientFactory）
│   │   └── rtu/            # RTU / ASCII のフレーム処理（Processor は RTU と ASCII で共通）
│   └── opcua/            # OPC UA（bamchoh/uastack 使用）
│       ├── factory.go      # Factory（VariableStoreInjector 実装）
│       ├── config.go       # Config（ネットワーク設定 + アプリケーション情報）
│       ├── server.go       # Server（uastack サーバーのライフサイクル）
│       ├── nodes.go        # plcNodes（公開変数 → uastack アドレス空間のノード、値の同期）
│       ├── types.go        # PLC 型解析・PLC 値 ↔ ua.Variant 変換
│       ├── client.go       # ClientFactory（OPC UA クライアント。接続先の設定・コマンドの定義・リクエストの組み立てと要約）
│       ├── client_conn.go  # Client（セッションの接続・送受信・応答の解釈）
│       ├── client_subscription.go # サブスクリプション（監視項目の定義・別のセッションでの監視・つなぎ直し）
│       └── datastore.go    # emptyDataStore（OPC UA はレジスタを持たない）
└── infrastructure/   # インフラ層（プロトコル固有実装は含まない）
    ├── httpapi/      # REST HTTP APIサーバー実装
    │   └── server.go       # HTTPAPIServer（net/http ServeMux使用）
    ├── appconfig/    # 設定フォルダの決定（旧フォルダからの移行を含む）
    ├── adapter/      # アダプター層
    │   ├── variable_datastore.go       # VariableBackedDataStore（変数↔DataStore双方向同期）
    │   ├── variable_store_accessor.go  # VariableStoreAccessor 実装（ノード公開型プロトコルが変数を読み書きする）
    │   ├── device_sync.go              # DeviceSync（変数とクライアントの接続先のアドレスの同期の状態。通信はしない）
    │   └── variable_codec.go           # 変数の値 ↔ ワード列の変換（VariableBackedDataStore と DeviceSync で共通）
    └── scripting/    # JSエンジン（goja使用）
```

**依存の向き**: `internal/protocols/<名前>/` は `internal/domain/protocol` だけに依存する。`internal/application` と `internal/infrastructure` は `internal/protocols` を import しない（プロトコルを知っているのは `app.go` と `internal/protocols/protocols.go` だけ）。

### 主要コンポーネント

- **PLCService** (`internal/application/plc_service*.go`、機能ごとにファイルを分割): メインサービス。プロトコル非依存で、複数のサーバーインスタンスを同時管理
  - `servers map[protocol.ProtocolType]*serverInstance` で各プロトコルのサーバーを保持
  - 各プロトコルタイプは最大1インスタンス（プロトコルタイプをサーバー識別子として利用）
  - プロトコルタイプの値は各プロトコルの実装が決める（Modbus は `"modbus-tcp"` / `"modbus-rtu"` / `"modbus-ascii"` の 3 つ、OPC UA は `"opcua"`）。ホストは値の意味を持たない
  - `variableStore` と `scriptEngine` は全サーバーで共有
  - `factories map[protocol.ProtocolType]protocol.ServerFactory` でプロトコルファクトリーを管理
  - `RegisterFactory()`: ファクトリーを登録する（起動時に `app.go` が `protocols.Factories()` の分だけ呼ぶ）
  - `AddServer()`: バリアントの指定が空なら、そのプロトコルの最初のバリアントにする。`VariableStoreInjector` を実装するファクトリーへ `VariableStoreAccessor` を注入する
  - `attachServer()`（`plc_service_instance.go`）: DataStore とサーバーを作る。DataStore は `VariableBackedDataStore` で包み、変数と双方向に同期する
    - **DataStore とサーバーは、サーバーを削除するまで同じものを使い続ける**（起動・停止では作り直さない）。そのため、起動前からレジスタを読み書きでき、レジスタの値と UnitID の応答設定は起動し直しても残る
    - プロジェクトを読み込むと、サーバーをすべて作り直す（レジスタの値は残らない）
  - `UpdateVariableMappings()` 後に `NodePublishingAware` を実装するサーバーへ変更通知を送信
- **protocols.Factories()** (`internal/protocols/protocols.go`): 組み込むプロトコルのファクトリーの一覧。プロトコルを足すときはここに 1 行足す
- **ServerFactory** (`internal/domain/protocol/server.go`): プロトコルサーバーを作成するファクトリーインターフェース
  - `GetConfigFields()`: スキーマ駆動UIのためのフィールド定義を返す
  - `GetProtocolCapabilities()`: UnitIDサポート等の機能情報を返す
  - `ConfigToMap()` / `MapToConfig()`: 設定の変換
- **Modbus のファクトリー** (`internal/protocols/modbus/factory.go`): `TCPFactory` / `RTUFactory` / `ASCIIFactory` の 3 種
  - それぞれ `ProtocolType()` が `"modbus-tcp"` / `"modbus-rtu"` / `"modbus-ascii"`（`modbus.ProtocolModbusTCP` など）を返す
  - ホスト側のテストでは `protocoltest.Factory`（`internal/domain/protocol/protocoltest`）を使用（プロトコル固有実装に依存しない）
- **opcua.Factory** (`internal/protocols/opcua/factory.go`): OPC UA サーバーのファクトリー
  - `ProtocolType()` が `"opcua"`（`opcua.ProtocolOPCUA`）を返す
  - `VariableStoreInjector` を実装。`AddServer()` が変数ストアへの accessor を注入する
  - `GetProtocolCapabilities()` が `SupportsNodePublishing: true` を返す
- **VariableBackedDataStore** (`internal/infrastructure/adapter/variable_datastore.go`): プロトコルの DataStore を包み、変数と双方向に同期する
  - 変数 → レジスタ: 変数が変わると（`OnVariableChanged`）、マッピング先のレジスタに書く
  - レジスタ → 変数: この DataStore に書かれると（`Write*`）、その範囲にマッピングされた変数に反映する。プロトコルのサーバーにはこの DataStore を渡すので、クライアントの書き込み・画面・スクリプトのどれもここを通る
  - **どちらも、呼び出しが戻った時点で反映が終わっている**（別のゴルーチンに回さない。Modbus のクライアントに応答が返った時点で、変数は更新済み）
  - 行き来を防ぐ仕組み: 変数 → レジスタは内側の DataStore に直接書く。レジスタ → 変数は `VariableStore.UpdateValueFrom()` で自分を通知先から外す
- **plcNodes** (`internal/protocols/opcua/nodes.go`): 公開変数を uastack の `AddressSpace` に実ノードとして登録する
  - `rebuild()`: `GetEnabledNodePublishings("opcua")` からノード構成を計画し、差分（追加・削除・型変更）だけノードを作り直す
  - 構造体フィールド・配列要素は子ノード（HasComponent）として事前に展開
  - `run()`: 変数の変更の通知（accessor の `SubscribeVariableChanges`）+ 1 秒ごとの再同期で、変数の値を `SetValue()`（変化したノードだけ uastack がサブスクリプションに即時通知）
  - `Node.OnWrite` でクライアントの書き込みを `WriteVariableValue` / `WriteVariableFieldValue` に転送
- **DataStore** (`internal/domain/protocol/server.go`): プロトコル共通のメモリ操作インターフェース
  - `ReadBits()`, `WriteBit()`, `ReadWords()`, `WriteWord()`: 汎用メモリ操作
  - `GetAreas()`: `MemoryArea` スライスを返す。`MemoryArea.OneOrigin` が true のエリアはUIで1オリジンアドレスを表示する（内部は常に0ベース）。Modbusの全4エリアは `OneOrigin: true`
  - `MemoryArea.ShortName`: マッピング列などに表示する短い名前（Modbus は `0` / `1` / `3` / `4`）。空ならエリア ID を表示する
- **VariableStoreAccessor** (`internal/domain/protocol/server.go`): プロトコルが VariableStore にアクセスするための汎用インターフェース
  - `GetEnabledNodePublishings(protocolType)`: 指定プロトコルで公開設定された変数一覧を返す
  - `ReadVariableValue(variableID)` / `WriteVariableValue(variableID, value)`: 変数値の読み書き
  - `GetStructFields(typeName)`: 構造体型のフィールド一覧を返す（子ノードブラウズ用）
  - 実装: `internal/infrastructure/adapter/variable_store_accessor.go`。`SubscribeVariableChanges(ctx, onChange)` も持ち、変数が変わるたびに変数 ID を通知する（プロトコル側は、あれば使う）
- **VariableStoreInjector** / **NodePublishingAware** (`internal/domain/protocol/server.go`): OPC UA 等のノードベースプロトコル向けインターフェース
  - `VariableStoreInjector`: `InjectVariableStore(accessor)` で遅延注入を受け付ける
  - `NodePublishingAware`: `OnNodePublishingUpdated()` でノード公開設定変更通知を受け付ける
  - `NodeAddressResolver`: `NodeAddresses(variables)`（`NodeVariable{ID, Name}` のスライス）で公開変数のノードアドレス（OPC UA なら NodeID 文字列）と、公開できない場合の理由（`NodeAddress.Error`）を返す。書式と判定はプロトコルが決める
  - `ProtocolCapabilities.SupportsNodePublishing: true` のプロトコルで利用される
- **UnitIDConfigurable** (`internal/domain/protocol/server.go`): UnitID ごとの応答有効・無効を設定できる ProtocolServer 用インターフェース（`GetDisabledUnitIDs` / `SetUnitIdEnabled` / `SetDisabledUnitIDs`）
  - `ProtocolCapabilities.SupportsUnitID: true` のプロトコルで利用される。ホストはこの型で判定する（個別のローカル定義は持たない）
  - 応答しない UnitID の一覧は、サーバーのオブジェクト（Modbus は `ModbusServer` の `DataStoreHandler`）が持つ。サーバーは削除するまで同じものを使うので、起動前から設定でき、起動し直しても残る
  - オフにした UnitID には、TCP / RTU / ASCII のどれも応答しない（例外応答も返さない）
  - Modbus TCP は simonvetter/modbus のサーバーを使うが、このライブラリは受け取ったリクエストに必ず応答する。そのため `TCPTransport`（`tcp_transport.go`）がクライアントとの間に中継を入れ、オフにした UnitID 宛てのフレームを捨てる。ライブラリのサーバーは 127.0.0.1 の空きポートで動く
- **ScriptEngine** (`internal/infrastructure/scripting/engine.go`): gojaベースのJavaScript実行エンジン
  - `plc`オブジェクトでDataStoreおよびVariableStoreにアクセス可能
  - スクリプトコードをIIFE `(function(){...})();` でラップして、const/let再宣言エラーを回避
  - エラー（コンパイルエラー・実行時のエラー・panic）はコンソールログに `[ERROR]` として出す。周期実行では、同じエラーが続く間は最初の 1 回だけ出す
  - TIME/DATE型シンタックスシュガー: `plc.readTimeMs(name)`, `plc.writeTimeMs(name, ms)` など、変数の読み取り〜数値変換〜書き込みをワンステップで実行（内部でparse/formatを自動適用）
  - LINT/ULINT BigInt API: `plc.readLintBig(name)`, `plc.writeLintBig(name, val)`, `plc.readUlintBig(name)`, `plc.writeUlintBig(name, val)`（2^53超の値をJavaScript BigInt型で精度損失なく操作。`readVariable()` で±2^53超の値を読んだ場合はコンソールに `[WARN]` を出力）

### フロントエンド構成（スキーマ駆動UI）

```
frontend/src/
├── components/
│   ├── ServerPanel.tsx     # 通信画面（サーバーとクライアントの一覧・起動と停止・保存していない変更の確認の流れ）
│   ├── ServerConfigPane.tsx    # 選択したサーバーの設定（スキーマ駆動のフォーム・UnitID の応答設定）
│   ├── DynamicField.tsx        # サーバー設定の 1 項目（種類ごとの入力欄）
│   ├── AddServerDialog.tsx     # サーバー追加ダイアログ
│   ├── UnsavedChangesDialog.tsx # 保存していない変更の確認ダイアログ（サーバーの切り替え・起動・画面の移動で共通）
│   ├── serverStatus.ts         # サーバーの状態に対応する CSS クラス
│   ├── RegisterPanel.tsx   # レジスタ画面（サブタブの切り替えとサーバー一覧の読み込み）
│   ├── RegisterListView.tsx    # レジスタの一覧表示（操作欄・10 列の表・キーボード操作）
│   ├── registerList.ts         # 一覧表示の純粋関数（行の組み立て・選択の移動・値の書式）
│   ├── RegisterWriteDialog.tsx # レジスタの書き込みダイアログ（一覧表示とモニタリングで共通）
│   ├── MonitoringView.tsx  # モニタリング（項目の追加・設定変更・並べ替え・削除）
│   ├── MonitoringRow.tsx       # モニタリングの 1 行（ドラッグで並べ替え）
│   ├── useMonitoringItems.ts   # モニタリング項目と現在値の読み込み・自動更新
│   ├── AddMonitoringDialog.tsx # モニタリング項目の追加ダイアログ
│   ├── VariableView.tsx    # 変数管理UI（変数一覧・行のキーボード操作・右クリックメニュー。ダイアログは下の各ファイル）
│   ├── AddVariableDialog.tsx   # 変数追加ダイアログ
│   ├── MetaEditDialog.tsx      # 変数の名前・データ型の編集ダイアログ
│   ├── ValueEditDialog.tsx     # 値の編集ダイアログ（再帰的エディタ）
│   ├── MappingDialog.tsx       # マッピング設定ダイアログ（レジスタマッピング + ノード公開設定）
│   ├── BulkMappingDialog.tsx   # 一括マッピング編集ダイアログ
│   ├── StructTypeDialog.tsx    # 構造体型管理ダイアログ
│   ├── StructArrayElementEditor.tsx  # 構造体配列要素のエディタ（ValueEditDialog で使用）
│   ├── DataTypeFields.tsx      # データ型を選ぶ入力欄（変数追加・変数の編集・構造体型のフィールドで共通）
│   ├── variableTypes.ts    # 変数のデータ型・値の純粋関数と行の型定義（state に依存しない）
│   ├── useNodeAddresses.ts # ノードアドレス（OPC UA の NodeID 等）の取得と保持（マッピングの 2 つのダイアログで使用）
│   ├── useEscapeKey.ts     # Esc キーでダイアログを閉じるためのフック
│   ├── ScriptPanel.tsx     # スクリプト管理（一覧・編集・1 回実行・コンソール）
│   ├── useClients.ts       # クライアントの接続先の一覧・使えるプロトコル・履歴の読み込み（通信画面で使う）
│   ├── ClientDetailPane.tsx    # 通信画面の右側（クライアントを選んだとき）: 接続先の設定（折りたためる）・コマンド・結果と履歴・ポーリング
│   ├── ClientCommandDialog.tsx # コマンドのダイアログ（項目から組み立てる / 16 進で入れる・送信・保存。新しいコマンドと保存したコマンドの編集で共通）
│   ├── ClientCommandLog.tsx    # コマンドの結果と履歴
│   ├── ClientSavedCommands.tsx # 保存したコマンドの一覧（「新しいコマンド」・送信・編集・削除）
│   ├── ClientPollingView.tsx   # ポーリング（周期・読み取り項目の追加・最後の結果と値）
│   ├── ClientSubscriptionView.tsx # サブスクリプション（通知の間隔・監視項目の追加・届いた値）。対応するプロトコルの接続先だけ
│   └── clientFormat.ts         # クライアント（通信画面の右側）で共通の表示の書式（状態の名前・時刻・かかった時間）
└── App.tsx                 # タブベースのメインUI
```

#### ServerPanel.tsx
通信画面（サイドバーの「Connections」）。サーバー（Karakuri が待ち受ける側）とクライアント（外部の機器へつなぐ側・マスター）を 1 つの画面で扱う。左に一覧、右に選んだものの設定（サーバーは `ServerConfigPane`、クライアントは `ClientDetailPane`）を並べる。
- **一覧**: 上に「SERVERS」、下に「CLIENTS」のグループ。それぞれに追加ボタン（「サーバーを追加」「接続先を追加」。クライアントのプロトコルが複数あるので、追加ボタンの左でプロトコルを選ぶ）。クライアントの行は `.server-tab-item.client-tab-item`
- **選択**: `{ kind: "server" | "client", id }` で持つ。選んでいるものが消えたら、先頭のサーバー、なければ先頭のクライアントを選ぶ
- **クライアント**: 一覧・プロトコル・履歴は `useClients()` が読み込む（一覧は 1 秒ごとに読み直す）。右側のサブタブ（コマンド / ポーリング / サブスクリプション）と、接続先の設定の欄を開いているかは `ServerPanel` が持ち、接続先を選び直しても保つ
- **サーバー一覧**: マウント時に `GetServerInstances()` / `GetAvailableProtocols()` を読み、以後は Wails イベント `plc:server-changed` / `plc:protocols-changed` で更新する
- **サーバー追加**: `AddServerDialog` で未追加のプロトコルから選択して追加（`AddServer(protocolType, variant)`）。ダイアログは開くたびにマウントし、選択中のプロトコルを自分で持つ
- **個別操作**: 一覧の再生・停止ボタンで起動と停止、設定の右上のごみ箱で削除（起動中は削除できない）
- **設定（`ServerConfigPane`）**: `GetProtocolSchema(protocolType)` のスキーマから `DynamicField` でフォームを作る。項目はカテゴリごとにまとめる。起動中は変更できない
  - UnitID をサポートするプロトコルでは「UnitID 応答設定」を出す。切り替えは保存ボタンを押さなくてもすぐ反映する
  - 親からは ref の `save()` / `revert()` で、保存していない変更を保存・破棄できる
- **保存していない変更の確認**: 設定を変更したまま、別のサーバー・クライアントへの切り替え・選択中のサーバーの起動・接続先の追加・別の画面への移動（`App.tsx`）をしようとすると `UnsavedChangesDialog` を出す。`ServerPanel` の `requestAction()` が「変更があれば確認してから実行する」をまとめている。サーバーとクライアントの右側は、どちらも ref の `save()` / `revert()` と `onDirtyChange` を持つ
- **画面のテスト**: `frontend/e2e/server-panel.spec.ts`（クライアントは `client-panel.spec.ts` など。e2e の `openClient(page, 名前)` で通信画面を開いて接続先を選ぶ）

#### RegisterPanel.tsx
「一覧表示」（`RegisterListView`）と「モニタリング」（`MonitoringView`）のサブタブを持ちます。サーバーの一覧は `RegisterPanel` が 1 秒ごとに読み、両方に渡す。
- `RegisterListView` は、タブを切り替えても設定（エリア・表示形式・表示範囲など）を保つため常にマウントし、`active` が false の間は描画と読み取りを止める。`MonitoringView` はタブを開いている間だけマウントする
- **プロトコル選択**: レジスタを持つサーバーが複数あるときだけ、上部にプロトコル選択セレクトを表示
  - `supportsNodePublishing=true` のサーバー（OPC UA 等）はレジスタを持たないため除外
- **一覧表示**: 選択中プロトコルのメモリエリアごとのレジスタ値を 10 列の表で表示（100ms ごとに読み直す）。セルのクリックか Enter で書き込みダイアログを開く
  - ビット幅が 32bit / 64bit のときは、複数ワードを 1 つのセルにまとめて表示する
  - 行の組み立て・キーによる選択の移動・値の書式は `registerList.ts` の純粋関数
- **書き込みダイアログ（`RegisterWriteDialog`）**: 一覧表示とモニタリングで共通。入力形式（10 進・16 進・8 進・2 進）を変えると入力中の値を変換する。32bit / 64bit はエンディアンに従って複数ワードに分けて書く
  - ビットの値の入れ方だけが違う: 一覧表示は ON / OFF のボタン（`bitInput="buttons"`）、モニタリングは文字（`bitInput="text"`）
- **1オリジンアドレス表示**: `area.oneOrigin` が true のエリアはアドレス表示を+1（内部値は0ベースを維持）
- **数値の変換**: ワードの結合・分解・書式は `frontend/src/utils/register.ts`、選択肢は `frontend/src/constants/register.ts`。画面ごとに書かない
- **画面のテスト**: `frontend/e2e/register-panel.spec.ts`

#### MonitoringView.tsx
プロトコルを横断したレジスタモニタリング。任意のレジスタを登録してリアルタイム監視・書き込みができる。
- **Props**: `serverInstances: ServerInstanceDTO[]`（RegisterPanel から渡す）
- **項目と現在値**: `useMonitoringItems(memoryAreasByProtocol, autoRefresh)` が持つ。自動更新が有効な間は 100ms ごとに読み直す
- **プロトコル別メモリエリア**: `memoryAreasByProtocol: Record<string, MemoryAreaDTO[]>` でプロトコルごとにエリア一覧をキャッシュ
- **項目追加ダイアログ（`AddMonitoringDialog`）**: 開始アドレスと個数を指定してまとめて追加できる。複数サーバー時はプロトコル選択セレクトを表示
- **行（`MonitoringRow`）**: ビット幅・エンディアン・表示形式を行ごとに変更できる。左端のつまみをドラッグして並べ替える（@dnd-kit 使用）
- **サーバー変化の検出**: `protocolTypesKey = serverInstances.map(i => i.protocolType).join(',')` でサーバー構成変化を効率的に検出してエリアを再取得
- **API 呼び出し**: `ReadWords(item.protocolType, item.memoryArea, ...)` など全 API に `protocolType` を渡す
- **画面のテスト**: `frontend/e2e/monitoring.spec.ts`

#### VariableView.tsx
`VariableView.tsx` は変数一覧と行の操作だけを持ち、各ダイアログは別ファイルのコンポーネントに分かれている（以下の説明は、ダイアログの分も含めた変数画面全体のもの）。
- **ダイアログの作り**: 各ダイアログはフォームの state を自分で持ち、`VariableView` に常にマウントされる。開くときは ref の `open()`（`MetaEditDialog` / `MappingDialog` は対象の変数、`ValueEditDialog` は `openRow(row)`）を呼ぶ。閉じているときは `null` を返す
  - 常にマウントしているのは、閉じても入力内容を保持するため（変数追加ダイアログはキャンセルしても入力が残る）
  - 親は `ref.current?.isOpen()` で開閉を調べ、ダイアログが開いている間は行のキーボード操作を止める
  - Esc で閉じる処理は各ダイアログが `useEscapeKey` で持つ
- **データ型を選ぶフォーム**: 入力欄は `DataTypeFields`、入力内容は `DataTypeFormValue`（`variableTypes.ts`）で共通。型の文字列との変換は `buildDataTypeFromForm()` / `parseDataTypeToForm()` を使い、ダイアログごとに組み立てない
  - ダイアログごとの違いは props で渡す: 変数追加は「初期値」の行（`scalarExtra`）、構造体型のフィールドは短い文言（`compact`）と、編集中の型自身を除いた構造体型の一覧
- **共有するデータ**: `dataTypes` / `structTypes` / `variables` / `serverInstances` / `memoryAreasByProtocol` は `VariableView` が読み込み、props で渡す。保存後の再読み込みは `onChanged`（`loadVariables` / `loadStructTypes`）で行う
- **画面のテスト**: `frontend/e2e/variable-dialogs.spec.ts` が各ダイアログの動きを確認する。ダイアログを変更したら実行する
- **データ型サポート**: スカラー型・配列型・構造体型（詳細は「変数管理とデータ型システム」）
- **変数表示**: 構造体フィールドと配列要素をフラット化して行単位で表示
  - 展開/折りたたみ機能（構造体・配列のヘッダー行）
  - アドレスオフセット計算（フィールド・要素ごと）
- **行の操作**: 値のセルのクリックで値の編集、マッピングのセルのクリックでマッピング設定、右クリックメニューで編集・マッピング・削除
  - キーボード: 矢印キーで移動と展開、Enter で値の編集（見出し行は展開）、Insert で追加、F2 で編集、M でマッピング、Delete で削除
- **変数メタデータ編集**: 右クリックメニューの「編集」または F2 で、変数の名前・データタイプを変更可能
  - データタイプ変更時は値をデフォルト値にリセット（マッピングは保持）
  - OPC UA サーバーが起動中の場合はノード再構築のため `OnNodePublishingUpdated()` を呼び出す
  - バックエンド: `VariableStore.UpdateMetadata()` → `PLCService.UpdateVariable()` → `App.UpdateVariable()`
- **値編集ダイアログ**:
  - 再帰的エディタで複雑なデータ構造に対応
  - 構造体配列要素は折りたたみ可能（`StructArrayElementEditor`）
  - 数値入力は10進、16進（0x）、2進（0b）対応
- **プロトコルマッピング**: 変数を、追加済みの複数サーバーのメモリアドレスにマッピング可能
  - マッピングダイアログのプロトコル選択は `GetServerInstances()` で取得した追加済みサーバーから生成
  - メモリエリア選択は `memoryAreasByProtocol[m.protocolType]` で取得
  - 2行表示レイアウト（ヘッダ行 + コントロール行）
  - マッピング競合警告: 他の変数と同じレジスタを使用している場合、ダイアログ内と変数一覧の両方に警告を表示
  - **マッピング先**: `GetMappingTargets()` が返すサーバーとクライアントの接続先（`protocolType` は `client:<接続先の ID>`、選択肢は「接続先: 名前（プロトコル）」）。接続先のエリアは `GetMemoryAreas("client:<ID>")`。マッピング列には接続先の名前（`label`）で出す
  - **同期の状態**: 接続先にマッピングした変数があるときだけ、`GetVariableDeviceStatuses()` を 500ms ごとに読み、値の右に「通信エラー」「書き込み未反映」「マッピング不可」を出す（ホバーで理由）
  - **マッピング列の表示**: レジスタマッピング（`protocol:area:addr`、area は `MemoryArea.shortName`）とノード公開設定（`opcua(R/W)` 等）をカンマ区切りで両方表示
  - **NodeID の表示**: マッピングダイアログ・一括マッピング編集で、`GetNodeAddresses(protocolType, variableIds)` でプロトコルの実装から取得した値（`nodeAddresses` state）を表示する。フロントエンドで NodeID を組み立てない。`error` がある変数は理由を表示し、公開を有効にした保存を止める
  - **ダイアログを開く際に `loadServerInstancesAndAreas()` を再実行**: マウント後に追加されたサーバーも「プロトコル公開設定」セクションに反映
  - `loadServerInstancesAndAreas` は `useCallback` で定義し、マウント時・ダイアログオープン時の両方から呼び出す
- **一括マッピング編集**: ツールバーの「一括マッピング編集」ボタンで全変数のマッピングをテーブル形式で一括設定
  - プロトコルを選択すると全変数が一覧表示され、まとめて編集・保存できる
  - **レジスタ型プロトコル**（Modbus など）: メモリエリア / アドレス（空欄=そのプロトコルのマッピング削除） / エンディアンをテーブル表示
  - **ノード公開型プロトコル**（OPC UA など、`SupportsNodePublishing=true`）: 公開チェック / アクセスモード / NodeID をテーブル表示
  - アドレス重複検出: `findBulkRowConflicts()` で同じメモリエリア内のアドレス範囲重複を検出し、該当行をオレンジハイライト・入力欄のボーダーをオレンジに・⚠アイコン表示。重複件数バナーをダイアログ下部（スクロール外・ボタン上）に固定表示
  - 保存時に他プロトコルのマッピングを保持（`variable.mappings.filter(m => m.protocolType !== bulkProtocol)`）
- **OPC UA 公開設定表示**: `formatNodePublishings()` で有効な NodePublishing を `opcua(RO)` / `opcua(WO)` / `opcua(R/W)` 形式にフォーマットして変数一覧の「マッピング」列に追加表示
- **構造体型管理**: 構造体型の登録・編集・削除機能
  - 編集時は既存の定義を読み込んでフォームに展開（`parseDataTypeToForm()`）。型名は変更できない
  - スクロール対応（多数のフィールドを持つ構造体に対応）

#### ScriptPanel.tsx
JavaScript（goja）でPLC動作を記述。
- **構成**: 左にスクリプトの一覧（開始・停止ボタン）、右に選択したスクリプトの編集欄（`ScriptEditPanel`）、その下にコンソール
  - 選択は ID（`selectedId`）で持ち、表示するスクリプトは一覧から引く（実行中かどうかなどが古くならないようにするため）
  - 編集欄は入力中の内容を自分で持ち、`key` にスクリプトの ID を渡して選び直すたびに作り直す
- **コンソール**: スクリプトの出力はすべてここに出る（タイムスタンプ・スクリプト名付き、自動スクロール、クリアボタン）
  - `console.log()` の出力、警告（`[WARN]`、黄色）、エラー（`[ERROR]`、赤）。種類は `ConsoleLogDTO.level`（`log` / `warn` / `error`）
  - スクリプトごとの「最後のエラー」の表示はない（エラーはコンソールで見る）
  - Wails イベント `plc:console-log-added` で 1 件ずつ受け取る（ポーリングしない）
- **1 回実行**: 編集欄の「1 回実行」ボタンで、入力中のコードを保存せずに 1 回だけ実行する（`RunScriptOnce`）。結果（`=> 値` または `完了`）とエラーは、スクリプト名「1 回実行」でコンソールに出る
- **操作のエラー**: 保存・削除・開始・停止に失敗したときは、コンソールの上にエラーを表示する（次の操作が成功すると消える）
- **画面のテスト**: `frontend/e2e/script-panel.spec.ts`
- **const/let対応**: スクリプトコードをIIFEでラップして再宣言エラーを回避
- **plcオブジェクト**:
  - メモリアクセス: `plc.readBit()`, `plc.writeBit()`, `plc.readWord()`, `plc.writeWord()`
  - 変数アクセス: `plc.readVariable()`, `plc.writeVariable()`, `plc.readArrayElement()`, `plc.writeArrayElement()`, `plc.readStructField()`, `plc.writeStructField()`
  - LINT/ULINT BigInt API: `plc.readLintBig(name)`, `plc.writeLintBig(name, val)`, `plc.readUlintBig(name)`, `plc.writeUlintBig(name, val)`（JavaScriptのBigInt型で64ビット整数を精度損失なく読み書き。例: `plc.writeLintBig("myVar", plc.readLintBig("myVar") + 1n)`）
    - `plc.readVariable()` でLINT/ULINT値が±2^53を超えた場合は `[WARN]` をコンソールに出力して `readLintBig()`/`readUlintBig()` の使用を促す
  - TIME/DATE シンタックスシュガー: `plc.readTimeMs()`, `plc.writeTimeMs()`, `plc.readDateSec()`, `plc.writeDateSec()`, `plc.readTimeOfDayMs()`, `plc.writeTimeOfDayMs()`, `plc.readDateAndTimeSec()`, `plc.writeDateAndTimeSec()`（変数の読み取り・パース・フォーマット・書き込みをワンステップで実行）
  - TIME/DATE 文字列変換: `plc.parseTime()`, `plc.formatTime()`, `plc.parseDate()`, `plc.formatDate()`, `plc.parseTimeOfDay()`, `plc.formatTimeOfDay()`, `plc.parseDateAndTime()`, `plc.formatDateAndTime()`
  - 外部の機器との同期の状態: `plc.getVariableStatus(name)`（クライアントの接続先にマッピングした変数。`{ state, writePending, error, client }`、マッピングしていなければ `null`）
  - 外部の機器へ送る: `plc.sendCommand(client, command)`（保存したコマンドを、接続先とコマンドの名前で指定）、`plc.sendRequest(client, hex)`（16 進で指定）。応答かタイムアウトまで待って `{ status, error, values: [{ label, value, word?, bit? }], sent, received, elapsedMs, summary, label }` を返す。見つからない・16 進が読めないときも例外にせず `status: "error"`。名前が重なれば追加した順で最初のもの。履歴の `label` は「スクリプト「名前」: コマンド名」（実装は `plc_service_client_script.go` の `scriptClientSender`、エンジンへは `ScriptEngine.SetClientSender()` で渡す）

### Wailsバインディング

`app.go`でフロントエンドに公開するメソッドを定義。`wails generate module`でTypeScript型定義を自動生成。

主要API:
- **サーバー管理**:
  - `GetServerInstances()`: 全サーバーインスタンスの一覧（protocolType, displayName, variant, status）を取得
  - `AddServer(protocolType, variantID)`: 新しいサーバーインスタンスを追加
  - `RemoveServer(protocolType)`: サーバーインスタンスを削除
  - `StartServer(protocolType)`, `StopServer(protocolType)`: サーバーの起動/停止
  - `GetServerStatus(protocolType)`: サーバーのステータス取得
  - `GetProtocolSchema(protocolType)`: プロトコルのスキーマ（バリアント、フィールド定義）を取得
  - `GetServerConfig(protocolType)`: 特定サーバーの設定を取得
  - `UpdateServerConfig(dto)`: サーバー設定を更新（`ServerConfigDTO.protocolType` で対象を指定）
  - `GetAvailableProtocols()`: 追加可能なプロトコル一覧を取得
  - `GetNodeAddresses(protocolType, variableIDs)`: 公開変数のノードアドレス（OPC UA なら NodeID）と、公開できない場合の理由を取得
  - `GetSerialPorts()`: 利用可能なシリアルポート一覧を取得（設定画面の選択肢）
- **UnitID 設定**:
  - `GetUnitIDSettings(protocolType)`, `SetUnitIDEnabled(protocolType, unitId, enabled)`: UnitID 応答設定
  - `GetDisabledUnitIDs(protocolType)`, `SetDisabledUnitIDs(protocolType, ids)`: 無効 UnitID の一括管理
- **メモリ操作**（全て `protocolType string` を第1引数に取る）:
  - `GetMemoryAreas(protocolType)`: メモリエリア一覧を取得
  - `ReadBits(protocolType, area, address, count)`, `WriteBit(protocolType, area, address, value)`
  - `ReadWords(protocolType, area, address, count)`, `WriteWord(protocolType, area, address, value)`
- **モニタリング**:
  - `GetMonitoringItems()`, `AddMonitoringItem()`, `UpdateMonitoringItem()`, `DeleteMonitoringItem()`, `MoveMonitoringItem()`, `ReorderMonitoringItem()`, `ClearMonitoringItems()`
- **変数管理**:
  - `GetVariables()`, `CreateVariable()`, `UpdateVariableValue()`, `DeleteVariable()`: 変数CRUD操作
  - `UpdateVariable(id, name, dataType)`: 変数の名前・データ型を変更
  - `UpdateVariableFieldValue(id, fieldPath, value)`: 構造体フィールド・配列要素を部分更新（`fieldPath` は `.field` / `[添字]` の連結。添字は外部インデックス）
  - `GetDataTypes()`: サポートされているデータ型一覧を取得
  - `GetStructTypes()`, `RegisterStructType()`, `DeleteStructType()`: 構造体型管理
  - `GetVariableMappings()`, `UpdateVariableMappings()`: 変数のプロトコルマッピング設定
  - `UpdateVariableNodePublishing(variableID, protocolType, dto)`: ノード公開設定（公開するか・アクセスモード）
- **スクリプト**:
  - `GetScripts()`, `GetScript()`, `CreateScript()`, `UpdateScript()`, `DeleteScript()`: スクリプトCRUD操作
  - `StartScript()`, `StopScript()`, `RunScriptOnce(code)`: スクリプト実行制御（`RunScriptOnce` は、渡したコードを保存せずに 1 回だけ実行する）
  - `GetIntervalPresets()`: 実行周期の選択肢
  - `GetConsoleLogs()`, `ClearConsoleLogs()`: コンソールログの取得・クリア
- **プロジェクト**:
  - `ExportProject()`, `ImportProject()`: プロジェクトのファイルへの保存・読み込み
- **クライアント**（外部の機器へつなぐ側）:
  - `GetClientProtocols()`: クライアントとして使えるプロトコル（接続先の設定項目・コマンドの定義）
  - `GetClients()`, `AddClient(protocolType, name)`, `UpdateClient(id, name, settings)`, `RemoveClient(id)`, `DisconnectClient(id)`: 接続先の管理
  - `BuildClientRequest(protocolType, commandID, params)`: コマンドと項目の値からリクエストを組み立て、16 進の文字列で返す
  - `SendClientRequest(id, requestHex)`: 16 進で指定したリクエストを送る。タイムアウト・例外応答などは結果の `status`（`ok` / `exception` / `timeout` / `error`）で返す
  - `GetClientCommandLogs()`, `ClearClientCommandLogs()`: 送ったコマンドの履歴（新しい順、最大 200 件）
  - 保存したコマンド: `GetClientSavedCommands(id)`, `AddClientSavedCommand(id, input)`, `UpdateClientSavedCommand(id, commandID, input)`, `RenameClientSavedCommand(id, commandID, name)`, `RemoveClientSavedCommand(id, commandID)`, `SendClientSavedCommand(id, commandID)`（`input` は `ClientSavedCommandInputDTO`: `mode` が `build` なら `commandId` / `params`、`hex` なら `request`）
  - `GetClientPolling(id)`: ポーリングの状態（周期・実行中か・周回数・読み取り項目と最後の結果）
  - `AddClientPollItem(id, commandID, params, format)`, `UpdateClientPollItem(id, itemID, commandID, params, format)`, `RemoveClientPollItem(id, itemID)`: 読み取り項目（`Pollable` のコマンドだけ。1 つの接続先に 50 個まで）
  - `SetClientPollInterval(id, ms)`（50〜3600000）, `StartClientPolling(id)`, `StopClientPolling(id)`
  - `SetClientFailThreshold(id, n)`（1〜100、既定 3）: 読み取りが続けて何回失敗したら、マッピングした変数を「通信エラー」にするか
  - `GetClientSubscription(id)`: サブスクリプションの状態（間隔・監視中か・全体の状態・監視項目と最後に届いた値）
  - `AddClientSubscriptionItems(id, params)`（1 回の入力から、いくつも足されることがある）, `RemoveClientSubscriptionItem(id, itemID)`, `SetClientSubscriptionInterval(id, ms)`, `StartClientSubscription(id)`, `StopClientSubscription(id)`
  - `GetMappingTargets()`: 変数をマッピングできる先（サーバーと、マッピングできる接続先）
  - `GetVariableDeviceStatuses()`: 接続先にマッピングした変数の同期の状態（`state`: `""`（まだ読めていない）/ `ok` / `comm_error` / `invalid` / `stopped`、`writePending`、`error`）
- **HTTP API 設定**:
  - `GetHTTPAPIPort()`: 現在のHTTP APIポート番号を返す
  - `SetHTTPAPIPort(port)`: ポートを変更してHTTP APIサーバーを再起動（1024〜65535、設定ファイルに永続化）

### 変数管理とデータ型システム

#### IEC 61131-3準拠のデータ型

変数は以下のデータ型をサポート：

1. **スカラー型**:
   - 整数: SINT(8bit), INT(16bit), DINT(32bit), LINT(64bit), USINT, UINT, UDINT, ULINT
   - 浮動小数点: REAL(32bit), LREAL(64bit)
   - 論理: BOOL
   - 文字列: STRING[n] (固定長、nはバイト数)
   - 時間・日付: TIME, DATE, TIME_OF_DAY, DATE_AND_TIME
     - TIME: 時間間隔（"T#1h30m45s" など、内部はミリ秒int32で2ワード保存）
     - DATE: 日付（"D#2024-01-01" など、内部はその日の0時0分0秒のUnix秒uint64で4ワード保存）
     - TIME_OF_DAY: 1日の中の時刻（"TOD#12:30:15" など、内部はミリ秒uint32で2ワード保存）
     - DATE_AND_TIME: 日付と時刻（"DT#2024-01-01-12:30:15" など、内部はUnix秒uint64で4ワード保存）

2. **配列型**: `ARRAY[開始..終了] OF 要素型`（IEC 61131-3 形式）
   - 例: `ARRAY[0..9] OF INT`, `ARRAY[1..5] OF MyStruct`
   - 多次元配列は `ARRAY[0..2, 0..4] OF INT`（最大 3 次元まで画面から作れる）
   - 値は入れ子の配列で持つ。画面の表示とフィールドパスの添字は外部インデックス（`ARRAY[1..3]` の先頭は `[1]`）
   - 旧形式（`ARRAY[INT;10]`、`ARRAY[ARRAY[INT;5];3]`）も読める（開始インデックスは 0）。画面からは新しい形式で作る

3. **構造体型**: カスタム定義可能
   - フィールドはスカラー、配列、構造体のいずれか
   - 再帰的なネストに対応
   - ワードオフセットは自動計算

#### 変数の内部構造

- **フラット化表示**: 構造体フィールドと配列要素を行単位で展開
- **アドレスオフセット**: 各フィールド・要素のワードオフセットを計算して表示
- **展開/折りたたみ**: 構造体・配列のヘッダー行で子要素の表示を制御
- **再帰的編集**: ネストされたデータ構造を再帰的に編集可能

#### プロトコルマッピング

変数を複数プロトコルのメモリアドレスにマッピング可能：
- 各マッピングは `protocolType`, `memoryArea`, `address`, `endianness` を指定
- フィールド・要素のアドレスは自動計算（ベースアドレス + オフセット）
- エンディアンは変数ごとに設定可能（big/little）
- **競合警告**: 複数の変数が同じレジスタに割り当てられた場合、ワーニングを表示（保存は可能）
  - ダイアログ内: 各マッピング行の下に `.mapping-conflict-warning` で警告メッセージ
  - 変数一覧: マッピング列の先頭に `.mapping-conflict-icon` で ⚠ アイコン（ホバーでツールチップ）
  - 同一レジスタに複数変数をマッピングした場合の動作は未定義（last-writer-wins、読み取りは非決定的）

### 主要 DTO

`internal/application/dto.go` に定義:
- **`ServerInstanceDTO`**: サーバーインスタンス一覧表示用（`protocolType`, `displayName`, `variant`, `status`）
- **`ServerConfigDTO`**: サーバー設定の取得/更新用（`protocolType`, `variant`, `settings`）
- **`ServerSnapshotDTO`**: Export/Import 用の単一サーバースナップショット
- **`MonitoringItemDTO`**: `protocolType` フィールドを含む（どのサーバーのアドレスかを示す）
- **`ProjectDataDTO`**: `Servers []ServerSnapshotDTO` にマルチサーバー構成を保存。レジスタのスナップショットは含まない（バージョンフィールドなし）

### 設定ファイル

アプリケーションの設定は以下の場所に保存されます：
- 設定フォルダは `appconfig.Dir()` / `appconfig.Path(name)`（`internal/infrastructure/appconfig`）で決める。フォルダ名を直接書かない
  - 設定フォルダがまだなく旧名のフォルダ（`%APPDATA%\PLCSimulator`）がある場合は、初回だけ直下のファイルをコピーして引き継ぐ（旧フォルダは残す）
  - 環境変数 `KARAKURI_CONFIG_DIR` で保存先を差し替えられる。`internal/application` のテストは `TestMain` で一時フォルダに差し替え、実際の設定を書き換えない
- **モニタリング設定**: `%APPDATA%\Karakuri\monitoring_config.json`
  - 登録したモニタリング項目（プロトコルタイプ、メモリエリア、アドレス、ビット幅、エンディアン、表示形式）
- **HTTP API 設定**: `%APPDATA%\Karakuri\httpapi_config.json`
  - `{"port": 8765}` 形式で保存。UI または `SetHTTPAPIPort()` で変更すると自動更新

## テストの構成

- **データ競合を作らない**: テストは CI で競合検出（`-race`）を有効にして実行される。ゴルーチンに渡す値は、あとで書き換えられるオブジェクトのポインタではなく、その時点の値のコピーにする
  - 変数ストア（`VariableStore`）が返す変数と、リスナーに渡す変数は、その時点の内容のコピー。ストアの中の変数は、ロックの中でしか読み書きしない。値（配列・構造体）は書き換えず、更新のたびに新しい値に差し替える
  - 構造体型の定義を引く `TypeResolver` は、ストアの外では `VariableStore.TypeResolver()`（引くたびにロックを取る）を使う。`VariableStore` 自身は `TypeResolver` を実装しない。ストアのメソッドの中（ロック済み）では `s.unlocked()` を使う（`sync.RWMutex` は同じゴルーチンから重ねて取れないため）

- **ホスト側のテストはフェイクのプロトコルで動かす**: `protocoltest.NewFactory()` を `PLCService.RegisterFactory()` に登録する。`internal/application` と `internal/infrastructure` のテストは、実際のプロトコルの実装を使わない
- **REST API**（`internal/infrastructure/httpapi/server_test.go`）: `httptest` でハンドラーを直接呼ぶ。エンドポイントを追加・変更したらここに足す
- **変数とレジスタの同期**（`internal/infrastructure/adapter/variable_datastore_test.go`）: 複数の変数にまたがる書き込み、同時の書き込み、行き来しないこと
- **組み込みの構成での確認**（`internal/protocols/integration_test.go`）: アプリと同じように `protocols.Factories()` を登録した `PLCService` に、実際の Modbus TCP / OPC UA のクライアントからつなぐ。サーバーの起動・停止をまたいだ状態（レジスタ、UnitID の設定）、変数との同期、プロジェクトの読み込みを確認する。**プロトコルを足したら、ここにそのプロトコルの確認を足す**
- **Modbus**（`internal/protocols/modbus/`）: `tcp_transport_test.go` は実際に TCP でつないで確認する。`rtu/rtu_test.go` は仕様書の既知のフレームで CRC / LRC を確認し、同じリクエストを RTU と ASCII の両方で処理して結果が一致することを確認する
- **OPC UA**（`internal/protocols/opcua/`）: 実際の OPC UA クライアントでつなぐ。`host_integration_test.go` は実際の変数ストアを使う（他のテストはフェイクの変数ストアを使うため、変数ストアが持つ値の型や型変換を通らない）
- 設定フォルダに書き込むパッケージのテストは、`TestMain` で `KARAKURI_CONFIG_DIR` を一時フォルダに向ける

## 新プロトコル追加手順

プロトコル固有のコード（プロトコルタイプの値、設定、メモリエリア、アドレス書式など）はすべて `internal/protocols/<name>/` に置く。ホスト（`internal/application`, `internal/infrastructure`, `app.go`）とフロントエンドはプロトコル名や固有の値を持たず、能力フラグ（`ProtocolCapabilities`）・スキーマ（`GetConfigFields()`）・`MemoryArea` の属性・`NodeAddressResolver` だけで動く。

1. `internal/protocols/<name>/` を作成し、以下を実装:
   - `factory.go`: `ServerFactory`（プロトコルタイプの定数もここで定義する）
   - `server.go`: `ProtocolServer`（ノード公開型なら `NodePublishingAware` / `NodeAddressResolver` も。UnitID を持つなら `UnitIDConfigurable` も）
   - `datastore.go`: `DataStore`（レジスタを持たないなら空の実装）
2. `internal/protocols/protocols.go` の `Factories()` に追加する
3. `internal/protocols/integration_test.go` に、実際のクライアントからつなぐ確認を足す
4. **ホスト・フロントエンドの変更不要** - スキーマと能力フラグから自動生成
5. クライアント（外部の機器へつなぐ側）にも対応するなら、`protocol.ClientFactory` / `protocol.Client`（`internal/domain/protocol/client.go`）を実装し、`protocols.ClientFactories()` に追加する
   - 接続先の設定項目（`ConnectionFields()`）とコマンドの定義（`Commands()`）は `ConfigField` で返す。画面はサーバー設定と同じ `DynamicField` で入力欄を作る
   - 読み取りのコマンドには `Pollable: true` を付ける（ポーリングに使える）。読み取った値をビット・ワードで返すなら `ResultValue.Bit` / `Word` にも入れ、コマンドに `WordValues: true` を付ける（画面で 10 進・16 進などの表示形式を選べる。付けなければ `Value` の文字列をそのまま出す）
   - 16 進で直接入れる欄に出す説明と入力例は、`protocol.RequestHelpProvider`（`RequestHelp()`）で返す
   - `ConfigField.Type` に `"password"` を使うと、入力を隠す欄になる（閉じた設定の要約にも出さない）。`"textarea"` は複数行の欄
   - 値の変化の通知（サブスクリプション）に対応するなら、ファクトリーで `protocol.SubscriptionClientFactory`（監視項目の入力項目・入力から監視項目を作る）、クライアントで `protocol.SubscribingClient`（`RunSubscription()`）を実装する。`RunSubscription()` は ctx が取り消されるまで戻らず、つながらない・切れたときは状態を知らせてから自分でつなぎ直す。`onEvent` は同時に呼ばない
   - 変数をマッピングできるようにするなら `protocol.MappableClientFactory`（エリア・1 回で読み書きできる個数・読み書きのリクエスト）も実装する。機器の側で書けないエリアは `ReadOnly` にする
   - `Send()` は通信の失敗を error ではなく `CommandResult.Status` で返す。タイムアウトしたら接続を閉じる（遅れて届く応答と取り違えないため）。次の `Send()` でつなぎ直す

実装するときの決まり:

- サーバーのオブジェクトは、サーバーを削除するまで使い回される。`Start()` / `Stop()` は何度も呼ばれるので、`Stop()` のあとにもう一度 `Start()` できるようにする
- `CreateServer()` に渡される DataStore は、変数と同期する包み（`VariableBackedDataStore`）。クライアントの書き込みは、必ず渡された DataStore に書く（自分で作った DataStore に直接書くと、変数に反映されない）
- プロトコルの処理で panic すると、アプリ全体が落ちる（別プロセスではない）。外から来たデータは長さと範囲を確かめてから使う

## 重要な実装ポイント

### 1オリジンアドレス表示

- **設計方針**: プロトコル固有の知識（どのエリアが1オリジンか）はバックエンドが保持し、フロントエンドはビュー層に持たせない
- **`MemoryArea.OneOrigin`**: `GetAreas()` が返す `MemoryArea` 構造体のフィールド。true の場合、UIでのアドレス表示を1オリジンにする
- **内部値は常に0ベース**: API (ReadWords/WriteWord等) には0ベースのアドレスを渡す。表示・入力変換のみUIで行う
- **フロントエンドの変換**: `area.oneOrigin` を参照して表示時に+1、入力時に-1。ハードコードの判定リストは持たない

### Reactコンポーネントの状態管理

- **コンポーネントの定義位置**: 関数コンポーネント内で別のコンポーネントを定義すると、再レンダリング時に新しいコンポーネントとして扱われ、`useState`などの状態がリセットされる
  - 例: `StructArrayElementEditor`は`ValueEditDialog`の外（`StructArrayElementEditor.tsx`）で定義
  - 内部で使用する関数は props で渡す
- **キーの設定**: リスト描画時は安定した`key`を設定（インデックスだけでなく、パスベースのキーを使用）

### スクリプトエンジン

- **const/let対応**: goja VMで同じプログラムを周期的に実行すると再宣言エラーが発生するため、IIFEでラップ
- **コンソールログ**: 出力はすべて `ScriptEngine.addLog()` を通る。バッファ（最大500件）に入れ、コールバック（`SetOnLogAdded`）で通知する。`ConsoleLogEntry` は scriptID・scriptName・Level・Message・At を持つ
  - 画面は、開いたときに `GetConsoleLogs()` でバッファを読み、以後は Wails イベント `plc:console-log-added` で受け取る
  - `createVM(scriptID, scriptName string)` にスクリプト識別子を渡すことで、どのスクリプトの出力かを記録
- **エラー**: コンパイルエラー・実行時のエラー・panic は、`[ERROR]` としてコンソールログに出す（スクリプトごとに保存はしない）
  - 周期実行では、同じエラーが続く間は最初の 1 回だけ出す（毎周期出すとコンソールが埋まるため）。成功を挟んでまた起きたら出し直す。エラーが出ても実行は続ける
  - 実行中のスクリプトを保存して新しいコードがコンパイルできなかった場合、スクリプトは停止し、`UpdateScript()` はエラーを返す（変更は保存される）
- **1 回実行（`RunOnce`）**: scriptID="" / scriptName="1 回実行" で、結果とエラーをコンソールログに出す。そのままコンパイルできないコード（トップレベルの `return` など）は、周期実行と同じように関数で包んで実行する

### ダイアログスタイルの統一

すべてのダイアログ（変数編集、マッピング設定、構造体型管理、モニタリング追加、レジスタ書き込み）は統一されたスタイルパターンを使用：
- **`.dialog-row`**: ラベルとコントロールを横並びに配置
- **インラインスタイルを書かない**: 見た目は `App.css` のクラスで指定する（末尾の「コンポーネントのインラインスタイルから移したもの」に、画面ごとにまとめてある）。`style` で指定するのは、値が実行時に決まるものだけ（変数一覧・値の編集の字下げ、右クリックメニューの位置、モニタリングのドラッグ中の移動）
  - 既存のルール（`.dialog-row label`、`.variable-table th`、`.dialog .variable-table td input` など）より弱いセレクタでは効かない。クラスを足すときは、`.variable-table th.col-name` のように既存のルールより強いセレクタにする
  - 行の色をクラスで指定するときは、`:hover` のルール（`.variable-table tbody tr:hover`）に負けないよう、`:hover` も並べて書く
- **スクロール対応**: ダイアログ全体に `maxHeight: '80vh'` を設定し、コンテンツ領域で `overflowY: 'auto'`
- **flexboxレイアウト**: ダイアログ全体を `display: flex, flexDirection: 'column'` でレイアウト
- **テーブル内コンポーネント**: ダイアログ内の `variable-table` の `td` に含まれる `select` / `input` は `.dialog-row` が適用されないため、`App.css` の `.dialog .variable-table td select/input` セレクタで同等のスタイル（`padding: 0.5rem`, `border`, `border-radius`, `background`, `color`, `font-size`, `width: 100%`, `box-sizing: border-box`）を別途定義している

### 値編集ダイアログ

- **スクロール**: ダイアログ全体で1つのスクロール領域を使用（配列要素部分に独自スクロールを設定しない）
- **再帰的エディタ**: `renderValueEditor`（`ValueEditDialog.tsx`）は`depth`パラメータで再帰の深さを追跡し、インデントを調整
- **行単位の編集**: 行（`FlatRow`）は、値をたどるための位置（`valuePath`、0 始まり）と、バックエンドに渡すフィールドパス（`fieldPath`、外部インデックス）の両方を持つ。保存には `fieldPath` を使う

### マッピング競合検出

- **`findMappingConflicts(mapping)`** (`MappingDialog.tsx`): マッピングダイアログで各行の競合チェック。`getWordCount` でワード数を計算し、アドレス範囲の重複をチェック
- **`getVariableMappingConflicts(variable)`** (`VariableView.tsx`): 変数一覧テーブルで各変数の競合チェック。保存済みマッピングをすべてスキャン
- **`findBulkRowConflicts(rows, protocolType)`** (`BulkMappingDialog.tsx`): 一括マッピング編集ダイアログ内のアドレス重複を検出。`addressStr` が入力されている行の0ベースアドレス範囲（`addr0 〜 addr0+wordCount-1`）を計算し、同じメモリエリア内で重なるペアを返す。空欄行は対象外。競合バナーはスクロール外（`dialog-buttons` の直前）に固定表示し、テーブルが縦にスクロールしてもバナーが見切れないようにする
- **未定義動作の注意**: 同一レジスタへの複数変数マッピングはサポート対象外。書き込みは last-writer-wins、DataStore → Variable の同期は Go マップの反復順に依存して非決定的

### 構造体型管理ダイアログ

- **編集機能**: 既存の構造体型を編集可能（編集ボタンで定義を読み込み、更新で保存）。更新は「古い型を削除して登録し直す」動き
- **データ型解析**: フィールドのデータ型文字列を `parseDataTypeToForm()`（`variableTypes.ts`）でフォームの入力内容に変換
  - `ARRAY[0..9, 0..4] OF 型` → 配列カテゴリ + 要素型 + 次元数 + 各次元の開始・終了インデックス
  - `STRING[n]` → スカラーカテゴリ + STRING型 + バイト長
  - 構造体型名 → 構造体カテゴリ + 型名
- **自己参照の防止**: フィールドの型として、編集中の型自身は選べない
- **型名の不変性**: 編集モード時は型名の入力欄を無効化（型名は変更不可）

### OPC UA 実装

#### NodeID スキーム

| ノード種別 | NodeID 形式 | 例 |
|-----------|------------|---|
| ルート変数ノード | `ns=X;s={変数名}` | `ns=2;s=Motor1` |
| 構造体フィールド | `ns=X;s={変数名}.fieldName` | `ns=2;s=Motor1.speed` |
| ネストフィールド | `ns=X;s={変数名}.field1.field2` | `ns=2;s=Line1.motor.speed` |
| 配列要素 | `ns=X;s={変数名}[index]` | `ns=2;s=Values[0]` |
| 公開変数フォルダ | `ns=X;s=PLCVariables` | Objects 直下（Organizes） |

- 名前空間 URI は設定の「ネームスペース」（既定 `urn:karakuri:plc`、通常 `ns=2`）。ns 番号は起動時に `AddressSpace.RegisterNamespace()` が割り当てるため固定値を使わない
- UI に表示する NodeID は `Server.NodeAddresses()`（`NodeAddressResolver`）が返す。起動中は `ns=N;s={変数名}`、停止中は番号が決まらないため `ns=???;s={変数名}`
  - 経路: フロントエンド → `App.GetNodeAddresses()` → `PLCService.GetNodeAddresses()` → `opcua.Server.NodeAddresses()`
- NodeID は変数名から作る（変数名はストア内で一意）。変数名を変えると NodeID も変わり、`rebuild()` が古いノードを消して新しい名前で作り直す。値の読み書きは内部で変数 ID を使う
  - NodeID が他のノードと重なる変数名は公開できない: フォルダと同じ `PLCVariables`、`.` `[` `]` を含む名前（他の変数の子ノード `A.b`、`A[0]` と区別できない）。判定は `publishNameError()`（`nodes.go`）
  - `NodeAddresses()` が該当する変数に `NodeAddress.Error`（理由）を付けて返す。ホストは `UpdateVariableNodePublishing()`（公開を有効にする保存）と `UpdateVariable()`（公開中の変数の改名）を拒否し、UI はマッピングダイアログ・一括マッピング編集で NodeID の代わりに理由を赤字で表示して保存を止める
  - インポートした設定などに残っていた場合、`rebuild()` はその変数を公開せずログに出す
- 配列要素の index は外部インデックス（`ARRAY[1..10]` なら `[1]`〜`[10]`）

#### 型の対応

| PLC 型 | OPC UA |
|--------|--------|
| BOOL / SINT / INT / DINT / LINT | Boolean / SByte / Int16 / Int32 / Int64 |
| USINT / UINT / UDINT / ULINT | Byte / UInt16 / UInt32 / UInt64 |
| REAL / LREAL | Float / Double |
| STRING, STRING[n], TIME, DATE, TIME_OF_DAY, DATE_AND_TIME | String（TIME 等は `T#1s` 形式の文字列） |
| スカラーの配列 | 要素型の 1 次元配列（ValueRank=1、ArrayDimensions=[要素数]） |
| 多次元配列（`ARRAY[0..1, 0..2, 0..3] OF INT`、旧形式 `ARRAY[ARRAY[INT;5];3]` も同じ） | 要素型の Matrix（ValueRank=次元数、ArrayDimensions=各次元の要素数）。値は行優先（最後の添字が最も速く変わる） |
| 構造体 | String（JSON）。フィールドは子ノードで型付きアクセス |
| 構造体の配列（多次元を含む） | String の 1 次元配列 / Matrix（各要素が JSON） |

- 多次元配列の子ノードは 1 次元ずつ減る（`{変数名}[1]` は 2 次元、`{変数名}[1][2]` は 1 次元、`{変数名}[1][2][3]` はスカラー）
- `shapeOf()`（`types.go`）が PLC 型から公開形（組み込み型・次元・JSON かどうか）を決める。`toVariant()` は入れ子の `[]interface{}` を平らにして `ua.NewMatrixVariant()` で Matrix にし、`fromVariant()` は `Variant.Dimensions()` に沿って入れ子に戻す
- 次元数の違う書き込み（3 次元のノードに 1 次元配列など）は uastack が ValueRank の検査で拒否する

- 書き込みは uastack が DataType / ValueRank を検査する（INT ノードには Int16 で書く必要がある）
- 構造体ノードへの書き込みは JSON 文字列を `map[string]interface{}` に変換してホストへ送る
- アクセスモード: `read` → CurrentRead、`write` → CurrentWrite、`readwrite` → 両方

#### 値の同期

- 変数 → OPC UA: accessor の `SubscribeVariableChanges` で変更された変数 ID を受け取り、その変数を読み直してルートと全子ノードに `SetValue()`。取りこぼし対策として 1 秒ごとに全変数を再同期
- OPC UA → 変数: `Node.OnWrite` → `WriteVariableValue`（ルート）/ `WriteVariableFieldValue`（子ノード、フィールドパスは外部インデックス）。書き込み後に同じ変数の他ノードも更新
- 変数ストアの値は Go の型をそのまま持つ（INT は `int16`、UINT は `uint16`、配列は `[]interface{}`、構造体は `map[string]interface{}`）。`toVariant()` は `int64` / `float64` などの広い型も受け付ける（テストのフェイクが使う）
- この経路全体（OPC UA クライアント → サーバー → 変数ストア）は `internal/protocols/opcua/host_integration_test.go` で確認する

#### サーバー設定

`GetConfigFields()` が返す設定項目。カテゴリごとにサーバー設定画面に表示される。

| カテゴリ | キー | 項目 | 既定値 | 反映先 |
|---------|------|------|--------|--------|
| ネットワーク | `host` | アドレス | `0.0.0.0` | 待ち受けアドレス |
| | `port` | ポート番号 | `4840` | 待ち受けポート |
| | `endpointHost` | エンドポイントのホスト名 | （空 = PC のホスト名） | エンドポイント URL |
| アプリケーション情報 | `application_name` | アプリケーション名 | `Karakuri PLC Simulator` | ApplicationDescription.ApplicationName、BuildInfo.ProductName |
| | `manufacture_name` | メーカー名 | `karakuri` | BuildInfo.ManufacturerName |
| | `product_uri` | プロダクト URI | `urn:karakuri` | ApplicationDescription.ProductUri、BuildInfo.ProductUri |
| | `namespace` | ネームスペース | `urn:karakuri:plc` | 公開変数ノードの名前空間 URI |

- アプリケーション情報のキー名は旧 C# 版 [opcua-plugin](https://github.com/bamchoh/opcua-plugin) と同じ（`manufacture_name` の綴りも同じ）
- `MapToConfig()` はマップにないキーを既定値にする（項目追加前に保存された設定も読める）。文字列は前後の空白を除く
- 検証: アプリケーション情報はすべて必須。ネームスペースに `http://opcfoundation.org/UA/`（名前空間 0）は使えない

#### OPC UA ライブラリ

- **bamchoh/uastack v0.1.2**（サーバーは `github.com/bamchoh/uastack/server`、クライアントは `github.com/bamchoh/uastack/client`）
- v0.1.2 より前は、自動のつなぎ直し（`AutoReconnect`）でセッションを作り直すと keep-alive とデータ競合になる（`go test -race` が落ちる）。サブスクリプションが `AutoReconnect` を使うので、v0.1.2 以上にする
- セキュリティなし・匿名認証のみ（証明書は作られない。`PKIDir` はアプリの exe と同じフォルダの `pki`）
- `Start()` で `net.Listen` してから `server.Serve(ln)` をゴルーチンで実行（ポート使用中を Start のエラーで返すため）
- `Stop()` は `server.Close()` を呼んで `Serve` の終了を待つ（v0.1.1 以降は Serve の開始前に Close しても Serve が終了する）
- `server.Server` は Close 後に再利用できないため、Start のたびに作り直す

### クライアント（外部の機器へつなぐ側）

- **作り**: `PLCService` の `clients`（`clientManager`、`plc_service_clients.go`）が接続先・ファクトリー・履歴を持つ。通信を待つ間に `PLCService.mu` を持たないよう、自分のロックを使う。接続を閉じる処理（送信中なら終わるまで待つ）は、そのロックの外で呼ぶ
- **接続先**: いくつでも追加できる（サーバーと違い、プロトコルごとに 1 つではない）。設定を変えるとクライアントを作り直す。プロジェクトに保存する（`ProjectDataDTO.Clients`）
- **送信の流れ**: 画面は、組み立てる方式でも `BuildClientRequest()` で 16 進にしてから `SendClientRequest()` で送る（送る経路は 1 つ）
- **保存したコマンド**（`plc_service_client_commands.go`）: 接続先ごとに、組み立てたコマンドを名前を付けて保存しておき、あとから送る（グループはない。最初はグループで作ったが、保存して使うまでの手順が多いのでやめた）
  - 保存するとき、組み立てたもの（`mode: "build"`、コマンドの ID と項目の値）か 16 進で入れたもの（`mode: "hex"`）から、送るバイト列を作っておく。送るときはそれを送る（`SendClientSavedCommand()`、履歴の `label` にコマンドの名前）
  - 名前が空なら、内容の要約を名前にする。1 つの接続先に 200 個まで
  - 画面: 「コマンド」タブには「保存したコマンド」の一覧と「結果と履歴」だけを並べる。組み立ては `ClientCommandDialog` で行う
    - 「新しいコマンド」で空のダイアログ、一覧の ✎ で保存したコマンドの中身と名前が入ったダイアログを開く（✎ のときの保存は上書き）
    - ダイアログのボタンは「閉じる」「送信」「保存して閉じる」。「送信」はダイアログを開いたまま送り、結果をダイアログの下に出す（保存せずに試せる。結果は画面の履歴にも残る）
    - ダイアログの中の誤り（16 進が読めないなど）は、ダイアログの中に出す。Enter で送信、Esc で閉じる（`FocusTrap` は開いた直後 100ms の Enter を無視するので、e2e で開いてすぐ Enter を押すと送られない。16 進の欄は自分で Enter を受けて送る）
  - プロジェクトに保存する（`ClientSnapshotDTO.SavedCommands`。ID は読み込むときに作り直し、使えないコマンドは読み飛ばす）
- **Modbus TCP**（`internal/protocols/modbus/client.go`）
  - リクエストは PDU（ファンクションコード + データ）。MBAP ヘッダー（トランザクション ID・UnitID）はクライアントが付ける。アドレスは PDU の値（0 始まり）
  - 組み立てられるコマンドは FC01〜06・15・16。16 進で入れれば、ほかのファンクションコードや異常なリクエストも送れる（応答はデータをそのまま表示する）
  - 接続は最初の送信でつなぐ。タイムアウト・通信エラーでは閉じ、例外応答では閉じない。トランザクション ID が違う応答（前のリクエストへの遅れた応答）は読み捨てる
- **OPC UA**（`internal/protocols/opcua/client.go` / `client_conn.go`、bamchoh/uastack の `client` を使用）
  - リクエストは「サービスのリクエスト本体」の UA Binary（型の NodeId + 構造体。`encodeMessage()` / `decodeRequest()`）。セキュアチャネル・セッション・リクエストヘッダー（認証トークン・ハンドル・時刻）はクライアントが受け持つ。結果の送信・受信のバイト列も同じ形（リクエスト本体・応答本体）で、チャネルのヘッダーや暗号化は含まない
  - 組み立てられるコマンドは Read（NodeId と属性。ポーリングに使える）・Write（NodeId・データ型・スカラー / 配列・値）・Write（複数のノード）・Browse（子ノードの一覧）。16 進で入れれば、ほかのサービスのリクエストも送れる（解釈を持たない応答は、応答の型の名前だけ出す）。リクエストとして読めないバイト列は、送らずにエラーにする
  - **複数のノード**: Read の NodeId は 1 行に 1 つずつ入れられる（1 回のリクエストでまとめて読む。属性は共通）。複数の書き込み（`writeMulti`）は 1 行に 1 つ「NodeId := 値」で入れる。型は欄の「データ型」が既定で、違う型のノードは「NodeId : 型 := 値」、配列は「NodeId : 型[] := 値, 値」（`parseWriteLine()`。NodeId の中の `:` と区別するため、最後の `:` のあとが型の名前のときだけ型の指定として読む）
    - 複数のノードの結果は、行のラベルに NodeId を付ける。1 つでも Bad なら全体を `exception` にする（読めた・書けたノードの結果も出す。書き込みは、書けたノードには書かれている）
  - NodeId は `ns=2;s=Motor1` の形の文字列で入れる（ns= の番号はサーバーごとに決まる。クライアントは名前空間 URI から引かない）
  - 結果の種類: サーバーがサービスやノードごとの結果を Bad のステータスにしたもの（`BadNodeIdUnknown`・`BadTypeMismatch` など）は `exception`（接続は閉じない）。タイムアウト・通信エラーでは接続を閉じ、次の送信でセッションを作り直す。セッションが無効になったステータス（`BadSessionIdInvalid` など）でも閉じる
  - 読み取った値は型付きなので `ResultValue.Value` の文字列だけで返す（`Bit` / `Word` は入れない。ポーリングの表示形式の選択は出ない）。スカラーは「Value（Int16）」の 1 行、配列は型の行と要素ごとの行（1000 行まで）
  - 接続先の設定: エンドポイント URL・セキュリティポリシー（None / Basic256Sha256 / Aes128_Sha256_RsaOaep / Aes256_Sha256_RsaPss / ECC_nistP256 / ECC_nistP384）・セキュリティモード・ユーザー名とパスワード（空なら匿名）・タイムアウト。パスワードはプロジェクトにそのまま保存する
  - None 以外のポリシーでは、クライアントの証明書を exe と同じフォルダの `pki-client` に作る（サーバーの `pki` とは分ける）。サーバーの証明書は確かめずに受け入れる（`AutoAcceptUntrustedCertificates`）
  - **サブスクリプション**（`client_subscription.go`）: 監視項目 1 つが、1 つのノードの Value 属性の MonitoredItem（サンプリング間隔はサブスクリプションの間隔と同じ、キューは 1）。NodeId を 1 行に 1 つ入れると、行の数だけ監視項目ができる
    - コマンド（`Send`）とは別のセッションで動かす（コマンドのタイムアウトで接続を閉じても、監視が切れないようにするため）。`Connected()` は、どちらかがつながっていれば true
    - 接続が切れたら uastack がつなぎ直し（`AutoReconnect`）、サブスクリプションと MonitoredItem を作り直す。それでも続けられないとき（サーバーがサブスクリプションを捨てたなど）と、最初からつながらないときは、1〜5 秒おきに接続から作り直す
    - サーバーにないノードなど、サーバーが断った項目はその項目だけ `exception` にする（ほかの項目は監視できる）
    - 値の行は Read と同じ（`variantRows()`）。配列は型の行と要素ごとの行（`[2]`）、多次元配列は `[1][0]` の形
  - 変数とのマッピングには対応していない（`MappableClientFactory` はレジスタのエリアとアドレスが前提。ノードへのマッピングは別の仕組みが要る）
- **サブスクリプション**（`plc_service_client_subscription.go`）: 対応するプロトコル（`protocol.SubscriptionClientFactory`。いまは OPC UA だけ）の接続先だけが使える。接続先ごとに監視項目の一覧（200 個まで）と通知の間隔（50〜3600000 ms、既定 1000）を持ち、監視中は 1 つのゴルーチンが `RunSubscription()` を動かして、知らせ（値・状態）を項目に入れる
  - ポーリングとの違い: こちらから周期的に読まない。機器の側が値の変化を知らせる。コマンドの履歴にも入れない
  - 監視全体の状態（`state`）: `connecting`（つなごうとしている）/ `ok`（監視中）/ `error`（つながっていない。プロトコルの実装がつなぎ直している。理由は `error`）。停止中は空
  - 項目は、最後に届いた値を残す（切れても・止めても残す）。画面は、監視できていない間は値を薄く表示する
  - 監視中に項目の追加・削除、間隔の変更、接続先の設定の変更をしたら、監視を作り直す（止めて、始め直す。項目の状態は「まだ届いていない」に戻る）。最後の項目を消したら止まる。作り直す前の監視からの知らせは捨てる（`recordSubscriptionEvent()` が、いまの監視かどうかを確かめる）
  - 止めるときはポーリングと同じ（ロックの中で取り消しだけして、終わりはロックの外で待つ）。接続先の削除・プロジェクトの読み込み・終了でも止める
  - 監視項目と間隔はプロジェクトに保存する（`ClientSnapshotDTO.Subscription`）。監視中かどうかは保存しない
  - 画面は、通信画面の右側の 3 つ目のタブ（`ClientSubscriptionView`）。対応していないプロトコルの接続先にはタブを出さない
- **ポーリング**（`plc_service_client_polling.go`）: 接続先ごとに読み取り項目の一覧と周期を持ち、ポーリング中は 1 つのゴルーチンが周期ごとに項目を上から順に送る
  - 項目は指定どおりに送る（まとめて読まない。存在しないアドレスが 1 つあると、まとめた全体が例外応答になるため）
  - 読めなかった項目は、最後に読めた値を残す（`values` / `valuesAt`）。画面は status が ok 以外なら値を薄く表示する
  - ある項目がタイムアウト・エラーになったら、その周期の残りの項目は送らない（status = `skipped`）。例外応答なら次の項目も送る
  - 1 周が周期より長くかかったら、待たずに次の周を始める（画面に「1 周が周期より長くかかっています」と出す）
  - ポーリングの送受信は、コマンドの履歴に入れない
  - 止めるときは、ロックの中で取り消しだけして、ゴルーチンの終わりはロックの外で待つ（ゴルーチンは結果を入れるときに `clientManager.mu` を取るため）。接続先の削除・プロジェクトの読み込み・終了でも止める
  - 読み取り項目・周期・表示形式（`format`。画面が決める値で、ホストは意味を持たない）はプロジェクトに保存する。ポーリング中かどうかは保存しない（読み込んだら停止中）
  - 値の表示形式を変えられるよう、`ResultValue` は表示用の文字列に加えて元の値（`Bit` / `Word`）を持つ
- **テスト**: `internal/protocols/modbus/client_test.go`（フェイクの TCP サーバー）・`client_mapping_test.go`、`internal/infrastructure/adapter/device_sync_test.go`（同期の決まり）、`internal/application/plc_service_clients_test.go` / `plc_service_client_polling_test.go`（`protocoltest.ClientFactory`）・`plc_service_client_sync_test.go`（`protocoltest.DeviceClientFactory`: フェイクの機器つき。`Device.Down` でタイムアウトにできる）、`internal/protocols/integration_test.go` の `TestModbusClient_AgainstOwnServer` / `TestModbusClient_Polling` / `TestModbusClient_VariableMapping`（Karakuri 自身の Modbus TCP サーバーにつなぐ）、画面は `client-panel.spec.ts` / `client-polling.spec.ts` / `client-mapping.spec.ts`。OPC UA は `internal/protocols/opcua/client_test.go`（Karakuri の OPC UA サーバー、セキュリティポリシーとユーザー名を使う uastack のサーバー、応答しないサーバー）、`integration_test.go` の `TestOPCUAClient_AgainstOwnServer`、画面は `client-opcua.spec.ts`。サブスクリプションのホスト側は `internal/application/plc_service_client_subscription_test.go`（`protocoltest.ClientFactory` の監視は、テストから `Subscription.Emit()` で知らせを送れる）
- **変数とのマッピング**（`plc_service_client_sync.go`、状態は `adapter.DeviceSync`）: 変数をサーバーのレジスタと同じ形（`ProtocolMapping`、`protocolType` は `client:<接続先の ID>`）で接続先のアドレスにマッピングする。同期はポーリング中だけ行う
  - 周期ごとに、書き込み未反映の変数を書く → 読み取り項目を読む → マッピングした範囲を読む。変数が変わって書き込み未反映が増えたら（周期の変更も）、待たずにポーリングのゴルーチンを起こす（`clientInstance.wake`）
  - 読む範囲は、エリアごとに重なるか隣り合うものだけまとめる。1 回で読める個数（`MaxReadCount`）より大きければ分けて読み、全部読めたら反映する（一部だけの範囲では変数を変えない）
  - **機器が先**: マッピングしてから一度も機器から読めていない変数は、機器に書かない（変数の初期値で機器を上書きしないため）。マッピング（エリア・アドレス・エンディアン）か型を変えたら、読み直すまで書かない
  - 読めた変数がこの同期以外（画面・スクリプト・サーバーのクライアント）から変わると「書き込み未反映」にする。書き込みが反映されるまでは、読み取った値で上書きしない（最後に書いた側が勝つ）。止めている間に変えた値も未反映のまま残り、開始したら書く
  - 機器の側で書けないエリア（Modbus のディスクリート入力・入力レジスタ）は読むだけ。変数を変えても、次の読み取りで機器の値に戻す
  - 読み取りが続けて `failThreshold` 回失敗したら「通信エラー」。値は最後に読めたまま残す。書き込みが失敗したら未反映のまま、次の周期に送り直す。タイムアウト・通信エラーになったら、その周期の残りの通信はしない
  - ビットのエリアには BOOL だけ。使えないマッピング（ないエリア・範囲外・ビットのエリアに BOOL 以外）は `invalid` にして読み書きしない
  - 読み取りで変数を更新するときは `UpdateValueFrom()` で自分を通知先から外す（機器へ書き戻さない）。`DeviceSync` のロックの中では変数ストアを更新しない
  - 接続先を削除すると、その接続先へのマッピングを変数から外す。プロジェクトには接続先の ID を保存し、読み込んでも ID を変えない（マッピングが接続先を指すため）
  - スクリプトからは `plc.getVariableStatus(name)` で状態を読める（`{ state, writePending, error, client }`、マッピングしていなければ `null`）

### REST HTTP API 実装

#### サーバー構成

- `internal/infrastructure/httpapi/server.go` に `Server` 構造体を定義
- `NewServer(svc *application.PLCService, port int)` で生成。`PLCService` を直接依存として持つ
- `Start()` でバックグラウンドゴルーチンとして起動（ノンブロッキング）
- `Shutdown(ctx)` でグレースフルシャットダウン
- `Restart(port int)` で既存サーバーをシャットダウンして新ポートで再起動
- Go 1.22+ の `net/http` ServeMux を使用（`GET /path/{id}` 形式のメソッド + パスパラメーター指定）

#### ポート管理

- デフォルトポート: `8765`（`app.go` の `defaultHTTPAPIPort` 定数）
- 設定ファイル: `%APPDATA%\Karakuri\httpapi_config.json`（`{"port": 8765}` 形式）
- `loadHTTPAPIPort()` / `saveHTTPAPIPort(port)` ヘルパー関数（`app.go` 内）
- `App.httpAPIPort` フィールドで現在のポートを保持

#### エンドポイント一覧

| カテゴリ | メソッド | パス |
|--------|---------|------|
| サーバー管理 | GET | `/api/servers` |
| | POST | `/api/servers` |
| | DELETE | `/api/servers/{protocolType}` |
| | POST | `/api/servers/{protocolType}/start` |
| | POST | `/api/servers/{protocolType}/stop` |
| | GET | `/api/servers/{protocolType}/status` |
| | GET/PUT | `/api/servers/{protocolType}/config` |
| メモリ操作 | GET | `/api/memory/{protocolType}/areas` |
| | GET | `/api/memory/{protocolType}/{area}/words?address=N&count=N` |
| | PUT | `/api/memory/{protocolType}/{area}/words/{address}` |
| | GET | `/api/memory/{protocolType}/{area}/bits?address=N&count=N` |
| | PUT | `/api/memory/{protocolType}/{area}/bits/{address}` |
| 変数管理 | GET | `/api/variables` |
| | POST | `/api/variables` |
| | PUT | `/api/variables/{id}/value` |
| | DELETE | `/api/variables/{id}` |
| プロジェクト | GET | `/api/project/export` |
| | POST | `/api/project/import` |

#### CORS

全エンドポイントに `corsMiddleware` を適用（`Access-Control-Allow-Origin: *`）。OPTIONS プリフライトリクエストに対して 204 を返す。

#### UI表示

`App.tsx` のヘッダーに HTTP API URL (`http://localhost:{port}/api`) を常時表示。✎ ボタンでポート編集モードに切り替え。保存時に `SetHTTPAPIPort()` を呼び出してサーバーを再起動。

## License

MIT License (Copyright 2026 bamchoh)
