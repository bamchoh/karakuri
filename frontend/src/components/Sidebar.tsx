// Sidebar.tsx
import {
  Server,
  Database,
  FileCode2,
  FolderOpen,
  Save,
  Columns2,
} from "lucide-react";

type Tab = "server" | "variables" | "scripts";

type Props = {
  // 最後に使った欄のページ（左の帯でほかのページを選ぶと、この欄のページが替わる）
  selected: Tab;
  // 2 画面のとき、出ているページ（1 画面なら空）
  shown: Tab[];
  split: boolean;
  onSelect: (tab: Tab) => void;
  onToggleSplit: () => void;
  onOpenProject: () => void;
  onSaveProject: () => void;
};

const items = [
  {
    id: "server",
    icon: Server,
    label: "Connections",
  },
  {
    id: "variables",
    icon: Database,
    label: "Variables",
  },
  {
    id: "scripts",
    icon: FileCode2,
    label: "Scripts",
  },
] as const;

// 左のアイコンの帯。上にページの切り替えと 2 画面の切り替え、下にプロジェクトの操作（開く・保存）を置く
export function Sidebar({
  selected,
  shown,
  split,
  onSelect,
  onToggleSplit,
  onOpenProject,
  onSaveProject,
}: Props) {
  return (
    <aside className="sidebar">
      {items.map((item) => {
        const Icon = item.icon;

        return (
          <button
            key={item.id}
            className={`sidebar-item ${selected === item.id ? "active" : shown.includes(item.id) ? "shown" : ""}`}
            onClick={() => onSelect(item.id)}
            title={item.label}
          >
            <Icon size={22} />
          </button>
        );
      })}
      <button
        className={`sidebar-item sidebar-split ${split ? "on" : ""}`}
        onClick={onToggleSplit}
        title={split ? "1 画面に戻す" : "2 画面にする（左右に並べる）"}
        aria-label="2 画面"
        aria-pressed={split}
      >
        <Columns2 size={22} />
      </button>
      <div className="sidebar-spacer" />
      <button
        className="sidebar-item"
        onClick={onOpenProject}
        title="プロジェクトオープン"
        aria-label="プロジェクトオープン"
      >
        <FolderOpen size={22} />
      </button>
      <button
        className="sidebar-item"
        onClick={onSaveProject}
        title="プロジェクト保存"
        aria-label="プロジェクト保存"
      >
        <Save size={22} />
      </button>
    </aside>
  );
}
