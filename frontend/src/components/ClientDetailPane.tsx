import {
  useState,
  forwardRef,
  useImperativeHandle,
  useEffect,
  useCallback,
} from "react";
import {
  UpdateClient,
  DisconnectClient,
  SendClientRequest,
  ClearClientCommandLogs,
  GetClientSavedCommands,
  AddClientSavedCommand,
  UpdateClientSavedCommand,
  SendClientSavedCommand,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { ChevronDown, ChevronRight, Trash2, Unplug } from "lucide-react";
import { DynamicField } from "./DynamicField";
import { ClientCommandDialog, CommandSaveInput } from "./ClientCommandDialog";
import { ClientSavedCommands } from "./ClientSavedCommands";
import { ClientCommandLog } from "./ClientCommandLog";
import { ClientPollingView } from "./ClientPollingView";

export type ClientSubTab = "command" | "polling";

// 親（通信画面）から、保存していない接続先の設定を保存・破棄するための操作
export interface ClientDetailPaneHandle {
  save: () => Promise<void>;
  revert: () => Promise<void>;
}

interface ClientDetailPaneProps {
  client: application.ClientDTO;
  protocol: application.ClientProtocolDTO;
  logs: application.ClientCommandLogDTO[]; // この接続先の履歴（新しい順）
  // 以下の 2 つは、接続先を選び直しても保つよう、親が持つ
  subTab: ClientSubTab;
  onSubTabChange: (tab: ClientSubTab) => void;
  settingsOpen: boolean; // 接続先の設定の欄を開いているか
  onSettingsOpenChange: (open: boolean) => void;
  onDirtyChange: (dirty: boolean) => void;
  onClientsChanged: () => Promise<unknown>;
  onLogsChanged: () => Promise<void>;
  onRemove: (id: string) => void;
}

const noPorts: string[] = [];
const noop = () => {};

// 設定の欄を閉じているときに出す 1 行の要約（例: "ホスト: 127.0.0.1 / ポート: 502"）
function settingsSummary(
  protocol: application.ClientProtocolDTO,
  settings: Record<string, any>,
): string {
  return protocol.connectionFields
    .map((f) => `${f.label}: ${settings[f.name] ?? f.default ?? ""}`)
    .join(" / ");
}

// 通信画面の右側（クライアントを選んだとき）: 接続先の設定・コマンドの組み立て・結果と履歴・ポーリング。
// 親が key に接続先の ID を渡すので、選び直すたびに作り直す
export const ClientDetailPane = forwardRef<
  ClientDetailPaneHandle,
  ClientDetailPaneProps
>(function ClientDetailPane(
  {
    client,
    protocol,
    logs,
    subTab,
    onSubTabChange,
    settingsOpen,
    onSettingsOpenChange,
    onDirtyChange,
    onClientsChanged,
    onLogsChanged,
    onRemove,
  },
  ref,
) {
  const [name, setName] = useState(client.name);
  const [settings, setSettings] = useState<Record<string, any>>(
    client.settings || {},
  );
  const [dirty, setDirty] = useState(false);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedSeq, setSelectedSeq] = useState<number | null>(null);

  // 保存したコマンド
  const [savedCommands, setSavedCommands] = useState<
    application.ClientSavedCommandDTO[]
  >([]);
  // コマンドのダイアログ（editing が null なら新しいコマンド）。閉じているときは null
  const [dialog, setDialog] = useState<{
    editing: application.ClientSavedCommandDTO | null;
  } | null>(null);

  const loadSavedCommands = useCallback(async () => {
    try {
      setSavedCommands((await GetClientSavedCommands(client.id)) || []);
    } catch (e) {
      setError(String(e));
    }
  }, [client.id]);

  useEffect(() => {
    loadSavedCommands();
  }, [loadSavedCommands]);

  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);

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

  useImperativeHandle(ref, () => ({
    save: async () => {
      await saveSettings();
    },
    revert: async () => {
      revertSettings();
    },
  }));

  const handleSettingChange = (fieldName: string, value: any) => {
    setSettings((prev) => ({ ...prev, [fieldName]: value }));
    setDirty(true);
  };

  // 履歴のリクエスト（16 進）を送り直す。保存していない設定があれば、先に保存する
  const send = async (requestHex: string) => {
    if (sending) return;
    try {
      setError(null);
      await sendFromDialog(requestHex);
    } catch (e) {
      setError(String(e));
    }
  };

  // 保存したコマンドを送る（履歴にはコマンドの名前が付く）
  const sendSaved = async (command: application.ClientSavedCommandDTO) => {
    if (sending) return;
    if (dirty && !(await saveSettings())) return;
    setSending(true);
    try {
      setError(null);
      const entry = await SendClientSavedCommand(client.id, command.id);
      setSelectedSeq(entry.seq);
    } catch (e) {
      setError(String(e));
    } finally {
      setSending(false);
      await Promise.all([onLogsChanged(), onClientsChanged()]);
    }
  };

  // ダイアログから送る。失敗（16 進が読めないなど）は例外のまま返し、ダイアログが表示する
  const sendFromDialog = async (
    requestHex: string,
  ): Promise<application.ClientCommandLogDTO> => {
    if (dirty && !(await saveSettings())) {
      throw new Error("接続先の設定を保存できませんでした");
    }
    setSending(true);
    try {
      const entry = await SendClientRequest(client.id, requestHex);
      setSelectedSeq(entry.seq);
      return entry;
    } finally {
      setSending(false);
      await Promise.all([onLogsChanged(), onClientsChanged()]);
    }
  };

  // ダイアログの内容を保存する（編集中のコマンドがあれば上書き）。失敗は例外のまま返し、ダイアログが表示する
  const saveFromDialog = async (input: CommandSaveInput) => {
    const dto = application.ClientSavedCommandInputDTO.createFrom(input);
    const editing = dialog?.editing;
    if (editing) {
      await UpdateClientSavedCommand(client.id, editing.id, dto);
    } else {
      await AddClientSavedCommand(client.id, dto);
    }
    await loadSavedCommands();
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
          <span className="server-instance-variant">
            クライアント（{client.displayName}）
          </span>
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
        <button
          className="vscode-setting-category client-settings-toggle"
          onClick={() => onSettingsOpenChange(!settingsOpen)}
          aria-expanded={settingsOpen}
        >
          {settingsOpen ? (
            <ChevronDown size={16} />
          ) : (
            <ChevronRight size={16} />
          )}
          接続先の設定
          {!settingsOpen && (
            <span className="client-settings-summary">
              {settingsSummary(protocol, settings)}
            </span>
          )}
        </button>
        {settingsOpen && (
          <>
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
          </>
        )}
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
            <div className="vscode-setting-category">
              保存したコマンド（{savedCommands.length}）
            </div>
            <ClientSavedCommands
              clientId={client.id}
              commands={savedCommands}
              onChanged={loadSavedCommands}
              sending={sending}
              onNew={() => setDialog({ editing: null })}
              onEdit={(c) => setDialog({ editing: c })}
              onSend={sendSaved}
            />
            {dialog && (
              <ClientCommandDialog
                protocol={protocol}
                editing={dialog.editing}
                sending={sending}
                onSend={sendFromDialog}
                onSave={saveFromDialog}
                onClose={() => setDialog(null)}
              />
            )}

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
});
