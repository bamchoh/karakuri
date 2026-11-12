import { forwardRef, useImperativeHandle, useRef, useState } from "react";
import { FocusTrap } from "./FocusTrap";
import { UpdateVariable } from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { DataTypeFields } from "./DataTypeFields";
import { useEscapeKey } from "./useEscapeKey";
import {
  buildDataTypeFromForm,
  DataTypeFormValue,
  defaultDataTypeForm,
  isStructType,
  parseDataTypeToForm,
} from "./variableTypes";

export interface MetaEditDialogHandle {
  // 変数の現在の名前・データ型をフォームに入れて開く
  open: (v: application.VariableDTO) => void;
  isOpen: () => boolean;
}

interface Props {
  dataTypes: application.DataTypeInfoDTO[];
  structTypes: application.StructTypeDTO[];
  // 変数を更新したあとに呼ぶ（一覧の再読み込み）
  onChanged: () => Promise<void>;
}

// 変数の名前・データ型を変更するダイアログ
export const MetaEditDialog = forwardRef<MetaEditDialogHandle, Props>(
  function MetaEditDialog({ dataTypes, structTypes, onChanged }, ref) {
    const [isOpen, setIsOpen] = useState(false);
    const [variableId, setVariableId] = useState("");
    const [name, setName] = useState("");
    const [type, setType] = useState<DataTypeFormValue>(defaultDataTypeForm);

    const open = (v: application.VariableDTO) => {
      setVariableId(v.id);
      setName(v.name);
      setType(
        parseDataTypeToForm(v.dataType, isStructType, defaultDataTypeForm()),
      );
      setIsOpen(true);
    };

    const isOpenRef = useRef(isOpen);
    isOpenRef.current = isOpen;
    useImperativeHandle(ref, () => ({ open, isOpen: () => isOpenRef.current }));
    useEscapeKey(isOpen, () => setIsOpen(false));

    // 変数の名前・データ型を保存する
    const handleSave = async () => {
      if (!name.trim()) return;
      try {
        await UpdateVariable(
          variableId,
          name.trim(),
          buildDataTypeFromForm(type),
        );
        await onChanged();
        setIsOpen(false);
      } catch (e) {
        console.error("Failed to update variable:", e);
        alert("変数の更新に失敗しました: " + e);
      }
    };

    if (!isOpen) return null;
    return (
      <FocusTrap onConfirm={handleSave}>
        <div className="dialog">
          <h3>変数を編集</h3>
          <div className="dialog-content">
            <div className="dialog-row">
              <label>変数名:</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                autoFocus
              />
            </div>
            <DataTypeFields
              value={type}
              onChange={setType}
              dataTypes={dataTypes}
              structTypes={structTypes}
            />
            <p className="dialog-note">
              ※ データタイプを変更すると値はデフォルト値にリセットされます
            </p>
          </div>
          <div className="dialog-buttons">
            <button onClick={() => setIsOpen(false)} className="btn-secondary">
              キャンセル
            </button>
            <button onClick={handleSave} className="btn-primary">
              保存
            </button>
          </div>
        </div>
      </FocusTrap>
    );
  },
);
