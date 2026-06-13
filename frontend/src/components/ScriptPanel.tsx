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
    <div className="panel">
      <h2>スクリプト</h2>

      {error && <div className="error-message">{error}</div>}

      <div className="script-list-header">
        <button onClick={handleNew} className="btn-primary">
          新規作成
        </button>
      </div>

      <div className="script-list">
        {scripts.length === 0 ? (
          <p className="empty-message">スクリプトがありません</p>
        ) : (
          scripts.map((script) => (
            <div
              key={script.id}
              className={`script-item ${script.isRunning ? "running" : ""}`}
            >
              <div className="script-info">
                <span className="script-name">{script.name}</span>
                <span className="script-interval">
                  {presets.find((p) => p.ms === script.intervalMs)?.label ||
                    `${script.intervalMs}ms`}
                </span>
                <span
                  className={`script-status ${script.isRunning ? "running" : "stopped"}`}
                >
                  {script.isRunning ? "実行中" : "停止"}
                </span>
              </div>
              {script.lastError && (
                <div
                  style={{
                    background: "rgba(255, 60, 60, 0.15)",
                    border: "1px solid rgba(255, 60, 60, 0.4)",
                    borderRadius: "4px",
                    padding: "4px 8px",
                    margin: "4px 0",
                    fontSize: "0.82em",
                    display: "flex",
                    justifyContent: "space-between",
                    alignItems: "flex-start",
                    gap: "8px",
                  }}
                >
                  <div style={{ color: "#ff6b6b", flex: 1 }}>
                    <span style={{ fontWeight: "bold" }}>Error: </span>
                    <span>{script.lastError}</span>
                    {script.errorAt > 0 && (
                      <span
                        style={{
                          color: "#999",
                          marginLeft: "8px",
                          fontSize: "0.9em",
                        }}
                      >
                        ({new Date(script.errorAt).toLocaleTimeString()})
                      </span>
                    )}
                  </div>
                  <button
                    onClick={() => handleClearError(script.id)}
                    style={{
                      background: "none",
                      border: "none",
                      color: "#999",
                      cursor: "pointer",
                      padding: "0 2px",
                      fontSize: "1em",
                      lineHeight: 1,
                    }}
                    title="エラーをクリア"
                  >
                    x
                  </button>
                </div>
              )}
              <div className="script-actions">
                <button
                  onClick={() => handleToggle(script)}
                  className={script.isRunning ? "btn-danger" : "btn-success"}
                >
                  {script.isRunning ? "停止" : "開始"}
                </button>
                <button
                  onClick={() => handleEdit(script)}
                  className="btn-secondary"
                >
                  編集
                </button>
                <button
                  onClick={() => handleDelete(script.id)}
                  className="btn-danger"
                  disabled={script.isRunning}
                >
                  削除
                </button>
              </div>
            </div>
          ))
        )}
      </div>

      <div className="console-section">
        <div className="console-header">
          <span>コンソール</span>
          <button onClick={handleClearConsoleLogs} className="btn-secondary">
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
                <span className="console-script">[{log.scriptName}]</span>
                <span className="console-message">{log.message}</span>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
