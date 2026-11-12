import React, { forwardRef, useImperativeHandle, useRef, useState } from "react";
import { FocusTrap } from "./FocusTrap";
import {
  DeleteStructType,
  RegisterStructType,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { useEscapeKey } from "./useEscapeKey";
import {
  buildArrayTypeFE,
  isArrayType,
  parseAllDimsFE,
  resolveBaseElemType,
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

// 構造体型の登録・編集・削除ダイアログ。閉じると入力中のフォームは初期状態に戻る。
export const StructTypeDialog = forwardRef<StructTypeDialogHandle, Props>(
  function StructTypeDialog({ dataTypes, structTypes, onChanged }, ref) {
    const [isOpen, setIsOpen] = useState(false);
    // 構造体型定義フォーム
    const [structTypeName, setStructTypeName] = useState("");
    const [editingStructTypeName, setEditingStructTypeName] = useState<
      string | null
    >(null);
    const [structTypeFields, setStructTypeFields] = useState<
      {
        name: string;
        category: "scalar" | "struct" | "array";
        dataType: string;
        stringLength: number;
        arrayElemType: string;
        arrayElemCategory: "scalar" | "struct";
        arrayDimCount: number;
        arrayDimBounds: [
          { lower: number; upper: number },
          { lower: number; upper: number },
          { lower: number; upper: number },
        ];
      }[]
    >([
      {
        name: "",
        category: "scalar",
        dataType: "INT",
        stringLength: 20,
        arrayElemType: "INT",
        arrayElemCategory: "scalar",
        arrayDimCount: 1,
        arrayDimBounds: [
          { lower: 0, upper: 9 },
          { lower: 0, upper: 4 },
          { lower: 0, upper: 4 },
        ],
      },
    ]);

    // 閉じて、フォームを初期状態に戻す
    const close = () => {
      setIsOpen(false);
      setEditingStructTypeName(null);
      setStructTypeName("");
      setStructTypeFields([
        {
          name: "",
          category: "scalar",
          dataType: "INT",
          stringLength: 20,
          arrayElemType: "INT",
          arrayElemCategory: "scalar",
          arrayDimCount: 1,
          arrayDimBounds: [
            { lower: 0, upper: 9 },
            { lower: 0, upper: 4 },
            { lower: 0, upper: 4 },
          ],
        },
      ]);
    };

    const isOpenRef = useRef(isOpen);
    isOpenRef.current = isOpen;
    useImperativeHandle(
      ref,
      () => ({ open: () => setIsOpen(true), isOpen: () => isOpenRef.current }),
      [],
    );
    useEscapeKey(isOpen, close);

    const resolveFieldDataType = (
      field: (typeof structTypeFields)[0],
    ): string => {
      if (field.category === "scalar") {
        if (field.dataType === "STRING") return `STRING[${field.stringLength}]`;
        return field.dataType;
      }
      if (field.category === "struct") return field.dataType;
      // array: IEC 61131-3 形式で生成（構造体フィールドは0ベース）
      const baseElem = resolveBaseElemType(
        field.arrayElemCategory,
        field.arrayElemType,
        field.stringLength,
      );
      const dims = field.arrayDimBounds.slice(0, field.arrayDimCount);
      return buildArrayTypeFE(baseElem, dims);
    };

    // 構造体型を登録または更新
    const handleRegisterStructType = async () => {
      if (!structTypeName.trim()) return;
      const validFields = structTypeFields.filter((f) => f.name.trim());
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
            dataType: resolveFieldDataType(f),
            offset: 0,
          })),
          wordCount: 0,
        } as application.StructTypeDTO);
        await onChanged();
        setStructTypeName("");
        setEditingStructTypeName(null);
        setStructTypeFields([
          {
            name: "",
            category: "scalar",
            dataType: "INT",
            stringLength: 20,
            arrayElemType: "INT",
            arrayElemCategory: "scalar",
            arrayDimCount: 1,
            arrayDimBounds: [
              { lower: 0, upper: 9 },
              { lower: 0, upper: 4 },
              { lower: 0, upper: 4 },
            ],
          },
        ]);
      } catch (e) {
        console.error("Failed to register struct type:", e);
        alert("構造体型の登録に失敗しました: " + e);
      }
    };

    // 構造体型を編集
    const handleEditStructType = (st: application.StructTypeDTO) => {
      setEditingStructTypeName(st.name);
      setStructTypeName(st.name);

      // フィールドをフォーム形式に変換
      const formFields = st.fields.map((f) => {
        const field: (typeof structTypeFields)[0] = {
          name: f.name,
          category: "scalar",
          dataType: f.dataType,
          stringLength: 20,
          arrayElemType: "INT",
          arrayElemCategory: "scalar",
          arrayDimCount: 1,
          arrayDimBounds: [
            { lower: 0, upper: 9 },
            { lower: 0, upper: 4 },
            { lower: 0, upper: 4 },
          ],
        };

        // データ型を解析してカテゴリを判定
        if (isArrayType(f.dataType)) {
          field.category = "array";
          // 全次元とベース要素型を収集（フラット形式・入れ子形式両対応）
          const allDims: Array<{ lower: number; upper: number }> = [];
          let currentType = f.dataType;
          while (isArrayType(currentType)) {
            const flat = parseAllDimsFE(currentType);
            if (flat) {
              allDims.push(...flat.dims);
              currentType = flat.baseElemType;
            } else {
              break;
            }
          }
          field.arrayDimCount = Math.min(Math.max(allDims.length, 1), 3);
          field.arrayDimBounds = [
            allDims[0] ?? { lower: 0, upper: 9 },
            allDims[1] ?? { lower: 0, upper: 4 },
            allDims[2] ?? { lower: 0, upper: 4 },
          ] as [
            { lower: number; upper: number },
            { lower: number; upper: number },
            { lower: number; upper: number },
          ];
          // ベース要素型を解析
          if (currentType.startsWith("STRING[")) {
            const lenMatch = currentType.match(/^STRING\[(\d+)\]$/);
            field.arrayElemCategory = "scalar";
            field.arrayElemType = "STRING";
            if (lenMatch) field.stringLength = parseInt(lenMatch[1]);
          } else if (structTypes.some((s) => s.name === currentType)) {
            field.arrayElemCategory = "struct";
            field.arrayElemType = currentType;
          } else {
            field.arrayElemCategory = "scalar";
            field.arrayElemType = currentType;
          }
        } else if (f.dataType.startsWith("STRING[")) {
          field.category = "scalar";
          field.dataType = "STRING";
          const match = f.dataType.match(/^STRING\[(\d+)\]$/);
          if (match) field.stringLength = parseInt(match[1]);
        } else if (structTypes.some((s) => s.name === f.dataType)) {
          field.category = "struct";
          field.dataType = f.dataType;
        } else {
          field.category = "scalar";
          field.dataType = f.dataType;
        }

        return field;
      });

      setStructTypeFields(formFields);
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
              {structTypeFields.map((field, index) => (
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
                        setStructTypeFields(
                          structTypeFields.filter((_, i) => i !== index),
                        )
                      }
                      className="btn-small btn-danger"
                      disabled={structTypeFields.length <= 1}
                    >
                      X
                    </button>
                  </div>

                  <div className="dialog-row">
                    <label>名前:</label>
                    <input
                      type="text"
                      value={field.name}
                      onChange={(e) => {
                        const updated = [...structTypeFields];
                        updated[index] = {
                          ...updated[index],
                          name: e.target.value,
                        };
                        setStructTypeFields(updated);
                      }}
                      placeholder="フィールド名"
                    />
                  </div>

                  <div className="dialog-row">
                    <label>型カテゴリ:</label>
                    <select
                      value={field.category}
                      onChange={(e) => {
                        const updated = [...structTypeFields];
                        const cat = e.target.value as
                          | "scalar"
                          | "struct"
                          | "array";
                        updated[index] = {
                          ...updated[index],
                          category: cat,
                          dataType:
                            cat === "struct"
                              ? structTypes.length > 0
                                ? structTypes[0].name
                                : ""
                              : "INT",
                        };
                        setStructTypeFields(updated);
                      }}
                    >
                      <option value="scalar">スカラー</option>
                      <option value="array">配列</option>
                      {structTypes.length > 0 && (
                        <option value="struct">構造体</option>
                      )}
                    </select>
                  </div>

                  {field.category === "scalar" && (
                    <>
                      <div className="dialog-row">
                        <label>データ型:</label>
                        <select
                          value={field.dataType}
                          onChange={(e) => {
                            const updated = [...structTypeFields];
                            updated[index] = {
                              ...updated[index],
                              dataType: e.target.value,
                            };
                            setStructTypeFields(updated);
                          }}
                        >
                          {dataTypes.map((t) => (
                            <option
                              key={t.id}
                              value={t.id}
                              title={t.description}
                            >
                              {t.displayName} ({t.description})
                            </option>
                          ))}
                          <option value="STRING">STRING (文字列)</option>
                        </select>
                      </div>
                      {field.dataType === "STRING" && (
                        <div className="dialog-row">
                          <label>バイト長:</label>
                          <input
                            type="number"
                            value={field.stringLength}
                            onChange={(e) => {
                              const updated = [...structTypeFields];
                              updated[index] = {
                                ...updated[index],
                                stringLength: parseInt(e.target.value) || 1,
                              };
                              setStructTypeFields(updated);
                            }}
                            min={1}
                            max={256}
                          />
                        </div>
                      )}
                    </>
                  )}

                  {field.category === "struct" && (
                    <div className="dialog-row">
                      <label>構造体型:</label>
                      <select
                        value={field.dataType}
                        onChange={(e) => {
                          const updated = [...structTypeFields];
                          updated[index] = {
                            ...updated[index],
                            dataType: e.target.value,
                          };
                          setStructTypeFields(updated);
                        }}
                      >
                        {structTypes
                          .filter((st) => st.name !== structTypeName.trim())
                          .map((st) => (
                            <option key={st.name} value={st.name}>
                              {st.name} ({st.wordCount}W)
                            </option>
                          ))}
                      </select>
                    </div>
                  )}

                  {field.category === "array" && (
                    <>
                      <div className="dialog-row">
                        <label>要素型:</label>
                        <select
                          value={field.arrayElemType}
                          onChange={(e) => {
                            const updated = [...structTypeFields];
                            const val = e.target.value;
                            updated[index] = {
                              ...updated[index],
                              arrayElemType: val,
                              arrayElemCategory: structTypes
                                .filter(
                                  (st) => st.name !== structTypeName.trim(),
                                )
                                .some((st) => st.name === val)
                                ? "struct"
                                : "scalar",
                            };
                            setStructTypeFields(updated);
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
                          {structTypes.filter(
                            (st) => st.name !== structTypeName.trim(),
                          ).length > 0 && (
                            <optgroup label="構造体型">
                              {structTypes
                                .filter(
                                  (st) => st.name !== structTypeName.trim(),
                                )
                                .map((st) => (
                                  <option key={st.name} value={st.name}>
                                    {st.name} ({st.wordCount}W)
                                  </option>
                                ))}
                            </optgroup>
                          )}
                        </select>
                      </div>
                      {field.arrayElemCategory === "scalar" &&
                        field.arrayElemType === "STRING" && (
                          <div className="dialog-row">
                            <label>バイト長:</label>
                            <input
                              type="number"
                              value={field.stringLength}
                              onChange={(e) => {
                                const updated = [...structTypeFields];
                                updated[index] = {
                                  ...updated[index],
                                  stringLength: parseInt(e.target.value) || 1,
                                };
                                setStructTypeFields(updated);
                              }}
                              min={1}
                              max={256}
                            />
                          </div>
                        )}
                      <div className="dialog-row">
                        <label>次元数:</label>
                        <select
                          value={field.arrayDimCount}
                          onChange={(e) => {
                            const updated = [...structTypeFields];
                            updated[index] = {
                              ...updated[index],
                              arrayDimCount: parseInt(e.target.value),
                            };
                            setStructTypeFields(updated);
                          }}
                        >
                          <option value={1}>1</option>
                          <option value={2}>2</option>
                          <option value={3}>3</option>
                        </select>
                      </div>
                      {([0, 1, 2] as const)
                        .slice(0, field.arrayDimCount)
                        .map((dimIdx) => (
                          <React.Fragment key={dimIdx}>
                            <div className="dialog-row">
                              <label>
                                開始インデックス({dimIdx + 1}次元目):
                              </label>
                              <input
                                type="number"
                                value={field.arrayDimBounds[dimIdx].lower}
                                onChange={(e) => {
                                  const val = parseInt(e.target.value);
                                  if (isNaN(val)) return;
                                  const updated = [...structTypeFields];
                                  const newBounds = [
                                    ...updated[index].arrayDimBounds,
                                  ] as typeof field.arrayDimBounds;
                                  newBounds[dimIdx] = {
                                    ...newBounds[dimIdx],
                                    lower: val,
                                  };
                                  updated[index] = {
                                    ...updated[index],
                                    arrayDimBounds: newBounds,
                                  };
                                  setStructTypeFields(updated);
                                }}
                              />
                            </div>
                            <div className="dialog-row">
                              <label>
                                終了インデックス({dimIdx + 1}次元目):
                              </label>
                              <input
                                type="number"
                                value={field.arrayDimBounds[dimIdx].upper}
                                onChange={(e) => {
                                  const val = parseInt(e.target.value);
                                  if (isNaN(val)) return;
                                  const updated = [...structTypeFields];
                                  const newBounds = [
                                    ...updated[index].arrayDimBounds,
                                  ] as typeof field.arrayDimBounds;
                                  newBounds[dimIdx] = {
                                    ...newBounds[dimIdx],
                                    upper: val,
                                  };
                                  updated[index] = {
                                    ...updated[index],
                                    arrayDimBounds: newBounds,
                                  };
                                  setStructTypeFields(updated);
                                }}
                              />
                            </div>
                          </React.Fragment>
                        ))}
                    </>
                  )}
                </div>
              ))}
              <button
                onClick={() =>
                  setStructTypeFields([
                    ...structTypeFields,
                    {
                      name: "",
                      category: "scalar",
                      dataType: "INT",
                      stringLength: 20,
                      arrayElemType: "INT",
                      arrayElemCategory: "scalar",
                      arrayDimCount: 1,
                      arrayDimBounds: [
                        { lower: 0, upper: 9 },
                        { lower: 0, upper: 4 },
                        { lower: 0, upper: 4 },
                      ],
                    },
                  ])
                }
                className="btn-secondary"
                style={{ marginTop: "0.5rem" }}
              >
                + フィールド追加
              </button>
            </div>
          </div>
          <div className="dialog-buttons">
            <button
              onClick={() => {
                setIsOpen(false);
                setEditingStructTypeName(null);
                setStructTypeName("");
                setStructTypeFields([
                  {
                    name: "",
                    category: "scalar",
                    dataType: "INT",
                    stringLength: 20,
                    arrayElemType: "INT",
                    arrayElemCategory: "scalar",
                    arrayDimCount: 1,
                    arrayDimBounds: [
                      { lower: 0, upper: 9 },
                      { lower: 0, upper: 4 },
                      { lower: 0, upper: 4 },
                    ],
                  },
                ]);
              }}
              className="btn-secondary"
            >
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
