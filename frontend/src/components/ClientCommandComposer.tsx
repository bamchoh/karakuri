import { useState, useEffect } from "react";
import { BuildClientRequest } from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { Save, Send } from "lucide-react";
import { DynamicField } from "./DynamicField";

type Mode = "build" | "hex";

// 保存したコマンドの、どこから読み込んだか（上書き保存に使う）
export interface ComposerLoadedFrom {
  commandId: string;
  name: string;
}

// 組み立て欄に読み込む内容。seq が変わるたびに読み込む
export interface ComposerLoad {
  seq: number;
  command: application.ClientSavedCommandDTO;
  from: ComposerLoadedFrom;
}

// 保存する内容（ClientSavedCommandInputDTO と同じ形）
export interface ComposerSaveInput {
  name: string;
  mode: Mode;
  commandId: string;
  params: Record<string, any>;
  request: string;
}

interface ClientCommandComposerProps {
  protocol: application.ClientProtocolDTO;
  sending: boolean;
  onSend: (requestHex: string) => void;
  // 保存（overwrite があれば、そのコマンドを上書きする）
  onSave: (
    input: ComposerSaveInput,
    overwrite: ComposerLoadedFrom | null,
  ) => Promise<boolean>;
  load: ComposerLoad | null;
}

const noPorts: string[] = [];
const noop = () => {};

function defaultParams(cmd: application.ClientCommandDTO): Record<string, any> {
  const params: Record<string, any> = {};
  for (const f of cmd.fields) params[f.name] = f.default;
  return params;
}

// コマンドの組み立て。2 つの入れ方がある:
// - 組み立てる: コマンド（Modbus ならファンクションコード）を選んで項目を入れる。組み立てたバイト列をその場で 16 進で見せる
// - 16 進で入れる: 送るバイト列（Modbus なら PDU）を直接入れる。組み立てた内容を写して、一部だけ変えて送ることもできる
export function ClientCommandComposer({
  protocol,
  sending,
  onSend,
  onSave,
  load,
}: ClientCommandComposerProps) {
  const commands = protocol.commands || [];
  const [mode, setMode] = useState<Mode>("build");
  const [commandId, setCommandId] = useState<string>(commands[0]?.id ?? "");
  // コマンドごとの入力内容（コマンドを切り替えても残す）
  const [paramsByCommand, setParamsByCommand] = useState<
    Record<string, Record<string, any>>
  >({});
  const [preview, setPreview] = useState("");
  const [buildError, setBuildError] = useState<string | null>(null);
  const [hexInput, setHexInput] = useState("");
  const [saveName, setSaveName] = useState("");
  const [loadedFrom, setLoadedFrom] = useState<ComposerLoadedFrom | null>(null);

  // 保存したコマンドを読み込む
  const loadSeq = load?.seq;
  useEffect(() => {
    if (!load) return;
    const c = load.command;
    if (c.mode === "build" && c.commandId) {
      setMode("build");
      setCommandId(c.commandId);
      setParamsByCommand((prev) => ({
        ...prev,
        [c.commandId!]: { ...(c.params || {}) },
      }));
    } else {
      setMode("hex");
      setHexInput(c.request);
    }
    setSaveName(c.name);
    setLoadedFrom(load.from);
    // seq が変わったときだけ読み込む
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadSeq]);

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

  const handleParamChange = (name: string, value: any) => {
    if (!command) return;
    setParamsByCommand((prev) => ({
      ...prev,
      [command.id]: { ...params, [name]: value },
    }));
  };

  const handleEditAsHex = () => {
    setHexInput(preview);
    setMode("hex");
  };

  const ready =
    mode === "build" ? preview !== "" && !buildError : hexInput.trim() !== "";
  const canSend = !sending && ready;

  const handleSend = () => {
    if (!canSend) return;
    onSend(mode === "build" ? preview : hexInput);
  };

  const saveInput = (): ComposerSaveInput => ({
    name: saveName,
    mode,
    commandId: mode === "build" ? (command?.id ?? "") : "",
    params: mode === "build" ? params : {},
    request: mode === "hex" ? hexInput : "",
  });

  const handleSaveNew = async () => {
    if (await onSave(saveInput(), null)) setLoadedFrom(null);
  };

  const handleOverwrite = async () => {
    if (loadedFrom) await onSave(saveInput(), loadedFrom);
  };

  return (
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
            />
          ))}
          <div className="vscode-setting-item">
            <div className="vscode-setting-label">送るリクエスト（16 進）</div>
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
          <div className="vscode-setting-label">送るリクエスト（16 進）</div>
          <div className="vscode-setting-description">
            {protocol.displayName} では PDU（ファンクションコード +
            データ）を入れます。ヘッダーはクライアントが付けます。空白・カンマ・0x
            は無視します。Enter で送信します。
          </div>
          <div className="vscode-setting-control client-hex-input">
            <input
              type="text"
              value={hexInput}
              onChange={(e) => setHexInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") handleSend();
              }}
              placeholder="03 00 00 00 0A"
              aria-label="送るリクエスト（16 進）"
              spellCheck={false}
            />
          </div>
        </div>
      )}

      <div className="client-send-row">
        <button
          className="btn-primary"
          onClick={handleSend}
          disabled={!canSend}
        >
          <Send className="icon-left" size={14} />
          {sending ? "送信中…" : "送信"}
        </button>
        <div className="client-save-group">
          <input
            type="text"
            value={saveName}
            onChange={(e) => setSaveName(e.target.value)}
            placeholder="名前（空なら内容の要約）"
            aria-label="保存する名前"
          />
          <button
            className="btn-secondary"
            onClick={handleSaveNew}
            disabled={!ready}
            title="名前を付けて、上の「保存したコマンド」に新しく保存する"
          >
            <Save className="icon-left" size={14} />
            コマンドを保存
          </button>
          {loadedFrom && (
            <button
              className="btn-secondary"
              onClick={handleOverwrite}
              disabled={!ready}
              title={`読み込んだ「${loadedFrom.name}」を、この内容で上書きする`}
            >
              「{loadedFrom.name}」を上書き保存
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
