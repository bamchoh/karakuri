import React from "react";
import { application } from "../../wailsjs/go/models";
import { DataTypeFormValue } from "./variableTypes";

interface Props {
  value: DataTypeFormValue;
  onChange: (next: DataTypeFormValue) => void;
  dataTypes: application.DataTypeInfoDTO[];
  // 選べる構造体型。1 つもなければ、型カテゴリに「構造体」を出さない
  structTypes: application.StructTypeDTO[];
  // 選択肢の文言を短くする（構造体型のフィールド用。「スカラー型」→「スカラー」など）
  compact?: boolean;
  // スカラーのデータ型を変えたときに呼ぶ（変数追加で、初期値を既定値に戻すため）
  onScalarTypeChange?: (dataType: string) => void;
  // スカラーのとき、データ型（とバイト長）の下に出す追加の行（変数追加の「初期値」）
  scalarExtra?: React.ReactNode;
}

// データ型を選ぶ入力欄（型カテゴリ・データ型・バイト長・配列の要素型と次元）。
// 変数追加・変数の編集・構造体型のフィールドで共通に使う。
// 入力内容からデータ型の文字列を作るには buildDataTypeFromForm() を使う。
export function DataTypeFields({
  value,
  onChange,
  dataTypes,
  structTypes,
  compact = false,
  onScalarTypeChange,
  scalarExtra,
}: Props) {
  const set = (patch: Partial<DataTypeFormValue>) =>
    onChange({ ...value, ...patch });

  const setDimBound = (
    i: number,
    key: "lower" | "upper",
    input: string,
  ) => {
    const val = parseInt(input);
    if (isNaN(val)) return;
    const dimBounds = [...value.dimBounds] as DataTypeFormValue["dimBounds"];
    dimBounds[i] = { ...dimBounds[i], [key]: val };
    set({ dimBounds });
  };

  const stringLengthRow = (
    <div className="dialog-row">
      <label>バイト長:</label>
      <input
        type="number"
        value={value.stringLength}
        onChange={(e) => set({ stringLength: parseInt(e.target.value) || 1 })}
        min={1}
        max={256}
      />
    </div>
  );

  return (
    <>
      <div className="dialog-row">
        <label>型カテゴリ:</label>
        <select
          value={value.category}
          onChange={(e) => {
            const category = e.target.value as DataTypeFormValue["category"];
            // スカラーと構造体は同じ dataType を使うので、切り替えたら選び直す
            if (category === "scalar") {
              set({ category, dataType: "INT" });
            } else if (category === "struct" && structTypes.length > 0) {
              set({ category, dataType: structTypes[0].name });
            } else {
              set({ category });
            }
          }}
        >
          <option value="scalar">{compact ? "スカラー" : "スカラー型"}</option>
          <option value="array">{compact ? "配列" : "配列型"}</option>
          {structTypes.length > 0 && (
            <option value="struct">{compact ? "構造体" : "構造体型"}</option>
          )}
        </select>
      </div>

      {value.category === "scalar" && (
        <>
          <div className="dialog-row">
            <label>データ型:</label>
            <select
              value={value.dataType}
              onChange={(e) => {
                set({ dataType: e.target.value });
                onScalarTypeChange?.(e.target.value);
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
          {value.dataType === "STRING" && stringLengthRow}
          {scalarExtra}
        </>
      )}

      {value.category === "struct" && (
        <div className="dialog-row">
          <label>構造体型:</label>
          <select
            value={value.dataType}
            onChange={(e) => set({ dataType: e.target.value })}
          >
            {structTypes.map((st) => (
              <option key={st.name} value={st.name}>
                {st.name} ({st.wordCount}
                {compact ? "W" : "ワード"})
              </option>
            ))}
          </select>
        </div>
      )}

      {value.category === "array" && (
        <>
          <div className="dialog-row">
            <label>要素型:</label>
            <select
              value={value.arrayElemType}
              onChange={(e) => {
                const arrayElemType = e.target.value;
                set({
                  arrayElemType,
                  arrayElemCategory: structTypes.some(
                    (st) => st.name === arrayElemType,
                  )
                    ? "struct"
                    : "scalar",
                });
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
          {value.arrayElemCategory === "scalar" &&
            value.arrayElemType === "STRING" &&
            stringLengthRow}
          <div className="dialog-row">
            <label>次元数:</label>
            <select
              value={value.dimCount}
              onChange={(e) => set({ dimCount: parseInt(e.target.value) })}
            >
              <option value={1}>1</option>
              <option value={2}>2</option>
              <option value={3}>3</option>
            </select>
          </div>
          {[0, 1, 2].slice(0, value.dimCount).map((i) => (
            <React.Fragment key={i}>
              <div className="dialog-row">
                <label>開始インデックス({i + 1}次元目):</label>
                <input
                  type="number"
                  value={value.dimBounds[i].lower}
                  onChange={(e) => setDimBound(i, "lower", e.target.value)}
                />
              </div>
              <div className="dialog-row">
                <label>終了インデックス({i + 1}次元目):</label>
                <input
                  type="number"
                  value={value.dimBounds[i].upper}
                  onChange={(e) => setDimBound(i, "upper", e.target.value)}
                />
              </div>
            </React.Fragment>
          ))}
        </>
      )}
    </>
  );
}
