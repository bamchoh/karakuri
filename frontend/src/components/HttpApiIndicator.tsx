import { useEffect, useState } from "react";
import { GetHTTPAPIPort, SetHTTPAPIPort } from "../../wailsjs/go/main/App";

// HTTP API の URL の表示と、ポートの変更。画面の下の帯（ログの見出しの行）の右端に出す
export function HttpApiIndicator() {
  const [port, setPort] = useState<number>(8765);
  const [editing, setEditing] = useState(false);
  const [input, setInput] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    GetHTTPAPIPort().then(setPort);
  }, []);

  const startEdit = () => {
    setInput(String(port));
    setError(null);
    setEditing(true);
  };

  const save = async () => {
    const value = parseInt(input, 10);
    if (isNaN(value) || value < 1024 || value > 65535) {
      setError("1024〜65535 の範囲で入力してください");
      return;
    }
    try {
      await SetHTTPAPIPort(value);
      setPort(value);
      setEditing(false);
      setError(null);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") save();
    if (e.key === "Escape") setEditing(false);
  };

  if (editing) {
    return (
      <div className="httpapi-indicator">
        <div className="httpapi-edit-form">
          {error && <span className="httpapi-error">{error}</span>}
          <span className="httpapi-label">API port:</span>
          <input
            type="number"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            min={1024}
            max={65535}
            className="httpapi-port-input"
            aria-label="HTTP API のポート"
            autoFocus
          />
          <button className="httpapi-save-btn" onClick={save}>
            保存
          </button>
          <button
            className="httpapi-cancel-btn"
            onClick={() => setEditing(false)}
            title="キャンセル"
            aria-label="ポートの変更をやめる"
          >
            ✕
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="httpapi-indicator" title="HTTP API の URL">
      <span className="httpapi-label">API</span>
      <span className="httpapi-url">http://localhost:{port}/api</span>
      <button
        className="httpapi-edit-btn"
        onClick={startEdit}
        title="ポートを変更"
        aria-label="HTTP API のポートを変更"
      >
        ✎
      </button>
    </div>
  );
}
