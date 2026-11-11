import React, { forwardRef, useImperativeHandle, useRef, useState } from "react";
import { FocusTrap } from "./FocusTrap";
import { CreateVariable } from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { useEscapeKey } from "./useEscapeKey";
import {
  buildArrayTypeFE,
  generateDefaultForType,
  generateStructDefault,
  getDefaultValue,
  parseValue,
  resolveBaseElemType,
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
    // 新規変数フォーム
    const [newName, setNewName] = useState("");
    const [newDataType, setNewDataType] = useState("INT");
    const [newValue, setNewValue] = useState("");
    // 配列型追加用
    const [newArrayElemType, setNewArrayElemType] = useState("INT");
    const [newArrayElemCategory, setNewArrayElemCategory] = useState<
      "scalar" | "struct"
    >("scalar");
    const [newDimCount, setNewDimCount] = useState(1);
    const [newDimBounds, setNewDimBounds] = useState<
      Array<{ lower: number; upper: number }>
    >([
      { lower: 0, upper: 9 },
      { lower: 0, upper: 4 },
      { lower: 0, upper: 2 },
    ]);
    // STRING型のバイト長
    const [newStringLength, setNewStringLength] = useState(20);
    // 型カテゴリ: 'scalar' | 'array' | 'struct'
    const [newTypeCategory, setNewTypeCategory] = useState<
      "scalar" | "array" | "struct"
    >("scalar");

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
      if (!newName.trim()) return;
      try {
        let dataType = newDataType;
        let value: any;

        if (newTypeCategory === "array") {
          const baseElemType = resolveBaseElemType(
            newArrayElemCategory,
            newArrayElemType,
            newStringLength,
          );
          dataType = buildArrayTypeFE(
            baseElemType,
            newDimBounds.slice(0, newDimCount),
          );
          value = generateDefaultForType(dataType, structTypes);
        } else if (newTypeCategory === "struct") {
          // 構造体のデフォルト値を再帰的に生成
          value = generateStructDefault(newDataType, structTypes);
        } else {
          // スカラー型
          // STRING選択時は STRING[n] 形式にする
          if (newDataType === "STRING") {
            dataType = `STRING[${newStringLength}]`;
          }
          value = parseValue(newValue || getDefaultValue(dataType), dataType);
        }

        await CreateVariable(newName.trim(), dataType, value);
        await onChanged();
        setIsOpen(false);
        setNewName("");
        setNewDataType("INT");
        setNewValue("");
        setNewTypeCategory("scalar");
        setNewArrayElemType("INT");
        setNewArrayElemCategory("scalar");
        setNewDimCount(1);
        setNewDimBounds([
          { lower: 0, upper: 9 },
          { lower: 0, upper: 4 },
          { lower: 0, upper: 2 },
        ]);
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
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="例: Motor_Speed"
                autoFocus
              />
            </div>
            <div className="dialog-row">
              <label>型カテゴリ:</label>
              <select
                value={newTypeCategory}
                onChange={(e) => {
                  const cat = e.target.value as "scalar" | "array" | "struct";
                  setNewTypeCategory(cat);
                  if (cat === "scalar") setNewDataType("INT");
                  else if (cat === "struct" && structTypes.length > 0)
                    setNewDataType(structTypes[0].name);
                }}
              >
                <option value="scalar">スカラー型</option>
                <option value="array">配列型</option>
                {structTypes.length > 0 && (
                  <option value="struct">構造体型</option>
                )}
              </select>
            </div>

            {newTypeCategory === "scalar" && (
              <>
                <div className="dialog-row">
                  <label>データ型:</label>
                  <select
                    value={newDataType}
                    onChange={(e) => {
                      setNewDataType(e.target.value);
                      setNewValue(getDefaultValue(e.target.value));
                    }}
                  >
                    {dataTypes.map((t) => (
                      <option key={t.id} value={t.id} title={t.description}>
                        {t.displayName} ({t.description})
                      </option>
                    ))}
                    <option value="STRING">STRING (文字列)</option>
                  </select>
                </div>
                {newDataType === "STRING" && (
                  <div className="dialog-row">
                    <label>バイト長:</label>
                    <input
                      type="number"
                      value={newStringLength}
                      onChange={(e) =>
                        setNewStringLength(parseInt(e.target.value) || 1)
                      }
                      min={1}
                      max={256}
                    />
                  </div>
                )}
                <div className="dialog-row">
                  <label>初期値:</label>
                  <input
                    type="text"
                    value={newValue}
                    onChange={(e) => setNewValue(e.target.value)}
                    placeholder={getDefaultValue(newDataType)}
                  />
                </div>
              </>
            )}

            {newTypeCategory === "array" && (
              <>
                <div className="dialog-row">
                  <label>要素型:</label>
                  <select
                    value={newArrayElemType}
                    onChange={(e) => {
                      const val = e.target.value;
                      setNewArrayElemType(val);
                      setNewArrayElemCategory(
                        structTypes.some((st) => st.name === val)
                          ? "struct"
                          : "scalar",
                      );
                    }}
                  >
                    <optgroup label="スカラー型">
                      {dataTypes.map((t) => (
                        <option key={t.id} value={t.id}>
                          {t.displayName}
                        </option>
                      ))}
                      <option value="STRING">STRING</option>
                    </optgroup>
                    {structTypes.length > 0 && (
                      <optgroup label="構造体型">
                        {structTypes.map((st) => (
                          <option key={st.name} value={st.name}>
                            {st.name} ({st.wordCount}W)
                          </option>
                        ))}
                      </optgroup>
                    )}
                  </select>
                </div>
                {newArrayElemCategory === "scalar" &&
                  newArrayElemType === "STRING" && (
                    <div className="dialog-row">
                      <label>バイト長:</label>
                      <input
                        type="number"
                        value={newStringLength}
                        onChange={(e) =>
                          setNewStringLength(parseInt(e.target.value) || 1)
                        }
                        min={1}
                        max={256}
                      />
                    </div>
                  )}
                <div className="dialog-row">
                  <label>次元数:</label>
                  <select
                    value={newDimCount}
                    onChange={(e) => setNewDimCount(parseInt(e.target.value))}
                  >
                    <option value={1}>1</option>
                    <option value={2}>2</option>
                    <option value={3}>3</option>
                  </select>
                </div>
                {[0, 1, 2].slice(0, newDimCount).map((i) => (
                  <React.Fragment key={i}>
                    <div className="dialog-row">
                      <label>開始インデックス({i + 1}次元目):</label>
                      <input
                        type="number"
                        value={newDimBounds[i].lower}
                        onChange={(e) => {
                          const val = parseInt(e.target.value);
                          if (isNaN(val)) return;
                          setNewDimBounds((prev) => {
                            const next = [...prev];
                            next[i] = { ...next[i], lower: val };
                            return next;
                          });
                        }}
                      />
                    </div>
                    <div className="dialog-row">
                      <label>終了インデックス({i + 1}次元目):</label>
                      <input
                        type="number"
                        value={newDimBounds[i].upper}
                        onChange={(e) => {
                          const val = parseInt(e.target.value);
                          if (isNaN(val)) return;
                          setNewDimBounds((prev) => {
                            const next = [...prev];
                            next[i] = { ...next[i], upper: val };
                            return next;
                          });
                        }}
                      />
                    </div>
                  </React.Fragment>
                ))}
              </>
            )}

            {newTypeCategory === "struct" && (
              <div className="dialog-row">
                <label>構造体型:</label>
                <select
                  value={newDataType}
                  onChange={(e) => setNewDataType(e.target.value)}
                >
                  {structTypes.map((st) => (
                    <option key={st.name} value={st.name}>
                      {st.name} ({st.wordCount}ワード)
                    </option>
                  ))}
                </select>
              </div>
            )}
          </div>
          <div className="dialog-buttons">
            <button
              onClick={() => setIsOpen(false)}
              className="btn-secondary"
            >
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
