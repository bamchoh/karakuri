import { forwardRef, useImperativeHandle, useRef, useState } from "react";
import { FocusTrap } from "./FocusTrap";
import {
  DeleteStructType,
  RegisterStructType,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { DataTypeFields } from "./DataTypeFields";
import { useEscapeKey } from "./useEscapeKey";
import {
  buildDataTypeFromForm,
  DataTypeFormValue,
  defaultDataTypeForm,
  parseDataTypeToForm,
} from "./variableTypes";

export interface StructTypeDialogHandle {
  open: () => void;
  isOpen: () => boolean;
}

interface Props {
  dataTypes: application.DataTypeInfoDTO[];
  structTypes: application.StructTypeDTO[];
  // 構造体型を登録・更新・削除したあとに呼ぶ（型一覧の再読み込み）
  onChanged: () => Promise<void>;
}

// フォーム上の 1 フィールド
interface FieldForm {
  name: string;
  type: DataTypeFormValue;
}

// フィールドのデータ型の初期値（3 次元目の終了インデックスの既定値は 4）
const defaultFieldType = () => defaultDataTypeForm(4);
const emptyField = (): FieldForm => ({ name: "", type: defaultFieldType() });

// 構造体型の登録・編集・削除ダイアログ。閉じると入力中のフォームは初期状態に戻る。
export const StructTypeDialog = forwardRef<StructTypeDialogHandle, Props>(
  function StructTypeDialog({ dataTypes, structTypes, onChanged }, ref) {
    const [isOpen, setIsOpen] = useState(false);
    // 構造体型定義フォーム
    const [structTypeName, setStructTypeName] = useState("");
    const [editingStructTypeName, setEditingStructTypeName] = useState<
      string | null
    >(null);
    const [fields, setFields] = useState<FieldForm[]>([emptyField()]);

    const resetForm = () => {
      setEditingStructTypeName(null);
      setStructTypeName("");
      setFields([emptyField()]);
    };

    // 閉じて、フォームを初期状態に戻す
    const close = () => {
      setIsOpen(false);
      resetForm();
    };

    const isOpenRef = useRef(isOpen);
    isOpenRef.current = isOpen;
    useImperativeHandle(
      ref,
      () => ({ open: () => setIsOpen(true), isOpen: () => isOpenRef.current }),
      [],
    );
    useEscapeKey(isOpen, close);

    const updateField = (index: number, patch: Partial<FieldForm>) =>
      setFields((prev) =>
        prev.map((f, i) => (i === index ? { ...f, ...patch } : f)),
      );

    // フィールドの型として選べる構造体型（編集中の型自身は選べない）
    const selectableStructTypes = structTypes.filter(
      (st) => st.name !== structTypeName.trim(),
    );

    // 構造体型を登録または更新
    const handleRegisterStructType = async () => {
      if (!structTypeName.trim()) return;
      const validFields = fields.filter((f) => f.name.trim());
      if (validFields.length === 0) {
        alert("少なくとも1つのフィールドを定義してください。");
        return;
      }
      try {
        // 編集モードの場合、既存の型を削除してから新しい型を登録
        if (editingStructTypeName) {
          await DeleteStructType(editingStructTypeName);
        }
        await RegisterStructType({
          name: structTypeName.trim(),
          fields: validFields.map((f) => ({
            name: f.name.trim(),
            dataType: buildDataTypeFromForm(f.type),
            offset: 0,
          })),
          wordCount: 0,
        } as application.StructTypeDTO);
        await onChanged();
        resetForm();
      } catch (e) {
        console.error("Failed to register struct type:", e);
        alert("構造体型の登録に失敗しました: " + e);
      }
    };

    // 構造体型を編集（既存の定義をフォームに入れる）
    const handleEditStructType = (st: application.StructTypeDTO) => {
      setEditingStructTypeName(st.name);
      setStructTypeName(st.name);
      setFields(
        st.fields.map((f) => ({
          name: f.name,
          type: parseDataTypeToForm(
            f.dataType,
            (name) => structTypes.some((s) => s.name === name),
            defaultFieldType(),
          ),
        })),
      );
    };

    // 構造体型を削除
    const handleDeleteStructType = async (name: string) => {
      if (!confirm(`構造体型 "${name}" を削除しますか?`)) return;
      try {
        await DeleteStructType(name);
        await onChanged();
      } catch (e) {
        console.error("Failed to delete struct type:", e);
        alert("構造体型の削除に失敗しました: " + e);
      }
    };

    if (!isOpen) return null;
    return (
      <FocusTrap onConfirm={handleRegisterStructType}>
        <div
          className="dialog"
          style={{
            minWidth: "500px",
            maxHeight: "80vh",
            display: "flex",
            flexDirection: "column",
          }}
        >
          <h3>構造体型管理</h3>
          <div
            className="dialog-content"
            style={{ flex: 1, overflowY: "auto" }}
          >
            {/* 既存の構造体型一覧 */}
            {structTypes.length > 0 && (
              <div className="dialog-section">
                <h4 className="dialog-section-title">登録済み構造体型</h4>
                <table className="variable-table">
                  <thead>
                    <tr>
                      <th>名前</th>
                      <th>フィールド</th>
                      <th>ワード数</th>
                      <th>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {structTypes.map((st) => (
                      <tr key={st.name}>
                        <td>{st.name}</td>
                        <td>
                          {st.fields
                            .map((f) => `${f.name}:${f.dataType}`)
                            .join(", ")}
                        </td>
                        <td>{st.wordCount}</td>
                        <td>
                          <button
                            onClick={() => handleEditStructType(st)}
                            className="btn-small btn-secondary"
                            style={{ marginRight: "0.5rem" }}
                          >
                            編集
                          </button>
                          <button
                            onClick={() => handleDeleteStructType(st.name)}
                            className="btn-small btn-danger"
                          >
                            削除
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            {/* 新規構造体型登録 */}
            <h4 className="dialog-section-title">
              {editingStructTypeName ? "構造体型を編集" : "新規構造体型"}
            </h4>
            <div className="dialog-row">
              <label>型名:</label>
              <input
                type="text"
                value={structTypeName}
                onChange={(e) => setStructTypeName(e.target.value)}
                placeholder="例: MotorData"
                disabled={!!editingStructTypeName}
              />
            </div>
            <div className="dialog-section">
              <label>フィールド:</label>
              {fields.map((field, index) => (
                <div key={index} className="dialog-field-editor">
                  <div
                    style={{
                      display: "flex",
                      justifyContent: "space-between",
                      marginBottom: "0.5rem",
                    }}
                  >
                    <strong>フィールド {index + 1}</strong>
                    <button
                      onClick={() =>
                        setFields(fields.filter((_, i) => i !== index))
                      }
                      className="btn-small btn-danger"
                      disabled={fields.length <= 1}
                    >
                      X
                    </button>
                  </div>

                  <div className="dialog-row">
                    <label>名前:</label>
                    <input
                      type="text"
                      value={field.name}
                      onChange={(e) =>
                        updateField(index, { name: e.target.value })
                      }
                      placeholder="フィールド名"
                    />
                  </div>

                  <DataTypeFields
                    value={field.type}
                    onChange={(type) => updateField(index, { type })}
                    dataTypes={dataTypes}
                    structTypes={selectableStructTypes}
                    compact
                  />
                </div>
              ))}
              <button
                onClick={() => setFields([...fields, emptyField()])}
                className="btn-secondary"
                style={{ marginTop: "0.5rem" }}
              >
                + フィールド追加
              </button>
            </div>
          </div>
          <div className="dialog-buttons">
            <button onClick={close} className="btn-secondary">
              閉じる
            </button>
            <button
              onClick={handleRegisterStructType}
              className="btn-primary"
            >
              {editingStructTypeName ? "更新" : "型を登録"}
            </button>
          </div>
        </div>
      </FocusTrap>
    );
  },
);
