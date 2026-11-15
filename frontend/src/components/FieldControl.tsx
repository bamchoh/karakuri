import { useRef } from "react";
import { Plus, X } from "lucide-react";
import { application } from "../../wailsjs/go/models";
import {
  addRow,
  canAddRow,
  removeRow,
  rowsOf,
  updateCell,
} from "../utils/rows";

// 項目 1 つの入力欄（ラベルと説明は付けない）。プロトコルの実装が返す項目の定義（種類・選択肢・範囲）に合わせて出す。
// DynamicField（ラベルと説明つきの 1 項目）と、表（"rows"）のセルで共通
export interface FieldControlProps {
  field: application.FieldDTO;
  value: any;
  onChange: (value: any) => void;
  disabled: boolean;
  serialPorts: string[];
  onRefreshPorts: () => void;
}

export function FieldControl({
  field,
  value,
  onChange,
  disabled,
  serialPorts,
  onRefreshPorts,
}: FieldControlProps) {
  const displayValue = value ?? field.default;

  switch (field.type) {
    case "text":
      return (
        <input
          type="text"
          value={displayValue || ""}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
          aria-label={field.label}
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
          aria-label={field.label}
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
          aria-label={field.label}
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
    case "rows":
      return (
        <RowsField
          field={field}
          value={value}
          onChange={onChange}
          disabled={disabled}
        />
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
}

const noPorts: string[] = [];
const noop = () => {};

let nextRowKey = 1;

// 行を足せる表（項目の種類 "rows"）。列（field.columns）ごとの入力欄を 1 行に並べ、行の追加・削除ができる。
// 値は行の並び（列の name をキーにしたオブジェクトの配列）。行の操作は utils/rows.ts の純粋な関数
function RowsField({
  field,
  value,
  onChange,
  disabled,
}: {
  field: application.FieldDTO;
  value: any;
  onChange: (value: any) => void;
  disabled: boolean;
}) {
  const columns = field.columns ?? [];
  const rows = rowsOf(value, field.default, columns);

  // 行の key（行の削除で、ほかの行の入力欄が入れ替わらないように）。値には入れず、ここで行の数に合わせて持つ
  const keys = useRef<number[]>([]);
  while (keys.current.length < rows.length) keys.current.push(nextRowKey++);
  if (keys.current.length > rows.length) keys.current.length = rows.length;

  const max = field.max ?? undefined;
  return (
    <div className="rows-field">
      <table className="rows-table">
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.name}>{c.label}</th>
            ))}
            <th className="rows-action-col" />
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={keys.current[i]}>
              {columns.map((c) => (
                <td key={c.name}>
                  <FieldControl
                    field={c}
                    value={row[c.name]}
                    onChange={(v) => onChange(updateCell(rows, i, c.name, v))}
                    disabled={disabled}
                    serialPorts={noPorts}
                    onRefreshPorts={noop}
                  />
                </td>
              ))}
              <td className="rows-action-col">
                <button
                  className="btn-secondary btn-small rows-remove"
                  onClick={() => {
                    keys.current.splice(i, 1);
                    onChange(removeRow(rows, i));
                  }}
                  disabled={disabled}
                  title="この行を削除"
                  aria-label={`${i + 1} 行目を削除`}
                >
                  <X size={14} />
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="rows-footer">
        <button
          className="btn-secondary btn-small"
          onClick={() => onChange(addRow(rows, columns))}
          disabled={disabled || !canAddRow(rows, max)}
        >
          <Plus className="icon-left" size={12} />
          行を追加
        </button>
        <span className="rows-count">
          {rows.length} 行{max ? ` / 最大 ${max}` : ""}
        </span>
      </div>
    </div>
  );
}
