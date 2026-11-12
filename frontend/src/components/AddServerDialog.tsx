import { useState } from "react";
import { FocusTrap } from "./FocusTrap";
import { application } from "../../wailsjs/go/models";
import { useEscapeKey } from "./useEscapeKey";

interface Props {
  // まだ追加していないプロトコル（1 つ以上）
  protocols: application.ProtocolInfoDTO[];
  // 追加を実行する。失敗したときの表示は呼び出し側が行い、ダイアログは開いたままにする
  onAdd: (protocolType: string, variantId: string) => void;
  onClose: () => void;
}

// サーバー追加ダイアログ。開くたびにマウントし、先頭のプロトコルを選んだ状態で始める
export function AddServerDialog({ protocols, onAdd, onClose }: Props) {
  const [selectedProtocol, setSelectedProtocol] = useState(
    protocols[0]?.type ?? "",
  );
  const [selectedVariant, setSelectedVariant] = useState(
    protocols[0]?.variants[0]?.id || "",
  );
  useEscapeKey(true, onClose);

  const handleProtocolChange = (protocolType: string) => {
    const proto = protocols.find((p) => p.type === protocolType);
    setSelectedProtocol(protocolType);
    setSelectedVariant(proto?.variants[0]?.id || "");
  };

  const variants =
    protocols.find((p) => p.type === selectedProtocol)?.variants ?? [];
  const handleAdd = () => onAdd(selectedProtocol, selectedVariant);

  return (
    <FocusTrap onConfirm={handleAdd}>
      <div className="dialog">
        <h3>サーバーを追加</h3>
        <div className="dialog-content">
          <div className="form-group">
            <label>プロトコル</label>
            <select
              value={selectedProtocol}
              onChange={(e) => handleProtocolChange(e.target.value)}
            >
              {protocols.map((p) => (
                <option key={p.type} value={p.type}>
                  {p.displayName}
                </option>
              ))}
            </select>
          </div>
          {variants.length > 1 && (
            <div className="form-group">
              <label>バリアント</label>
              <select
                value={selectedVariant}
                onChange={(e) => setSelectedVariant(e.target.value)}
              >
                {variants.map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.displayName}
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>
        <div className="dialog-buttons">
          <button onClick={onClose} className="btn-secondary">
            キャンセル
          </button>
          <button onClick={handleAdd} className="btn-primary">
            追加
          </button>
        </div>
      </div>
    </FocusTrap>
  );
}
