import { useState } from "react";
import {
  RenameClientSavedCommand,
  RemoveClientSavedCommand,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { Pencil, Save, Send, Trash2, Upload } from "lucide-react";

interface ClientSavedCommandsProps {
  clientId: string;
  commands: application.ClientSavedCommandDTO[];
  onChanged: () => Promise<void>; // 名前の変更・削除のあとに読み直す
  sending: boolean;
  onSend: (command: application.ClientSavedCommandDTO) => void;
  onLoad: (command: application.ClientSavedCommandDTO) => void;
}

// 保存したコマンドの一覧。組み立てたコマンドを名前を付けて保存しておき、あとから 1 つずつ送る
export function ClientSavedCommands({
  clientId,
  commands,
  onChanged,
  sending,
  onSend,
  onLoad,
}: ClientSavedCommandsProps) {
  const [editing, setEditing] = useState<{ id: string; value: string } | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);

  const run = async (fn: () => Promise<unknown>) => {
    try {
      setError(null);
      await fn();
    } catch (e) {
      setError(String(e));
    }
    await onChanged();
  };

  // 名前の変更を決める（Enter・フォーカスが外れたとき）
  const commitEditing = () => {
    const e = editing;
    if (!e) return;
    setEditing(null);
    const cmd = commands.find((c) => c.id === e.id);
    if (!cmd || e.value === cmd.name) return;
    run(() => RenameClientSavedCommand(clientId, e.id, e.value));
  };

  return (
    <div className="client-saved-commands-area">
      {error && <div className="error-message config-error">{error}</div>}
      {commands.length === 0 ? (
        <div className="client-note client-groups-empty">
          保存したコマンドはありません。下のコマンド欄で作って
          <Save size={12} />
          「コマンドを保存」を押すと、ここに並び、あとから送れます。
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
                  <td className="client-saved-name">
                    {editing?.id === c.id ? (
                      <input
                        className="client-name-editor"
                        type="text"
                        value={editing.value}
                        autoFocus
                        aria-label="コマンドの名前"
                        onChange={(e) =>
                          setEditing({ id: c.id, value: e.target.value })
                        }
                        onKeyDown={(e) => {
                          if (e.key === "Enter") commitEditing();
                          if (e.key === "Escape") {
                            e.stopPropagation();
                            setEditing(null);
                          }
                        }}
                        onBlur={commitEditing}
                      />
                    ) : (
                      c.name
                    )}
                  </td>
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
                      onClick={() => onLoad(c)}
                      title="組み立て欄に読み込む（中身を変えて送る・上書き保存する）"
                      aria-label="組み立て欄に読み込む"
                    >
                      <Upload size={12} />
                    </button>
                    <button
                      className="btn-secondary btn-small"
                      onClick={() => setEditing({ id: c.id, value: c.name })}
                      title="名前を変える"
                      aria-label="名前を変える"
                    >
                      <Pencil size={12} />
                    </button>
                    <button
                      className="btn-secondary btn-small"
                      onClick={() =>
                        run(() => RemoveClientSavedCommand(clientId, c.id))
                      }
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
