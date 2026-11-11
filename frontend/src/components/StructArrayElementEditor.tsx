import React, { useState } from "react";

// 構造体型配列の要素エディタ（ローカルで展開/折りたたみ状態を管理）
export const StructArrayElementEditor = ({
  elemType,
  elem,
  onChange,
  renderEditor,
}: {
  elemType: string;
  elem: any;
  onChange: (v: any) => void;
  renderEditor: (
    dataType: string,
    value: any,
    onChange: (v: any) => void,
  ) => JSX.Element;
}) => {
  const [isExpanded, setIsExpanded] = useState(false);
  return (
    <div
      style={{
        border: "1px solid #444",
        borderRadius: "3px",
        padding: "2px 4px",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          cursor: "pointer",
          gap: "4px",
        }}
        onClick={() => setIsExpanded(!isExpanded)}
      >
        <span style={{ fontSize: "0.8em" }}>
          {isExpanded ? "\u25BC" : "\u25B6"}
        </span>
        <span style={{ fontSize: "0.8em", color: "#aaa" }}>{elemType}</span>
      </div>
      {isExpanded && (
        <div style={{ marginTop: "4px" }}>
          {renderEditor(elemType, elem, onChange)}
        </div>
      )}
    </div>
  );
};
