import { forwardRef, useImperativeHandle, useRef, useState } from "react";
import { FocusTrap } from "./FocusTrap";
import { CreateVariable } from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { DataTypeFields } from "./DataTypeFields";
import { useEscapeKey } from "./useEscapeKey";
import {
  buildDataTypeFromForm,
  DataTypeFormValue,
  defaultDataTypeForm,
  generateDefaultForType,
  getDefaultValue,
  parseValue,
} from "./variableTypes";

export interface AddVariableDialogHandle {
  open: () => void;
  isOpen: () => boolean;
}

interface Props {
  dataTypes: application.DataTypeInfoDTO[];
  structTypes: application.StructTypeDTO[];
  // 変数を追加したあとに呼ぶ（一覧の再読み込み）
  onChanged: () => Promise<void>;
}

// 変数追加ダイアログ。
// 常にマウントしておき、キャンセルで閉じても入力内容を保持する（追加に成功したら初期状態に戻す）。
export const AddVariableDialog = forwardRef<AddVariableDialogHandle, Props>(
  function AddVariableDialog({ dataTypes, structTypes, onChanged }, ref) {
    const [isOpen, setIsOpen] = useState(false);
    const [name, setName] = useState("");
    const [type, setType] = useState<DataTypeFormValue>(defaultDataTypeForm);
    // スカラー型の初期値（空欄ならデータ型の既定値）
    const [initialValue, setInitialValue] = useState("");

    const isOpenRef = useRef(isOpen);
    isOpenRef.current = isOpen;
    useImperativeHandle(
      ref,
      () => ({ open: () => setIsOpen(true), isOpen: () => isOpenRef.current }),
      [],
    );
    useEscapeKey(isOpen, () => setIsOpen(false));

    // 変数を追加
    const handleAddVariable = async () => {
      if (!name.trim()) return;
      try {
        const dataType = buildDataTypeFromForm(type);
        // 配列と構造体は既定値で作る。スカラーは入力された初期値を使う
        const value =
          type.category === "scalar"
            ? parseValue(initialValue || getDefaultValue(dataType), dataType)
            : generateDefaultForType(dataType, structTypes);

        await CreateVariable(name.trim(), dataType, value);
        await onChanged();
        setIsOpen(false);
        setName("");
        setInitialValue("");
        // バイト長だけは、次に追加するときのために残す
        setType((prev) => ({
          ...defaultDataTypeForm(),
          stringLength: prev.stringLength,
        }));
      } catch (e) {
        console.error("Failed to create variable:", e);
        alert("変数の作成に失敗しました: " + e);
      }
    };

    if (!isOpen) return null;
    return (
      <FocusTrap onConfirm={handleAddVariable}>
        <div className="dialog">
          <h3>変数を追加</h3>
          <div className="dialog-content">
            <div className="dialog-row">
              <label>変数名:</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例: Motor_Speed"
                autoFocus
              />
            </div>
            <DataTypeFields
              value={type}
              onChange={setType}
              dataTypes={dataTypes}
              structTypes={structTypes}
              onScalarTypeChange={(dataType) =>
                setInitialValue(getDefaultValue(dataType))
              }
              scalarExtra={
                <div className="dialog-row">
                  <label>初期値:</label>
                  <input
                    type="text"
                    value={initialValue}
                    onChange={(e) => setInitialValue(e.target.value)}
                    placeholder={getDefaultValue(type.dataType)}
                  />
                </div>
              }
            />
          </div>
          <div className="dialog-buttons">
            <button onClick={() => setIsOpen(false)} className="btn-secondary">
              キャンセル
            </button>
            <button onClick={handleAddVariable} className="btn-primary">
              追加
            </button>
          </div>
        </div>
      </FocusTrap>
    );
  },
);
