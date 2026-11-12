import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from "react";
import { FocusTrap } from "./FocusTrap";
import {
  UpdateVariableFieldValue,
  UpdateVariableValue,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { StructArrayElementEditor } from "./StructArrayElementEditor";
import { useEscapeKey } from "./useEscapeKey";
import {
  coerceFloatIfNeeded,
  FlatRow,
  isArrayType,
  isStructType,
  parseArrayTypeFE,
  parseBigIntInput,
  parseNumericInput,
} from "./variableTypes";

export interface ValueEditDialogHandle {
  // 変数一覧の行の値を編集する。見出し行（構造体・配列）なら変数全体を編集する
  openRow: (row: FlatRow) => void;
}

interface Props {
  structTypes: application.StructTypeDTO[];
  // 値を更新したあとに呼ぶ（一覧の再読み込み）
  onChanged: () => Promise<void>;
  // 開閉が変わったときに呼ぶ（親が、閉じたあとに行へフォーカスを戻すため）
  onOpenChange: (open: boolean) => void;
}

// 変数の値を編集するダイアログ（スカラー・構造体・配列を再帰的に編集する）
export const ValueEditDialog = forwardRef<ValueEditDialogHandle, Props>(
  function ValueEditDialog({ structTypes, onChanged, onOpenChange }, ref) {
    const [isOpen, setIsOpen] = useState(false);
    const [editingVariable, setEditingVariable] =
      useState<application.VariableDTO | null>(null);
    // 行単位で編集しているときの行（変数全体を編集しているときは null）
    const [editingRow, setEditingRow] = useState<FlatRow | null>(null);
    const [editData, setEditData] = useState<any>(null);
    const editDialogRef = useRef<HTMLDivElement>(null);

    // 編集ダイアログを開く（行単位）
    const open = (v: application.VariableDTO) => {
      setEditingVariable(v);
      // 構造体・配列はディープコピーして編集用データを作る
      if (isArrayType(v.dataType) || isStructType(v.dataType)) {
        setEditData(JSON.parse(JSON.stringify(v.value ?? null)));
      } else {
        setEditData(v.value);
      }
      setEditingRow(null);

      setIsOpen(true);
    };

    // フラット行から個別の値を編集
    const openRow = (row: FlatRow) => {
      if (row.isHeader) {
        // ヘッダー行の場合は変数全体の編集
        open(row.variable);
        return;
      }
      setEditingVariable(row.variable);
      setEditingRow(row);
      setEditData(
        row.value != null ? JSON.parse(JSON.stringify(row.value)) : row.value,
      );

      setIsOpen(true);
    };

    useImperativeHandle(ref, () => ({ openRow }));
    useEscapeKey(isOpen, () => {
      setIsOpen(false);
      setEditData(null);
      setEditingRow(null);
    });

    useEffect(() => {
      onOpenChange(isOpen);
      // 開閉の変化だけを親に伝える
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [isOpen]);

    // 値編集ダイアログが開いたら最初の入力にフォーカス
    useEffect(() => {
      if (isOpen && editDialogRef.current) {
        const first =
          editDialogRef.current.querySelector<HTMLElement>("input, select");
        first?.focus();
      }
    }, [isOpen]);

    // 行単位の値更新
    const handleUpdateRow = async () => {
      if (!editingVariable) return;
      try {
        if (editingRow && editingRow.valuePath.length > 0) {
          // フィールドパス（配列の添字は外部インデックス）を使って部分更新
          await UpdateVariableFieldValue(
            editingVariable.id,
            editingRow.fieldPath,
            coerceFloatIfNeeded(editData, editingRow.dataType),
          );
        } else {
          const dataType = editingVariable.dataType;
          await UpdateVariableValue(
            editingVariable.id,
            coerceFloatIfNeeded(editData, dataType),
          );
        }
        await onChanged();
        setIsOpen(false);
        setEditingVariable(null);
        setEditingRow(null);
        setEditData(null);
      } catch (e) {
        console.error("Failed to update variable:", e);
        alert("変数の更新に失敗しました: " + e);
      }
    };

    // スカラー値エディタ
    const renderScalarEditor = (
      dataType: string,
      value: any,
      onChange: (v: any) => void,
    ) => {
      if (dataType === "BOOL") {
        return (
          <select
            className="value-input"
            value={value ? "true" : "false"}
            onChange={(e) => onChange(e.target.value === "true")}
          >
            <option value="true">TRUE</option>
            <option value="false">FALSE</option>
          </select>
        );
      }
      if (dataType === "STRING" || dataType.startsWith("STRING[")) {
        const maxLen = dataType.startsWith("STRING[")
          ? parseInt(dataType.match(/\[(\d+)\]/)?.[1] || "0")
          : 0;
        return (
          <input
            className="value-input"
            type="text"
            value={value ?? ""}
            onChange={(e) =>
              onChange(
                maxLen > 0 ? e.target.value.slice(0, maxLen) : e.target.value,
              )
            }
            onFocus={(e) => e.target.select()}
            maxLength={maxLen > 0 ? maxLen : undefined}
            placeholder={maxLen > 0 ? `最大${maxLen}バイト` : undefined}
          />
        );
      }
      // 64ビット整数型（LINT/ULINT）- 文字列で精度を保持
      if (dataType === "LINT" || dataType === "ULINT") {
        const placeholder =
          dataType === "LINT"
            ? "-9223372036854775808〜9223372036854775807 (0x/0b対応)"
            : "0〜18446744073709551615 (0x/0b対応)";
        return (
          <input
            className="value-input"
            type="text"
            value={String(value ?? "0")}
            onChange={(e) => {
              // 入力中は文字列のまま保持（確定時に正規化）
              onChange(e.target.value);
            }}
            onFocus={(e) => e.target.select()}
            onBlur={(e) => {
              const normalized = parseBigIntInput(e.target.value, dataType);
              if (normalized !== null) {
                onChange(normalized);
              }
            }}
            placeholder={placeholder}
          />
        );
      }
      // 浮動小数点型（REAL/LREAL）- 入力中は文字列のまま保持してドット入力を可能にする
      if (dataType === "REAL" || dataType === "LREAL") {
        return (
          <input
            className="value-input"
            type="text"
            value={String(value ?? "0")}
            onChange={(e) => {
              // 入力中は文字列のまま保持（"1." のようなドット末尾も維持）
              onChange(e.target.value);
            }}
            onFocus={(e) => e.target.select()}
            onBlur={(e) => {
              const parsed = parseFloat(e.target.value);
              onChange(isNaN(parsed) ? 0 : parsed);
            }}
            placeholder="例: 3.14"
          />
        );
      }
      // 時間・日付型（文字列として編集）
      if (
        dataType === "TIME" ||
        dataType === "DATE" ||
        dataType === "TIME_OF_DAY" ||
        dataType === "DATE_AND_TIME"
      ) {
        const placeholders: { [key: string]: string } = {
          TIME: "T#1s, T#100ms, T#1h30m",
          DATE: "D#2024-01-01",
          TIME_OF_DAY: "TOD#12:30:15",
          DATE_AND_TIME: "DT#2024-01-01-12:30:15",
        };
        return (
          <input
            className="value-input"
            type="text"
            value={value ?? ""}
            onChange={(e) => onChange(e.target.value)}
            onFocus={(e) => e.target.select()}
            placeholder={placeholders[dataType] || ""}
          />
        );
      }
      // 数値型
      return (
        <input
          className="value-input"
          type="text"
          value={value ?? 0}
          onChange={(e) => {
            const parsed = parseNumericInput(e.target.value);
            if (!isNaN(parsed)) {
              onChange(parsed);
            }
          }}
          onFocus={(e) => e.target.select()}
          onBlur={(e) => {
            const parsed = parseNumericInput(e.target.value);
            if (!isNaN(parsed)) {
              onChange(parsed);
            }
          }}
          placeholder="10進, 0x(16進), 0b(2進)"
        />
      );
    };

    // 再帰的値エディタ
    const renderValueEditor = (
      dataType: string,
      value: any,
      onChange: (v: any) => void,
      depth: number = 0,
    ): JSX.Element => {
      const indent = depth * 16;

      // 配列型
      if (isArrayType(dataType)) {
        const arr = parseArrayTypeFE(dataType);
        if (!arr || !Array.isArray(value)) {
          return <span>-</span>;
        }
        const elemType = arr.elemType;
        const elemIsStruct = isStructType(elemType) || isArrayType(elemType);

        return (
          <div style={{ marginLeft: indent }}>
            {value.map((elem: any, i: number) => (
              <div className="value-array-row" key={i}>
                <span className="value-array-index">[{i}]</span>
                {elemIsStruct ? (
                  <StructArrayElementEditor
                    elemType={elemType}
                    elem={elem}
                    onChange={(newElem) => {
                      const newArr = [...value];
                      newArr[i] = newElem;
                      onChange(newArr);
                    }}
                    renderEditor={renderValueEditor}
                  />
                ) : (
                  renderScalarEditor(elemType, elem, (newVal) => {
                    const newArr = [...value];
                    newArr[i] = newVal;
                    onChange(newArr);
                  })
                )}
              </div>
            ))}
          </div>
        );
      }

      // 構造体型
      if (isStructType(dataType)) {
        const st = structTypes.find((s) => s.name === dataType);
        if (!st || typeof value !== "object" || value === null) {
          return <span>-</span>;
        }
        return (
          <div style={{ marginLeft: indent }}>
            {st.fields.map((field) => (
              <div className="value-field" key={field.name}>
                <div className="value-field-header">
                  <span className="value-field-name">{field.name}</span>
                  <span className="value-field-type">({field.dataType})</span>
                </div>
                <div className="value-field-body">
                  {renderValueEditor(
                    field.dataType,
                    value[field.name],
                    (newVal) => {
                      onChange({ ...value, [field.name]: newVal });
                    },
                    depth + 1,
                  )}
                </div>
              </div>
            ))}
          </div>
        );
      }

      // スカラー型
      return renderScalarEditor(dataType, value, onChange);
    };

    if (!isOpen || !editingVariable) return null;
    return (
      <FocusTrap onConfirm={handleUpdateRow}>
        <div
          ref={editDialogRef}
          className="dialog dialog-scrollable dialog-w450"
        >
          <h3>値を編集</h3>
          <div className="dialog-content dialog-content-scroll">
            <div className="dialog-row">
              <label>変数名:</label>
              <span className="dialog-value">
                {editingRow ? editingRow.displayName : editingVariable.name}
              </span>
            </div>
            <div className="dialog-row">
              <label>データ型:</label>
              <span className="dialog-value">
                {editingRow ? editingRow.dataType : editingVariable.dataType}
              </span>
            </div>
            <div className="dialog-row dialog-row-stacked">
              <label>値:</label>
              <div className="value-editor-root">
                {editingRow
                  ? renderValueEditor(
                      editingRow.dataType,
                      editData,
                      setEditData,
                    )
                  : renderValueEditor(
                      editingVariable.dataType,
                      editData,
                      setEditData,
                    )}
              </div>
            </div>
          </div>
          <div className="dialog-buttons">
            <button
              onClick={() => {
                setIsOpen(false);
                setEditData(null);
                setEditingRow(null);
              }}
              className="btn-secondary"
            >
              キャンセル
            </button>
            <button onClick={handleUpdateRow} className="btn-primary">
              更新
            </button>
          </div>
        </div>
      </FocusTrap>
    );
  },
);
