import { useEffect, useRef, useState } from "react";
import { FocusTrap } from "./FocusTrap";
import { useEscapeKey } from "./useEscapeKey";
import {
  WatchCandidate,
  filterCandidates,
  parseVariableInput,
} from "../utils/watch";

// 下の帯の「モニタリング」で使うダイアログ

interface PickProps {
  title: string;
  // 候補がないときに出す文
  emptyText: string;
  candidates: WatchCandidate[];
  onAdd: (keys: string[]) => void | Promise<void>;
  onClose: () => void;
}

// WatchPickDialog は、モニタリングに足すものを、候補の一覧からチェックで選ぶ
export function WatchPickDialog({
  title,
  emptyText,
  candidates,
  onAdd,
  onClose,
}: PickProps) {
  const [query, setQuery] = useState("");
  const [checked, setChecked] = useState<Set<string>>(new Set());
  useEscapeKey(true, onClose);

  const shown = filterCandidates(candidates, query);
  const toggle = (key: string) =>
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  const submit = () => {
    if (checked.size > 0)
      onAdd(candidates.filter((c) => checked.has(c.key)).map((c) => c.key));
  };

  return (
    <FocusTrap onConfirm={submit}>
      <div className="dialog watch-pick-dialog">
        <h3>{title}</h3>
        <div className="dialog-content">
          {candidates.length === 0 ? (
            <p className="watch-pick-empty">{emptyText}</p>
          ) : (
            <>
              <input
                type="text"
                className="dialog-input watch-pick-filter"
                placeholder="絞り込み"
                aria-label="絞り込み"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
              <div className="watch-pick-list">
                {shown.map((c) => (
                  <label key={c.key} className="watch-pick-item">
                    <input
                      type="checkbox"
                      checked={checked.has(c.key)}
                      onChange={() => toggle(c.key)}
                    />
                    <span className="watch-pick-label">{c.label}</span>
                    {c.detail && (
                      <span className="watch-pick-detail">{c.detail}</span>
                    )}
                  </label>
                ))}
                {shown.length === 0 && (
                  <p className="watch-pick-empty">当てはまるものがありません</p>
                )}
              </div>
            </>
          )}
        </div>
        <div className="dialog-buttons">
          <button onClick={onClose} className="btn-secondary">
            キャンセル
          </button>
          <button
            onClick={submit}
            className="btn-primary"
            disabled={checked.size === 0}
          >
            追加{checked.size > 0 ? `（${checked.size}）` : ""}
          </button>
        </div>
      </div>
    </FocusTrap>
  );
}

interface WriteProps {
  // 変数の名前（フィールド・要素ならパスまで）
  label: string;
  // いまの値（数値・真偽・文字列）と、その表示
  current: unknown;
  currentText: string;
  onWrite: (value: unknown) => Promise<void>;
  onClose: () => void;
}

// VariableWriteDialog は、変数（スカラー）に値を書く
export function VariableWriteDialog({
  label,
  current,
  currentText,
  onWrite,
  onClose,
}: WriteProps) {
  const [text, setText] = useState(currentText);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  useEscapeKey(true, onClose);

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  const write = async (value: unknown) => {
    try {
      await onWrite(value);
    } catch (e) {
      setError(String(e));
    }
  };
  const submit = () => {
    const parsed = parseVariableInput(text, current);
    if (!parsed.ok) {
      setError(parsed.error);
      return;
    }
    write(parsed.value);
  };
  const isBool = typeof current === "boolean";

  return (
    <FocusTrap onConfirm={isBool ? undefined : submit}>
      <div className="dialog">
        <h3>変数の書き込み</h3>
        <div className="dialog-content">
          <div className="dialog-row">
            <label>変数:</label>
            <span className="dialog-value">{label}</span>
          </div>
          <div className="dialog-row">
            <label>現在の値:</label>
            <span className="dialog-value">{currentText}</span>
          </div>
          <div className="dialog-row">
            <label>新しい値:</label>
            {isBool ? (
              <div className="bit-buttons">
                <button
                  className="btn-primary bit-on-button"
                  onClick={() => write(true)}
                >
                  TRUE
                </button>
                <button className="btn-secondary" onClick={() => write(false)}>
                  FALSE
                </button>
              </div>
            ) : (
              <input
                ref={inputRef}
                type="text"
                value={text}
                onChange={(e) => setText(e.target.value)}
                className="dialog-input"
              />
            )}
          </div>
          {error && <p className="watch-write-error">{error}</p>}
        </div>
        <div className="dialog-buttons">
          <button onClick={onClose} className="btn-secondary">
            キャンセル
          </button>
          {!isBool && (
            <button onClick={submit} className="btn-primary">
              書き込み
            </button>
          )}
        </div>
      </div>
    </FocusTrap>
  );
}
