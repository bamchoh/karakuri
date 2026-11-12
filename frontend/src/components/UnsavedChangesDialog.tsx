import { FocusTrap } from "./FocusTrap";

interface Props {
  // 「保存せずに○○」「保存してから○○」のボタンの文言
  discardLabel: string;
  saveLabel: string;
  onCancel: () => void;
  onDiscard: () => void;
  onSave: () => void;
}

// サーバーの設定を保存しないまま別の操作をしようとしたときの確認ダイアログ
export function UnsavedChangesDialog({
  discardLabel,
  saveLabel,
  onCancel,
  onDiscard,
  onSave,
}: Props) {
  return (
    <FocusTrap onConfirm={onSave}>
      <div className="dialog">
        <h3>設定が保存されていません</h3>
        <div className="dialog-content">
          <p>通信設定が変更されています。</p>
        </div>
        <div className="dialog-buttons">
          <button onClick={onCancel} className="btn-secondary">
            キャンセル
          </button>
          <button onClick={onDiscard} className="btn-secondary">
            {discardLabel}
          </button>
          <button onClick={onSave} className="btn-primary">
            {saveLabel}
          </button>
        </div>
      </div>
    </FocusTrap>
  );
}
