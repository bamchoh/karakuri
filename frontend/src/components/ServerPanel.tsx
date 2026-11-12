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
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { application } from "../../wailsjs/go/models";
import { Square, Play, SquarePlus } from "lucide-react";
import { AddServerDialog } from "./AddServerDialog";
import { ServerConfigPane, ServerConfigPaneHandle } from "./ServerConfigPane";
import { serverStatusClass } from "./serverStatus";
import { UnsavedChangesDialog } from "./UnsavedChangesDialog";

// サーバー画面: 左にサーバーの一覧（起動・停止）、右に選択したサーバーの設定（ServerConfigPane）。
// 設定を保存しないままサーバーの切り替え・起動をしようとしたときは、確認してから実行する。
export interface ServerPanelHandle {
  save: () => Promise<void>;
  revert: () => Promise<void>;
}
interface ServerPanelProps {
  onDirtyChange?: (dirty: boolean) => void;
}

export const ServerPanel = forwardRef<ServerPanelHandle, ServerPanelProps>(
  function ServerPanel({ onDirtyChange }, ref) {
    const [serverInstances, setServerInstances] = useState<
      application.ServerInstanceDTO[]
    >([]);
    const [error, setError] = useState<string | null>(null);
    const [serialPorts, setSerialPorts] = useState<string[]>([]);
    const [protocols, setProtocols] = useState<application.ProtocolInfoDTO[]>(
      [],
    );
    const [selectedProtocolType, setSelectedProtocolType] = useState<
      string | null
    >(null);
    const [selectedPaneDirty, setSelectedPaneDirty] = useState(false);
    const [pendingAction, setPendingAction] = useState<{
      execute: () => Promise<void>;
      discardLabel: string;
      saveLabel: string;
    } | null>(null);
    const configPaneRef = useRef<ServerConfigPaneHandle>(null);

    useImperativeHandle(ref, () => ({
      save: async () => {
        await configPaneRef.current?.save();
      },
      revert: async () => {
        await configPaneRef.current?.revert();
      },
    }));

    const handlePaneDirtyChange = useCallback(
      (dirty: boolean) => {
        setSelectedPaneDirty(dirty);
        onDirtyChange?.(dirty);
      },
      [onDirtyChange],
    );

    // サーバー追加ダイアログ
    const [isAddDialogOpen, setIsAddDialogOpen] = useState(false);

    useEffect(() => {
      loadInitialData();
      const offServer = EventsOn(
        "plc:server-changed",
        (instances: application.ServerInstanceDTO[]) => {
          const list = instances || [];
          setServerInstances(list);
          // 選択中サーバーが消えた場合は先頭を選択
          setSelectedProtocolType((prev) => {
            if (prev && list.some((i) => i.protocolType === prev)) return prev;
            return list.length > 0 ? list[0].protocolType : null;
          });
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

    const loadInitialData = async () => {
      try {
        const [instances, protos, ports] = await Promise.all([
          GetServerInstances(),
          GetAvailableProtocols(),
          GetSerialPorts(),
        ]);
        const list = instances || [];
        setServerInstances(list);
        setProtocols(protos || []);
        setSerialPorts(ports || []);
        if (list.length > 0) setSelectedProtocolType(list[0].protocolType);
      } catch (e) {
        setError(String(e));
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
      await configPaneRef.current?.save();
      await action.execute();
    };

    const handleConfirmDiscard = async () => {
      if (!pendingAction) return;
      const action = pendingAction;
      setPendingAction(null);
      await configPaneRef.current?.revert();
      await action.execute();
    };

    const handleCancelAction = () => {
      setPendingAction(null);
    };

    const handleSelectTab = (protocolType: string) => {
      if (protocolType === selectedProtocolType) return;
      requestAction(
        async () => setSelectedProtocolType(protocolType),
        "保存せずに切り替え",
        "保存してから切り替え",
      );
    };

    const handleStart = (protocolType: string) => {
      const isSameTab = protocolType === selectedProtocolType;
      if (isSameTab) {
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

    const handleRemove = async (protocolType: string) => {
      try {
        setError(null);
        await RemoveServer(protocolType);
        const list = (await GetServerInstances()) || [];
        setServerInstances(list);
        setSelectedProtocolType(list.length > 0 ? list[0].protocolType : null);
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
        const list = (await GetServerInstances()) || [];
        setServerInstances(list);
        setSelectedProtocolType(protocolType);
        setIsAddDialogOpen(false);
      } catch (e) {
        setError(String(e));
      }
    };

    const selectedInstance =
      serverInstances.find((i) => i.protocolType === selectedProtocolType) ??
      null;

    return (
      <div className="panel panel-fill">
        {error && <div className="error-message">{error}</div>}

        <div className="server-vertical-tabs">
          {/* 左ペイン: タブリスト */}
          <div className="server-tab-list">
            <div className="server-panel-header">
              <span>SERVERS</span>
              <div className="panel-header-actions">
                <button
                  onClick={handleOpenAddDialog}
                  disabled={availableProtocols.length === 0}
                  className="toolbar-icon-button"
                >
                  <SquarePlus size={14} />
                </button>
              </div>
            </div>

            {serverInstances.map((instance) => {
              const isSelected = instance.protocolType === selectedProtocolType;
              const statusClass = serverStatusClass(instance.status);
              const isRunning = instance.status === "Running";
              return (
                <div
                  key={instance.protocolType}
                  className={`server-tab-item${isSelected ? " selected" : ""}`}
                  onClick={() => handleSelectTab(instance.protocolType)}
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
          </div>

          {/* 右ペイン: 設定 */}
          {serverInstances.length === 0 ? (
            <div className="server-instance-empty">
              サーバーが登録されていません。
              <SquarePlus className="icon-left" size={14} />
              ボタンでサーバーを追加してください。
            </div>
          ) : (
            <div className="server-tab-content">
              {selectedInstance ? (
                <ServerConfigPane
                  key={selectedInstance.protocolType}
                  ref={configPaneRef}
                  instance={selectedInstance}
                  serialPorts={serialPorts}
                  onRefreshPorts={handleRefreshPorts}
                  onRemove={handleRemove}
                  onDirtyChange={handlePaneDirtyChange}
                />
              ) : (
                <div className="server-instance-empty">
                  サーバーを選択してください。
                </div>
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
      </div>
    );
  },
);
