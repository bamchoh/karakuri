import { useState } from "react";
import { RemoveClientSavedCommand } from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { Pencil, Send, SquarePlus, Trash2 } from "lucide-react";

interface ClientSavedCommandsProps {
  clientId: string;
  commands: application.ClientSavedCommandDTO[];
  onChanged: () => Promise<void>; // 削除のあとに読み直す
  sending: boolean;
  onNew: () => void; // 新しいコマンドのダイアログを開く
  onEdit: (command: application.ClientSavedCommandDTO) => void; // 保存したコマンドをダイアログで開く
  onSend: (command: application.ClientSavedCommandDTO) => void;
}

// 保存したコマンドの一覧。組み立てたコマンドを名前を付けて保存しておき、あとから 1 つずつ送る。
// 作る・直すのはダイアログ（ClientCommandDialog）で行う
export function ClientSavedCommands({
  clientId,
  commands,
  onChanged,
  sending,
  onNew,
  onEdit,
  onSend,
}: ClientSavedCommandsProps) {
  const [error, setError] = useState<string | null>(null);

  const handleRemove = async (c: application.ClientSavedCommandDTO) => {
    try {
      setError(null);
      await RemoveClientSavedCommand(clientId, c.id);
    } catch (e) {
      setError(String(e));
    }
    await onChanged();
  };

  return (
    <div className="client-saved-commands-area">
      {error && <div className="error-message config-error">{error}</div>}
      <div className="client-saved-toolbar">
        <button className="btn-primary" onClick={onNew}>
          <SquarePlus className="icon-left" size={14} />
          新しいコマンド
        </button>
      </div>
      {commands.length === 0 ? (
        <div className="client-note client-groups-empty">
          保存したコマンドはありません。「新しいコマンド」で組み立てて送れます。
          「保存して閉じる」で保存すると、ここに並び、あとから送れます。
        </div>
      ) : (
        <div className="client-history-scroll client-saved-scroll">
          <table className="variable-table client-saved-commands">
            <thead>
              <tr>
                <th>名前</th>
                <th>内容</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {commands.map((c) => (
                <tr key={c.id} data-testid="client-saved-command">
                  <td className="client-saved-name">{c.name}</td>
                  <td>
                    <div>{c.summary}</div>
                    <code className="client-hex client-history-request">
                      {c.request}
                    </code>
                  </td>
                  <td className="client-saved-actions">
                    <button
                      className="btn-primary btn-small"
                      onClick={() => onSend(c)}
                      disabled={sending}
                      title="送信"
                      aria-label="送信"
                    >
                      <Send size={12} />
                    </button>
                    <button
                      className="btn-secondary btn-small"
                      onClick={() => onEdit(c)}
                      title="編集（中身・名前を変える）"
                      aria-label="編集"
                    >
                      <Pencil size={12} />
                    </button>
                    <button
                      className="btn-secondary btn-small"
                      onClick={() => handleRemove(c)}
                      title="削除"
                      aria-label="削除"
                    >
                      <Trash2 size={12} />
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
