import { application } from "../../wailsjs/go/models";
import { Eraser, Send } from "lucide-react";
import {
  clientStatusLabel as statusLabel,
  formatClientElapsed as formatElapsed,
  formatClientTime as formatTime,
} from "./clientFormat";

interface ClientCommandLogProps {
  logs: application.ClientCommandLogDTO[]; // 新しい順
  selected: application.ClientCommandLogDTO | null;
  sending: boolean;
  onSelect: (seq: number) => void;
  onResend: (requestHex: string) => void;
  onClear: () => void;
}

// コマンドの結果（選んだ 1 件の詳細）と履歴の一覧
export function ClientCommandLog({
  logs,
  selected,
  sending,
  onSelect,
  onResend,
  onClear,
}: ClientCommandLogProps) {
  return (
    <div className="client-log">
      {selected ? (
        <div className="client-result" data-testid="client-result">
          <div className="client-result-header">
            <span className={`client-status client-status-${selected.status}`}>
              {statusLabel(selected.status)}
            </span>
            <span className="client-result-summary">{selected.summary}</span>
            <span className="client-result-elapsed">
              {formatElapsed(selected.elapsedMs)}
            </span>
          </div>
          {selected.error && (
            <div className="client-result-error">{selected.error}</div>
          )}
          <table className="client-bytes">
            <tbody>
              <tr>
                <th>送信</th>
                <td>
                  <code className="client-hex">
                    {selected.sent || "（なし）"}
                  </code>
                </td>
              </tr>
              <tr>
                <th>受信</th>
                <td>
                  <code className="client-hex">
                    {selected.received || "（なし）"}
                  </code>
                </td>
              </tr>
            </tbody>
          </table>
          {selected.values && selected.values.length > 0 && (
            <table className="variable-table client-values">
              <tbody>
                {selected.values.map((v, i) => (
                  <tr key={i}>
                    <th>{v.label}</th>
                    <td>{v.value}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      ) : (
        <div className="client-note client-result-empty">
          まだコマンドを送っていません。
        </div>
      )}

      <div className="client-history-header">
        <span>履歴（{logs.length} 件）</span>
        <button
          className="btn-secondary btn-small"
          onClick={onClear}
          disabled={logs.length === 0}
          title="すべての接続先の履歴を消す"
        >
          <Eraser className="icon-left" size={12} />
          すべて消す
        </button>
      </div>
      {logs.length > 0 && (
        <div className="client-history-scroll">
          <table className="variable-table client-history">
            <thead>
              <tr>
                <th>時刻</th>
                <th>内容</th>
                <th>結果</th>
                <th>時間</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {logs.map((l) => (
                <tr
                  key={l.seq}
                  className={
                    selected?.seq === l.seq ? "client-history-selected" : ""
                  }
                  onClick={() => onSelect(l.seq)}
                >
                  <td className="client-history-time">{formatTime(l.at)}</td>
                  <td>
                    <div>{l.summary}</div>
                    <code className="client-hex client-history-request">
                      {l.request}
                    </code>
                  </td>
                  <td>
                    <span className={`client-status client-status-${l.status}`}>
                      {statusLabel(l.status)}
                    </span>
                  </td>
                  <td className="client-history-elapsed">
                    {formatElapsed(l.elapsedMs)}
                  </td>
                  <td>
                    <button
                      className="btn-secondary btn-small"
                      title="同じリクエストをもう一度送る"
                      aria-label="もう一度送る"
                      disabled={sending}
                      onClick={(e) => {
                        e.stopPropagation();
                        onResend(l.request);
                      }}
                    >
                      <Send size={12} />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
