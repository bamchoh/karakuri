# プロトコル開発ガイド

Karakuri にプロトコルを追加する手順です。プロトコルの実装はアプリ本体に組み込みます（別の exe やプラグインにはしません）。追加したいプロトコルがある場合は、この形で Pull Request を送ってください。

## 仕組み

```
画面（React） ⇄ PLCService ⇄ 変数ストア
                    │
      protocol.ServerFactory などのインターフェース
                    │
          internal/protocols/<名前>/
```

- プロトコル固有のコード（プロトコルタイプの値、設定項目、メモリエリア、アドレスの書式）は、すべて `internal/protocols/<名前>/` に置きます
- 本体（`internal/application`、`internal/infrastructure`、`app.go`）と画面は、プロトコルの名前や固有の値を持ちません。次の情報だけで動きます
  - 能力フラグ（`ProtocolCapabilities`）
  - 設定項目の定義（`GetConfigFields()`）。設定画面はここから自動で作られます
  - メモリエリアの属性（`MemoryArea`）
  - ノードアドレス（`NodeAddressResolver`。ノード公開型のプロトコルだけ）
- インターフェースの定義は `internal/domain/protocol/server.go` にあります

## 追加の手順

1. `internal/protocols/<名前>/` を作り、次を実装する

   | ファイル | 実装するもの |
   |---|---|
   | `factory.go` | `protocol.ServerFactory`。プロトコルタイプの定数もここで定義する |
   | `server.go` | `protocol.ProtocolServer` |
   | `datastore.go` | `protocol.DataStore`。レジスタを持たないプロトコルは空の実装にする |

2. `internal/protocols/protocols.go` の `Factories()` に追加する
3. `internal/protocols/integration_test.go` に、実際のクライアントからつなぐ確認を足す
4. 本体と画面は変更しない（設定画面・サーバーの一覧・マッピングの画面は、自動で対応します）

既存の実装が参考になります。

- レジスタ型（メモリエリアを持つ）: `internal/protocols/modbus/`
- ノード公開型（変数をノードとして公開する）: `internal/protocols/opcua/`

## 必要に応じて実装するインターフェース

| インターフェース | 実装する場所 | 用途 |
|---|---|---|
| `UnitIDConfigurable` | サーバー | UnitID ごとの応答のオン・オフ。`ProtocolCapabilities.SupportsUnitID` を true にする |
| `VariableStoreInjector` | ファクトリー | 変数ストアを直接読み書きする（ノード公開型）。`SupportsNodePublishing` を true にする |
| `NodePublishingAware` | サーバー | 変数の公開設定が変わったときの通知を受ける |
| `NodeAddressResolver` | サーバー | 画面に出すノードアドレス（OPC UA なら NodeID）と、公開できない変数名の理由を返す |

## 実装するときの決まり

- **サーバーのオブジェクトは使い回される。** サーバーを削除するまで同じオブジェクトが使われ、`Start()` / `Stop()` は何度も呼ばれます。`Stop()` のあとにもう一度 `Start()` できるようにしてください。起動をまたいで残したい状態（UnitID の設定など）は、サーバーのオブジェクトに持たせます
- **クライアントの書き込みは、渡された DataStore に書く。** `CreateServer()` に渡される DataStore は、変数と同期する包みです。自分で作った DataStore に直接書くと、変数に反映されません
- **DataStore の書き込みは、戻った時点で変数に反映されている。** クライアントに応答を返す前に書き込めば、応答が届いた時点で変数も更新済みになります
- **panic させない。** プロトコルの処理で panic すると、アプリ全体が落ちます。外から来たデータは、長さと範囲を確かめてから使ってください
- **設定の検証は `MapToConfig()` で行う。** エラーを返すと、設定画面にそのまま表示されます
- **アドレスは内部では 0 始まり。** 画面で 1 始まりに見せたいエリアは `MemoryArea.OneOrigin` を true にします

## テスト

- プロトコルのフォルダの中に、実際のクライアントでつなぐテストを書く（`modbus/tcp_transport_test.go`、`opcua/server_test.go` が例）
- `internal/protocols/integration_test.go` で、アプリと同じ組み立て（`PLCService` に登録した状態）での動きを確認する。サーバーの起動・停止をまたいだ状態と、変数との同期が対象です
- 画面の確認は `frontend/e2e/`（Playwright）。手順は `frontend/e2e/README.md`
