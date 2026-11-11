import React, { forwardRef, useImperativeHandle, useRef, useState } from "react";
import { FocusTrap } from "./FocusTrap";
import { UpdateVariable } from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { useEscapeKey } from "./useEscapeKey";
import {
  buildArrayTypeFE,
  isArrayType,
  isStructType,
  parseAllDimsFE,
  resolveBaseElemType,
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
    const [metaEditVariableId, setMetaEditVariableId] = useState("");
    const [metaEditName, setMetaEditName] = useState("");
    const [metaEditDataType, setMetaEditDataType] = useState("INT");
    const [metaEditTypeCategory, setMetaEditTypeCategory] = useState<
      "scalar" | "array" | "struct"
    >("scalar");
    const [metaEditArrayElemType, setMetaEditArrayElemType] = useState("INT");
    const [metaEditArrayElemCategory, setMetaEditArrayElemCategory] = useState<
      "scalar" | "struct"
    >("scalar");
    const [metaEditDimCount, setMetaEditDimCount] = useState(1);
    const [metaEditDimBounds, setMetaEditDimBounds] = useState<
      Array<{ lower: number; upper: number }>
    >([
      { lower: 0, upper: 9 },
      { lower: 0, upper: 4 },
      { lower: 0, upper: 2 },
    ]);
    const [metaEditStringLength, setMetaEditStringLength] = useState(20);

    // 変数メタデータ編集ダイアログを開く
    const open = (v: application.VariableDTO) => {
      setMetaEditVariableId(v.id);
      setMetaEditName(v.name);

      const dt = v.dataType;
      if (isArrayType(dt)) {
        const allDims = parseAllDimsFE(dt);
        if (allDims) {
          setMetaEditTypeCategory("array");
          setMetaEditDimCount(allDims.dims.length);
          setMetaEditDimBounds([
            allDims.dims[0] ?? { lower: 0, upper: 9 },
            allDims.dims[1] ?? { lower: 0, upper: 4 },
            allDims.dims[2] ?? { lower: 0, upper: 2 },
          ]);
          const base = allDims.baseElemType;
          if (base.startsWith("STRING[")) {
            const sLen = base.match(/^STRING\[(\d+)\]$/);
            setMetaEditArrayElemCategory("scalar");
            setMetaEditArrayElemType("STRING");
            setMetaEditStringLength(sLen ? parseInt(sLen[1]) : 20);
          } else if (isStructType(base)) {
            setMetaEditArrayElemCategory("struct");
            setMetaEditArrayElemType(base);
          } else {
            setMetaEditArrayElemCategory("scalar");
            setMetaEditArrayElemType(base);
          }
        }
      } else if (dt.startsWith("STRING[")) {
        const sLen = dt.match(/^STRING\[(\d+)\]$/);
        setMetaEditTypeCategory("scalar");
        setMetaEditDataType("STRING");
        setMetaEditStringLength(sLen ? parseInt(sLen[1]) : 20);
      } else if (isStructType(dt)) {
        setMetaEditTypeCategory("struct");
        setMetaEditDataType(dt);
      } else {
        setMetaEditTypeCategory("scalar");
        setMetaEditDataType(dt);
      }

      setIsOpen(true);
    };

    const isOpenRef = useRef(isOpen);
    isOpenRef.current = isOpen;
    useImperativeHandle(ref, () => ({ open, isOpen: () => isOpenRef.current }));
    useEscapeKey(isOpen, () => setIsOpen(false));

    // 変数メタデータ（名前・データタイプ）を保存する
    const handleSaveMetaEdit = async () => {
      if (!metaEditName.trim()) return;
      try {
        let dataType = metaEditDataType;
        if (metaEditTypeCategory === "array") {
          const baseElemType = resolveBaseElemType(
            metaEditArrayElemCategory,
            metaEditArrayElemType,
            metaEditStringLength,
          );
          dataType = buildArrayTypeFE(
            baseElemType,
            metaEditDimBounds.slice(0, metaEditDimCount),
          );
        } else if (
          metaEditTypeCategory === "scalar" &&
          metaEditDataType === "STRING"
        ) {
          dataType = `STRING[${metaEditStringLength}]`;
        }
        await UpdateVariable(metaEditVariableId, metaEditName.trim(), dataType);
        await onChanged();
        setIsOpen(false);
      } catch (e) {
        console.error("Failed to update variable:", e);
        alert("変数の更新に失敗しました: " + e);
      }
    };

    if (!isOpen) return null;
    return (
      <FocusTrap onConfirm={handleSaveMetaEdit}>
        <div className="dialog">
          <h3>変数を編集</h3>
          <div className="dialog-content">
            <div className="dialog-row">
              <label>変数名:</label>
              <input
                type="text"
                value={metaEditName}
                onChange={(e) => setMetaEditName(e.target.value)}
                autoFocus
              />
            </div>
            <div className="dialog-row">
              <label>型カテゴリ:</label>
              <select
                value={metaEditTypeCategory}
                onChange={(e) => {
                  const cat = e.target.value as "scalar" | "array" | "struct";
                  setMetaEditTypeCategory(cat);
                  if (cat === "scalar") setMetaEditDataType("INT");
                  else if (cat === "struct" && structTypes.length > 0)
                    setMetaEditDataType(structTypes[0].name);
                }}
              >
                <option value="scalar">スカラー型</option>
                <option value="array">配列型</option>
                {structTypes.length > 0 && (
                  <option value="struct">構造体型</option>
                )}
              </select>
            </div>

            {metaEditTypeCategory === "scalar" && (
              <>
                <div className="dialog-row">
                  <label>データ型:</label>
                  <select
                    value={metaEditDataType}
                    onChange={(e) => setMetaEditDataType(e.target.value)}
                  >
                    {dataTypes.map((t) => (
                      <option key={t.id} value={t.id} title={t.description}>
                        {t.displayName} ({t.description})
                      </option>
                    ))}
                    <option value="STRING">STRING (文字列)</option>
                  </select>
                </div>
                {metaEditDataType === "STRING" && (
                  <div className="dialog-row">
                    <label>バイト長:</label>
                    <input
                      type="number"
                      value={metaEditStringLength}
                      onChange={(e) =>
                        setMetaEditStringLength(parseInt(e.target.value) || 1)
                      }
                      min={1}
                      max={256}
                    />
                  </div>
                )}
              </>
            )}

            {metaEditTypeCategory === "array" && (
              <>
                <div className="dialog-row">
                  <label>要素型:</label>
                  <select
                    value={metaEditArrayElemType}
                    onChange={(e) => {
                      const val = e.target.value;
                      setMetaEditArrayElemType(val);
                      setMetaEditArrayElemCategory(
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
                {metaEditArrayElemCategory === "scalar" &&
                  metaEditArrayElemType === "STRING" && (
                    <div className="dialog-row">
                      <label>バイト長:</label>
                      <input
                        type="number"
                        value={metaEditStringLength}
                        onChange={(e) =>
                          setMetaEditStringLength(
                            parseInt(e.target.value) || 1,
                          )
                        }
                        min={1}
                        max={256}
                      />
                    </div>
                  )}
                <div className="dialog-row">
                  <label>次元数:</label>
                  <select
                    value={metaEditDimCount}
                    onChange={(e) =>
                      setMetaEditDimCount(parseInt(e.target.value))
                    }
                  >
                    <option value={1}>1</option>
                    <option value={2}>2</option>
                    <option value={3}>3</option>
                  </select>
                </div>
                {[0, 1, 2].slice(0, metaEditDimCount).map((i) => (
                  <React.Fragment key={i}>
                    <div className="dialog-row">
                      <label>開始インデックス({i + 1}次元目):</label>
                      <input
                        type="number"
                        value={metaEditDimBounds[i].lower}
                        onChange={(e) => {
                          const val = parseInt(e.target.value);
                          if (isNaN(val)) return;
                          setMetaEditDimBounds((prev) => {
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
                        value={metaEditDimBounds[i].upper}
                        onChange={(e) => {
                          const val = parseInt(e.target.value);
                          if (isNaN(val)) return;
                          setMetaEditDimBounds((prev) => {
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

            {metaEditTypeCategory === "struct" && (
              <div className="dialog-row">
                <label>構造体型:</label>
                <select
                  value={metaEditDataType}
                  onChange={(e) => setMetaEditDataType(e.target.value)}
                >
                  {structTypes.map((st) => (
                    <option key={st.name} value={st.name}>
                      {st.name} ({st.wordCount}ワード)
                    </option>
                  ))}
                </select>
              </div>
            )}

            <p
              style={{ fontSize: "0.85em", color: "#aaa", margin: "8px 0 0" }}
            >
              ※ データタイプを変更すると値はデフォルト値にリセットされます
            </p>
          </div>
          <div className="dialog-buttons">
            <button
              onClick={() => setIsOpen(false)}
              className="btn-secondary"
            >
              キャンセル
            </button>
            <button onClick={handleSaveMetaEdit} className="btn-primary">
              保存
            </button>
          </div>
        </div>
      </FocusTrap>
    );
  },
);
