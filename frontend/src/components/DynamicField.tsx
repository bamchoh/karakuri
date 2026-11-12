import { application } from "../../wailsjs/go/models";

// サーバー設定の 1 項目。プラグインが返す項目の定義（種類・選択肢・範囲）に合わせて入力欄を出す
interface DynamicFieldProps {
  field: application.FieldDTO;
  value: any;
  onChange: (value: any) => void;
  disabled: boolean;
  serialPorts: string[];
  onRefreshPorts: () => void;
}

export function DynamicField({
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
