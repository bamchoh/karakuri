import {
  useState,
  useEffect,
  useCallback,
  useRef,
  forwardRef,
  useImperativeHandle,
} from "react";
import { FocusTrap } from "./FocusTrap";
import {
  GetServerInstances,
  AddServer,
  RemoveServer,
  StartServer,
  StopServer,
  GetSerialPorts,
  GetAvailableProtocols,
  GetProtocolSchema,
  GetServerConfig,
  UpdateServerConfig,
  GetUnitIDSettings,
  SetUnitIDEnabled,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { application } from "../../wailsjs/go/models";
import { SpaceIcon, Square, Play, Trash2, SquarePlus } from "lucide-react";

// 動的フィールドコンポーネント
interface DynamicFieldProps {
  field: application.FieldDTO;
  value: any;
  onChange: (value: any) => void;
  disabled: boolean;
  serialPorts: string[];
  onRefreshPorts: () => void;
}

function DynamicField({
  field,
  value,
  onChange,
  disabled,
  serialPorts,
  onRefreshPorts,
}: DynamicFieldProps) {
  const displayValue = value ?? field.default;

  const renderControl = () => {
    switch (field.type) {
      case "text":
        return (
          <input
            type="text"
            value={displayValue || ""}
            onChange={(e) => onChange(e.target.value)}
            disabled={disabled}
          />
        );
      case "number":
        return (
          <input
            type="number"
            min={field.min ?? undefined}
            max={field.max ?? undefined}
            value={displayValue ?? 0}
            onChange={(e) => onChange(parseInt(e.target.value))}
            disabled={disabled}
          />
        );
      case "select":
        return (
          <select
            value={String(displayValue ?? "")}
            onChange={(e) => {
              const val = e.target.value;
              const numVal = parseInt(val);
              onChange(isNaN(numVal) ? val : numVal);
            }}
            disabled={disabled}
          >
            {field.options?.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        );
      case "serialport":
        return (
          <div style={{ display: "flex", gap: "8px" }}>
            <select
              value={displayValue || ""}
              onChange={(e) => onChange(e.target.value)}
              disabled={disabled}
              style={{ flex: 1 }}
            >
              {serialPorts.length === 0 && (
                <option value="">ポートが見つかりません</option>
              )}
              {serialPorts.map((port) => (
                <option key={port} value={port}>
                  {port}
                </option>
              ))}
              {displayValue && !serialPorts.includes(displayValue) && (
                <option value={displayValue}>{displayValue} (未検出)</option>
              )}
            </select>
            <button
              onClick={onRefreshPorts}
              disabled={disabled}
              className="btn-secondary"
              title="ポート一覧を更新"
              style={{ padding: "4px 8px" }}
            >
              ↻
            </button>
          </div>
        );
      default:
        return (
          <input
            type="text"
            value={displayValue || ""}
            onChange={(e) => onChange(e.target.value)}
            disabled={disabled}
          />
        );
    }
  };

  return (
    <div className="vscode-setting-item">
      <div className="vscode-setting-label">{field.label}</div>
      {field.description && (
        <div className="vscode-setting-description">{field.description}</div>
      )}
      <div className="vscode-setting-control">{renderControl()}</div>
    </div>
  );
}

// 右ペイン: 選択されたサーバーの設定
interface ServerConfigPaneHandle {
  save: () => Promise<void>;
  revert: () => Promise<void>;
}

interface ServerConfigPaneProps {
  instance: application.ServerInstanceDTO;
  serialPorts: string[];
  onRefreshPorts: () => void;
  onRemove: (protocolType: string) => void;
  onDirtyChange: (dirty: boolean) => void;
}

const ServerConfigPane = forwardRef<
  ServerConfigPaneHandle,
  ServerConfigPaneProps
>(function ServerConfigPane(
  { instance, serialPorts, onRefreshPorts, onRemove, onDirtyChange },
  ref,
) {
  const [schema, setSchema] = useState<application.ProtocolSchemaDTO | null>(
    null,
  );
  const [config, setConfig] = useState<application.ServerConfigDTO | null>(
    null,
  );
  const [isDirty, setIsDirty] = useState(false);
  const [paneError, setPaneError] = useState<string | null>(null);
  const [unitIDSettings, setUnitIDSettings] =
    useState<application.UnitIDSettingsDTO | null>(null);
  const [disabledUnitIds, setDisabledUnitIds] = useState<Set<number>>(
    new Set(),
  );

  const isRunning = instance.status === "Running";

  const statusClass =
    instance.status === "Running"
      ? "running"
      : instance.status === "Error"
        ? "error"
        : "stopped";

  const loadSettings = useCallback(async () => {
    try {
      setPaneError(null);
      const [s, cfg, unitSettings] = await Promise.all([
        GetProtocolSchema(instance.protocolType),
        GetServerConfig(instance.protocolType),
        GetUnitIDSettings(instance.protocolType),
      ]);
      setSchema(s);
      setConfig(cfg);
      if (unitSettings) {
        setUnitIDSettings(unitSettings);
        setDisabledUnitIds(new Set(unitSettings.disabledIds || []));
      }
      setIsDirty(false);
      onDirtyChange(false);
    } catch (e) {
      setPaneError(String(e));
    }
  }, [instance.protocolType, onDirtyChange]);

  useEffect(() => {
    loadSettings();
  }, [loadSettings]);

  useImperativeHandle(
    ref,
    () => ({
      save: async () => {
        if (config) {
          try {
            setPaneError(null);
            await UpdateServerConfig(config);
            await loadSettings();
          } catch (e) {
            setPaneError(String(e));
          }
        }
      },
      revert: async () => {
        await loadSettings();
      },
    }),
    [config, loadSettings],
  );

  const currentVariantId = config?.variant || instance.variant;
  const currentVariant = schema?.variants.find(
    (v) => v.id === currentVariantId,
  );
  const fields = currentVariant?.fields || [];

  const handleVariantChange = (variantId: string) => {
    if (config) {
      setConfig({ ...config, variant: variantId });
      setIsDirty(true);
      onDirtyChange(true);
    }
  };

  const handleSettingChange = (fieldName: string, value: any) => {
    if (config) {
      setConfig({
        ...config,
        settings: { ...config.settings, [fieldName]: value },
      });
      setIsDirty(true);
      onDirtyChange(true);
    }
  };

  const handleSaveConfig = async () => {
    if (config) {
      try {
        setPaneError(null);
        await UpdateServerConfig(config);
        await loadSettings();
      } catch (e) {
        setPaneError(String(e));
      }
    }
  };

  const handleUnitIdToggle = async (unitId: number, enabled: boolean) => {
    try {
      await SetUnitIDEnabled(instance.protocolType, unitId, enabled);
      const newDisabled = new Set(disabledUnitIds);
      if (enabled) {
        newDisabled.delete(unitId);
      } else {
        newDisabled.add(unitId);
      }
      setDisabledUnitIds(newDisabled);
    } catch (e) {
      setPaneError(String(e));
    }
  };

  const unitIdRange = unitIDSettings
    ? Array.from(
        { length: unitIDSettings.max - unitIDSettings.min + 1 },
        (_, i) => unitIDSettings.min + i,
      )
    : [];

  return (
    <div className="server-config-pane">
      <div className="server-config-pane-header">
        <div className="server-config-pane-title">
          <span className="server-config-pane-name">
            {instance.displayName}
          </span>
          <span className={`server-status-badge ${statusClass}`}>
            {instance.status}
          </span>
        </div>
        <div className="server-config-pane-actions">
          <button
            onClick={() => onRemove(instance.protocolType)}
            className="btn-danger"
            disabled={isRunning}
          >
            <Trash2 size={16} />
          </button>
        </div>
      </div>

      {paneError && (
        <div className="error-message" style={{ margin: "0.5rem 1rem" }}>
          {paneError}
        </div>
      )}

      <div className="vscode-settings-list">
        {schema && schema.variants.length > 1 && config && (
          <div className="vscode-setting-item">
            <div className="vscode-setting-label">サーバータイプ</div>
            <div className="vscode-setting-control">
              <select
                value={config.variant}
                onChange={(e) => handleVariantChange(e.target.value)}
                disabled={isRunning}
              >
                {schema.variants.map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.displayName}
                  </option>
                ))}
              </select>
            </div>
          </div>
        )}

        {(() => {
          // カテゴリ順序を保持しながらグループ化（空カテゴリは先頭）
          const categoryOrder: string[] = [];
          const grouped: Record<string, application.FieldDTO[]> = {};
          for (const field of fields) {
            const cat = field.category || "";
            if (!grouped[cat]) {
              grouped[cat] = [];
              categoryOrder.push(cat);
            }
            grouped[cat].push(field);
          }
          const hasCategories = categoryOrder.some((c) => c !== "");
          return categoryOrder.map((cat) => (
            <div key={cat || "__uncategorized__"}>
              {hasCategories && cat && (
                <div className="vscode-setting-category">{cat}</div>
              )}
              {grouped[cat].map((field) => (
                <DynamicField
                  key={field.name}
                  field={field}
                  value={config?.settings?.[field.name]}
                  onChange={(value) => handleSettingChange(field.name, value)}
                  disabled={isRunning}
                  serialPorts={serialPorts}
                  onRefreshPorts={onRefreshPorts}
                />
              ))}
            </div>
          ));
        })()}

        {schema?.capabilities.supportsUnitId && unitIDSettings && (
          <div className="unit-id-section">
            <h4>UnitID 応答設定</h4>
            <p className="help-text">
              オフにしたUnitIDには応答しません（デフォルト: 全て応答）
            </p>
            <div className="unit-id-grid">
              {unitIdRange.map((unitId) => (
                <label key={unitId} className="unit-id-toggle">
                  <input
                    type="checkbox"
                    checked={!disabledUnitIds.has(unitId)}
                    onChange={(e) =>
                      handleUnitIdToggle(unitId, e.target.checked)
                    }
                  />
                  <span className="unit-id-label">{unitId}</span>
                </label>
              ))}
            </div>
          </div>
        )}
      </div>

      <div className="server-config-pane-footer">
        <button
          onClick={handleSaveConfig}
          disabled={isRunning}
          className={isDirty ? "btn-primary" : "btn-secondary"}
        >
          {isDirty ? "保存 *" : "保存"}
        </button>
      </div>
    </div>
  );
});

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
    const [selectedProtocol, setSelectedProtocol] = useState<string>("");
    const [selectedVariant, setSelectedVariant] = useState<string>("");

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

    const handleOpenAddDialog = () => {
      if (availableProtocols.length === 0) return;
      const firstProto = availableProtocols[0];
      setSelectedProtocol(firstProto.type);
      setSelectedVariant(firstProto.variants[0]?.id || "");
      setIsAddDialogOpen(true);
    };

    const handleAddProtocolChange = (protocolType: string) => {
      const proto = availableProtocols.find((p) => p.type === protocolType);
      setSelectedProtocol(protocolType);
      setSelectedVariant(proto?.variants[0]?.id || "");
    };

    const handleAddServer = async () => {
      try {
        setError(null);
        await AddServer(selectedProtocol, selectedVariant);
        const list = (await GetServerInstances()) || [];
        setServerInstances(list);
        setSelectedProtocolType(selectedProtocol);
        setIsAddDialogOpen(false);
      } catch (e) {
        setError(String(e));
      }
    };

    useEffect(() => {
      if (!isAddDialogOpen) return;
      const handleKeyDown = (e: KeyboardEvent) => {
        if (e.key === "Escape") setIsAddDialogOpen(false);
      };
      document.addEventListener("keydown", handleKeyDown);
      return () => document.removeEventListener("keydown", handleKeyDown);
    }, [isAddDialogOpen]);

    const selectedInstance =
      serverInstances.find((i) => i.protocolType === selectedProtocolType) ??
      null;

    return (
      <div
        className="panel"
        style={{ display: "flex", flexDirection: "column", height: "100%" }}
      >
        {error && <div className="error-message">{error}</div>}

        <div className="server-vertical-tabs">
          {/* 左ペイン: タブリスト */}
          <div className="server-tab-list">
            <div className="server-panel-header">
              <span>SERVERS</span>
              <div
                style={{ display: "flex", gap: "8px", alignItems: "center" }}
              >
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
              const statusClass =
                instance.status === "Running"
                  ? "running"
                  : instance.status === "Error"
                    ? "error"
                    : "stopped";
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
                    {instance.variant && (
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
              <SquarePlus size={14} style={{ marginRight: "0.5rem" }} />
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
          <FocusTrap onConfirm={handleConfirmSave}>
            <div className="dialog">
              <h3>設定が保存されていません</h3>
              <div className="dialog-content">
                <p>通信設定が変更されています。</p>
              </div>
              <div className="dialog-buttons">
                <button onClick={handleCancelAction} className="btn-secondary">
                  キャンセル
                </button>
                <button
                  onClick={handleConfirmDiscard}
                  className="btn-secondary"
                >
                  {pendingAction.discardLabel}
                </button>
                <button onClick={handleConfirmSave} className="btn-primary">
                  {pendingAction.saveLabel}
                </button>
              </div>
            </div>
          </FocusTrap>
        )}

        {/* サーバー追加ダイアログ */}
        {isAddDialogOpen &&
          (() => {
            const selectedProtoVariants =
              availableProtocols.find((p) => p.type === selectedProtocol)
                ?.variants ?? [];
            return (
              <FocusTrap onConfirm={handleAddServer}>
                <div className="dialog">
                  <h3>サーバーを追加</h3>
                  <div className="dialog-content">
                    <div className="form-group">
                      <label>プロトコル</label>
                      <select
                        value={selectedProtocol}
                        onChange={(e) =>
                          handleAddProtocolChange(e.target.value)
                        }
                      >
                        {availableProtocols.map((p) => (
                          <option key={p.type} value={p.type}>
                            {p.displayName}
                          </option>
                        ))}
                      </select>
                    </div>
                    {selectedProtoVariants.length > 1 && (
                      <div className="form-group">
                        <label>バリアント</label>
                        <select
                          value={selectedVariant}
                          onChange={(e) => setSelectedVariant(e.target.value)}
                        >
                          {selectedProtoVariants.map((v) => (
                            <option key={v.id} value={v.id}>
                              {v.displayName}
                            </option>
                          ))}
                        </select>
                      </div>
                    )}
                  </div>
                  <div className="dialog-buttons">
                    <button
                      onClick={() => setIsAddDialogOpen(false)}
                      className="btn-secondary"
                    >
                      キャンセル
                    </button>
                    <button onClick={handleAddServer} className="btn-primary">
                      追加
                    </button>
                  </div>
                </div>
              </FocusTrap>
            );
          })()}
      </div>
    );
  },
);
