import { useState } from "react";

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
    <div className="struct-elem">
      <div
        className="struct-elem-header"
        onClick={() => setIsExpanded(!isExpanded)}
      >
        <span className="struct-elem-index">
          {isExpanded ? "\u25BC" : "\u25B6"}
        </span>
        <span className="struct-elem-type">{elemType}</span>
      </div>
      {isExpanded && (
        <div className="struct-elem-body">
          {renderEditor(elemType, elem, onChange)}
        </div>
      )}
    </div>
  );
};
