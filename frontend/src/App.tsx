import { useState, useEffect, useRef, useCallback, ReactNode } from "react";
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
import { PageActiveContext } from "./components/usePageActive";
import {
  DEFAULT_LAYOUT,
  PageId,
  PaneLayout,
  PaneSide,
  clampRatio,
  focusPane,
  focusedPage,
  hiddenBy,
  isSplit,
  paneOf,
  parseLayout,
  showPage,
  toggleSplit,
} from "./utils/panes";

const APP_VERSION = "v0.0.51";

const LAYOUT_KEY = "karakuri.panes";

// 前の並べ方（その PC の画面に覚えておく。読めなければ既定）
function loadLayout(): PaneLayout {
  try {
    return parseLayout(localStorage.getItem(LAYOUT_KEY));
  } catch {
    return DEFAULT_LAYOUT;
  }
}

// Page は、ページ 1 つの枠。出ていない間は隠し、中の周期的な読み直しを止める（usePageActive）。
// 2 画面のときは、左右のどちらの欄に出すかを CSS で決める（DOM の順は変えない。変えると作り直しになる）
function Page({
  name,
  layout,
  onFocus,
  children,
}: {
  name: PageId;
  layout: PaneLayout;
  onFocus: (side: PaneSide) => void;
  children: ReactNode;
}) {
  const side = paneOf(layout, name);
  const split = isSplit(layout);
  const classes = ["app-page"];
  if (!side) classes.push("app-page-hidden");
  if (split && side) {
    classes.push(`app-page-${side}`);
    if (layout.focused === side) classes.push("app-page-focused");
  }
  return (
    <div
      className={classes.join(" ")}
      data-page={name}
      data-pane={split && side ? side : undefined}
      style={
        split && side === "left"
          ? { flex: `0 0 ${layout.ratio * 100}%` }
          : undefined
      }
      onPointerDownCapture={side ? () => onFocus(side) : undefined}
      onFocusCapture={side ? () => onFocus(side) : undefined}
    >
      <PageActiveContext.Provider value={side !== null}>
        {children}
      </PageActiveContext.Provider>
    </div>
  );
}

function App() {
  // 画面の並べ方（1 画面か 2 画面か・どの欄にどのページを出すか・左の欄の幅）
  const [layout, setLayout] = useState<PaneLayout>(loadLayout);
  // 2 画面をやめる前に、右に出していたページ（もう一度 2 画面にしたときに出す）
  const lastRight = useRef<PageId | null>(layout.right);
  // 開いている（保存した）プロジェクトのファイル。タイトルバーに名前を出す
  const [projectPath, setProjectPath] = useState("");
  const [serverPanelDirty, setServerPanelDirty] = useState(false);
  // 通信画面に保存していない変更があるので、確認を待っている並べ方
  const [pendingLayout, setPendingLayout] = useState<PaneLayout | null>(null);
  const serverPanelRef = useRef<ServerPanelHandle>(null);
  const mainRef = useRef<HTMLElement>(null);

  useEffect(() => {
    if (layout.right) lastRight.current = layout.right;
    try {
      localStorage.setItem(LAYOUT_KEY, JSON.stringify(layout));
    } catch {
      // 覚えられなくても、そのまま使える
    }
  }, [layout]);

  // ログの行から移るときの、選んでほしいもの（移った先の画面が選んだら null に戻す）
  const [navRequest, setNavRequest] = useState<LogTarget | null>(null);
  const clearNavRequest = useCallback(() => setNavRequest(null), []);

  // 並べ方を変える。通信画面が隠れることになり、保存していない変更があれば、先に確認する
  const applyLayout = (next: PaneLayout) => {
    if (serverPanelDirty && hiddenBy(layout, next).includes("server")) {
      setPendingLayout(next);
    } else {
      setLayout(next);
    }
  };

  // ログの行の出どころを押したとき: 該当のページを出し、そのサーバー・接続先・スクリプトを選ぶ
  const handleLogNavigate = (target: LogTarget) => {
    setNavRequest(target);
    applyLayout(
      showPage(layout, target.kind === "script" ? "scripts" : "server"),
    );
  };

  const handleFocusPane = useCallback(
    (side: PaneSide) => setLayout((l) => focusPane(l, side)),
    [],
  );

  const handleConfirmSaveAndNavigate = async () => {
    if (!pendingLayout) return;
    const next = pendingLayout;
    setPendingLayout(null);
    await serverPanelRef.current?.save();
    setLayout(next);
  };

  const handleConfirmDiscardAndNavigate = async () => {
    if (!pendingLayout) return;
    const next = pendingLayout;
    setPendingLayout(null);
    await serverPanelRef.current?.revert();
    setLayout(next);
  };

  // 左右の境目をドラッグして、左の欄の幅を変える
  const ratioAt = (clientX: number): number => {
    const rect = mainRef.current?.getBoundingClientRect();
    if (!rect || rect.width === 0) return layout.ratio;
    return clampRatio((clientX - rect.left) / rect.width);
  };
  const setRatio = (ratio: number) =>
    setLayout((l) => ({ ...l, ratio: clampRatio(ratio) }));

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
          selected={focusedPage(layout)}
          shown={isSplit(layout) ? [layout.left, layout.right!] : []}
          split={isSplit(layout)}
          onSelect={(page) => {
            setNavRequest(null);
            applyLayout(showPage(layout, page));
          }}
          onToggleSplit={() => {
            setNavRequest(null);
            applyLayout(toggleSplit(layout, lastRight.current));
          }}
          onOpenProject={handleOpenProject}
          onSaveProject={handleSaveProject}
        />
        <main
          className={`app-main${isSplit(layout) ? " app-main-split" : ""}`}
          ref={mainRef}
        >
          {/* ページは、ほかのページを見ている間も作り直さずに隠すだけ（選んでいたもの・入力中の内容を保つ） */}
          <Page name="server" layout={layout} onFocus={handleFocusPane}>
            <ServerPanel
              ref={serverPanelRef}
              onDirtyChange={setServerPanelDirty}
              selectRequest={
                paneOf(layout, "server") &&
                navRequest &&
                navRequest.kind !== "script"
                  ? { kind: navRequest.kind, id: navRequest.id }
                  : null
              }
              onSelectRequestHandled={clearNavRequest}
            />
          </Page>
          <Page name="variables" layout={layout} onFocus={handleFocusPane}>
            <VariableView />
          </Page>
          <Page name="scripts" layout={layout} onFocus={handleFocusPane}>
            <ScriptPanel
              selectRequest={
                paneOf(layout, "scripts") && navRequest?.kind === "script"
                  ? navRequest.id
                  : null
              }
              onSelectRequestHandled={clearNavRequest}
            />
          </Page>
          {isSplit(layout) && (
            <div
              className="pane-splitter"
              role="separator"
              aria-orientation="vertical"
              aria-label="左右の欄の幅"
              aria-valuemin={20}
              aria-valuemax={80}
              aria-valuenow={Math.round(layout.ratio * 100)}
              tabIndex={0}
              title="ドラッグで幅を変える（ダブルクリックで半分に戻す）"
              onPointerDown={(e) => {
                e.preventDefault();
                e.currentTarget.setPointerCapture(e.pointerId);
              }}
              onPointerMove={(e) => {
                if (e.currentTarget.hasPointerCapture(e.pointerId)) {
                  setRatio(ratioAt(e.clientX));
                }
              }}
              onDoubleClick={() => setRatio(0.5)}
              onKeyDown={(e) => {
                if (e.key === "ArrowLeft") setRatio(layout.ratio - 0.02);
                else if (e.key === "ArrowRight") setRatio(layout.ratio + 0.02);
                else return;
                e.preventDefault();
              }}
            />
          )}
        </main>{" "}
        {pendingLayout && (
          <UnsavedChangesDialog
            discardLabel="保存せずに移動"
            saveLabel="保存してから移動"
            onCancel={() => {
              setPendingLayout(null);
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
