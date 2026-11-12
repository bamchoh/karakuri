import { useState, useEffect, useCallback } from "react";
import {
  GetClientProtocols,
  GetClients,
  AddClient,
  RemoveClient,
  GetClientCommandLogs,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { SquarePlus } from "lucide-react";
import { ClientDetailPane, ClientSubTab } from "./ClientDetailPane";

// 一覧（接続中・ポーリング中の表示）を読み直す間隔
const CLIENTS_REFRESH_MS = 1000;

// クライアント画面: 外部の機器（Modbus TCP サーバーなど）につないで、コマンドを送る。
// 左に接続先の一覧、右に選んだ接続先の設定・コマンドの組み立て・結果・履歴を並べる。
// 接続先の設定項目とコマンドの定義はプロトコルの実装（GetClientProtocols）から受け取る。
export function ClientPanel() {
  const [protocols, setProtocols] = useState<application.ClientProtocolDTO[]>(
    [],
  );
  const [clients, setClients] = useState<application.ClientDTO[]>([]);
  const [logs, setLogs] = useState<application.ClientCommandLogDTO[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [addProtocol, setAddProtocol] = useState<string>("");
  const [error, setError] = useState<string | null>(null);
  const [subTab, setSubTab] = useState<ClientSubTab>("command");

  const loadClients = useCallback(async () => {
    const list = (await GetClients()) || [];
    setClients(list);
    return list;
  }, []);

  const loadLogs = useCallback(async () => {
    setLogs((await GetClientCommandLogs()) || []);
  }, []);

  useEffect(() => {
    (async () => {
      try {
        const protos = (await GetClientProtocols()) || [];
        setProtocols(protos);
        if (protos.length > 0) setAddProtocol(protos[0].protocolType);
        const list = await loadClients();
        if (list.length > 0) setSelectedId(list[0].id);
        await loadLogs();
      } catch (e) {
        setError(String(e));
      }
    })();
  }, [loadClients, loadLogs]);

  // ポーリング中は接続の状態が変わるので、一覧を読み直す
  useEffect(() => {
    const timer = setInterval(() => {
      loadClients().catch(() => {});
    }, CLIENTS_REFRESH_MS);
    return () => clearInterval(timer);
  }, [loadClients]);

  const handleAdd = async () => {
    if (!addProtocol) return;
    try {
      setError(null);
      const added = await AddClient(addProtocol, "");
      await loadClients();
      setSelectedId(added.id);
    } catch (e) {
      setError(String(e));
    }
  };

  const handleRemove = async (id: string) => {
    try {
      setError(null);
      await RemoveClient(id);
      const list = await loadClients();
      setSelectedId(list.length > 0 ? list[0].id : null);
    } catch (e) {
      setError(String(e));
    }
  };

  const selected = clients.find((c) => c.id === selectedId) ?? null;
  const selectedProtocol = selected
    ? (protocols.find((p) => p.protocolType === selected.protocolType) ?? null)
    : null;

  return (
    <div className="panel panel-fill">
      {error && <div className="error-message">{error}</div>}

      <div className="server-vertical-tabs">
        <div className="server-tab-list">
          <div className="server-panel-header">
            <span>CLIENTS</span>
            <div className="panel-header-actions">
              {protocols.length > 1 && (
                <select
                  className="client-add-protocol"
                  value={addProtocol}
                  onChange={(e) => setAddProtocol(e.target.value)}
                  aria-label="追加する接続先のプロトコル"
                >
                  {protocols.map((p) => (
                    <option key={p.protocolType} value={p.protocolType}>
                      {p.displayName}
                    </option>
                  ))}
                </select>
              )}
              <button
                onClick={handleAdd}
                disabled={protocols.length === 0}
                className="toolbar-icon-button"
                title="接続先を追加"
                aria-label="接続先を追加"
              >
                <SquarePlus size={14} />
              </button>
            </div>
          </div>

          {clients.map((c) => (
            <div
              key={c.id}
              className={`server-tab-item${c.id === selectedId ? " selected" : ""}`}
              onClick={() => setSelectedId(c.id)}
            >
              <div className="server-tab-left">
                <div
                  className={`server-status-dot ${c.connected ? "running" : "stopped"}`}
                  title={c.connected ? "接続中" : "未接続"}
                />
                <span className="server-tab-name">{c.name}</span>
                <span className="server-instance-variant">{c.displayName}</span>
                {c.polling && (
                  <span className="client-polling-badge" title="ポーリング中">
                    ポーリング中
                  </span>
                )}
              </div>
            </div>
          ))}
        </div>

        {clients.length === 0 ? (
          <div className="server-instance-empty">
            接続先が登録されていません。
            <SquarePlus className="icon-left" size={14} />
            ボタンで接続先を追加してください。
          </div>
        ) : (
          <div className="server-tab-content">
            {selected && selectedProtocol ? (
              <ClientDetailPane
                key={selected.id}
                client={selected}
                protocol={selectedProtocol}
                logs={logs.filter((l) => l.clientId === selected.id)}
                subTab={subTab}
                onSubTabChange={setSubTab}
                onClientsChanged={loadClients}
                onLogsChanged={loadLogs}
                onRemove={handleRemove}
              />
            ) : (
              <div className="server-instance-empty">
                接続先を選択してください。
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
