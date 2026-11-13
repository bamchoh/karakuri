import { application } from "../../wailsjs/go/models";
import { FolderTree } from "lucide-react";

// サーバー設定の 1 項目。プロトコルの実装が返す項目の定義（種類・選択肢・範囲）に合わせて入力欄を出す
interface DynamicFieldProps {
  field: application.FieldDTO;
  value: any;
  onChange: (value: any) => void;
  disabled: boolean;
  serialPorts: string[];
  onRefreshPorts: () => void;
  // 渡すと、値を機器のアドレスの一覧から選べる項目（field.browse）に「参照」ボタンを出す
  onBrowse?: () => void;
}

export function DynamicField({
  field,
  value,
  onChange,
  disabled,
  serialPorts,
  onRefreshPorts,
  onBrowse,
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
      case "password":
        return (
          <input
            type="password"
            value={displayValue || ""}
            onChange={(e) => onChange(e.target.value)}
            disabled={disabled}
            autoComplete="off"
          />
        );
      case "textarea":
        return (
          <textarea
            rows={3}
            value={displayValue || ""}
            onChange={(e) => onChange(e.target.value)}
            disabled={disabled}
            spellCheck={false}
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
          <div className="serial-port-field">
            <select
              className="serial-port-select"
              value={displayValue || ""}
              onChange={(e) => onChange(e.target.value)}
              disabled={disabled}
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
              className="btn-secondary serial-port-refresh"
              title="ポート一覧を更新"
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
      {field.browse && onBrowse ? (
        <div className="vscode-setting-control field-with-browse">
          {renderControl()}
          <button
            className="btn-secondary btn-small"
            onClick={onBrowse}
            disabled={disabled}
            title="接続先の機器から一覧を読んで選ぶ"
            aria-label={`${field.label} を参照`}
          >
            <FolderTree className="icon-left" size={12} />
            参照
          </button>
        </div>
      ) : (
        <div className="vscode-setting-control">{renderControl()}</div>
      )}
    </div>
  );
}
