import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { ArrowDown, ArrowUp, Copy, Plus, X } from "lucide-react";
import { FocusTrap } from "./FocusTrap";
import {
  DeleteStructType,
  RegisterStructType,
  UpdateStructType,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { useEscapeKey } from "./useEscapeKey";
import { getWordCount } from "./variableTypes";
import {
  FieldRow,
  buildFields,
  duplicateRow,
  emptyRow,
  hasErrors,
  layoutRows,
  moveRow,
  rowFromField,
  selectableStructs,
  validateStructForm,
} from "../utils/structForm";

export interface StructTypeDialogHandle {
  open: () => void;
  isOpen: () => boolean;
}

interface Props {
  dataTypes: application.DataTypeInfoDTO[];
  structTypes: application.StructTypeDTO[];
  // 構造体型を登録・更新・削除したあとに呼ぶ（型一覧の再読み込み）
  onChanged: () => Promise<void>;
}

// 編集中の内容。original は、比べるための「開いたときの内容」（保存していない変更があるかを見る）
interface Draft {
  // 登録済みの型を編集しているときの型名。新しい型なら null
  editing: string | null;
  name: string;
  rows: FieldRow[];
  original: string;
}

// 比べるための形（型名と、登録するフィールド）
const snapshot = (name: string, rows: FieldRow[]) =>
  JSON.stringify([name.trim(), buildFields(rows)]);

// 構造体型のダイアログ。左に型の一覧、右に選んだ型のフィールドの表（1 フィールド 1 行）。
//   - 配列は「配列」にチェックして、範囲を 0..9 の形で入れる（多次元は 0..2, 0..4。個数だけ 10 と書いてもよい）
//   - 行は、上下に動かす・写す・消すことができる。名前の欄で Enter を押すと次の行へ（最後の行なら、行を足す）
//   - 登録済みの型も、変数が使っているまま変えられる（変数の値は、名前の同じフィールドを残して新しい形に直る）
//   - 行の計算と検査は utils/structForm.ts の純粋な関数
export const StructTypeDialog = forwardRef<StructTypeDialogHandle, Props>(
  function StructTypeDialog({ dataTypes, structTypes, onChanged }, ref) {
    const [isOpen, setIsOpen] = useState(false);
    const nextKey = useRef(1);
    const newKey = () => nextKey.current++;

    const newDraft = (): Draft => {
      const rows = [emptyRow(newKey())];
      return { editing: null, name: "", rows, original: snapshot("", rows) };
    };
    const draftOf = (st: application.StructTypeDTO): Draft => {
      const rows = st.fields.map((f) =>
        rowFromField(newKey(), f.name, f.dataType),
      );
      return {
        editing: st.name,
        name: st.name,
        rows,
        original: snapshot(st.name, rows),
      };
    };

    const [draft, setDraft] = useState<Draft>(newDraft);
    const [error, setError] = useState<string | null>(null);
    // 保存を押したあとは、行ごとの理由を出す（打っている途中には出さない）
    const [showErrors, setShowErrors] = useState(false);
    // 次の描画のあとにフォーカスを移す行（名前の欄）
    const focusKey = useRef<number | null>(null);
    const nameInputs = useRef(new Map<number, HTMLInputElement>());
    const typeNameInput = useRef<HTMLInputElement>(null);

    const dirty = snapshot(draft.name, draft.rows) !== draft.original;

    const show = (next: Draft) => {
      setDraft(next);
      setError(null);
      setShowErrors(false);
    };

    // 保存していない変更があれば、捨ててよいか確かめる
    const confirmDiscard = () =>
      !dirty || confirm("保存していない変更があります。捨てますか?");

    const close = () => {
      if (!confirmDiscard()) return;
      setIsOpen(false);
    };

    // 型が 1 つもなくて「新しい型」で開いた（型の一覧をまだ読み終えていないだけかもしれない）。
    // 何も触らないうちに型の一覧が届いたら、先頭の型を出す
    const pendingFirst = useRef(false);
    useEffect(() => {
      if (!isOpen || !pendingFirst.current || structTypes.length === 0) return;
      pendingFirst.current = false;
      if (!dirty && draft.editing === null) show(draftOf(structTypes[0]));
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [structTypes, isOpen]);

    const isOpenRef = useRef(isOpen);
    isOpenRef.current = isOpen;
    useImperativeHandle(
      ref,
      () => ({
        // 開いたときは、型があれば先頭の型を、なければ新しい型を出す
        open: () => {
          show(structTypes.length > 0 ? draftOf(structTypes[0]) : newDraft());
          pendingFirst.current = structTypes.length === 0;
          setIsOpen(true);
        },
        isOpen: () => isOpenRef.current,
      }),
      // eslint-disable-next-line react-hooks/exhaustive-deps
      [structTypes],
    );
    useEscapeKey(isOpen, close);

    useLayoutEffect(() => {
      if (focusKey.current === null) return;
      nameInputs.current.get(focusKey.current)?.focus();
      focusKey.current = null;
    });

    const setRows = (rows: FieldRow[]) => setDraft((d) => ({ ...d, rows }));
    const updateRow = (key: number, patch: Partial<FieldRow>) =>
      setDraft((d) => ({
        ...d,
        rows: d.rows.map((r) => (r.key === key ? { ...r, ...patch } : r)),
      }));

    const addRow = () => {
      const row = emptyRow(newKey());
      focusKey.current = row.key;
      setRows([...draft.rows, row]);
    };

    // 名前の欄で Enter: 次の行の名前へ。最後の行なら、行を足す
    const focusNextRow = (index: number) => {
      const next = draft.rows[index + 1];
      if (next) nameInputs.current.get(next.key)?.focus();
      else addRow();
    };

    const scalarTypes = dataTypes.map((t) => t.id);
    const errors = validateStructForm(draft.name, draft.rows, {
      isNew: draft.editing === null,
      existingNames: structTypes.map((s) => s.name),
      scalarTypes,
    });
    const layout = layoutRows(draft.rows, (dt) =>
      getWordCount(dt, dataTypes, structTypes),
    );
    // フィールドに選べる構造体型（自分と、自分を使っている型は選べない）
    const structOptions = selectableStructs(draft.editing ?? "", structTypes);

    const handleSelect = (st: application.StructTypeDTO) => {
      if (st.name === draft.editing) return;
      if (confirmDiscard()) show(draftOf(st));
    };

    const handleNew = () => {
      if (!confirmDiscard()) return;
      pendingFirst.current = false;
      show(newDraft());
      requestAnimationFrame(() => typeNameInput.current?.focus());
    };

    const handleRevert = () => {
      const st = structTypes.find((s) => s.name === draft.editing);
      show(st ? draftOf(st) : newDraft());
    };

    const handleSave = async () => {
      pendingFirst.current = false;
      if (hasErrors(errors)) {
        setShowErrors(true);
        return;
      }
      const dto = {
        name: draft.name.trim(),
        fields: buildFields(draft.rows).map((f) => ({ ...f, offset: 0 })),
        wordCount: 0,
      } as application.StructTypeDTO;
      try {
        const saved = draft.editing
          ? await UpdateStructType(dto)
          : await RegisterStructType(dto);
        await onChanged();
        show(draftOf(saved));
      } catch (e) {
        setError(`保存できませんでした: ${e}`);
      }
    };

    const handleDelete = async () => {
      const name = draft.editing;
      if (!name || !confirm(`構造体型 "${name}" を削除しますか?`)) return;
      try {
        await DeleteStructType(name);
        await onChanged();
        const rest = structTypes.filter((s) => s.name !== name);
        show(rest.length > 0 ? draftOf(rest[0]) : newDraft());
      } catch (e) {
        setError(`削除できませんでした: ${e}`);
      }
    };

    if (!isOpen) return null;
    return (
      <FocusTrap>
        <div
          className="dialog struct-dialog"
          onKeyDown={(e) => {
            // Ctrl + Enter で保存
            if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
              e.preventDefault();
              handleSave();
            }
          }}
        >
          <h3>構造体型管理</h3>
          <div className="struct-dialog-body">
            {/* 左: 型の一覧 */}
            <div className="struct-type-list" data-testid="struct-type-list">
              {structTypes.map((st) => (
                <button
                  key={st.name}
                  className={`struct-type-item${st.name === draft.editing ? " selected" : ""}`}
                  onClick={() => handleSelect(st)}
                >
                  <span className="struct-type-name">{st.name}</span>
                  <span className="struct-type-words">{st.wordCount}W</span>
                </button>
              ))}
              <button
                className={`struct-type-item struct-type-new${draft.editing === null ? " selected" : ""}`}
                onClick={handleNew}
              >
                <Plus size={14} />
                新しい型
              </button>
            </div>

            {/* 右: 選んだ型のフィールドの表 */}
            <div className="struct-editor">
              <div className="struct-editor-head">
                <label htmlFor="struct-type-name">型名</label>
                <input
                  id="struct-type-name"
                  ref={typeNameInput}
                  type="text"
                  className={showErrors && errors.name ? "invalid" : ""}
                  value={draft.name}
                  onChange={(e) =>
                    setDraft((d) => ({ ...d, name: e.target.value }))
                  }
                  placeholder="例: MotorData"
                  disabled={draft.editing !== null}
                  title={
                    draft.editing !== null
                      ? "登録した型の名前は変えられません"
                      : undefined
                  }
                />
                <span className="struct-total" data-testid="struct-total">
                  合計 {layout.total} ワード
                </span>
              </div>
              {showErrors && errors.name && (
                <p className="struct-error">{errors.name}</p>
              )}

              <div className="struct-rows">
                <table className="struct-field-table">
                  <thead>
                    <tr>
                      <th className="col-index">#</th>
                      <th className="col-name">名前</th>
                      <th className="col-type">型</th>
                      <th className="col-array">配列</th>
                      <th className="col-num" title="先頭からのワードの位置">
                        位置
                      </th>
                      <th className="col-num" title="ワード数">
                        W
                      </th>
                      <th className="col-actions" />
                    </tr>
                  </thead>
                  <tbody>
                    {draft.rows.map((row, index) => {
                      const rowError = showErrors
                        ? errors.rows[row.key]
                        : undefined;
                      const l = layout.rows[row.key];
                      // 登録済みの定義に、いまは選べない型（なくなった型など）が残っていても、選択肢に出す
                      const unknownType =
                        row.type !== "STRING" &&
                        !scalarTypes.includes(row.type) &&
                        !structOptions.some((s) => s.name === row.type);
                      return (
                        <tr
                          key={row.key}
                          className={rowError ? "struct-row-invalid" : ""}
                          data-testid="struct-field-row"
                        >
                          <td className="col-index">{index + 1}</td>
                          <td className="col-name">
                            <input
                              ref={(el) => {
                                if (el) nameInputs.current.set(row.key, el);
                                else nameInputs.current.delete(row.key);
                              }}
                              type="text"
                              aria-label={`フィールド ${index + 1} の名前`}
                              value={row.name}
                              placeholder="フィールド名"
                              onChange={(e) =>
                                updateRow(row.key, { name: e.target.value })
                              }
                              onKeyDown={(e) => {
                                if (
                                  e.key === "Enter" &&
                                  !e.ctrlKey &&
                                  !e.metaKey
                                ) {
                                  e.preventDefault();
                                  focusNextRow(index);
                                } else if (e.altKey && e.key === "ArrowUp") {
                                  e.preventDefault();
                                  focusKey.current = row.key;
                                  setRows(moveRow(draft.rows, index, -1));
                                } else if (e.altKey && e.key === "ArrowDown") {
                                  e.preventDefault();
                                  focusKey.current = row.key;
                                  setRows(moveRow(draft.rows, index, 1));
                                }
                              }}
                            />
                            {rowError && (
                              <span className="struct-row-error">
                                {rowError}
                              </span>
                            )}
                          </td>
                          <td className="col-type">
                            <select
                              aria-label={`フィールド ${index + 1} の型`}
                              value={row.type}
                              onChange={(e) =>
                                updateRow(row.key, { type: e.target.value })
                              }
                            >
                              <optgroup label="スカラー">
                                {dataTypes.map((t) => (
                                  <option
                                    key={t.id}
                                    value={t.id}
                                    title={t.description}
                                  >
                                    {t.displayName}
                                  </option>
                                ))}
                                <option value="STRING">STRING</option>
                              </optgroup>
                              {(structOptions.length > 0 || unknownType) && (
                                <optgroup label="構造体">
                                  {structOptions.map((st) => (
                                    <option key={st.name} value={st.name}>
                                      {st.name}
                                    </option>
                                  ))}
                                  {unknownType && (
                                    <option value={row.type}>{row.type}</option>
                                  )}
                                </optgroup>
                              )}
                            </select>
                            {row.type === "STRING" && (
                              <input
                                type="number"
                                className="struct-string-length"
                                aria-label={`フィールド ${index + 1} のバイト長`}
                                title="バイト長"
                                min={1}
                                max={256}
                                value={row.stringLength}
                                onChange={(e) =>
                                  updateRow(row.key, {
                                    stringLength: parseInt(e.target.value) || 0,
                                  })
                                }
                              />
                            )}
                          </td>
                          <td className="col-array">
                            <input
                              type="checkbox"
                              aria-label={`フィールド ${index + 1} を配列にする`}
                              checked={row.isArray}
                              onChange={(e) =>
                                updateRow(row.key, {
                                  isArray: e.target.checked,
                                })
                              }
                            />
                            {row.isArray && (
                              <input
                                type="text"
                                className="struct-bounds"
                                aria-label={`フィールド ${index + 1} の配列の範囲`}
                                title="範囲（0..9。多次元は 0..2, 0..4。個数だけ 10 と書いてもよい）"
                                value={row.bounds}
                                placeholder="0..9"
                                onChange={(e) =>
                                  updateRow(row.key, { bounds: e.target.value })
                                }
                              />
                            )}
                          </td>
                          <td className="col-num">{l?.offset ?? ""}</td>
                          <td className="col-num">{l?.words ?? ""}</td>
                          <td className="col-actions">
                            <button
                              className="struct-row-button"
                              title="上へ（Alt + ↑）"
                              aria-label={`フィールド ${index + 1} を上へ`}
                              disabled={index === 0}
                              onClick={() =>
                                setRows(moveRow(draft.rows, index, -1))
                              }
                            >
                              <ArrowUp size={14} />
                            </button>
                            <button
                              className="struct-row-button"
                              title="下へ（Alt + ↓）"
                              aria-label={`フィールド ${index + 1} を下へ`}
                              disabled={index === draft.rows.length - 1}
                              onClick={() =>
                                setRows(moveRow(draft.rows, index, 1))
                              }
                            >
                              <ArrowDown size={14} />
                            </button>
                            <button
                              className="struct-row-button"
                              title="この行を写して、下に足す"
                              aria-label={`フィールド ${index + 1} を写す`}
                              onClick={() => {
                                const key = newKey();
                                focusKey.current = key;
                                setRows(duplicateRow(draft.rows, index, key));
                              }}
                            >
                              <Copy size={14} />
                            </button>
                            <button
                              className="struct-row-button struct-row-delete"
                              title="この行を消す"
                              aria-label={`フィールド ${index + 1} を消す`}
                              disabled={draft.rows.length <= 1}
                              onClick={() =>
                                setRows(
                                  draft.rows.filter((r) => r.key !== row.key),
                                )
                              }
                            >
                              <X size={14} />
                            </button>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
                <button
                  className="btn-secondary struct-add-field"
                  onClick={addRow}
                >
                  <Plus size={14} />
                  フィールド
                </button>
              </div>

              {showErrors && errors.form && (
                <p className="struct-error">{errors.form}</p>
              )}
              {error && <p className="struct-error">{error}</p>}
            </div>
          </div>

          <div className="dialog-buttons struct-dialog-buttons">
            {draft.editing !== null && (
              <button
                onClick={handleDelete}
                className="btn-danger struct-delete"
              >
                この型を削除
              </button>
            )}
            <span className="struct-hint">
              Enter で次の行・Ctrl + Enter で保存
            </span>
            <button onClick={close} className="btn-secondary">
              閉じる
            </button>
            <button
              onClick={handleRevert}
              className="btn-secondary"
              disabled={!dirty}
            >
              元に戻す
            </button>
            <button
              onClick={handleSave}
              className="btn-primary"
              disabled={!dirty}
            >
              {draft.editing !== null ? "保存" : "型を登録"}
            </button>
          </div>
        </div>
      </FocusTrap>
    );
  },
);
