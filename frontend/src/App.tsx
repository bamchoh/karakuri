import { useState, useEffect, useRef, useCallback } from "react";
import "./App.css";
import { ServerPanel, ServerPanelHandle } from "./components/ServerPanel";
import { UnsavedChangesDialog } from "./components/UnsavedChangesDialog";
import { VariableView } from "./components/VariableView";
import { ScriptPanel } from "./components/ScriptPanel";
import { AppLogPanel, LogTarget } from "./components/AppLogPanel";
import { Sidebar } from "./components/Sidebar";
import { HttpApiIndicator } from "./components/HttpApiIndicator";
import { ExportProject, ImportProject } from "../wailsjs/go/main/App";
import { WindowSetTitle } from "../wailsjs/runtime/runtime";
import { windowTitle } from "./utils/windowTitle";

const APP_VERSION = "v0.0.43";

type Tab = "server" | "variables" | "scripts";

function App() {
  const [activeTab, setActiveTab] = useState<Tab>("server");
  // 開いている（保存した）プロジェクトのファイル。タイトルバーに名前を出す
  const [projectPath, setProjectPath] = useState("");
  const [serverPanelDirty, setServerPanelDirty] = useState(false);
  const [pendingTab, setPendingTab] = useState<Tab | null>(null);
  const serverPanelRef = useRef<ServerPanelHandle>(null);

  // ログの行から移るときの、選んでほしいもの（移った先の画面が選んだら null に戻す）
  const [navRequest, setNavRequest] = useState<LogTarget | null>(null);
  const clearNavRequest = useCallback(() => setNavRequest(null), []);

  // ログの行の出どころを押したとき: 該当のページに移り、そのサーバー・接続先・スクリプトを選ぶ
  const handleLogNavigate = (target: LogTarget) => {
    setNavRequest(target);
    handleTabClick(target.kind === "script" ? "scripts" : "server");
  };

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

  // アプリの名前とバージョンは、ウィンドウのタイトルバーに出す（画面の中には出さない）
  useEffect(() => {
    const title = windowTitle(APP_VERSION, projectPath);
    document.title = title;
    try {
      WindowSetTitle(title);
    } catch (e) {
      console.error(e);
    }
  }, [projectPath]);

  // 保存・読み込みは、選んだファイルのパスを返す（やめたときは空）
  const handleSaveProject = async () => {
    try {
      const path = await ExportProject();
      if (path) setProjectPath(path);
    } catch (e) {
      console.error(e);
    }
  };

  const handleOpenProject = async () => {
    try {
      const path = await ImportProject();
      if (path) setProjectPath(path);
    } catch (e) {
      console.error(e);
    }
  };

  return (
    <div id="App">
      <div className="main-layout">
        <Sidebar
          selected={activeTab}
          onSelect={(tab) => {
            setNavRequest(null);
            handleTabClick(tab);
          }}
          onOpenProject={handleOpenProject}
          onSaveProject={handleSaveProject}
        />
        <main className="app-main">
          {activeTab === "server" && (
            <ServerPanel
              ref={serverPanelRef}
              onDirtyChange={setServerPanelDirty}
              selectRequest={
                navRequest && navRequest.kind !== "script"
                  ? { kind: navRequest.kind, id: navRequest.id }
                  : null
              }
              onSelectRequestHandled={clearNavRequest}
            />
          )}
          {activeTab === "variables" && <VariableView />}
          {activeTab === "scripts" && (
            <ScriptPanel
              selectRequest={
                navRequest?.kind === "script" ? navRequest.id : null
              }
              onSelectRequestHandled={clearNavRequest}
            />
          )}
        </main>{" "}
        {pendingTab && (
          <UnsavedChangesDialog
            discardLabel="保存せずに移動"
            saveLabel="保存してから移動"
            onCancel={() => {
              setPendingTab(null);
              setNavRequest(null);
            }}
            onDiscard={handleConfirmDiscardAndNavigate}
            onSave={handleConfirmSaveAndNavigate}
          />
        )}
      </div>

      {/* アプリ全体のログの欄（どのページでも下に出す。見出しの行の右端に HTTP API の URL） */}
      <AppLogPanel
        onNavigate={handleLogNavigate}
        status={<HttpApiIndicator />}
      />
    </div>
  );
}

export default App;
