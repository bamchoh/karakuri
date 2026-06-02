# Karakuri

Karakuri は Modbus（TCP/RTU/ASCII）と OPC UA に対応した PLC シミュレーターです。GUI でレジスタの値を確認・編集でき、JavaScript でカスタムロジックを記述できます。

![Screenshot](docs/screenshot.png)

## 機能

- **マルチプロトコル対応**
  - **Modbus TCP / Modbus RTU / Modbus ASCII** を独立したサーバーとして個別に追加・起動可能
    - 全 UnitID (1-247) に応答（個別に無効化可能）
    - コイル、ディスクリート入力、保持レジスタ、入力レジスタ（各65536点）
  - **OPC UA** サーバーを起動可能（セキュリティなし・匿名認証）
    - 変数管理で定義した変数をノードとして公開
    - 構造体フィールドと配列要素を個別のノードとしてブラウズ可能
    - フィールド・要素単位でのサブスクリプション（変更通知）に対応
    - スカラー・配列・構造体の読み書き対応

- **複数サーバー同時実行**
  - 例: Modbus TCP と OPC UA を同時に起動可能
  - 各サーバーは独立したメモリ空間を持つ
  - サーバーパネルで個別に設定・起動・停止が可能

- **変数管理（v0.0.8〜）**
  - **変数の名前・データタイプ変更（v0.0.15〜）**: 追加済みの変数の名前やデータタイプを後から変更可能。「編集」ボタンからダイアログで変更（データタイプ変更時は値をデフォルト値にリセット、マッピングは保持）
  - **IEC 61131-3準拠のデータ型**
    - スカラー型: BOOL, SINT, INT, DINT, **LINT**, USINT, UINT, UDINT, **ULINT**, REAL, LREAL, STRING[n], TIME, DATE, TIME_OF_DAY, DATE_AND_TIME
    - 配列型: ARRAY[型;サイズ]（例: ARRAY[INT;10], ARRAY[LINT;4]）
    - 構造体型: カスタム構造体定義（ネスト可能）
    - 時間・日付型はメモリ上で数値として保存（TIME: int32/2ワード、DATE: uint64/4ワード、TIME_OF_DAY: uint32/2ワード、DATE_AND_TIME: uint64/4ワード）
    - LINT（符号付き64ビット整数）・ULINT（符号なし64ビット整数）: 各4ワード保存
  - 構造体フィールドと配列要素をフラット化して表示
  - 再帰的値編集ダイアログ（複雑なデータ構造に対応）
  - プロトコルマッピング: 変数を複数プロトコルのメモリアドレスにマッピング
  - マッピング競合警告: 同じレジスタを複数の変数で使用している場合にダイアログと一覧の両方で警告表示
  - OPC UA 公開設定: 変数のマッピング列に OPC UA 公開状態（`opcua(R/W)` 等）を表示
    - マッピング設定ダイアログの「プロトコル公開設定」で公開有効化・アクセスモード（RO/WO/R/W）を設定
  - **一括マッピング編集**: 「一括マッピング編集」ボタンで全変数のマッピングをテーブル形式で一括設定
    - Modbus系: メモリエリア・アドレス・エンディアンを変数一覧表示で編集（アドレス空欄でマッピング削除）
    - OPC UA系: 公開有効/無効・アクセスモードを変数一覧表示で編集
    - アドレス重複時は該当行をオレンジハイライト・⚠ アイコン表示。ダイアログ下部に重複件数バナーを固定表示

- **レジスタ操作**
  - マトリクス表示でレジスタ値を一覧表示
  - キーボードナビゲーション対応
  - 10進数/16進数/8進数/2進数表示切替
  - 16bit/32bit/64bit 表示（リトルエンディアン/ビッグエンディアン対応）
  - 自動更新（100ms周期）
  - Modbus アドレスは 1オリジン表示（内部は 0ベース）

- **モニタリング機能**
  - 任意のレジスタを登録してリアルタイム監視
  - プロトコル、メモリエリア、アドレス、ビット幅、エンディアン、表示形式を設定可能
  - 複数サーバー起動時は異なるプロトコルのレジスタを同一リストで監視可能
  - ドラッグ＆ドロップで並び替え対応
  - 設定はファイルに自動保存（次回起動時に復元）

- **スクリプト機能**
  - JavaScript でカスタムロジックを記述
  - const/let 対応（IIFE wrapping）
  - 実行時エラーを GUI に表示（タイムスタンプ付き、クリアボタン）
  - `console.log()` の出力をコンソールパネルに表示（スクリプト名・タイムスタンプ付き、クリアボタン付き）
  - 周期実行（100ms〜1時間）
  - `plc` オブジェクトでメモリアクセス（プロトコル非依存）

- **REST HTTP API（v0.0.16〜）**
  - アプリをネットワーク経由で外部から操作可能（デフォルトポート: 8765）
  - ヘッダーに API URL を常時表示。✎ ボタンでポートを変更可能（次回起動時も反映）
  - 対応操作: サーバーの起動/停止/設定、レジスタの読み書き、変数のCRUD、プロジェクトのエクスポート/インポート

- **プラグインアーキテクチャ（v0.0.17〜）**
  - Modbus / OPC UA の各プロトコルを別プロセスのプラグインとして実装
  - gRPC（protobuf）による高速・型安全なプロセス間通信
  - `plugins/<プラグイン名>/plugin.json` マニフェストでプラグインを管理
  - Go 以外の言語（C# 等）でもプラグインを実装可能
  - プラグインプロセスのクラッシュがホストアプリに影響しない
  - v0.0.18 以降: プラグインプロセスのコンソールウィンドウを非表示化（Windows）
  - v0.0.19 以降: Modbus / OPC UA の実装を `cmd/*/internal/` に移動し、ホスト側からプロトコル固有の依存を完全排除

- **プロジェクト管理**
  - 設定・レジスタ・スクリプト・変数・モニタリング項目を JSON ファイルにエクスポート/インポート（GUI またはHTTP API経由）

## インストール

### 必要要件

- Go 1.21 以上
- Node.js 18 以上
- [Wails CLI](https://wails.io/docs/gettingstarted/installation)

### ビルド

```bash
# 依存関係のインストール
go mod tidy
cd frontend && npm install && cd ..

# プラグインバイナリをビルド（初回および変更時に必要）
task plugins

# プロダクションビルド（プラグインビルドを含む）
task build
```

ビルド成果物は `build/bin/` に生成されます。タスクランナーは [go-task](https://taskfile.dev/) を使用（`Taskfile.yaml`）。

## 開発

```bash
# 開発モード（プラグインビルド → ホットリロード起動）
task dev

# Go バインディングの再生成
wails generate module

# Go コードのビルド確認
go build ./...

# 全テスト実行
task test
```

## 使い方

### サーバー設定

1. 「サーバー」タブにサーバー一覧が表示される（初期状態: Modbus TCP）
2. 「サーバーを追加」ボタンで新しいプロトコルのサーバーを追加
3. 各サーバーの「設定」ボタンをクリックして接続設定（アドレス、ポート等）を変更
4. 「開始」ボタンでサーバーを起動（各サーバーを独立して開始/停止可能）
5. 複数のサーバーを同時に起動することが可能

### UnitID 応答設定

デフォルトでは全ての UnitID (1-247) に応答します。特定の UnitID への応答を無効にするには、該当のチェックボックスをオフにしてください。（UnitID をサポートするプロトコルのみ表示）

### レジスタ操作

1. 「レジスタ」タブを選択
2. 複数サーバーが起動している場合は上部のプロトコル選択で対象を切り替え
3. メモリエリアを選択して「一覧表示」サブタブでマトリクス表示
4. セルをクリックまたはキーボードで選択
5. Enter キーまたはダブルクリックで値を編集

### モニタリング

1. 「レジスタ」タブの「モニタリング」サブタブを選択
2. 「追加」ボタンで監視したいレジスタを登録
3. 複数サーバー起動時はプロトコルを選択してから、メモリエリア・開始アドレス・個数・ビット幅・エンディアン・表示形式を設定
4. 登録した項目の値がリアルタイムで更新される
5. 値をクリックして直接書き込み可能

### 変数管理

「変数」タブで IEC 61131-3 準拠の変数を管理できます。

1. 「変数を追加」ボタンで新しい変数を作成
2. 型カテゴリ（スカラー、配列、構造体）を選択
3. データ型を選択（STRING の場合はバイト長も指定）
4. 変数をクリックして値を編集
5. 「編集」ボタンで変数の名前・データタイプを変更（データタイプ変更時は値リセット、マッピングは保持）
6. 「マッピング」ボタンで個別にプロトコルのメモリアドレスにマッピング
7. 「一括マッピング編集」ボタンで全変数のマッピングをテーブル形式でまとめて設定
   - プロトコルを選択し、各変数のアドレスを入力して「一括保存」
   - アドレスが重複している場合は行がオレンジでハイライトされ、ダイアログ下部に警告バナーが表示される

#### 構造体型の管理

構造体型は「構造体型管理」ボタンから登録・編集できます。

- **新規登録**: 型名とフィールドを定義して「型を登録」
- **編集**: 既存の構造体型の「編集」ボタンをクリックして定義を変更し「更新」
- **削除**: 「削除」ボタンで構造体型を削除

フィールドはスカラー、配列、構造体のいずれかを指定可能（ネスト対応）。編集時は型名の変更はできませんが、フィールドの追加・削除・変更が可能です。

### スクリプト

JavaScript でカスタムロジックを記述できます。const/let が使用可能です。

```javascript
// プロトコル非依存の汎用 API（推奨）
const bit = plc.readBit("coil", 0);
plc.writeBit("coil", 0, !bit);

const word = plc.readWord("holding_register", 0);
plc.writeWord("holding_register", 0, word + 1);

// Modbus 互換 API（下位互換性のため残存）
const value = plc.getHoldingRegister(0);
plc.setHoldingRegister(0, value + 1);
```

実行時エラーはスクリプト一覧に表示されます（タイムスタンプ付き）。

#### 利用可能な API

**プロトコル非依存 API（推奨）**:

| メソッド                              | 説明                                        |
| ------------------------------------- | ------------------------------------------- |
| `plc.readBit(area, address)`          | 指定メモリエリアのビットを読み取り          |
| `plc.writeBit(area, address, value)`  | 指定メモリエリアのビットを書き込み          |
| `plc.readWord(area, address)`         | 指定メモリエリアのワード（16bit）を読み取り |
| `plc.writeWord(area, address, value)` | 指定メモリエリアのワード（16bit）を書き込み |

メモリエリアは Modbus の "coils", "discreteInputs", "holdingRegisters", "inputRegisters" です。

**変数アクセス API**:

| メソッド                                       | 説明                                |
| ---------------------------------------------- | ----------------------------------- |
| `plc.readVariable(name)`                       | 変数名で値を読み取り（文字列/数値） |
| `plc.writeVariable(name, value)`               | 変数名で値を書き込み                |
| `plc.readArrayElement(name, index)`            | 配列変数の要素を読み取り            |
| `plc.writeArrayElement(name, index, value)`    | 配列変数の要素を書き込み            |
| `plc.readStructField(name, fieldName)`         | 構造体変数のフィールドを読み取り    |
| `plc.writeStructField(name, fieldName, value)` | 構造体変数のフィールドを書き込み    |
| `plc.getVariables()`                           | 全変数名の一覧を取得                |

> **注意**: `plc.readVariable()` で LINT/ULINT の値が ±2^53 を超えていた場合、JavaScript の `Number` 精度の都合でコンソールに `[WARN]` が出力されます。大きな値を扱う場合は下記の **LINT/ULINT BigInt API** を使用してください。

**LINT/ULINT BigInt API（64ビット整数を精度損失なく操作）**:

JavaScript の `BigInt` 型（`n` サフィックス）を使用するため、±2^53 を超える値でも正確に計算できます。

| メソッド                       | 説明                                         |
| ------------------------------ | -------------------------------------------- |
| `plc.readLintBig(name)`        | LINT 変数を BigInt として読み取り            |
| `plc.writeLintBig(name, val)`  | BigInt または Number を LINT 変数に書き込み  |
| `plc.readUlintBig(name)`       | ULINT 変数を BigInt として読み取り           |
| `plc.writeUlintBig(name, val)` | BigInt または Number を ULINT 変数に書き込み |

```javascript
// LINT 変数に 1 加算する例（精度損失なし）
plc.writeLintBig("counter", plc.readLintBig("counter") + 1n);

// ULINT 変数を 16進リテラルで書き込む例
plc.writeUlintBig("flags", 0xffffffffffffffffn);
```

**TIME/DATE シンタックスシュガー**:

読み取り〜数値変換〜書き込みをワンステップで実行します。

| メソッド                             | 型            | 内部値 | 説明                                  |
| ------------------------------------ | ------------- | ------ | ------------------------------------- |
| `plc.readTimeMs(name)`               | TIME          | ms     | TIME 変数をミリ秒（数値）で読み取り   |
| `plc.writeTimeMs(name, ms)`          | TIME          | ms     | ミリ秒（数値）を TIME 変数に書き込み  |
| `plc.readDateSec(name)`              | DATE          | 秒     | DATE 変数を Unix 秒（数値）で読み取り |
| `plc.writeDateSec(name, sec)`        | DATE          | 秒     | Unix 秒（数値）を DATE 変数に書き込み |
| `plc.readTimeOfDayMs(name)`          | TIME_OF_DAY   | ms     | TOD 変数をミリ秒（数値）で読み取り    |
| `plc.writeTimeOfDayMs(name, ms)`     | TIME_OF_DAY   | ms     | ミリ秒（数値）を TOD 変数に書き込み   |
| `plc.readDateAndTimeSec(name)`       | DATE_AND_TIME | 秒     | DT 変数を Unix 秒（数値）で読み取り   |
| `plc.writeDateAndTimeSec(name, sec)` | DATE_AND_TIME | 秒     | Unix 秒（数値）を DT 変数に書き込み   |

```javascript
// TIME_OF_DAY 変数に 1 秒加算する例
plc.writeTimeOfDayMs("TOD1", plc.readTimeOfDayMs("TOD1") + 1000);

// DATE 変数を 1 日進める例
plc.writeDateSec("date1", plc.readDateSec("date1") + 86400);
```

**TIME/DATE 文字列変換 API**:

| メソッド                     | 説明                                           |
| ---------------------------- | ---------------------------------------------- |
| `plc.parseTime(str)`         | `"T#1h30m"` → ミリ秒（number）                 |
| `plc.formatTime(ms)`         | ミリ秒（number） → `"T#1h30m"`                 |
| `plc.parseDate(str)`         | `"D#2024-01-01"` → Unix 秒（number）           |
| `plc.formatDate(sec)`        | Unix 秒（number） → `"D#2024-01-01"`           |
| `plc.parseTimeOfDay(str)`    | `"TOD#12:30:15"` → ミリ秒（number）            |
| `plc.formatTimeOfDay(ms)`    | ミリ秒（number） → `"TOD#12:30:15"`            |
| `plc.parseDateAndTime(str)`  | `"DT#2024-01-01-12:30:15"` → Unix 秒（number） |
| `plc.formatDateAndTime(sec)` | Unix 秒（number） → `"DT#2024-01-01-12:30:15"` |

**Modbus 互換 API**:

| メソッド                                 | 説明                         |
| ---------------------------------------- | ---------------------------- |
| `plc.getCoil(address)`                   | コイルの値を取得             |
| `plc.setCoil(address, value)`            | コイルの値を設定             |
| `plc.getDiscreteInput(address)`          | ディスクリート入力の値を取得 |
| `plc.setDiscreteInput(address, value)`   | ディスクリート入力の値を設定 |
| `plc.getHoldingRegister(address)`        | 保持レジスタの値を取得       |
| `plc.setHoldingRegister(address, value)` | 保持レジスタの値を設定       |
| `plc.getInputRegister(address)`          | 入力レジスタの値を取得       |
| `plc.setInputRegister(address, value)`   | 入力レジスタの値を設定       |

### エクスポート/インポート

「サーバー」タブの「エクスポート」「インポート」ボタンで、以下のデータを JSON ファイルとして保存・復元できます：

- 全サーバーの設定（プロトコル、接続設定）—複数サーバーの構成を含む
- UnitID 応答設定
- スクリプト
- 変数定義・マッピング設定
- モニタリング項目（プロトコル情報含む）

レジスタ（メモリ）の値はエクスポート対象外です。

HTTP API 経由でもエクスポート/インポートが可能です（後述）。

### REST HTTP API

アプリ起動時に自動で HTTP REST API サーバーが起動します。デフォルトポートは **8765**。

ヘッダーに表示された URL（例: `http://localhost:8765/api`）の右にある ✎ ボタンでポートを変更できます。変更は次回起動時にも反映されます。

#### 主なエンドポイント

**サーバー管理**

```bash
# サーバー一覧取得
curl http://localhost:8765/api/servers

# Modbus TCP サーバーを起動
curl -X POST http://localhost:8765/api/servers/modbus-tcp/start

# Modbus TCP サーバーを停止
curl -X POST http://localhost:8765/api/servers/modbus-tcp/stop
```

**レジスタ読み書き**

```bash
# ホールディングレジスタをアドレス0から10ワード読み取り
curl "http://localhost:8765/api/memory/modbus-tcp/holdingRegisters/words?address=0&count=10"

# アドレス5に値100を書き込み
curl -X PUT http://localhost:8765/api/memory/modbus-tcp/holdingRegisters/words/5 \
  -H "Content-Type: application/json" -d '{"value": 100}'

# コイルのアドレス3を読み取り（8点）
curl "http://localhost:8765/api/memory/modbus-tcp/coils/bits?address=3&count=8"

# コイルのアドレス0をONに書き込み
curl -X PUT http://localhost:8765/api/memory/modbus-tcp/coils/bits/0 \
  -H "Content-Type: application/json" -d '{"value": true}'
```

**変数管理**

```bash
# 変数一覧取得
curl http://localhost:8765/api/variables

# 変数を作成
curl -X POST http://localhost:8765/api/variables \
  -H "Content-Type: application/json" \
  -d '{"name": "counter", "dataType": "INT", "value": 0}'

# 変数の値を更新（IDは変数一覧から取得）
curl -X PUT http://localhost:8765/api/variables/{id}/value \
  -H "Content-Type: application/json" -d '{"value": 42}'
```

**プロジェクトのエクスポート/インポート**

```bash
# プロジェクトをファイルに保存
curl http://localhost:8765/api/project/export > project.json

# プロジェクトをファイルから復元
curl -X POST http://localhost:8765/api/project/import \
  -H "Content-Type: application/json" -d @project.json
```

PowerShell の場合:

```powershell
# エクスポート
(Invoke-WebRequest "http://localhost:8765/api/project/export").Content | Out-File project.json

# インポート
Invoke-WebRequest -Method Post "http://localhost:8765/api/project/import" `
  -ContentType "application/json" -InFile project.json
```

## アーキテクチャ

```
┌──────────────────────────────────────────────────────────┐
│  ホストプロセス（Wails アプリ）                           │
│  PLCService / HostGrpcServer / PluginProcessManager      │
└─────────────┬──────────────────────────┬─────────────────┘
              │ gRPC (localhost)          │ gRPC (localhost)
              ▼                          ▼
┌─────────────────────┐   ┌─────────────────────────────┐
│ modbus-plugin.exe   │   │ opcua-plugin.exe             │
│ PluginService       │   │ PluginService                │
│ DataStoreService    │   │ DataStoreService（空実装）   │
└─────────────────────┘   └─────────────────────────────┘
```

```
cmd/
├── modbus-plugin/        # Modbus プラグインバイナリ
│   ├── internal/modbus/  # Modbus プロトコル実装（ホスト側から Go internal/ 制約で隔離）
│   └── server/           # gRPC サーバー実装
└── opcua-plugin/         # OPC UA プラグインバイナリ
    ├── internal/opcua/   # OPC UA プロトコル実装（ホスト側から Go internal/ 制約で隔離）
    └── server/           # gRPC サーバー実装
internal/
├── domain/           # ドメイン層
│   ├── protocol/     # プロトコル共通インターフェース
│   ├── variable/     # 変数エンティティ（IEC 61131-3データ型）
│   ├── script/       # スクリプトエンティティ
│   └── datastore/    # DataStore 共通定義
├── application/      # アプリケーション層（プロトコル固有実装に依存しない）
│   ├── plc_service.go  # メインサービス（複数サーバー管理・モニタリング・変数管理含む）
│   └── dto.go          # DTO 定義（ServerInstanceDTO, ServerConfigDTO 等）
└── infrastructure/   # インフラ層（ホスト側のみ。プロトコル固有実装は含まない）
    ├── plugin/       # プラグインインフラ（HostGrpcServer, RemoteFactory 等）
    ├── httpapi/      # REST HTTP API サーバー（net/http、デフォルトポート 8765）
    ├── adapter/      # 変数とDataStoreのアダプタ・VariableStoreAccessor
    └── scripting/    # JavaScript エンジン（goja）
```

PLCService は `servers map[protocol.ProtocolType]*serverInstance` で複数のサーバーインスタンスを管理します。各プロトコル（`"modbus-tcp"`, `"modbus-rtu"`, `"modbus-ascii"`, `"opcua"`）は gRPC プラグインプロセスとして別プロセスで動作し、ホストは `RemoteServerFactory` / `RemoteProtocolServer` / `RemoteDataStore` を通じて gRPC 経由で操作します。

### プラグイン仕様（他言語での実装向け）

`plugins/<name>/plugin.json` の形式:

```json
{
  "name": "My Plugin",
  "entrypoint": "my-plugin.exe",
  "version": "1.0.0"
}
```

プラグインは起動時にランダムポートで gRPC サーバーを起動し、stdout に `GRPC_PORT=<port>` を出力します。ホストがこれを読み取って gRPC 接続を確立します。`PluginService` と `DataStoreService` を同一ポートで実装する必要があります（proto 定義: `proto/plugin.proto`）。

## 設定ファイル

設定は以下の場所に自動保存されます：

| ファイル                 | 内容                                      |
| ------------------------ | ----------------------------------------- |
| `monitoring_config.json` | モニタリング項目の登録内容                |
| `httpapi_config.json`    | HTTP API のポート番号（デフォルト: 8765） |

- **Windows**: `%APPDATA%\PLCSimulator\`
- **macOS**: `~/Library/Application Support/PLCSimulator/`
- **Linux**: `~/.config/PLCSimulator/`

## 技術スタック

- **バックエンド**: Go + [Wails v2](https://wails.io/)
- **フロントエンド**: React + TypeScript + Vite（ビルドターゲット: Chrome 87+, Edge 88+, Firefox 78+, **Safari 14+**）
- **プラグイン IPC**: gRPC + protobuf（[google.golang.org/grpc](https://pkg.go.dev/google.golang.org/grpc)）
- **Modbus**: [simonvetter/modbus](https://github.com/simonvetter/modbus)
- **OPC UA**: [gopcua](https://github.com/gopcua/opcua) v0.8.0
- **JavaScript エンジン**: [goja](https://github.com/dop251/goja)
- **タスクランナー**: [go-task](https://taskfile.dev/)

> **注**: フロントエンドのビルドターゲットに Safari 14+ が必要なのは、LINT/ULINT の値編集に JavaScript `BigInt` を使用しているためです。

## ライセンス

MIT License

Copyright (c) 2026 bamchoh
