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
- **スクリプト機能**: JavaScriptで周期処理を記述。const/let対応（IIFE wrapping）、実行エラーのGUI表示
- **レジスタ操作**: GUIからメモリエリアの値を直接操作可能
- **モニタリング**: 任意のレジスタをリアルタイム監視・書き込み（ドラッグ&ドロップ並べ替え対応）
- **プロトコルマッピング**: 変数を複数プロトコルのメモリアドレスにマッピング可能
- **REST HTTP API**: アプリをネットワーク経由で外部から操作可能（デフォルトポート 8765）。サーバー管理・レジスタ読み書き・変数管理・プロジェクトエクスポート/インポートに対応

## Build and Development Commands

```bash
# 開発モード（プラグインビルド → ホットリロード起動）
task dev

# プロダクションビルド（プラグインビルド → wails build）
task build

# プラグインバイナリのみビルド（plugins/ 以下にサブフォルダ + plugin.json を生成）
task plugins

# proto ファイルから Go コードを再生成
task proto

# 全テスト実行
task test

# ビルド成果物を削除
task clean

# Goバインディングの再生成
wails generate module

# フロントエンドのみビルド
cd frontend && npm run build

# Goコードのビルド確認
go build ./...
```

タスクランナーは [go-task](https://taskfile.dev/) を使用。`Taskfile.yaml` に定義。

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

### ディレクトリ構造（DDD + マルチプロトコル + プラグイン）

```
proto/                    # protobuf 定義
│   ├── plugin_service.proto            # PluginService（ファクトリー情報・サーバーライフサイクル・ノードアドレス）
│   ├── datastore_service.proto         # DataStoreService（メモリエリア・レジスタ読み書き）
│   └── variable_accessor_service.proto # VariableAccessorService（ホストの変数アクセス）
pb/pluginpb/              # protoc 生成 Go コード（コミット対象）
cmd/
├── modbus-plugin/        # Modbus プラグインバイナリ
│   ├── main.go           # gRPC サーバー起動・GRPC_PORT 出力
│   ├── internal/
│   │   └── modbus/       # Modbus プロトコル実装（ホストから隔離）
│   │       ├── factory.go      # ModbusServerFactory（TCP/RTU/ASCIIの3ファクトリー）
│   │       ├── server.go       # ModbusServer
│   │       ├── datastore.go    # ModbusDataStore（SetChangeHook でクライアント書き込みフック）
│   │       └── rtu/            # RTU/ASCII フレーム処理
│   └── server/
│       └── plugin_server.go  # PluginService + DataStoreService 実装
└── opcua-plugin/         # OPC UA プラグインバイナリ（bamchoh/uastack 使用）
    ├── main.go           # pluginruntime.RunFactory で gRPC サーバー起動
    ├── plugin-manifest/  # plugin.json 生成（task plugins から実行）
    └── internal/
        └── opcua/        # OPC UA プロトコル実装（ホストから隔離）
            ├── factory.go      # Factory（VariableStoreInjector 実装）
            ├── config.go       # Config（ネットワーク設定 + アプリケーション情報）
            ├── server.go       # Server（uastack サーバーのライフサイクル）
            ├── nodes.go        # plcNodes（公開変数 → uastack アドレス空間のノード、値の同期）
            ├── types.go        # PLC 型解析・PLC 値 ↔ ua.Variant 変換
            └── datastore.go    # emptyDataStore（OPC UA はレジスタを持たない）
plugins/                  # ビルド済みプラグイン配置先（task plugins で生成）
├── modbus-plugin/
│   ├── modbus-plugin.exe
│   └── plugin.json
└── opcua-plugin/
    ├── opcua-plugin.exe
    └── plugin.json
internal/
├── domain/           # ドメイン層
│   ├── protocol/     # プロトコル共通インターフェース（ServerFactory, DataStore, ConfigField等）
│   ├── datastore/    # DataStore共通定義
│   ├── script/       # スクリプトエンティティ
│   └── variable/     # 変数・構造体型・NodePublishing
├── application/      # アプリケーション層
│   ├── plc_service.go  # メインサービス（プロトコル非依存、モニタリング管理含む）
│   └── dto.go          # DTO定義（ProtocolSchemaDTO, MonitoringItemDTO等）
└── infrastructure/   # インフラ層（ホスト側インフラ。プロトコル固有実装は含まない）
    ├── plugin/       # プラグインインフラ（ホスト側）
    │   ├── process_manager.go         # PluginProcessManager（plugin.json スキャン・オンデマンドプロセス起動）
    │   ├── process_manager_windows.go # Windows 向け: コンソールウィンドウ非表示（CREATE_NO_WINDOW）
    │   ├── process_manager_unix.go    # 非Windows 向け: スタブ
    │   ├── host_grpc_server.go        # HostGrpcServer（VariableAccessorService gRPC サーバー）
    │   ├── remote_factory.go          # LazyRemoteServerFactory（マニフェストからプロトコル情報を返し gRPC はオンデマンド）
    │   ├── remote_server.go           # RemoteProtocolServer（gRPC クライアント実装）
    │   ├── remote_datastore.go        # RemoteDataStore（gRPC クライアント実装）
    │   └── remote_listener.go         # RemoteVariableChangeListener（変数→DataStore gRPC 同期）
    ├── httpapi/      # REST HTTP APIサーバー実装
    │   └── server.go       # HTTPAPIServer（net/http ServeMux使用）
    ├── adapter/      # アダプター層
    │   ├── variable_datastore.go       # VariableBackedDataStore（変数↔DataStore双方向同期）
    │   └── variable_store_accessor.go  # VariableStoreAccessor 実装（OPC UA用）
    └── scripting/    # JSエンジン（goja使用）
```

### 主要コンポーネント

- **PLCService** (`internal/application/plc_service.go`): メインサービス。プロトコル非依存で、複数のサーバーインスタンスを同時管理
  - `servers map[protocol.ProtocolType]*serverInstance` で各プロトコルのサーバーを保持
  - 各プロトコルタイプは最大1インスタンス（プロトコルタイプをサーバー識別子として利用）
  - Modbus プラグインの ProtocolType は `"modbus"`（TCP/RTU/ASCII はバリアントとして管理）
  - OPC UA の ProtocolType は `"opcua"`
  - `variableStore` と `scriptEngine` は全サーバーで共有
  - `factories map[protocol.ProtocolType]protocol.ServerFactory` でプロトコルファクトリーを管理（registry は廃止）
  - `RegisterPluginFactory()`: ファクトリーを登録する（ホスト起動時に `InitPlugins()` が呼び出す）
  - `InitPlugins(pluginsDir)`: `PluginProcessManager.DiscoverManifests()` でマニフェストを読み込み `LazyRemoteServerFactory` を登録（プロセスは起動しない）
  - `AddServer()` 時に `LazyRemoteServerFactory.EnsureStarted()` でプラグインプロセスをオンデマンド起動
  - `RemoveServer()` 時に `LazyRemoteServerFactory.StopProcess()` でプラグインプロセスを停止
  - `StartHostGrpcServer()`: `HostGrpcServer` を起動（OPC UA プラグインが変数にアクセスするための gRPC サーバー）
  - `AddServer()` 時に `VariableStoreInjector` を実装するファクトリーへ `VariableStoreAccessor` を注入
  - リモート DataStore: `RemoteVariableChangeListener` で変数↔プラグイン DataStore を双方向 gRPC 同期
  - インプロセス DataStore（テスト用）: `VariableBackedDataStore`（後方互換）
  - `UpdateVariableMappings()` 後に `NodePublishingAware` を実装するサーバーへ変更通知を送信
- **ServerFactory** (`internal/domain/protocol/server.go`): プロトコルサーバーを作成するファクトリーインターフェース
  - `GetConfigFields()`: スキーマ駆動UIのためのフィールド定義を返す
  - `GetProtocolCapabilities()`: UnitIDサポート等の機能情報を返す
  - `ConfigToMap()` / `MapToConfig()`: 設定の変換
- **ModbusServerFactory** (`cmd/modbus-plugin/internal/modbus/factory.go`): `fixedVariant` フィールドで TCP/RTU/ASCII を固定した3種のファクトリー
  - `NewModbusTCPServerFactory()`, `NewModbusRTUServerFactory()`, `NewModbusASCIIServerFactory()` で生成
  - それぞれ `ProtocolType()` が `"modbus-tcp"` / `"modbus-rtu"` / `"modbus-ascii"` を返す
  - ホスト本体からは直接使用しない（プラグインバイナリ `cmd/modbus-plugin/` からインポート）
  - テストでは `fakeServerFactory`（`internal/application/fake_factory_test.go`）を使用（プロトコル固有実装に依存しない）
- **opcua.Factory** (`cmd/opcua-plugin/internal/opcua/factory.go`): OPC UA サーバーのファクトリー
  - `ProtocolType()` が `"opcua"`（`protocol.ProtocolOPCUA`）を返す
  - `VariableStoreInjector` を実装。プラグイン内の `pluginruntime.Server.CreateAndStart()` が `host_grpc_addr` に接続した `RemoteVariableStoreAccessor` を注入する
  - `GetProtocolCapabilities()` が `SupportsNodePublishing: true` を返す
  - ホスト本体からは直接使用しない（プラグインバイナリ `cmd/opcua-plugin/` からインポート）
- **PluginProcessManager** (`internal/infrastructure/plugin/process_manager.go`): プラグインプロセスのライフサイクル管理
  - `DiscoverManifests(pluginsDir)`: `plugins/*/plugin.json` を走査してマニフェストを読み込む（プロセスは起動しない）
  - `Launch(entrypoint)`: プラグイン exe を起動し、stdout から `GRPC_PORT=N` を読み取って gRPC 接続を確立
  - `plugin.json` スキーマ: `name`, `entrypoint`, `version`, `protocol_type`(必須), `display_name`(必須), `variants`(必須), `capabilities`(省略可), `author`(省略可), `description`(省略可)
  - クラッシュ監視 goroutine で `crashed`/`exitErr` を更新
  - `setSysProcAttr(cmd)`: Windows では `CREATE_NO_WINDOW` フラグでコンソールウィンドウを非表示にする（`process_manager_windows.go`）
- **LazyRemoteServerFactory** (`internal/infrastructure/plugin/remote_factory.go`): オンデマンドプロセス起動のファクトリー
  - `ProtocolType()`, `DisplayName()`, `ConfigVariants()`, `GetProtocolCapabilities()`: `plugin.json` のマニフェストから返す（gRPC 不要）
  - `EnsureStarted()`: プロセスが未起動または停止済みの場合に `PluginProcessManager.Launch()` でプロセスを起動
  - `StopProcess()`: プロセスを停止してリセット（次回 `EnsureStarted()` で再起動可能）
  - `CreateServer()`, `CreateDataStore()`, `GetConfigFields()` 等 gRPC が必要なメソッドはプロセス起動後のみ呼ばれる
- **HostGrpcServer** (`internal/infrastructure/plugin/host_grpc_server.go`): ホスト側 gRPC サーバー
  - `VariableAccessorService` を実装（OPC UA プラグインが変数を読み書きするため）
  - `SubscribeVariableChanges()` ストリームで変数変更を OPC UA プラグインに配信
- **RemoteProtocolServer** / **RemoteDataStore** (`internal/infrastructure/plugin/`):
  - `ProtocolServer`, `DataStore` の各インターフェースを gRPC クライアント呼び出しで実装
- **ModbusDataStore** (`cmd/modbus-plugin/internal/modbus/datastore.go`):
  - `SetChangeHook(DataChangeHook)`: Modbus クライアントが書き込んだ変更をフックで通知（循環防止のためホスト→プラグイン書き込み時は通知しない）
- **plcNodes** (`cmd/opcua-plugin/internal/opcua/nodes.go`): 公開変数を uastack の `AddressSpace` に実ノードとして登録する
  - `rebuild()`: `GetEnabledNodePublishings("opcua")` からノード構成を計画し、差分（追加・削除・型変更）だけノードを作り直す
  - 構造体フィールド・配列要素は子ノード（HasComponent）として事前に展開
  - `run()`: `SubscribeVariableChanges` ストリーム + 1 秒ごとの再同期でホストの値を `SetValue()`（変化したノードだけ uastack がサブスクリプションに即時通知）
  - `Node.OnWrite` でクライアントの書き込みを `WriteVariableValue` / `WriteVariableFieldValue` に転送
- **pluginruntime** (`internal/pluginruntime/`): プラグイン共通の gRPC サーバー実装（Modbus / OPC UA 共通）
  - `GetMetadata()` はファクトリーの `GetProtocolCapabilities()` を返す
  - `CreateAndStart()`: ファクトリーが `VariableStoreInjector` なら `host_grpc_addr` に接続して `RemoteVariableStoreAccessor`（`variable_accessor.go`）を注入
  - `OnNodePublishingUpdated()`: サーバーが `NodePublishingAware` なら転送
  - `NewManifest(factory, entrypoint)`: ホストの `PluginManifest` と同じ JSON 形式（`capabilities` は snake_case）でマニフェストを作る
- **DataStore** (`internal/domain/protocol/server.go`): プロトコル共通のメモリ操作インターフェース
  - `ReadBits()`, `WriteBit()`, `ReadWords()`, `WriteWord()`: 汎用メモリ操作
  - `Snapshot()`, `Restore()`: Export/Import用
  - `GetAreas()`: `MemoryArea` スライスを返す。`MemoryArea.OneOrigin` が true のエリアはUIで1オリジンアドレスを表示する（内部は常に0ベース）。Modbusの全4エリアは `OneOrigin: true`
  - `MemoryArea.ShortName`: マッピング列などに表示する短い名前（Modbus は `0` / `1` / `3` / `4`）。空ならエリア ID を表示する
- **VariableStoreAccessor** (`internal/domain/protocol/server.go`): プロトコルが VariableStore にアクセスするための汎用インターフェース
  - `GetEnabledNodePublishings(protocolType)`: 指定プロトコルで公開設定された変数一覧を返す
  - `ReadVariableValue(variableID)` / `WriteVariableValue(variableID, value)`: 変数値の読み書き
  - `GetStructFields(typeName)`: 構造体型のフィールド一覧を返す（子ノードブラウズ用）
  - 実装: `internal/infrastructure/adapter/variable_store_accessor.go`
- **VariableStoreInjector** / **NodePublishingAware** (`internal/domain/protocol/server.go`): OPC UA 等のノードベースプロトコル向けインターフェース
  - `VariableStoreInjector`: `InjectVariableStore(accessor)` で遅延注入を受け付ける
  - `NodePublishingAware`: `OnNodePublishingUpdated()` でノード公開設定変更通知を受け付ける
  - `NodeAddressResolver`: `NodeAddresses(variables)`（`NodeVariable{ID, Name}` のスライス）で公開変数のノードアドレス（OPC UA なら NodeID 文字列）と、公開できない場合の理由（`NodeAddress.Error`）を返す。書式と判定はプロトコルが決める
  - `ProtocolCapabilities.SupportsNodePublishing: true` のプロトコルで利用される
- **ScriptEngine** (`internal/infrastructure/scripting/engine.go`): gojaベースのJavaScript実行エンジン
  - `plc`オブジェクトでDataStoreおよびVariableStoreにアクセス可能
  - スクリプトコードをIIFE `(function(){...})();` でラップして、const/let再宣言エラーを回避
  - 実行時エラーを保存して`GetLastError()`で取得可能
  - 周期実行中のpanicをキャッチしてエラーとして記録
  - TIME/DATE型シンタックスシュガー: `plc.readTimeMs(name)`, `plc.writeTimeMs(name, ms)` など、変数の読み取り〜数値変換〜書き込みをワンステップで実行（内部でparse/formatを自動適用）
  - LINT/ULINT BigInt API: `plc.readLintBig(name)`, `plc.writeLintBig(name, val)`, `plc.readUlintBig(name)`, `plc.writeUlintBig(name, val)`（2^53超の値をJavaScript BigInt型で精度損失なく操作。`readVariable()` で±2^53超の値を読んだ場合はコンソールに `[WARN]` を出力）

### フロントエンド構成（スキーマ駆動UI）

```
frontend/src/
├── components/
│   ├── ServerPanel.tsx     # スキーマ駆動のサーバー設定UI
│   ├── RegisterPanel.tsx   # 汎用メモリ操作UI（サブタブで一覧/モニタリング切替）
│   ├── MonitoringView.tsx  # カスタムレジスタモニタリング
│   ├── VariableView.tsx    # 変数管理UI（IEC 61131-3データ型対応）
│   └── ScriptPanel.tsx     # スクリプト管理（エラー表示機能付き）
└── App.tsx                 # タブベースのメインUI
```

#### ServerPanel.tsx
サーバーインスタンスの一覧を表示・管理します。
- **サーバー一覧**: `GetServerInstances()` と `GetAvailableProtocols()` を1秒ポーリングして全サーバーのステータスと利用可能プロトコルを更新（プラグイン起動タイミングのずれに対応）
- **サーバー追加**: 「サーバーを追加」ダイアログで未追加のプロトコルから選択して追加（`AddServer(protocolType, variant)`）
- **個別操作**: 各サーバーの開始/停止/設定変更/削除が独立して可能
- **設定パネル**: `GetProtocolSchema(protocolType)` から取得したスキーマに基づき `DynamicField` で動的フォームを生成
- `ServerInstanceRow` と `DynamicField` は `ServerPanel` 関数の外部に定義（state リセット防止）

#### RegisterPanel.tsx
「一覧表示」と「モニタリング」のサブタブを持ちます。
- **プロトコル選択**: 複数サーバー起動時は上部にプロトコル選択セレクトを表示
  - `supportsNodePublishing=true` のサーバー（OPC UA 等）はレジスタを持たないため除外
- **一覧表示**: 選択中プロトコルのメモリエリアごとのレジスタ値を表示・編集
- **モニタリング**: 任意のレジスタを登録してリアルタイム監視・書き込み可能
  - ドラッグ＆ドロップで並び替え可能（@dnd-kit使用）
- **1オリジンアドレス表示**: `area.oneOrigin` が true のエリアはアドレス表示を+1（内部値は0ベースを維持）

#### MonitoringView.tsx
プロトコルを横断したレジスタモニタリング。
- **Props**: `serverInstances: ServerInstanceDTO[]`（RegisterPanel から渡す）
- **プロトコル別メモリエリア**: `memoryAreasByProtocol: Record<string, MemoryAreaDTO[]>` でプロトコルごとにエリア一覧をキャッシュ
- **項目追加ダイアログ**: 複数サーバー時はプロトコル選択セレクトを表示
- **サーバー変化の検出**: `protocolTypesKey = serverInstances.map(i => i.protocolType).join(',')` でサーバー構成変化を効率的に検出してエリアを再取得
- **API 呼び出し**: `ReadWords(item.protocolType, item.memoryArea, ...)` など全 API に `protocolType` を渡す

#### VariableView.tsx
IEC 61131-3準拠の変数管理機能。
- **データ型サポート**:
  - スカラー型: BOOL, SINT, INT, DINT, USINT, UINT, UDINT, REAL, LREAL, STRING[n]
  - 配列型: ARRAY[型;サイズ]（例: ARRAY[INT;10]）
  - 構造体型: カスタム構造体定義（ネスト可能）
- **変数表示**: 構造体フィールドと配列要素をフラット化して行単位で表示
  - 展開/折りたたみ機能（構造体・配列のヘッダー行）
  - アドレスオフセット計算（フィールド・要素ごと）
- **変数メタデータ編集**: 操作列の「編集」ボタンで変数の名前・データタイプを変更可能
  - 変数追加ダイアログと同じ型カテゴリ/データタイプ選択フォームを使用
  - データタイプ変更時は値をデフォルト値にリセット（マッピングは保持）
  - OPC UA サーバーが起動中の場合はノード再構築のため `OnNodePublishingUpdated()` を呼び出す
  - バックエンド: `VariableStore.UpdateMetadata()` → `PLCService.UpdateVariable()` → `App.UpdateVariable()`
- **値編集ダイアログ**:
  - 再帰的エディタで複雑なデータ構造に対応
  - 構造体配列要素は折りたたみ可能（`StructArrayElementEditor`）
  - 数値入力は10進、16進（0x）、2進（0b）対応
- **プロトコルマッピング**: 変数を複数の起動中サーバーのメモリアドレスにマッピング可能
  - マッピングダイアログのプロトコル選択は `GetServerInstances()` で取得した起動中サーバーから生成
  - メモリエリア選択は `memoryAreasByProtocol[m.protocolType]` で取得
  - 2行表示レイアウト（ヘッダ行 + コントロール行）
  - マッピング競合警告: 他の変数と同じレジスタを使用している場合、ダイアログ内と変数一覧の両方に警告を表示
  - **マッピング列の表示**: レジスタマッピング（`protocol:area:addr`、area は `MemoryArea.shortName`）とノード公開設定（`opcua(R/W)` 等）をカンマ区切りで両方表示
  - **NodeID の表示**: マッピングダイアログ・一括マッピング編集で、`GetNodeAddresses(protocolType, variableIds)` でプラグインから取得した値（`nodeAddresses` state）を表示する。フロントエンドで NodeID を組み立てない。`error` がある変数は理由を表示し、公開を有効にした保存を止める
  - **ダイアログを開く際に `loadServerInstancesAndAreas()` を再実行**: マウント後に追加されたサーバーも「プロトコル公開設定」セクションに反映
  - `loadServerInstancesAndAreas` は `useCallback` で定義し、マウント時・ダイアログオープン時の両方から呼び出す
- **一括マッピング編集**: ツールバーの「一括マッピング編集」ボタンで全変数のマッピングをテーブル形式で一括設定
  - プロトコルを選択すると全変数が一覧表示され、まとめて編集・保存できる
  - **Modbus系**: メモリエリア / アドレス（空欄=そのプロトコルのマッピング削除） / エンディアンをテーブル表示
  - **OPC UA系** (`SupportsNodePublishing=true`): 公開チェック / アクセスモード / NodeID をテーブル表示
  - アドレス重複検出: `findBulkRowConflicts()` で同じメモリエリア内のアドレス範囲重複を検出し、該当行をオレンジハイライト・入力欄のボーダーをオレンジに・⚠アイコン表示。重複件数バナーをダイアログ下部（スクロール外・ボタン上）に固定表示
  - 保存時に他プロトコルのマッピングを保持（`variable.mappings.filter(m => m.protocolType !== bulkProtocol)`）
- **OPC UA 公開設定表示**: `formatNodePublishings()` で有効な NodePublishing を `opcua(RO)` / `opcua(WO)` / `opcua(R/W)` 形式にフォーマットして変数一覧の「マッピング」列に追加表示
- **構造体型管理**: 構造体型の登録・編集・削除機能
  - 編集時は既存の定義を読み込んでフォームに展開
  - データ型の自動解析（ARRAY、STRING、構造体、スカラー）
  - スクロール対応（多数のフィールドを持つ構造体に対応）

#### ScriptPanel.tsx
JavaScript（goja）でPLC動作を記述。
- **エラー表示**: 実行時エラーをGUIに表示（タイムスタンプ付き、クリアボタン）
- **コンソールログ表示**: スクリプト一覧の下に「コンソール」セクションを常時表示
  - `console.log()` の出力をタイムスタンプ・スクリプト名付きで一覧表示
  - 新しいログ追加時に自動スクロール、クリアボタン付き
  - テスト実行（「テスト実行」ボタン）時の出力もスクリプト名「テスト実行」で表示
- **const/let対応**: スクリプトコードをIIFEでラップして再宣言エラーを回避
- **plcオブジェクト**:
  - メモリアクセス: `plc.readBit()`, `plc.writeBit()`, `plc.readWord()`, `plc.writeWord()`
  - 変数アクセス: `plc.readVariable()`, `plc.writeVariable()`, `plc.readArrayElement()`, `plc.writeArrayElement()`, `plc.readStructField()`, `plc.writeStructField()`
  - LINT/ULINT BigInt API: `plc.readLintBig(name)`, `plc.writeLintBig(name, val)`, `plc.readUlintBig(name)`, `plc.writeUlintBig(name, val)`（JavaScriptのBigInt型で64ビット整数を精度損失なく読み書き。例: `plc.writeLintBig("myVar", plc.readLintBig("myVar") + 1n)`）
    - `plc.readVariable()` でLINT/ULINT値が±2^53を超えた場合は `[WARN]` をコンソールに出力して `readLintBig()`/`readUlintBig()` の使用を促す
  - TIME/DATE シンタックスシュガー: `plc.readTimeMs()`, `plc.writeTimeMs()`, `plc.readDateSec()`, `plc.writeDateSec()`, `plc.readTimeOfDayMs()`, `plc.writeTimeOfDayMs()`, `plc.readDateAndTimeSec()`, `plc.writeDateAndTimeSec()`（変数の読み取り・パース・フォーマット・書き込みをワンステップで実行）
  - TIME/DATE 文字列変換: `plc.parseTime()`, `plc.formatTime()`, `plc.parseDate()`, `plc.formatDate()`, `plc.parseTimeOfDay()`, `plc.formatTimeOfDay()`, `plc.parseDateAndTime()`, `plc.formatDateAndTime()`

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
- **UnitID 設定**:
  - `GetUnitIDSettings(protocolType)`, `SetUnitIDEnabled(protocolType, unitId, enabled)`: UnitID 応答設定
  - `GetDisabledUnitIDs(protocolType)`, `SetDisabledUnitIDs(protocolType, ids)`: 無効 UnitID の一括管理
- **メモリ操作**（全て `protocolType string` を第1引数に取る）:
  - `GetMemoryAreas(protocolType)`: メモリエリア一覧を取得
  - `ReadBits(protocolType, area, address, count)`, `WriteBit(protocolType, area, address, value)`
  - `ReadWords(protocolType, area, address, count)`, `WriteWord(protocolType, area, address, value)`
- **モニタリング**:
  - `GetMonitoringItems()`, `AddMonitoringItem()`, `UpdateMonitoringItem()`, `DeleteMonitoringItem()`, `ReorderMonitoringItem()`, `ClearMonitoringItems()`
- **変数管理**:
  - `GetVariables()`, `CreateVariable()`, `UpdateVariableValue()`, `DeleteVariable()`: 変数CRUD操作
  - `GetDataTypes()`: サポートされているデータ型一覧を取得
  - `GetStructTypes()`, `RegisterStructType()`, `DeleteStructType()`: 構造体型管理
  - `UpdateVariableMappings()`: 変数のプロトコルマッピング設定
- **スクリプト**:
  - `GetScripts()`, `GetScript()`, `CreateScript()`, `UpdateScript()`, `DeleteScript()`: スクリプトCRUD操作
  - `StartScript()`, `StopScript()`: スクリプト実行制御
  - `ClearScriptError()`: スクリプトエラーをクリア
  - `GetConsoleLogs()`, `ClearConsoleLogs()`: コンソールログの取得・クリア
- **HTTP API 設定**:
  - `GetHTTPAPIPort()`: 現在のHTTP APIポート番号を返す
  - `SetHTTPAPIPort(port)`: ポートを変更してHTTP APIサーバーを再起動（1024〜65535、設定ファイルに永続化）

### 変数管理とデータ型システム

#### IEC 61131-3準拠のデータ型

変数は以下のデータ型をサポート：

1. **スカラー型**:
   - 整数: SINT(8bit), INT(16bit), DINT(32bit), USINT, UINT, UDINT
   - 浮動小数点: REAL(32bit), LREAL(64bit)
   - 論理: BOOL
   - 文字列: STRING[n] (固定長、nはバイト数)
   - 時間・日付: TIME, DATE, TIME_OF_DAY, DATE_AND_TIME
     - TIME: 時間間隔（"T#1h30m45s" など、内部はミリ秒int32で2ワード保存）
     - DATE: 日付（"D#2024-01-01" など、内部はその日の0時0分0秒のUnix秒uint64で4ワード保存）
     - TIME_OF_DAY: 1日の中の時刻（"TOD#12:30:15" など、内部はミリ秒uint32で2ワード保存）
     - DATE_AND_TIME: 日付と時刻（"DT#2024-01-01-12:30:15" など、内部はUnix秒uint64で4ワード保存）

2. **配列型**: `ARRAY[要素型;要素数]`
   - 例: `ARRAY[INT;10]`, `ARRAY[MyStruct;5]`
   - 多次元配列は `ARRAY[ARRAY[INT;5];3]` のように表現

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
- **モニタリング設定**: `%APPDATA%\PLCSimulator\monitoring_config.json`
  - 登録したモニタリング項目（プロトコルタイプ、メモリエリア、アドレス、ビット幅、エンディアン、表示形式）
- **HTTP API 設定**: `%APPDATA%\PLCSimulator\httpapi_config.json`
  - `{"port": 8765}` 形式で保存。UI または `SetHTTPAPIPort()` で変更すると自動更新

## 新プロトコル追加手順

プロトコル固有のコード（プロトコルタイプの値、設定、メモリエリア、アドレス書式など）はすべてプラグインのフォルダ（`cmd/<name>-plugin/`）に置く。ホスト（`internal/`, `app.go`）とフロントエンドはプロトコル名や固有の値を持たず、能力フラグ（`ProtocolCapabilities`）・スキーマ（`GetConfigFields()`）・`MemoryArea` の属性・`NodeAddressResolver` だけで動く。

1. `cmd/<name>-plugin/internal/<name>/` を作成し、以下を実装:
   - `factory.go`: `ServerFactory`（プロトコルタイプの定数もここで定義する）
   - `server.go`: `ProtocolServer`（ノード公開型なら `NodePublishingAware` / `NodeAddressResolver` も）
   - `datastore.go`: `DataStore`（レジスタを持たないなら空の実装）
2. `cmd/<name>-plugin/main.go` で `pluginruntime.RunFactory(factory, debug)` を呼ぶ
3. `cmd/<name>-plugin/plugin-manifest/main.go` で `pluginruntime.NewManifest()` を出力し、`Taskfile.yaml` の `plugins` タスクに追加
4. **ホスト・フロントエンドの変更不要** - スキーマと能力フラグから自動生成

## 重要な実装ポイント

### 1オリジンアドレス表示

- **設計方針**: プロトコル固有の知識（どのエリアが1オリジンか）はバックエンドが保持し、フロントエンドはビュー層に持たせない
- **`MemoryArea.OneOrigin`**: `GetAreas()` が返す `MemoryArea` 構造体のフィールド。true の場合、UIでのアドレス表示を1オリジンにする
- **内部値は常に0ベース**: API (ReadWords/WriteWord等) には0ベースのアドレスを渡す。表示・入力変換のみUIで行う
- **フロントエンドの変換**: `area.oneOrigin` を参照して表示時に+1、入力時に-1。ハードコードの判定リストは持たない

### Reactコンポーネントの状態管理

- **コンポーネントの定義位置**: 関数コンポーネント内で別のコンポーネントを定義すると、再レンダリング時に新しいコンポーネントとして扱われ、`useState`などの状態がリセットされる
  - 例: `StructArrayElementEditor`は`VariableView`の外で定義
  - 内部で使用する関数は props で渡す
- **キーの設定**: リスト描画時は安定した`key`を設定（インデックスだけでなく、パスベースのキーを使用）

### スクリプトエンジン

- **const/let対応**: goja VMで同じプログラムを周期的に実行すると再宣言エラーが発生するため、IIFEでラップ
- **エラーハンドリング**: runtime panic をキャッチして`lastError`フィールドに保存し、GUI表示可能に
- **コンソールログ**: `console.log()` はバッファ（最大500件）に蓄積。`ConsoleLogEntry` に scriptID・scriptName・message・At を保存。フロントエンドは1秒ポーリングで `GetConsoleLogs()` を取得して表示。ミューテックスで保護
  - `createVM(scriptID, scriptName string)` にスクリプト識別子を渡すことで、どのスクリプトの出力かを記録
  - テスト実行（`RunOnce`）は scriptID="" / scriptName="テスト実行" でバッファに追加

### ダイアログスタイルの統一

すべてのダイアログ（変数編集、マッピング設定、構造体型管理、モニタリング追加、レジスタ書き込み）は統一されたスタイルパターンを使用：
- **`.dialog-row`**: ラベルとコントロールを横並びに配置
- **インラインスタイル削除**: すべての `<select>` と `<input>` からインラインスタイルを削除
- **スクロール対応**: ダイアログ全体に `maxHeight: '80vh'` を設定し、コンテンツ領域で `overflowY: 'auto'`
- **flexboxレイアウト**: ダイアログ全体を `display: flex, flexDirection: 'column'` でレイアウト
- **テーブル内コンポーネント**: ダイアログ内の `variable-table` の `td` に含まれる `select` / `input` は `.dialog-row` が適用されないため、`App.css` の `.dialog .variable-table td select/input` セレクタで同等のスタイル（`padding: 0.5rem`, `border`, `border-radius`, `background`, `color`, `font-size`, `width: 100%`, `box-sizing: border-box`）を別途定義している

### 値編集ダイアログ

- **スクロール**: ダイアログ全体で1つのスクロール領域を使用（配列要素部分に独自スクロールを設定しない）
- **再帰的エディタ**: `renderValueEditor`は`depth`パラメータで再帰の深さを追跡し、インデントを調整

### マッピング競合検出

- **`findMappingConflicts(mapping)`** (`VariableView.tsx`): マッピングダイアログで各行の競合チェック。`getWordCount` でワード数を計算し、アドレス範囲の重複をチェック
- **`getVariableMappingConflicts(variable)`** (`VariableView.tsx`): 変数一覧テーブルで各変数の競合チェック。保存済みマッピングをすべてスキャン
- **`findBulkRowConflicts(rows, protocolType)`** (`VariableView.tsx`): 一括マッピング編集ダイアログ内のアドレス重複を検出。`addressStr` が入力されている行の0ベースアドレス範囲（`addr0 〜 addr0+wordCount-1`）を計算し、同じメモリエリア内で重なるペアを返す。空欄行は対象外。競合バナーはスクロール外（`dialog-buttons` の直前）に固定表示し、テーブルが縦にスクロールしてもバナーが見切れないようにする
- **未定義動作の注意**: 同一レジスタへの複数変数マッピングはサポート対象外。書き込みは last-writer-wins、DataStore → Variable の同期は Go マップの反復順に依存して非決定的

### 構造体型管理ダイアログ

- **編集機能**: 既存の構造体型を編集可能（編集ボタンで定義を読み込み、更新で保存）
- **データ型解析**: フィールドのデータ型文字列を解析してフォーム形式に変換
  - `ARRAY[型;サイズ]` → 配列カテゴリ + 要素型 + サイズ
  - `STRING[n]` → スカラーカテゴリ + STRING型 + バイト長
  - 構造体型名 → 構造体カテゴリ + 型名
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
  - 経路: フロントエンド → `App.GetNodeAddresses()` → `PLCService.GetNodeAddresses()` → `RemoteProtocolServer` → gRPC `GetNodeAddresses` → `pluginruntime.Server` → `opcua.Server`
  - 起動していないときは、プラグインがホストの現在の設定（リクエストの `settings_json`）で起動しないサーバーを作って問い合わせる（OPC UA は設定を使わないが、アドレスが設定に依存するプロトコルのため）
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

- ホスト → OPC UA: `SubscribeVariableChanges` で変更された変数 ID を受け取り、その変数を読み直してルートと全子ノードに `SetValue()`。取りこぼし対策として 1 秒ごとに全変数を再同期
- OPC UA → ホスト: `Node.OnWrite` → `WriteVariableValue`（ルート）/ `WriteVariableFieldValue`（子ノード、フィールドパスは外部インデックス）。書き込み後に同じ変数の他ノードも更新
- msgpack は `MapType=map[string]interface{}` でデコードし、構造体値を JSON 化できるようにする

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

- **bamchoh/uastack v0.1.1**（`github.com/bamchoh/uastack/server`）
- セキュリティなし・匿名認証のみ（証明書は作られない。`PKIDir` はプラグイン exe と同じフォルダの `pki`）
- `Start()` で `net.Listen` してから `server.Serve(ln)` をゴルーチンで実行（ポート使用中を Start のエラーで返すため）
- `Stop()` は `server.Close()` を呼んで `Serve` の終了を待つ（v0.1.1 以降は Serve の開始前に Close しても Serve が終了する）
- `server.Server` は Close 後に再利用できないため、Start のたびに作り直す

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
- 設定ファイル: `%APPDATA%\PLCSimulator\httpapi_config.json`（`{"port": 8765}` 形式）
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
