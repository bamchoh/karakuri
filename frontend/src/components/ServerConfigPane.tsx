import {
  useState,
  useEffect,
  useCallback,
  forwardRef,
  useImperativeHandle,
} from "react";
import {
  GetProtocolSchema,
  GetServerConfig,
  UpdateServerConfig,
  GetUnitIDSettings,
  SetUnitIDEnabled,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { Trash2 } from "lucide-react";
import { DynamicField } from "./DynamicField";
import { serverStatusClass } from "./serverStatus";

// サーバー画面の右側: 選択したサーバーの設定（プラグインのスキーマから作るフォーム・UnitID の応答設定）。
// 親からは ref の save() / revert() で、保存していない変更を保存・破棄できる。
export interface ServerConfigPaneHandle {
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

export const ServerConfigPane = forwardRef<
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

  const statusClass = serverStatusClass(instance.status);

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
