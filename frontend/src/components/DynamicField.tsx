import { application } from "../../wailsjs/go/models";
import { FolderTree } from "lucide-react";
import { FieldControl } from "./FieldControl";

// 表示条件（field.showWhen）を満たす項目だけを返す。
// 条件は「別の項目の値が、決まった値のとき」。まだ入力していない項目は、既定値で判定する
export function visibleFields(
  fields: application.FieldDTO[],
  values: Record<string, any> | undefined,
): application.FieldDTO[] {
  return fields.filter((field) => {
    const cond = field.showWhen;
    if (!cond) return true;
    const current =
      values?.[cond.field] ??
      fields.find((f) => f.name === cond.field)?.default;
    return String(current) === cond.value;
  });
}

// サーバー設定の 1 項目（ラベル・説明・入力欄）。入力欄は FieldControl（項目の種類ごと）
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
  const control = (
    <FieldControl
      field={field}
      value={value}
      onChange={onChange}
      disabled={disabled}
      serialPorts={serialPorts}
      onRefreshPorts={onRefreshPorts}
    />
  );

  // 表（"rows"）は幅が要るので、入力欄の幅の上限を広げる
  const controlClass =
    field.type === "rows"
      ? "vscode-setting-control rows-control"
      : "vscode-setting-control";

  return (
    <div className="vscode-setting-item">
      <div className="vscode-setting-label">{field.label}</div>
      {field.description && (
        <div className="vscode-setting-description">{field.description}</div>
      )}
      {field.browse && onBrowse ? (
        <div className={`${controlClass} field-with-browse`}>
          {control}
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
        <div className={controlClass}>{control}</div>
      )}
    </div>
  );
}
