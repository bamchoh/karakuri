import { useState } from "react";
import {
  AddClientCommandGroup,
  RenameClientCommandGroup,
  RemoveClientCommandGroup,
  RenameClientSavedCommand,
  RemoveClientSavedCommand,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { FolderPlus, Pencil, Send, Trash2, Upload } from "lucide-react";

interface ClientCommandGroupsProps {
  clientId: string;
  groups: application.ClientCommandGroupDTO[];
  selectedGroupId: string | null;
  onSelectGroup: (groupId: string | null) => void;
  onChanged: () => Promise<void>; // グループ・コマンドを変えたあとに読み直す
  sending: boolean;
  onSend: (
    group: application.ClientCommandGroupDTO,
    command: application.ClientSavedCommandDTO,
  ) => void;
  onLoad: (
    group: application.ClientCommandGroupDTO,
    command: application.ClientSavedCommandDTO,
  ) => void;
}

type Editing = { kind: "group" | "command"; id: string; value: string } | null;

// 名前をその場で変える入力欄（Enter で決定、Esc で取り消し）
function NameEditor({
  value,
  label,
  onChange,
  onCommit,
  onCancel,
}: {
  value: string;
  label: string;
  onChange: (v: string) => void;
  onCommit: () => void;
  onCancel: () => void;
}) {
  return (
    <input
      className="client-name-editor"
      type="text"
      value={value}
      autoFocus
      aria-label={label}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Enter") onCommit();
        if (e.key === "Escape") {
          e.stopPropagation();
          onCancel();
        }
      }}
      onBlur={onCommit}
    />
  );
}

// コマンドグループ: 保存したコマンドをグループに分けて並べる。グループを選ぶと、そのコマンドが並び、1 つずつ送れる
export function ClientCommandGroups({
  clientId,
  groups,
  selectedGroupId,
  onSelectGroup,
  onChanged,
  sending,
  onSend,
  onLoad,
}: ClientCommandGroupsProps) {
  const [editing, setEditing] = useState<Editing>(null);
  const [error, setError] = useState<string | null>(null);

  const group = groups.find((g) => g.id === selectedGroupId) ?? null;

  const run = async (fn: () => Promise<unknown>) => {
    try {
      setError(null);
      await fn();
    } catch (e) {
      setError(String(e));
    }
    await onChanged();
  };

  const handleAddGroup = () =>
    run(async () => {
      const g = await AddClientCommandGroup(clientId, "");
      onSelectGroup(g.id);
      setEditing({ kind: "group", id: g.id, value: g.name });
    });

  const handleRemoveGroup = () => {
    if (!group) return;
    const n = group.commands.length;
    if (
      n > 0 &&
      !confirm(
        `グループ「${group.name}」と、保存した ${n} 個のコマンドを削除しますか?`,
      )
    )
      return;
    run(() => RemoveClientCommandGroup(clientId, group.id));
  };

  const commitEditing = () => {
    const e = editing;
    if (!e || !group) return;
    setEditing(null);
    if (e.kind === "group") {
      if (e.value.trim() === "" || e.value === group.name) return;
      run(() => RenameClientCommandGroup(clientId, e.id, e.value));
    } else {
      const cmd = group.commands.find((c) => c.id === e.id);
      if (!cmd || e.value === cmd.name) return;
      run(() => RenameClientSavedCommand(clientId, group.id, e.id, e.value));
    }
  };

  return (
    <div className="client-command-groups">
      {error && <div className="error-message config-error">{error}</div>}

      <div className="vscode-setting-item client-group-bar">
        {editing?.kind === "group" && group ? (
          <NameEditor
            value={editing.value}
            label="グループの名前"
            onChange={(v) => setEditing({ ...editing, value: v })}
            onCommit={commitEditing}
            onCancel={() => setEditing(null)}
          />
        ) : (
          <select
            value={selectedGroupId ?? ""}
            onChange={(e) => onSelectGroup(e.target.value || null)}
            aria-label="コマンドグループ"
            disabled={groups.length === 0}
          >
            {groups.length === 0 && <option value="">（グループなし）</option>}
            {groups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}（{g.commands.length}）
              </option>
            ))}
          </select>
        )}
        <button
          className="btn-secondary btn-small"
          onClick={handleAddGroup}
          title="グループを追加"
          aria-label="グループを追加"
        >
          <FolderPlus size={14} />
        </button>
        <button
          className="btn-secondary btn-small"
          onClick={() =>
            group &&
            setEditing({ kind: "group", id: group.id, value: group.name })
          }
          disabled={!group}
          title="グループの名前を変える"
          aria-label="グループの名前を変える"
        >
          <Pencil size={14} />
        </button>
        <button
          className="btn-secondary btn-small"
          onClick={handleRemoveGroup}
          disabled={!group}
          title="グループを削除"
          aria-label="グループを削除"
        >
          <Trash2 size={14} />
        </button>
      </div>

      {groups.length === 0 ? (
        <div className="client-note client-groups-empty">
          コマンドグループはありません。
          <FolderPlus size={12} />
          でグループを作り、下のコマンド欄で作ったコマンドを「保存」すると、ここから送れます。
        </div>
      ) : group && group.commands.length === 0 ? (
        <div className="client-note client-groups-empty">
          このグループに保存したコマンドはありません。下のコマンド欄で作って「「
          {group.name}」に保存」を押してください。
        </div>
      ) : (
        group && (
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
                {group.commands.map((c) => (
                  <tr key={c.id} data-testid="client-saved-command">
                    <td className="client-saved-name">
                      {editing?.kind === "command" && editing.id === c.id ? (
                        <NameEditor
                          value={editing.value}
                          label="コマンドの名前"
                          onChange={(v) => setEditing({ ...editing, value: v })}
                          onCommit={commitEditing}
                          onCancel={() => setEditing(null)}
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
                        onClick={() => onSend(group, c)}
                        disabled={sending}
                        title="送信"
                        aria-label="送信"
                      >
                        <Send size={12} />
                      </button>
                      <button
                        className="btn-secondary btn-small"
                        onClick={() => onLoad(group, c)}
                        title="組み立て欄に読み込む（中身を変えて送る・上書き保存する）"
                        aria-label="組み立て欄に読み込む"
                      >
                        <Upload size={12} />
                      </button>
                      <button
                        className="btn-secondary btn-small"
                        onClick={() =>
                          setEditing({
                            kind: "command",
                            id: c.id,
                            value: c.name,
                          })
                        }
                        title="名前を変える"
                        aria-label="名前を変える"
                      >
                        <Pencil size={12} />
                      </button>
                      <button
                        className="btn-secondary btn-small"
                        onClick={() =>
                          run(() =>
                            RemoveClientSavedCommand(clientId, group.id, c.id),
                          )
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
        )
      )}
    </div>
  );
}
