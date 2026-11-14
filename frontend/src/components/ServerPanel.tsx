import {
  useState,
  useEffect,
  useCallback,
  useRef,
  forwardRef,
  useImperativeHandle,
} from "react";
import {
  GetServerInstances,
  AddServer,
  RemoveServer,
  StartServer,
  StopServer,
  GetSerialPorts,
  GetAvailableProtocols,
  AddClient,
  RemoveClient,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { application } from "../../wailsjs/go/models";
import { Square, Play, SquarePlus } from "lucide-react";
import { AddServerDialog } from "./AddServerDialog";
import { AddClientDialog } from "./AddClientDialog";
import {
  ServerConfigPane,
  ServerConfigPaneHandle,
  ServerSubTab,
} from "./ServerConfigPane";
import { RegisterListView } from "./RegisterListView";
import { MonitoringView } from "./MonitoringView";
import { ClientDetailPane, ClientSubTab } from "./ClientDetailPane";
import { serverStatusClass } from "./serverStatus";
import { UnsavedChangesDialog } from "./UnsavedChangesDialog";
import { useClients } from "./useClients";

// 通信画面: 左にサーバー（Karakuri が待ち受ける側）とクライアント（外部の機器へつなぐ側・マスター）の一覧、
// 右に選んだものの設定（サーバーは ServerConfigPane、クライアントは ClientDetailPane）。
// 設定を保存しないまま、別のものへの切り替え・起動をしようとしたときは、確認してから実行する。
export interface ServerPanelHandle {
  save: () => Promise<void>;
  revert: () => Promise<void>;
}
interface ServerPanelProps {
  onDirtyChange?: (dirty: boolean) => void;
}

// 選んでいるもの。サーバーはプロトコルタイプ、クライアントは接続先の ID で指す
type Selection = { kind: "server" | "client"; id: string } | null;

const sameSelection = (a: Selection, b: Selection) =>
  a?.kind === b?.kind && a?.id === b?.id;

export const ServerPanel = forwardRef<ServerPanelHandle, ServerPanelProps>(
  function ServerPanel({ onDirtyChange }, ref) {
    const [serverInstances, setServerInstances] = useState<
      application.ServerInstanceDTO[]
    >([]);
    const [serversLoaded, setServersLoaded] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [serialPorts, setSerialPorts] = useState<string[]>([]);
    const [protocols, setProtocols] = useState<application.ProtocolInfoDTO[]>(
      [],
    );
    const {
      protocols: clientProtocols,
      clients,
      logs: clientLogs,
      loaded: clientsLoaded,
      error: clientsError,
      loadClients,
      loadLogs,
    } = useClients();
    const [selected, setSelected] = useState<Selection>(null);
    // サーバーの右側のタブ（設定 / レジスタ / モニタリング）。選び直しても保つ
    const [serverSubTab, setServerSubTab] = useState<ServerSubTab>("settings");
    // クライアントの右側で、選び直しても保つもの
    const [clientSubTab, setClientSubTab] = useState<ClientSubTab>("command");
    const [clientSettingsOpen, setClientSettingsOpen] = useState(true);

    const [selectedPaneDirty, setSelectedPaneDirty] = useState(false);
    const [pendingAction, setPendingAction] = useState<{
      execute: () => Promise<void>;
      discardLabel: string;
      saveLabel: string;
    } | null>(null);
    // 右側の設定（サーバー・クライアントのどちらも save / revert を持つ）
    const paneRef = useRef<ServerConfigPaneHandle>(null);

    useImperativeHandle(ref, () => ({
      save: async () => {
        await paneRef.current?.save();
      },
      revert: async () => {
        await paneRef.current?.revert();
      },
    }));

    const handlePaneDirtyChange = useCallback(
      (dirty: boolean) => {
        setSelectedPaneDirty(dirty);
        onDirtyChange?.(dirty);
      },
      [onDirtyChange],
    );

    // サーバー追加ダイアログ・接続先（クライアント）追加ダイアログ
    const [isAddDialogOpen, setIsAddDialogOpen] = useState(false);
    const [isAddClientDialogOpen, setIsAddClientDialogOpen] = useState(false);

    useEffect(() => {
      loadInitialData();
      const offServer = EventsOn(
        "plc:server-changed",
        (instances: application.ServerInstanceDTO[]) => {
          setServerInstances(instances || []);
        },
      );
      const offProtocols = EventsOn(
        "plc:protocols-changed",
        (protos: application.ProtocolInfoDTO[]) => {
          setProtocols(protos || []);
        },
      );
      return () => {
        offServer();
        offProtocols();
      };
    }, []);

    // 選んでいるものが消えたら（または何も選んでいなければ）、先頭のサーバー、なければ先頭のクライアントを選ぶ
    useEffect(() => {
      if (!serversLoaded || !clientsLoaded) return;
      const exists =
        selected &&
        (selected.kind === "server"
          ? serverInstances.some((i) => i.protocolType === selected.id)
          : clients.some((c) => c.id === selected.id));
      if (exists) return;
      if (serverInstances.length > 0) {
        setSelected({ kind: "server", id: serverInstances[0].protocolType });
      } else if (clients.length > 0) {
        setSelected({ kind: "client", id: clients[0].id });
      } else if (selected) {
        setSelected(null);
      }
    }, [selected, serverInstances, clients, serversLoaded, clientsLoaded]);

    const loadInitialData = async () => {
      try {
        const [instances, protos, ports] = await Promise.all([
          GetServerInstances(),
          GetAvailableProtocols(),
          GetSerialPorts(),
        ]);
        setServerInstances(instances || []);
        setProtocols(protos || []);
        setSerialPorts(ports || []);
      } catch (e) {
        setError(String(e));
      } finally {
        setServersLoaded(true);
      }
    };

    const doStart = async (protocolType: string) => {
      try {
        setError(null);
        await StartServer(protocolType);
        setServerInstances((await GetServerInstances()) || []);
      } catch (e) {
        setError(String(e));
      }
    };

    // 右側に保存していない変更があれば、確認してから execute を実行する
    const requestAction = (
      execute: () => Promise<void>,
      discardLabel: string,
      saveLabel: string,
    ) => {
      if (selectedPaneDirty) {
        setPendingAction({ execute, discardLabel, saveLabel });
      } else {
        execute();
      }
    };

    const handleConfirmSave = async () => {
      if (!pendingAction) return;
      const action = pendingAction;
      setPendingAction(null);
      await paneRef.current?.save();
      await action.execute();
    };

    const handleConfirmDiscard = async () => {
      if (!pendingAction) return;
      const action = pendingAction;
      setPendingAction(null);
      await paneRef.current?.revert();
      await action.execute();
    };

    const handleCancelAction = () => {
      setPendingAction(null);
    };

    const handleSelect = (next: Selection) => {
      if (sameSelection(next, selected)) return;
      requestAction(
        async () => setSelected(next),
        "保存せずに切り替え",
        "保存してから切り替え",
      );
    };

    const handleStart = (protocolType: string) => {
      const isSelected = sameSelection(selected, {
        kind: "server",
        id: protocolType,
      });
      if (isSelected) {
        requestAction(
          async () => doStart(protocolType),
          "保存せずに開始",
          "保存してから開始",
        );
      } else {
        doStart(protocolType);
      }
    };

    const handleStop = async (protocolType: string) => {
      try {
        setError(null);
        await StopServer(protocolType);
        setServerInstances((await GetServerInstances()) || []);
      } catch (e) {
        setError(String(e));
      }
    };

    const handleRemoveServer = async (protocolType: string) => {
      try {
        setError(null);
        await RemoveServer(protocolType);
        setServerInstances((await GetServerInstances()) || []);
      } catch (e) {
        setError(String(e));
      }
    };

    const handleRefreshPorts = async () => {
      try {
        setSerialPorts((await GetSerialPorts()) || []);
      } catch (e) {
        console.error("Failed to load serial ports:", e);
      }
    };

    const addedProtocolTypes = new Set(
      serverInstances.map((i) => i.protocolType),
    );
    const availableProtocols = protocols.filter(
      (p) => !addedProtocolTypes.has(p.type),
    );

    const hasVariantChoice = (instance: application.ServerInstanceDTO) =>
      (protocols.find((p) => p.type === instance.protocolType)?.variants
        .length ?? 0) > 1;

    const handleOpenAddDialog = () => {
      if (availableProtocols.length === 0) return;
      setIsAddDialogOpen(true);
    };

    const handleAddServer = async (protocolType: string, variantId: string) => {
      try {
        setError(null);
        await AddServer(protocolType, variantId);
        setServerInstances((await GetServerInstances()) || []);
        setSelected({ kind: "server", id: protocolType });
        setIsAddDialogOpen(false);
      } catch (e) {
        setError(String(e));
      }
    };

    // ダイアログで決めたプロトコルと名前で、接続先を追加する。
    // 追加すると右側が切り替わるので、保存していない変更があれば、ダイアログを閉じてから確認する
    const handleAddClient = (protocolType: string, name: string) => {
      if (!protocolType) return;
      setIsAddClientDialogOpen(false);
      requestAction(
        async () => {
          try {
            setError(null);
            const added = await AddClient(protocolType, name);
            await loadClients();
            setSelected({ kind: "client", id: added.id });
          } catch (e) {
            setError(String(e));
          }
        },
        "保存せずに追加",
        "保存してから追加",
      );
    };

    const handleRemoveClient = async (id: string) => {
      try {
        setError(null);
        await RemoveClient(id);
        await loadClients();
      } catch (e) {
        setError(String(e));
      }
    };

    const selectedInstance =
      selected?.kind === "server"
        ? (serverInstances.find((i) => i.protocolType === selected.id) ?? null)
        : null;
    const selectedClient =
      selected?.kind === "client"
        ? (clients.find((c) => c.id === selected.id) ?? null)
        : null;
    const selectedClientProtocol = selectedClient
      ? (clientProtocols.find(
          (p) => p.protocolType === selectedClient.protocolType,
        ) ?? null)
      : null;

    // レジスタを持たないサーバー（ノード公開型のプロトコル）には、設定のタブしかない
    const selectedServerHasRegisters =
      !!selectedInstance && !selectedInstance.supportsNodePublishing;
    const selectedServerTab: ServerSubTab = selectedServerHasRegisters
      ? serverSubTab
      : "settings";

    // 右側に何も出していなければ、保存していない変更もない（削除したものの変更が残らないように）
    const hasPane = !!selectedInstance || !!selectedClient;
    useEffect(() => {
      if (!hasPane) handlePaneDirtyChange(false);
    }, [hasPane, handlePaneDirtyChange]);

    const shownError = error ?? clientsError;

    return (
      <div className="panel panel-fill">
        {shownError && <div className="error-message">{shownError}</div>}

        <div className="server-vertical-tabs">
          {/* 左ペイン: サーバーとクライアントの一覧 */}
          <div className="server-tab-list">
            <div className="server-panel-header">
              <span>SERVERS</span>
              <div className="panel-header-actions">
                <button
                  onClick={handleOpenAddDialog}
                  disabled={availableProtocols.length === 0}
                  className="toolbar-icon-button"
                  title="サーバーを追加"
                  aria-label="サーバーを追加"
                >
                  <SquarePlus size={14} />
                </button>
              </div>
            </div>

            {serverInstances.map((instance) => {
              const isSelected = sameSelection(selected, {
                kind: "server",
                id: instance.protocolType,
              });
              const statusClass = serverStatusClass(instance.status);
              const isRunning = instance.status === "Running";
              return (
                <div
                  key={instance.protocolType}
                  className={`server-tab-item${isSelected ? " selected" : ""}`}
                  onClick={() =>
                    handleSelect({ kind: "server", id: instance.protocolType })
                  }
                >
                  <div className="server-tab-left">
                    <div
                      className={`server-status-dot ${statusClass}`}
                      title={instance.status}
                    />
                    <span className="server-tab-name">
                      {instance.displayName}
                    </span>
                    {/* バリアントは、選べるプロトコル（2 つ以上ある）のときだけ出す */}
                    {instance.variant && hasVariantChoice(instance) && (
                      <span className="server-instance-variant">
                        {instance.variant}
                      </span>
                    )}
                  </div>
                  <div className="server-actions">
                    <button
                      title={isRunning ? "停止" : "開始"}
                      onClick={(e) => {
                        e.stopPropagation();
                        isRunning
                          ? handleStop(instance.protocolType)
                          : handleStart(instance.protocolType);
                      }}
                      className="server-run-icon-button"
                    >
                      {isRunning ? <Square size={14} /> : <Play size={14} />}
                    </button>
                  </div>
                </div>
              );
            })}
            {serversLoaded && serverInstances.length === 0 && (
              <div className="server-list-empty">
                サーバーが登録されていません
              </div>
            )}

            <div className="server-panel-header client-group-header">
              <span title="Karakuri から外部の機器へつなぐ側（Modbus ではマスター）">
                CLIENTS
              </span>
              <div className="panel-header-actions">
                <button
                  onClick={() => setIsAddClientDialogOpen(true)}
                  disabled={clientProtocols.length === 0}
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
                className={`server-tab-item client-tab-item${
                  sameSelection(selected, { kind: "client", id: c.id })
                    ? " selected"
                    : ""
                }`}
                onClick={() => handleSelect({ kind: "client", id: c.id })}
              >
                <div className="server-tab-left">
                  <div
                    className={`server-status-dot ${c.connected ? "running" : "stopped"}`}
                    title={c.connected ? "接続中" : "未接続"}
                  />
                  <span className="server-tab-name">{c.name}</span>
                  <span className="server-instance-variant">
                    {c.displayName}
                  </span>
                  {c.polling && (
                    <span className="client-polling-badge" title="ポーリング中">
                      ポーリング中
                    </span>
                  )}
                  {c.subscribing && (
                    <span
                      className="client-polling-badge"
                      title="サブスクリプションで監視中"
                    >
                      監視中
                    </span>
                  )}
                </div>
              </div>
            ))}
            {clientsLoaded && clients.length === 0 && (
              <div className="server-list-empty">
                接続先が登録されていません
              </div>
            )}
          </div>

          {/* 右ペイン: 選んだものの設定 */}
          {selectedInstance ? (
            <div className="server-tab-content">
              <ServerConfigPane
                key={`server:${selectedInstance.protocolType}`}
                ref={paneRef}
                instance={selectedInstance}
                serialPorts={serialPorts}
                onRefreshPorts={handleRefreshPorts}
                onRemove={handleRemoveServer}
                onDirtyChange={handlePaneDirtyChange}
                hasRegisters={selectedServerHasRegisters}
                tab={selectedServerTab}
                onTabChange={setServerSubTab}
              >
                {/* 一覧表示は、タブを切り替えても選んだエリアなどを保つため、マウントしたままにする */}
                <RegisterListView
                  protocolType={selectedInstance.protocolType}
                  active={selectedServerTab === "registers"}
                />
                {selectedServerTab === "monitoring" && (
                  <MonitoringView server={selectedInstance} />
                )}
              </ServerConfigPane>
            </div>
          ) : selectedClient && selectedClientProtocol ? (
            <div className="server-tab-content">
              <ClientDetailPane
                key={`client:${selectedClient.id}`}
                ref={paneRef}
                client={selectedClient}
                protocol={selectedClientProtocol}
                logs={clientLogs.filter(
                  (l) => l.clientId === selectedClient.id,
                )}
                subTab={clientSubTab}
                onSubTabChange={setClientSubTab}
                settingsOpen={clientSettingsOpen}
                onSettingsOpenChange={setClientSettingsOpen}
                onDirtyChange={handlePaneDirtyChange}
                onClientsChanged={loadClients}
                onLogsChanged={loadLogs}
                onRemove={handleRemoveClient}
              />
            </div>
          ) : (
            <div className="server-instance-empty">
              {serverInstances.length === 0 && clients.length === 0 ? (
                <>
                  <p>サーバーもクライアントも登録されていません。</p>
                  <p>
                    SERVERS の
                    <SquarePlus size={14} />
                    でサーバーを、CLIENTS の
                    <SquarePlus size={14} />
                    で外部の機器への接続先を追加してください。
                  </p>
                </>
              ) : (
                "左の一覧から選んでください。"
              )}
            </div>
          )}
        </div>

        {/* 未保存設定の確認ダイアログ */}
        {pendingAction && (
          <UnsavedChangesDialog
            discardLabel={pendingAction.discardLabel}
            saveLabel={pendingAction.saveLabel}
            onCancel={handleCancelAction}
            onDiscard={handleConfirmDiscard}
            onSave={handleConfirmSave}
          />
        )}

        {/* サーバー追加ダイアログ */}
        {isAddDialogOpen && (
          <AddServerDialog
            protocols={availableProtocols}
            onAdd={handleAddServer}
            onClose={() => setIsAddDialogOpen(false)}
          />
        )}

        {/* 接続先（クライアント）追加ダイアログ */}
        {isAddClientDialogOpen && (
          <AddClientDialog
            protocols={clientProtocols}
            onAdd={handleAddClient}
            onClose={() => setIsAddClientDialogOpen(false)}
          />
        )}
      </div>
    );
  },
);
