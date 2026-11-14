import { useState } from "react";
import { FocusTrap } from "./FocusTrap";
import { application } from "../../wailsjs/go/models";
import { useEscapeKey } from "./useEscapeKey";

interface Props {
  // クライアントとして使えるプロトコル（1 つ以上）
  protocols: application.ClientProtocolDTO[];
  // 追加を実行する。失敗したときの表示は呼び出し側が行う
  onAdd: (protocolType: string, name: string) => void;
  onClose: () => void;
}

// 接続先（クライアント）の追加ダイアログ。サーバーの追加ダイアログ（AddServerDialog）と同じ形にする。
// 開くたびにマウントし、先頭のプロトコルを選んだ状態で始める
export function AddClientDialog({ protocols, onAdd, onClose }: Props) {
  const [selectedProtocol, setSelectedProtocol] = useState(
    protocols[0]?.protocolType ?? "",
  );
  const [name, setName] = useState("");
  useEscapeKey(true, onClose);

  const handleAdd = () => onAdd(selectedProtocol, name.trim());

  return (
    <FocusTrap onConfirm={handleAdd}>
      <div className="dialog">
        <h3>接続先を追加</h3>
        <div className="dialog-content">
          <div className="form-group">
            <label>プロトコル</label>
            <select
              value={selectedProtocol}
              onChange={(e) => setSelectedProtocol(e.target.value)}
            >
              {protocols.map((p) => (
                <option key={p.protocolType} value={p.protocolType}>
                  {p.displayName}
                </option>
              ))}
            </select>
          </div>
          <div className="form-group">
            <label>名前</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="空なら自動で付けます"
            />
          </div>
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
