// Sidebar.tsx
import { Server, Database, FileCode2, FolderOpen, Save } from "lucide-react";

type Tab = "server" | "variables" | "scripts";

type Props = {
  selected: Tab;
  onSelect: (tab: Tab) => void;
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

// 左のアイコンの帯。上にページの切り替え、下にプロジェクトの操作（開く・保存）を置く
export function Sidebar({
  selected,
  onSelect,
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
            className={`sidebar-item ${selected === item.id ? "active" : ""}`}
            onClick={() => onSelect(item.id)}
            title={item.label}
          >
            <Icon size={22} />
          </button>
        );
      })}
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
