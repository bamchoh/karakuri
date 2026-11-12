import { useState, useEffect } from "react";
import { GetServerInstances } from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { MonitoringView } from "./MonitoringView";
import { RegisterListView } from "./RegisterListView";

export type RegisterTab = "list" | "monitoring";

interface RegisterPanelProps {
  activeSubTab: RegisterTab;
  onSubTabChange: (tab: RegisterTab) => void;
}

// レジスタ画面: 「一覧表示」（RegisterListView）と「モニタリング」（MonitoringView）のサブタブを切り替える。
// サーバーの一覧はここで読み込み、両方のタブに渡す。
export function RegisterPanel({
  activeSubTab,
  onSubTabChange,
}: RegisterPanelProps) {
  const [serverInstances, setServerInstances] = useState<
    application.ServerInstanceDTO[]
  >([]);

  // サーバーインスタンスをポーリング
  useEffect(() => {
    const loadInstances = async () => {
      try {
        const instances = await GetServerInstances();
        setServerInstances(instances || []);
      } catch (e) {
        console.error("Failed to load server instances:", e);
      }
    };
    loadInstances();
    const interval = setInterval(loadInstances, 1000);
    return () => clearInterval(interval);
  }, []);

  return (
    <div className="panel">
      <h2>レジスタ</h2>

      {/* サブタブナビゲーション */}
      <div className="sub-tab-nav">
        <button
          className={`sub-tab-button ${activeSubTab === "list" ? "active" : ""}`}
          onClick={() => onSubTabChange("list")}
        >
          一覧表示
        </button>
        <button
          className={`sub-tab-button ${activeSubTab === "monitoring" ? "active" : ""}`}
          onClick={() => onSubTabChange("monitoring")}
        >
          モニタリング
        </button>
      </div>

      {/* モニタリングタブ */}
      {activeSubTab === "monitoring" && (
        <MonitoringView serverInstances={serverInstances} />
      )}

      {/* 一覧表示タブ（タブを切り替えても設定を保つため、常にマウントしておく） */}
      <RegisterListView
        serverInstances={serverInstances}
        active={activeSubTab === "list"}
      />
    </div>
  );
}
