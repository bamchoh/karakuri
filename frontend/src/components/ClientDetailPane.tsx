import { useState } from "react";
import {
  UpdateClient,
  DisconnectClient,
  SendClientRequest,
  ClearClientCommandLogs,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { Trash2, Unplug } from "lucide-react";
import { DynamicField } from "./DynamicField";
import { ClientCommandComposer } from "./ClientCommandComposer";
import { ClientCommandLog } from "./ClientCommandLog";
import { ClientPollingView } from "./ClientPollingView";

export type ClientSubTab = "command" | "polling";

interface ClientDetailPaneProps {
  client: application.ClientDTO;
  protocol: application.ClientProtocolDTO;
  logs: application.ClientCommandLogDTO[]; // この接続先の履歴（新しい順）
  subTab: ClientSubTab; // 接続先を選び直しても保つよう、親が持つ
  onSubTabChange: (tab: ClientSubTab) => void;
  onClientsChanged: () => Promise<unknown>;
  onLogsChanged: () => Promise<void>;
  onRemove: (id: string) => void;
}

const noPorts: string[] = [];
const noop = () => {};

// クライアント画面の右側: 選んだ接続先の設定・コマンドの組み立て・結果と履歴。
// 親が key に接続先の ID を渡すので、選び直すたびに作り直す
export function ClientDetailPane({
  client,
  protocol,
  logs,
  subTab,
  onSubTabChange,
  onClientsChanged,
  onLogsChanged,
  onRemove,
}: ClientDetailPaneProps) {
  const [name, setName] = useState(client.name);
  const [settings, setSettings] = useState<Record<string, any>>(
    client.settings || {},
  );
  const [dirty, setDirty] = useState(false);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedSeq, setSelectedSeq] = useState<number | null>(null);

  const saveSettings = async (): Promise<boolean> => {
    try {
      setError(null);
      await UpdateClient(client.id, name, settings);
      setDirty(false);
      await onClientsChanged();
      return true;
    } catch (e) {
      setError(String(e));
      return false;
    }
  };

  const revertSettings = () => {
    setName(client.name);
    setSettings(client.settings || {});
    setDirty(false);
    setError(null);
  };

  const handleSettingChange = (fieldName: string, value: any) => {
    setSettings((prev) => ({ ...prev, [fieldName]: value }));
    setDirty(true);
  };

  // リクエスト（16 進）を送る。保存していない設定があれば、先に保存する
  const send = async (requestHex: string) => {
    if (sending) return;
    if (dirty && !(await saveSettings())) return;
    setSending(true);
    try {
      setError(null);
      const entry = await SendClientRequest(client.id, requestHex);
      setSelectedSeq(entry.seq);
    } catch (e) {
      setError(String(e));
    } finally {
      setSending(false);
      await Promise.all([onLogsChanged(), onClientsChanged()]);
    }
  };

  const handleDisconnect = async () => {
    try {
      setError(null);
      await DisconnectClient(client.id);
      await onClientsChanged();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleClearLogs = async () => {
    await ClearClientCommandLogs();
    setSelectedSeq(null);
    await onLogsChanged();
  };

  const selectedLog =
    logs.find((l) => l.seq === selectedSeq) ??
    (selectedSeq === null ? logs[0] : undefined) ??
    null;

  return (
    <div className="server-config-pane">
      <div className="server-config-pane-header">
        <div className="server-config-pane-title">
          <span className="server-config-pane-name">{client.name}</span>
          <span
            className={`server-status-badge ${client.connected ? "running" : "stopped"}`}
          >
            {client.connected ? "接続中" : "未接続"}
          </span>
        </div>
        <div className="server-config-pane-actions">
          <button
            onClick={handleDisconnect}
            className="btn-secondary"
            disabled={!client.connected}
            title="切断（次の送信でつなぎ直す）"
            aria-label="切断"
          >
            <Unplug size={16} />
          </button>
          <button
            onClick={() => onRemove(client.id)}
            className="btn-danger"
            title="接続先を削除"
            aria-label="接続先を削除"
          >
            <Trash2 size={16} />
          </button>
        </div>
      </div>

      {error && <div className="error-message config-error">{error}</div>}

      <div className="vscode-settings-list">
        <div className="vscode-setting-category">接続先</div>
        <div className="vscode-setting-item">
          <div className="vscode-setting-label">名前</div>
          <div className="vscode-setting-control">
            <input
              type="text"
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                setDirty(true);
              }}
              aria-label="名前"
            />
          </div>
        </div>
        {protocol.connectionFields.map((field) => (
          <DynamicField
            key={field.name}
            field={field}
            value={settings[field.name]}
            onChange={(v) => handleSettingChange(field.name, v)}
            disabled={false}
            serialPorts={noPorts}
            onRefreshPorts={noop}
          />
        ))}
        {dirty && (
          <div className="client-settings-actions">
            <span className="client-note">
              保存していない変更があります（送信・ポーリングの開始で保存します）
            </span>
            <button className="btn-secondary" onClick={revertSettings}>
              元に戻す
            </button>
            <button className="btn-primary" onClick={saveSettings}>
              保存
            </button>
          </div>
        )}

        <div className="sub-tab-nav client-sub-tabs">
          <button
            className={`sub-tab-button ${subTab === "command" ? "active" : ""}`}
            onClick={() => onSubTabChange("command")}
          >
            コマンド
          </button>
          <button
            className={`sub-tab-button ${subTab === "polling" ? "active" : ""}`}
            onClick={() => onSubTabChange("polling")}
          >
            ポーリング{client.polling ? "（実行中）" : ""}
          </button>
        </div>

        {subTab === "command" ? (
          <>
            <ClientCommandComposer
              protocol={protocol}
              sending={sending}
              onSend={send}
            />

            <div className="vscode-setting-category">結果と履歴</div>
            <ClientCommandLog
              logs={logs}
              selected={selectedLog}
              sending={sending}
              onSelect={setSelectedSeq}
              onResend={send}
              onClear={handleClearLogs}
            />
          </>
        ) : (
          <ClientPollingView
            client={client}
            protocol={protocol}
            beforeStart={async () => !dirty || (await saveSettings())}
            onClientsChanged={onClientsChanged}
          />
        )}
      </div>
    </div>
  );
}
