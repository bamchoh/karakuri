import { useState, useEffect, useRef } from "react";
import "./App.css";
import { ServerPanel, ServerPanelHandle } from "./components/ServerPanel";
import { FocusTrap } from "./components/FocusTrap";
import { VariableView } from "./components/VariableView";
import { RegisterPanel, RegisterTab } from "./components/RegisterPanel";
import { ScriptPanel } from "./components/ScriptPanel";
import { CommunicationIndicator } from "./components/CommunicationIndicator";
import { KarakuriLogo } from "./components/KarakuriLogo";
import { GetHTTPAPIPort, SetHTTPAPIPort } from "../wailsjs/go/main/App";
import { Sidebar } from "./components/Sidebar";
import { ExportProject, ImportProject } from "../wailsjs/go/main/App";

const APP_VERSION = "v0.0.38";

type Tab = "server" | "variables" | "registers" | "scripts";

function App() {
  const [activeTab, setActiveTab] = useState<Tab>("server");
  const [registerSubTab, setRegisterSubTab] = useState<RegisterTab>("list");
  const [httpAPIPort, setHttpAPIPort] = useState<number>(8765);
  const [editingPort, setEditingPort] = useState(false);
  const [portInput, setPortInput] = useState("");
  const [portError, setPortError] = useState<string | null>(null);
  const [serverPanelDirty, setServerPanelDirty] = useState(false);
  const [pendingTab, setPendingTab] = useState<Tab | null>(null);
  const serverPanelRef = useRef<ServerPanelHandle>(null);

  const handleTabClick = (tab: Tab) => {
    if (tab !== activeTab && activeTab === "server" && serverPanelDirty) {
      setPendingTab(tab);
    } else {
      setActiveTab(tab);
    }
  };

  const handleConfirmSaveAndNavigate = async () => {
    if (!pendingTab) return;
    const tab = pendingTab;
    setPendingTab(null);
    await serverPanelRef.current?.save();
    setActiveTab(tab);
  };

  const handleConfirmDiscardAndNavigate = async () => {
    if (!pendingTab) return;
    const tab = pendingTab;
    setPendingTab(null);
    await serverPanelRef.current?.revert();
    setActiveTab(tab);
  };

  useEffect(() => {
    GetHTTPAPIPort().then(setHttpAPIPort);
  }, []);

  const handleStartEdit = () => {
    setPortInput(String(httpAPIPort));
    setPortError(null);
    setEditingPort(true);
  };

  const handleSavePort = async () => {
    const port = parseInt(portInput, 10);
    if (isNaN(port) || port < 1024 || port > 65535) {
      setPortError("1024〜65535 の範囲で入力してください");
      return;
    }
    try {
      await SetHTTPAPIPort(port);
      setHttpAPIPort(port);
      setEditingPort(false);
      setPortError(null);
    } catch (e: unknown) {
      setPortError(e instanceof Error ? e.message : String(e));
    }
  };

  const handlePortKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") handleSavePort();
    if (e.key === "Escape") setEditingPort(false);
  };

  return (
    <div id="App">
      <header className="app-header">
        <h1>
          <KarakuriLogo size={28} className="header-logo" />
          Karakuri({APP_VERSION})
        </h1>
        <CommunicationIndicator />
        <div className="httpapi-indicator">
          {editingPort ? (
            <div className="httpapi-edit-form">
              <span className="httpapi-label">API port:</span>
              <input
                type="number"
                value={portInput}
                onChange={(e) => setPortInput(e.target.value)}
                onKeyDown={handlePortKeyDown}
                min={1024}
                max={65535}
                className="httpapi-port-input"
                autoFocus
              />
              <button className="httpapi-save-btn" onClick={handleSavePort}>
                保存
              </button>
              <button
                className="httpapi-cancel-btn"
                onClick={() => setEditingPort(false)}
              >
                ✕
              </button>
              {portError && <span className="httpapi-error">{portError}</span>}
            </div>
          ) : (
            <>
              <span className="httpapi-url">
                http://localhost:{httpAPIPort}/api
              </span>
              <button
                className="httpapi-edit-btn"
                onClick={handleStartEdit}
                title="ポートを変更"
              >
                ✎
              </button>
            </>
          )}
        </div>
        <nav className="tab-nav">
          <button
            className={`btn-secondary`}
            onClick={() => serverPanelRef.current?.exportProject()}
          >
            エクスポート
          </button>
          <button
            className={`btn-secondary`}
            onClick={() => serverPanelRef.current?.importProject()}
          >
            インポート
          </button>
        </nav>
      </header>

      <div style={{ display: "flex" }}>
        <Sidebar selected={activeTab} onSelect={(tab) => handleTabClick(tab)} />
        <main className="app-main">
          {activeTab === "server" && (
            <ServerPanel
              ref={serverPanelRef}
              onDirtyChange={setServerPanelDirty}
            />
          )}
          {activeTab === "variables" && <VariableView />}
          {activeTab === "registers" && (
            <RegisterPanel
              activeSubTab={registerSubTab}
              onSubTabChange={setRegisterSubTab}
            />
          )}
          <div
            style={{ display: activeTab === "scripts" ? undefined : "none" }}
          >
            <ScriptPanel />
          </div>
        </main>{" "}
        {pendingTab && (
          <FocusTrap onConfirm={handleConfirmSaveAndNavigate}>
            <div className="dialog">
              <h3>設定が保存されていません</h3>
              <div className="dialog-content">
                <p>通信設定が変更されています。</p>
              </div>
              <div className="dialog-buttons">
                <button
                  onClick={() => setPendingTab(null)}
                  className="btn-secondary"
                >
                  キャンセル
                </button>
                <button
                  onClick={handleConfirmDiscardAndNavigate}
                  className="btn-secondary"
                >
                  保存せずに移動
                </button>
                <button
                  onClick={handleConfirmSaveAndNavigate}
                  className="btn-primary"
                >
                  保存してから移動
                </button>
              </div>
            </div>
          </FocusTrap>
        )}
      </div>
    </div>
  );
}

export default App;
