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
  ClearScriptError,
  GetConsoleLogs,
  ClearConsoleLogs,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { application } from "../../wailsjs/go/models";
import { SpaceIcon, Square, Play, Trash2, SquarePlus } from "lucide-react";
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

const ScriptEditPanel = ({
  selectedScript,
}: {
  selectedScript: application.ScriptDTO | null;
}) => {
  const [editName, setEditName] = useState(selectedScript?.name ?? "");
  const [editInterval, setEditInterval] = useState(1000);
  const [presets, setPresets] = useState<application.IntervalPresetDTO[]>([]);
  const [editCode, setEditCode] = useState(
    selectedScript?.code ?? DEFAULT_CODE,
  );
  const [testOutput, setTestOutput] = useState<string | null>(null);

  useEffect(() => {
    loadData();
  }, []);

  const handleSave = async () => {
    try {
      if (selectedScript) {
        await UpdateScript(selectedScript.id, editName, editCode, editInterval);
      } else {
        await CreateScript(editName, editCode, editInterval);
      }
    } catch (e) {}
  };

  const handleTest = async () => {
    try {
      const result = await RunScriptOnce(editCode);
      setTestOutput(
        result !== undefined ? JSON.stringify(result, null, 2) : "(no output)",
      );
    } catch (e) {
      setTestOutput(null);
    }
  };

  const handleCancel = () => {
    setTestOutput(null);
  };

  const handleDelete = async (id: string) => {
    if (confirm("このスクリプトを削除しますか？")) {
      try {
        await DeleteScript(id);
      } catch (e) {}
    }
  };

  useEffect(() => {
    const off = EventsOn("project:imported", () => {
      loadData();
    });

    return off;
  }, []);

  const loadData = async () => {
    await Promise.all([loadPresets()]);
  };

  const loadPresets = async () => {
    try {
      const p = await GetIntervalPresets();
      setPresets(p || []);
    } catch (e) {
      console.error("Failed to load presets:", e);
    }
  };

  return (
    <div className="panel">
      {/* error && <div className="error-message">{error}</div> */}

      <div className="form-group">
        <label>名前</label>
        <input
          type="text"
          value={editName}
          onChange={(e) => setEditName(e.target.value)}
        />
      </div>

      <div className="form-group">
        <label>実行周期</label>
        <select
          value={editInterval}
          onChange={(e) => setEditInterval(parseInt(e.target.value))}
        >
          {presets.map((p) => (
            <option key={p.ms} value={p.ms}>
              {p.label}
            </option>
          ))}
        </select>
      </div>

      <div className="form-group">
        <label>コード</label>
        <textarea
          value={editCode}
          onChange={(e) => setEditCode(e.target.value)}
          className="code-editor"
          spellCheck={false}
        />
      </div>

      {testOutput && (
        <div className="test-output">
          <label>テスト結果:</label>
          <pre>{testOutput}</pre>
        </div>
      )}

      <div className="button-group">
        <button onClick={handleTest} className="btn-secondary">
          テスト実行
        </button>
        <button onClick={handleSave} className="btn-primary">
          保存
        </button>
        <button onClick={handleCancel} className="btn-secondary">
          キャンセル
        </button>
        <button
          onClick={() => handleDelete(selectedScript?.id ?? "")}
          className="btn-danger"
          disabled={selectedScript?.isRunning}
        >
          削除
        </button>
      </div>
    </div>
  );
};

export function ScriptPanel() {
  const [scripts, setScripts] = useState<application.ScriptDTO[]>([]);
  const [presets, setPresets] = useState<application.IntervalPresetDTO[]>([]);
  const [selectedScript, setSelectedScript] =
    useState<application.ScriptDTO | null>(null);
  const [isEditing, setIsEditing] = useState(false);
  const [editName, setEditName] = useState("");
  const [editCode, setEditCode] = useState("");
  const [editInterval, setEditInterval] = useState(1000);
  const [testOutput, setTestOutput] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [consoleLogs, setConsoleLogs] = useState<application.ConsoleLogDTO[]>(
    [],
  );
  const consoleRef = useRef<HTMLDivElement>(null);

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
    return () => {
      offScripts();
      offLog();
    };
  }, []);

  useEffect(() => {
    if (consoleRef.current) {
      consoleRef.current.scrollTop = consoleRef.current.scrollHeight;
    }
  }, [consoleLogs]);

  useEffect(() => {
    const off = EventsOn("project:imported", () => {
      loadData();
    });

    return off;
  }, []);

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

  const handleNew = () => {
    setSelectedScript(null);
    setIsEditing(true);
    setEditName("新しいスクリプト");
    setEditCode(DEFAULT_CODE);
    setEditInterval(1000);
    setError(null);
    setTestOutput(null);
  };

  const handleEdit = (script: application.ScriptDTO) => {
    setSelectedScript(script);
    setIsEditing(true);
    setEditName(script.name);
    setEditCode(script.code);
    setEditInterval(script.intervalMs);
    setError(null);
    setTestOutput(null);
  };

  const handleSave = async () => {
    try {
      setError(null);
      if (selectedScript) {
        await UpdateScript(selectedScript.id, editName, editCode, editInterval);
      } else {
        await CreateScript(editName, editCode, editInterval);
      }
      setIsEditing(false);
      setSelectedScript(null);
      await loadScripts();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleDelete = async (id: string) => {
    if (confirm("このスクリプトを削除しますか？")) {
      try {
        await DeleteScript(id);
        await loadScripts();
      } catch (e) {
        setError(String(e));
      }
    }
  };

  const handleToggle = async (script: application.ScriptDTO) => {
    try {
      if (script.isRunning) {
        await StopScript(script.id);
      } else {
        await StartScript(script.id);
      }
      await loadScripts();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleClearError = async (id: string) => {
    try {
      await ClearScriptError(id);
      await loadScripts();
    } catch (e) {
      console.error("Failed to clear script error:", e);
    }
  };

  const handleTest = async () => {
    try {
      setError(null);
      const result = await RunScriptOnce(editCode);
      setTestOutput(
        result !== undefined ? JSON.stringify(result, null, 2) : "(no output)",
      );
    } catch (e) {
      setError(String(e));
      setTestOutput(null);
    }
  };

  const handleCancel = () => {
    setIsEditing(false);
    setSelectedScript(null);
    setError(null);
    setTestOutput(null);
  };

  const handleSelectTab = (scriptId: string) => {
    if (scriptId === selectedScript?.id) return;
    const foundScript = scripts.find((s) => s.id === scriptId);
    setSelectedScript(foundScript ?? null); // undefined なら null にする
  };

  if (isEditing) {
    return (
      <div className="panel">
        <h2>{selectedScript ? "スクリプト編集" : "新しいスクリプト"}</h2>

        {error && <div className="error-message">{error}</div>}

        <div className="form-group">
          <label>名前</label>
          <input
            type="text"
            value={editName}
            onChange={(e) => setEditName(e.target.value)}
          />
        </div>

        <div className="form-group">
          <label>実行周期</label>
          <select
            value={editInterval}
            onChange={(e) => setEditInterval(parseInt(e.target.value))}
          >
            {presets.map((p) => (
              <option key={p.ms} value={p.ms}>
                {p.label}
              </option>
            ))}
          </select>
        </div>

        <div className="form-group">
          <label>コード</label>
          <textarea
            value={editCode}
            onChange={(e) => setEditCode(e.target.value)}
            className="code-editor"
            spellCheck={false}
          />
        </div>

        {testOutput && (
          <div className="test-output">
            <label>テスト結果:</label>
            <pre>{testOutput}</pre>
          </div>
        )}

        <div className="button-group">
          <button onClick={handleTest} className="btn-secondary">
            テスト実行
          </button>
          <button onClick={handleSave} className="btn-primary">
            保存
          </button>
          <button onClick={handleCancel} className="btn-secondary">
            キャンセル
          </button>
        </div>
      </div>
    );
  }

  return (
    <div
      className="panel"
      style={{ display: "flex", flexDirection: "column", height: "100%" }}
    >
      <Group orientation="horizontal">
        <Panel className="server-tab-list" defaultSize="25%">
          <div className="server-panel-header">
            <span>SCRIPTS</span>
            <div style={{ display: "flex", gap: "8px", alignItems: "center" }}>
              <button onClick={handleNew} className="toolbar-icon-button">
                <SquarePlus size={14} />
              </button>
            </div>
          </div>

          {scripts.map((script) => {
            const isSelected = script.id === selectedScript?.id;
            const statusClass = script.isRunning
              ? "running"
              : script.lastError
                ? "error"
                : "stopped";
            const isRunning = script.isRunning;
            return (
              <div
                key={script.id}
                className={`server-tab-item${isSelected ? " selected" : ""}`}
                onClick={() => handleSelectTab(script.id)}
              >
                <div className="server-tab-left">
                  <div
                    className={`server-status-dot ${statusClass}`}
                    title={
                      script.isRunning
                        ? "Running"
                        : script.lastError
                          ? "Error"
                          : "Stopped"
                    }
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
        <Panel className="server-tab-content">
          <Group orientation="vertical">
            <Panel>
              {scripts.length === 0 ? (
                <p className="empty-message">スクリプトがありません</p>
              ) : selectedScript ? (
                <ScriptEditPanel
                  key={selectedScript.id}
                  selectedScript={selectedScript}
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
                      <div key={i} className="console-entry">
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
