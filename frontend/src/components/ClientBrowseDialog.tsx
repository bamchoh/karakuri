import { useState, useEffect, useCallback, useRef } from "react";
import { BrowseClient } from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { ChevronDown, ChevronRight, RefreshCw } from "lucide-react";
import { FocusTrap } from "./FocusTrap";
import { useEscapeKey } from "./useEscapeKey";

type Entry = application.ClientBrowseEntryDTO;

interface ClientBrowseDialogProps {
  clientId: string;
  // 入れる先の項目。"textarea" ならいくつも選べる。browse が "all" なら、値を持たないアドレスも選べる
  field: application.FieldDTO;
  onPick: (entries: Entry[]) => void;
  onClose: () => void;
}

// ツリーの 1 つの親について、読み込んだ子と状態（キーは親のアドレス。いちばん上は ""）
interface Branch {
  open: boolean;
  loading: boolean;
  error: string | null;
  entries: Entry[] | null; // まだ読んでいなければ null
}

const ROOT = "";

// 選んだアドレスを入力欄に入れる内容にする。
// - "textarea" の項目: 選んだものを 1 行ずつ、いまの内容のあとに足す（同じ行は足さない）
// - ほかの項目: 選んだ 1 つで置き換える。同じフォームにある項目のうち、機器が内容を返したもの
//   （OPC UA の Write なら、データ型とスカラー / 配列）にも入れる
export function applyBrowsePick(
  field: application.FieldDTO,
  fields: application.FieldDTO[],
  params: Record<string, any>,
  picked: Entry[],
): Record<string, any> {
  if (picked.length === 0) return params;
  const text = (e: Entry, name: string) => e.fields?.[name] ?? e.address;
  if (field.type === "textarea") {
    const current = String(params[field.name] ?? "");
    const lines = current.split("\n").filter((l) => l.trim() !== "");
    for (const e of picked) {
      const line = text(e, field.name);
      if (!lines.some((l) => l.trim() === line.trim())) lines.push(line);
    }
    return { ...params, [field.name]: lines.join("\n") };
  }
  const entry = picked[0];
  const next = { ...params, [field.name]: text(entry, field.name) };
  for (const f of fields) {
    if (f.name === field.name || f.type === "textarea") continue;
    const v = entry.fields?.[f.name];
    if (v !== undefined) next[f.name] = v;
  }
  return next;
}

// 機器が持つアドレス（OPC UA ならノード）をツリーでたどって選ぶダイアログ。
// 子は、開いたときに読む。開くたびにマウントする
export function ClientBrowseDialog({
  clientId,
  field,
  onPick,
  onClose,
}: ClientBrowseDialogProps) {
  const multi = field.type === "textarea";
  const any = field.browse === "all";
  const [branches, setBranches] = useState<Record<string, Branch>>({});
  const [selected, setSelected] = useState<Entry[]>([]);
  useEscapeKey(true, onClose);

  // 閉じたあとに届いた応答で state を変えない
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const load = useCallback(
    async (parent: string) => {
      setBranches((prev) => ({
        ...prev,
        [parent]: {
          entries: prev[parent]?.entries ?? null,
          open: true,
          loading: true,
          error: null,
        },
      }));
      try {
        const entries = (await BrowseClient(clientId, parent)) || [];
        if (!alive.current) return;
        setBranches((prev) => ({
          ...prev,
          [parent]: { entries, open: true, loading: false, error: null },
        }));
      } catch (e) {
        if (!alive.current) return;
        setBranches((prev) => ({
          ...prev,
          [parent]: {
            entries: prev[parent]?.entries ?? null,
            open: true,
            loading: false,
            error: String(e),
          },
        }));
      }
    },
    [clientId],
  );

  useEffect(() => {
    load(ROOT);
  }, [load]);

  const toggle = (entry: Entry) => {
    const branch = branches[entry.address];
    if (!branch || (!branch.entries && !branch.loading)) {
      load(entry.address);
      return;
    }
    setBranches((prev) => ({
      ...prev,
      [entry.address]: { ...branch, open: !branch.open },
    }));
  };

  const canSelect = (entry: Entry) => any || entry.selectable;
  const isSelected = (entry: Entry) =>
    selected.some((s) => s.address === entry.address);

  const select = (entry: Entry) => {
    if (!canSelect(entry)) return;
    if (!multi) {
      setSelected([entry]);
      return;
    }
    setSelected((prev) =>
      prev.some((s) => s.address === entry.address)
        ? prev.filter((s) => s.address !== entry.address)
        : [...prev, entry],
    );
  };

  const confirm = (entries: Entry[] = selected) => {
    if (entries.length === 0) return;
    onPick(entries);
    onClose();
  };

  const renderBranch = (parent: string, depth: number) => {
    const branch = branches[parent];
    if (!branch || !branch.open) return null;
    // 字下げは深さで決まるので、style で指定する
    const indent = { paddingLeft: `${depth * 18 + 8}px` };
    return (
      <>
        {branch.loading && !branch.entries && (
          <div className="client-browse-note" style={indent}>
            読み込み中…
          </div>
        )}
        {branch.error && (
          <div
            className="client-browse-note client-browse-error"
            style={indent}
          >
            {branch.error}
          </div>
        )}
        {branch.entries?.length === 0 && !branch.error && (
          <div className="client-browse-note" style={indent}>
            （子はありません）
          </div>
        )}
        {branch.entries?.map((entry) => {
          const open = !!branches[entry.address]?.open;
          const selectable = canSelect(entry);
          return (
            <div key={entry.address}>
              <div
                className={`client-browse-row${isSelected(entry) ? " selected" : ""}${selectable ? "" : " not-selectable"}`}
                style={indent}
                data-testid="client-browse-row"
                data-address={entry.address}
                onClick={() => select(entry)}
                onDoubleClick={() => {
                  // 1 つだけ選ぶ欄では、ダブルクリックで決める。選べない行は、開閉する
                  if (selectable && !multi) confirm([entry]);
                  else if (!selectable && entry.hasChildren) toggle(entry);
                }}
              >
                {entry.hasChildren ? (
                  <button
                    className="client-browse-toggle"
                    onClick={(e) => {
                      e.stopPropagation();
                      toggle(entry);
                    }}
                    onDoubleClick={(e) => e.stopPropagation()}
                    aria-label={
                      open ? `${entry.name} を閉じる` : `${entry.name} を開く`
                    }
                    aria-expanded={open}
                  >
                    {open ? (
                      <ChevronDown size={14} />
                    ) : (
                      <ChevronRight size={14} />
                    )}
                  </button>
                ) : (
                  <span className="client-browse-toggle-space" />
                )}
                {multi && selectable && (
                  <input
                    type="checkbox"
                    checked={isSelected(entry)}
                    onChange={() => select(entry)}
                    onClick={(e) => e.stopPropagation()}
                    aria-label={`${entry.name} を選ぶ`}
                  />
                )}
                <span className="client-browse-name">{entry.name}</span>
                <span className="client-browse-kind">
                  {entry.detail || entry.kind}
                </span>
                <span className="client-browse-address">{entry.address}</span>
              </div>
              {renderBranch(entry.address, depth + 1)}
            </div>
          );
        })}
      </>
    );
  };

  return (
    <FocusTrap onConfirm={() => confirm()}>
      <div className="dialog dialog-scrollable client-browse-dialog">
        <h3>{field.label} を選ぶ</h3>
        <div className="client-browse-toolbar">
          <span className="client-note">
            {multi
              ? "チェックを付けたものを、欄に 1 行ずつ追加します。"
              : "行を選んで「選択」を押すか、ダブルクリックで決めます。"}
          </span>
          <button
            className="btn-secondary btn-small"
            onClick={() => {
              setBranches({});
              load(ROOT);
            }}
            title="最初から読み直す"
          >
            <RefreshCw className="icon-left" size={12} />
            再読み込み
          </button>
        </div>
        <div
          className="dialog-content dialog-content-scroll client-browse-tree"
          data-testid="client-browse-tree"
        >
          {renderBranch(ROOT, 0)}
        </div>
        <div className="dialog-buttons">
          <span className="client-browse-count">
            {multi && `${selected.length} 個を選択中`}
            {!multi && selected[0] && selected[0].address}
          </span>
          <button onClick={onClose} className="btn-secondary">
            キャンセル
          </button>
          <button
            onClick={() => confirm()}
            className="btn-primary"
            disabled={selected.length === 0}
          >
            {multi ? "追加" : "選択"}
          </button>
        </div>
      </div>
    </FocusTrap>
  );
}
