import { useState, useEffect, useRef } from "react";
import {
  GetScripts,
  GetIntervalPresets,
  CreateScript,
  UpdateScript,
  DeleteScript,
  StartScript,
  StopScript,
  RunScriptOnce,
  GetConsoleLogs,
  ClearConsoleLogs,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { application } from "../../wailsjs/go/models";
import { Square, Play, SquarePlus } from "lucide-react";
import { Group, Panel, Separator } from "react-resizable-panels";

const DEFAULT_CODE = `// PLCオブジェクトで変数にアクセスできます
//
// === 基本 ===
// plc.readVariable(name)              - 変数の値を読む
// plc.writeVariable(name, value)      - 変数に値を書く
// plc.getVariables()                  - 全変数名の一覧を取得
//
// === 配列 ===
// plc.readArrayElement(name, index)          - 配列要素を読む
// plc.writeArrayElement(name, index, value)  - 配列要素に書く
//
// === 構造体 ===
// plc.readStructField(name, fieldName)          - 構造体フィールドを読む
// plc.writeStructField(name, fieldName, value)  - 構造体フィールドに書く

// 例: 変数 "Counter" をインクリメント
const count = plc.readVariable("Counter");
plc.writeVariable("Counter", count + 1);
`;

function generateNewScriptName(scripts: application.ScriptDTO[]) {
  const baseName = "新しいスクリプト";

  const exists = scripts.some((s) => s.name === baseName);

  if (!exists) {
    return baseName;
  }

  let index = 1;

  while (scripts.some((s) => s.name === `${baseName}${index}`)) {
    index++;
  }

  return `${baseName}${index}`;
}

// 選択したスクリプトの編集欄。入力中の内容は自分で持つ（key にスクリプトの ID を渡して、選び直すたびに作り直す）
function ScriptEditPanel({
  script,
  presets,
  onSave,
  onRunOnce,
  onDelete,
}: {
  script: application.ScriptDTO;
  presets: application.IntervalPresetDTO[];
  onSave: (name: string, code: string, intervalMs: number) => void;
  onRunOnce: (code: string) => void;
  onDelete: () => void;
}) {
  const [name, setName] = useState(script.name);
  const [code, setCode] = useState(script.code);
  const [intervalMs, setIntervalMs] = useState(script.intervalMs);

  return (
    <div className="script-panel">
      <div className="editor-card">
        <div className="editor-header-row">
          <div className="form-group">
            <label>名前</label>
            <input
              type="text"
              aria-label="名前"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <div className="form-group">
            <label>実行周期</label>
            <select
              aria-label="実行周期"
              value={intervalMs}
              onChange={(e) => setIntervalMs(parseInt(e.target.value))}
            >
              {/* 選択肢にない周期（読み込んだプロジェクトなど）も、そのまま選ばれた状態にする */}
              {!presets.some((p) => p.ms === intervalMs) && (
                <option value={intervalMs}>{intervalMs}ms</option>
              )}
              {presets.map((p) => (
                <option key={p.ms} value={p.ms}>
                  {p.label}
                </option>
              ))}
            </select>
          </div>
        </div>
      </div>

      <div className="editor-body">
        <textarea
          aria-label="コード"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          className="code-editor"
          spellCheck={false}
        />
      </div>

      <div className="editor-footer">
        <div className="editor-footer-left">
          <button
            onClick={() => onSave(name, code, intervalMs)}
            className="btn-primary"
          >
            保存
          </button>
          <button
            onClick={() => onRunOnce(code)}
            className="btn-secondary"
            title="入力中のコードを、保存せずに 1 回だけ実行します。結果とエラーはコンソールに出ます"
          >
            1 回実行
          </button>
        </div>
        <button
          onClick={onDelete}
          className="btn-danger"
          disabled={script.isRunning}
          title={script.isRunning ? "実行中は削除できません" : undefined}
        >
          削除
        </button>
      </div>
    </div>
  );
}

export function ScriptPanel() {
  const [scripts, setScripts] = useState<application.ScriptDTO[]>([]);
  const [presets, setPresets] = useState<application.IntervalPresetDTO[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  // 保存・削除・開始・停止に失敗したときのエラー（次の操作が成功すると消える）。
  // スクリプトの実行中のエラーは、ここではなくコンソールに出る
  const [error, setError] = useState<string | null>(null);
  const [consoleLogs, setConsoleLogs] = useState<application.ConsoleLogDTO[]>(
    [],
  );
  const consoleRef = useRef<HTMLDivElement>(null);

  const selectedScript = scripts.find((s) => s.id === selectedId) ?? null;

  useEffect(() => {
    loadData();
    const offScripts = EventsOn(
      "plc:scripts-changed",
      (scripts: application.ScriptDTO[]) => {
        setScripts(scripts || []);
      },
    );
    const offLog = EventsOn(
      "plc:console-log-added",
      (entry: application.ConsoleLogDTO) => {
        setConsoleLogs((prev) => {
          const next = [...prev, entry];
          return next.length > 500 ? next.slice(-500) : next;
        });
      },
    );
    const offImported = EventsOn("project:imported", () => {
      loadData();
    });
    return () => {
      offScripts();
      offLog();
      offImported();
    };
  }, []);

  useEffect(() => {
    if (consoleRef.current) {
      consoleRef.current.scrollTop = consoleRef.current.scrollHeight;
    }
  }, [consoleLogs]);

  // 選択中のスクリプトがなくなったら（削除・プロジェクトの読み込み）、先頭を選ぶ
  useEffect(() => {
    if (selectedId && !scripts.some((s) => s.id === selectedId)) {
      setSelectedId(scripts[0]?.id ?? null);
    }
  }, [scripts, selectedId]);

  const loadData = async () => {
    await Promise.all([loadScripts(), loadPresets(), loadConsoleLogs()]);
  };

  const loadScripts = async () => {
    try {
      const s = await GetScripts();
      setScripts(s || []);
    } catch (e) {
      console.error("Failed to load scripts:", e);
    }
  };

  const loadPresets = async () => {
    try {
      const p = await GetIntervalPresets();
      setPresets(p || []);
    } catch (e) {
      console.error("Failed to load presets:", e);
    }
  };

  const loadConsoleLogs = async () => {
    try {
      const logs = await GetConsoleLogs();
      setConsoleLogs(logs || []);
    } catch (e) {
      console.error("Failed to load console logs:", e);
    }
  };

  const handleClearConsoleLogs = async () => {
    try {
      await ClearConsoleLogs();
      setConsoleLogs([]);
    } catch (e) {
      console.error("Failed to clear console logs:", e);
    }
  };

  // 操作を実行し、失敗したらエラーを表示する。一覧はどちらの場合も読み直す
  const run = async (action: () => Promise<unknown>) => {
    try {
      await action();
      setError(null);
    } catch (e) {
      setError(String(e));
    }
    await loadScripts();
  };

  const handleNew = () =>
    run(async () => {
      const created = await CreateScript(
        generateNewScriptName(scripts),
        DEFAULT_CODE,
        1000,
      );
      setSelectedId(created.id);
    });

  const handleSave = (
    id: string,
    name: string,
    code: string,
    intervalMs: number,
  ) => run(() => UpdateScript(id, name, code, intervalMs));

  // 結果もエラーもコンソールに出るので、ここでは何も表示しない
  const handleRunOnce = (code: string) => {
    RunScriptOnce(code).catch(() => {});
  };

  const handleDelete = (id: string) => {
    if (!confirm("このスクリプトを削除しますか？")) return;
    // 削除したあとは、同じ位置（なければ 1 つ前）のスクリプトを選ぶ
    const index = scripts.findIndex((s) => s.id === id);
    const rest = scripts.filter((s) => s.id !== id);
    run(async () => {
      await DeleteScript(id);
      if (selectedId === id) {
        setSelectedId((rest[index] ?? rest[index - 1])?.id ?? null);
      }
    });
  };

  const handleToggle = (script: application.ScriptDTO) =>
    run(() =>
      script.isRunning ? StopScript(script.id) : StartScript(script.id),
    );

  return (
    <div className="panel panel-fill">
      <Group orientation="horizontal">
        <Panel className="server-tab-list" defaultSize="25%">
          <div className="server-panel-header">
            <span>SCRIPTS</span>
            <div className="panel-header-actions">
              <button
                onClick={handleNew}
                className="toolbar-icon-button"
                title="スクリプトを追加"
              >
                <SquarePlus size={14} />
              </button>
            </div>
          </div>

          {scripts.map((script) => {
            const isSelected = script.id === selectedId;
            const isRunning = script.isRunning;
            return (
              <div
                key={script.id}
                className={`server-tab-item${isSelected ? " selected" : ""}`}
                onClick={() => setSelectedId(script.id)}
              >
                <div className="server-tab-left">
                  <div
                    className={`server-status-dot ${isRunning ? "running" : "stopped"}`}
                    title={isRunning ? "Running" : "Stopped"}
                  />
                  <span className="server-tab-name">{script.name}</span>
                  {script.name && (
                    <span className="script-interval">
                      {presets.find((p) => p.ms === script.intervalMs)?.label ||
                        `${script.intervalMs}ms`}
                    </span>
                  )}
                </div>
                <div className="server-actions">
                  <button
                    title={isRunning ? "停止" : "開始"}
                    onClick={(e) => {
                      e.stopPropagation();
                      handleToggle(script);
                    }}
                    className="server-run-icon-button"
                  >
                    {isRunning ? <Square size={14} /> : <Play size={14} />}
                  </button>
                </div>
              </div>
            );
          })}
        </Panel>
        <Panel className="script-tab-content">
          <Group orientation="vertical">
            <Panel className="script-editor-panel">
              {scripts.length === 0 ? (
                <p className="empty-message">スクリプトがありません</p>
              ) : selectedScript ? (
                <ScriptEditPanel
                  key={selectedScript.id}
                  script={selectedScript}
                  presets={presets}
                  onSave={(name, code, intervalMs) =>
                    handleSave(selectedScript.id, name, code, intervalMs)
                  }
                  onRunOnce={handleRunOnce}
                  onDelete={() => handleDelete(selectedScript.id)}
                />
              ) : (
                <p className="empty-message">スクリプトが選択されていません</p>
              )}
            </Panel>
            <Separator className="console-separator" />
            <Panel defaultSize="25%">
              {error && <div className="error-message">{error}</div>}

              <div className="console-section">
                <div className="console-header">
                  <span>コンソール</span>
                  <button
                    onClick={handleClearConsoleLogs}
                    className="btn-secondary"
                  >
                    クリア
                  </button>
                </div>
                <div className="console-output" ref={consoleRef}>
                  {consoleLogs.length === 0 ? (
                    <span className="console-empty">出力なし</span>
                  ) : (
                    consoleLogs.map((log, i) => (
                      <div key={i} className={`console-entry ${log.level}`}>
                        <span className="console-time">
                          {new Date(log.at).toLocaleTimeString()}
                        </span>
                        <span className="console-script">
                          [{log.scriptName}]
                        </span>
                        <span className="console-message">{log.message}</span>
                      </div>
                    ))
                  )}
                </div>
              </div>
            </Panel>
          </Group>
        </Panel>
      </Group>
    </div>
  );
}
