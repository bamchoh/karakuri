// Sidebar.tsx
import { Server, Database, Activity, FileCode2 } from "lucide-react";

type Tab = "server" | "variables" | "registers" | "scripts";

type Props = {
  selected: Tab;
  onSelect: (tab: Tab) => void;
};

const items = [
  {
    id: "server",
    icon: Server,
    label: "Server",
  },
  {
    id: "variables",
    icon: Database,
    label: "Variables",
  },
  {
    id: "registers",
    icon: Activity,
    label: "Registers",
  },
  {
    id: "scripts",
    icon: FileCode2,
    label: "Scripts",
  },
] as const;

export function Sidebar({ selected, onSelect }: Props) {
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
    </aside>
  );
}
