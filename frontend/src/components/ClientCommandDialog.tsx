import { useState, useEffect, useRef } from "react";
import { BuildClientRequest } from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { Save, Send } from "lucide-react";
import { DynamicField } from "./DynamicField";
import { FocusTrap } from "./FocusTrap";
import { useEscapeKey } from "./useEscapeKey";
import { clientStatusLabel, formatClientElapsed } from "./clientFormat";
import { ClientBrowseDialog, applyBrowsePick } from "./ClientBrowseDialog";

type Mode = "build" | "hex";

// 保存する内容（ClientSavedCommandInputDTO と同じ形）
export interface CommandSaveInput {
  name: string;
  mode: Mode;
  commandId: string;
  params: Record<string, any>;
  request: string;
}

interface ClientCommandDialogProps {
  clientId: string;
  protocol: application.ClientProtocolDTO;
  // 機器のアドレスの一覧を開く前に呼ぶ（保存していない接続先の設定を保存する）。false なら開かない
  beforeBrowse: () => Promise<boolean>;
  // 編集する保存したコマンド（新しいコマンドなら null）
  editing: application.ClientSavedCommandDTO | null;
  sending: boolean;
  // 送る。結果（履歴の 1 件）を返す。送る前に分かる誤り（16 進が読めないなど）は例外にする（ダイアログが表示する）
  onSend: (requestHex: string) => Promise<application.ClientCommandLogDTO>;
  // 保存する（editing があれば上書き）。失敗は例外にする（ダイアログが表示する）。成功したらダイアログを閉じる
  onSave: (input: CommandSaveInput) => Promise<void>;
  onClose: () => void;
}

const noPorts: string[] = [];
const noop = () => {};
// ダイアログに出す結果の値の数（それより多ければ「ほか N 個」）
const MAX_RESULT_VALUES = 8;

function defaultParams(cmd: application.ClientCommandDTO): Record<string, any> {
  const params: Record<string, any> = {};
  for (const f of cmd.fields) params[f.name] = f.default;
  return params;
}

// コマンドのダイアログ。新しいコマンドを作る・保存したコマンドを直す。2 つの入れ方がある:
// - 組み立てる: コマンド（Modbus ならファンクションコード）を選んで項目を入れる。組み立てたバイト列をその場で 16 進で見せる
// - 16 進で入れる: 送るバイト列（Modbus なら PDU、OPC UA ならサービスのリクエスト本体）を直接入れる。何を入れるかの説明はプロトコルの実装が返す。組み立てた内容を写して、一部だけ変えて送ることもできる
// 「送信」はダイアログを開いたまま送り、結果をダイアログの下に出す（保存せずに試せる）。開くたびにマウントする
export function ClientCommandDialog({
  clientId,
  protocol,
  beforeBrowse,
  editing,
  sending,
  onSend,
  onSave,
  onClose,
}: ClientCommandDialogProps) {
  const commands = protocol.commands || [];
  const startBuild =
    !editing || (editing.mode === "build" && !!editing.commandId);
  const [mode, setMode] = useState<Mode>(startBuild ? "build" : "hex");
  const [commandId, setCommandId] = useState<string>(
    (editing?.mode === "build" && editing.commandId) || commands[0]?.id || "",
  );
  // コマンドごとの入力内容（コマンドを切り替えても残す）
  const [paramsByCommand, setParamsByCommand] = useState<
    Record<string, Record<string, any>>
  >(() =>
    editing?.mode === "build" && editing.commandId
      ? { [editing.commandId]: { ...(editing.params || {}) } }
      : {},
  );
  const [preview, setPreview] = useState("");
  const [buildError, setBuildError] = useState<string | null>(null);
  const [hexInput, setHexInput] = useState(
    editing?.mode === "hex" ? editing.request : "",
  );
  const [name, setName] = useState(editing?.name ?? "");
  const [result, setResult] = useState<application.ClientCommandLogDTO | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  // アドレスの一覧（参照のダイアログ）を開いている項目
  const [browseField, setBrowseField] = useState<application.FieldDTO | null>(
    null,
  );
  // 参照のダイアログを開いている間は、Esc はそちらを閉じる
  useEscapeKey(!browseField, onClose);

  // 送った結果・エラーは入力欄の下に出るので、見える位置までスクロールする
  const feedbackRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (result || error) {
      feedbackRef.current?.scrollIntoView({ block: "nearest" });
    }
  }, [result, error]);

  const command = commands.find((c) => c.id === commandId);
  const params = command
    ? (paramsByCommand[command.id] ?? defaultParams(command))
    : {};

  // 入力が変わるたびに組み立て直す（検証はプロトコルの実装が行う）
  const paramsKey = JSON.stringify(params);
  useEffect(() => {
    if (!command) return;
    let cancelled = false;
    BuildClientRequest(protocol.protocolType, command.id, JSON.parse(paramsKey))
      .then((hex) => {
        if (cancelled) return;
        setPreview(hex);
        setBuildError(null);
      })
      .catch((e) => {
        if (cancelled) return;
        setPreview("");
        setBuildError(String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [protocol.protocolType, command, paramsKey]);

  const handleParamChange = (fieldName: string, value: any) => {
    if (!command) return;
    setParamsByCommand((prev) => ({
      ...prev,
      [command.id]: { ...params, [fieldName]: value },
    }));
  };

  const handleEditAsHex = () => {
    setHexInput(preview);
    setMode("hex");
  };

  const ready =
    mode === "build" ? preview !== "" && !buildError : hexInput.trim() !== "";

  const handleSend = async () => {
    // 参照のダイアログを開いている間の Enter は、そちらの確定
    if (!ready || sending || browseField) return;
    try {
      setError(null);
      setResult(await onSend(mode === "build" ? preview : hexInput));
    } catch (e) {
      setError(String(e));
    }
  };

  const handleSave = async () => {
    if (!ready) return;
    try {
      setError(null);
      await onSave({
        name,
        mode,
        commandId: mode === "build" ? (command?.id ?? "") : "",
        params: mode === "build" ? params : {},
        request: mode === "hex" ? hexInput : "",
      });
      onClose();
    } catch (e) {
      setError(String(e));
    }
  };

  const shownValues = result?.values?.slice(0, MAX_RESULT_VALUES) ?? [];
  const moreValues = (result?.values?.length ?? 0) - shownValues.length;

  return (
    <FocusTrap onConfirm={handleSend}>
      <div className="dialog dialog-scrollable client-command-dialog">
        <h3>
          {editing ? `コマンドを編集: ${editing.name}` : "新しいコマンド"}
        </h3>
        <div className="dialog-content dialog-content-scroll">
          <div className="client-composer">
            <div className="vscode-setting-item">
              <div className="client-mode-switch" role="radiogroup">
                <label>
                  <input
                    type="radio"
                    name="client-mode"
                    checked={mode === "build"}
                    onChange={() => setMode("build")}
                  />
                  組み立てる
                </label>
                <label>
                  <input
                    type="radio"
                    name="client-mode"
                    checked={mode === "hex"}
                    onChange={() => setMode("hex")}
                  />
                  16 進で入れる
                </label>
              </div>
            </div>

            {mode === "build" ? (
              <>
                <div className="vscode-setting-item">
                  <div className="vscode-setting-label">コマンド</div>
                  <div className="vscode-setting-control">
                    <select
                      value={commandId}
                      onChange={(e) => setCommandId(e.target.value)}
                      aria-label="コマンド"
                    >
                      {commands.map((c) => (
                        <option key={c.id} value={c.id}>
                          {c.displayName}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>
                {command?.fields.map((field) => (
                  <DynamicField
                    key={`${command.id}:${field.name}`}
                    field={field}
                    value={params[field.name]}
                    onChange={(v) => handleParamChange(field.name, v)}
                    disabled={false}
                    serialPorts={noPorts}
                    onRefreshPorts={noop}
                    onBrowse={async () => {
                      if (await beforeBrowse()) setBrowseField(field);
                    }}
                  />
                ))}
                <div className="vscode-setting-item">
                  <div className="vscode-setting-label">
                    送るリクエスト（16 進）
                  </div>
                  {buildError ? (
                    <div className="client-build-error">{buildError}</div>
                  ) : (
                    <div className="client-hex-row">
                      <code className="client-hex" data-testid="client-preview">
                        {preview}
                      </code>
                      <button
                        className="btn-secondary btn-small"
                        onClick={handleEditAsHex}
                        disabled={!preview}
                      >
                        16 進で編集
                      </button>
                    </div>
                  )}
                </div>
              </>
            ) : (
              <div className="vscode-setting-item">
                <div className="vscode-setting-label">
                  送るリクエスト（16 進）
                </div>
                <div className="vscode-setting-description">
                  {protocol.requestDescription &&
                    `${protocol.displayName}: ${protocol.requestDescription} `}
                  空白・カンマ・0x は無視します。Enter で送信します。
                </div>
                <div className="vscode-setting-control client-hex-input">
                  <input
                    type="text"
                    value={hexInput}
                    onChange={(e) => setHexInput(e.target.value)}
                    onKeyDown={(e) => {
                      // Enter で送る（ダイアログ全体の Enter の処理には渡さない。二重に送らないため）
                      if (e.key === "Enter") {
                        e.preventDefault();
                        e.stopPropagation();
                        e.nativeEvent.stopImmediatePropagation();
                        handleSend();
                      }
                    }}
                    placeholder={protocol.requestExample}
                    aria-label="送るリクエスト（16 進）"
                    spellCheck={false}
                  />
                </div>
              </div>
            )}

            <div className="vscode-setting-item">
              <div className="vscode-setting-label">名前</div>
              <div className="vscode-setting-description">
                保存するときの名前。空なら内容の要約にします。
              </div>
              <div className="vscode-setting-control">
                <input
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="例: 速度を読む"
                  aria-label="保存する名前"
                />
              </div>
            </div>
          </div>

          <div ref={feedbackRef}>
            {error && <div className="error-message config-error">{error}</div>}
            {result && (
              <div
                className="client-result client-dialog-result"
                data-testid="client-dialog-result"
              >
                <div className="client-result-header">
                  <span
                    className={`client-status client-status-${result.status}`}
                  >
                    {clientStatusLabel(result.status)}
                  </span>
                  <span className="client-result-summary">
                    {result.summary}
                  </span>
                  <span className="client-result-elapsed">
                    {formatClientElapsed(result.elapsedMs)}
                  </span>
                </div>
                {result.error && (
                  <div className="client-result-error">{result.error}</div>
                )}
                {shownValues.length > 0 && (
                  <div className="client-poll-values">
                    {shownValues.map((v, i) => (
                      <div key={i} className="client-poll-value">
                        <span className="client-poll-value-label">
                          {v.label}
                        </span>
                        <span className="client-poll-value-text">
                          {v.value}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
                {moreValues > 0 && (
                  <div className="client-note">
                    ほか {moreValues} 個（画面の「結果と履歴」で見られます）
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
        <div className="dialog-buttons">
          <button onClick={onClose} className="btn-secondary">
            閉じる
          </button>
          <button
            onClick={handleSend}
            className="btn-secondary"
            disabled={!ready || sending}
            title="保存せずに送る（ダイアログは開いたまま）"
          >
            <Send className="icon-left" size={14} />
            {sending ? "送信中…" : "送信"}
          </button>
          <button
            onClick={handleSave}
            className="btn-primary"
            disabled={!ready}
            title={
              editing
                ? `「${editing.name}」を上書きして閉じる`
                : "保存して閉じる"
            }
          >
            <Save className="icon-left" size={14} />
            保存して閉じる
          </button>
        </div>
      </div>
      {browseField && command && (
        <ClientBrowseDialog
          clientId={clientId}
          field={browseField}
          onPick={(picked) =>
            setParamsByCommand((prev) => ({
              ...prev,
              [command.id]: applyBrowsePick(
                browseField,
                command.fields,
                params,
                picked,
              ),
            }))
          }
          onClose={() => setBrowseField(null)}
        />
      )}
    </FocusTrap>
  );
}
