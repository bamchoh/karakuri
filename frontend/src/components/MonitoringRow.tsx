import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { application } from "../../wailsjs/go/models";
import {
  BIT_WIDTHS,
  DISPLAY_FORMATS,
  ENDIANNESS_OPTIONS,
} from "../constants/register";
import type { MonitoringItemWithValue } from "./useMonitoringItems";

// モニタリングの 1 行。左端のつまみをドラッグして並べ替えられる
interface Props {
  itemWithValue: MonitoringItemWithValue;
  memoryAreasByProtocol: Record<string, application.MemoryAreaDTO[]>;
  serverInstances: application.ServerInstanceDTO[];
  onSettingChange: (
    item: MonitoringItemWithValue,
    field: "displayFormat" | "bitWidth" | "endianness",
    value: string | number,
  ) => void;
  onValueClick: (item: MonitoringItemWithValue) => void;
  onDelete: (id: string) => void;
}

export function MonitoringRow({
  itemWithValue,
  memoryAreasByProtocol,
  serverInstances,
  onSettingChange,
  onValueClick,
  onDelete,
}: Props) {
  const item = itemWithValue.item;
  const areas = memoryAreasByProtocol[item.protocolType] || [];
  const area = areas.find((a) => a.id === item.memoryArea);
  const server = serverInstances.find(
    (s) => s.protocolType === item.protocolType,
  );

  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: item.id });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
    backgroundColor: isDragging ? "rgba(233, 69, 96, 0.1)" : undefined,
  };

  return (
    <tr ref={setNodeRef} style={style}>
      <td className="monitoring-drag-handle" {...attributes} {...listeners}>
        <span className="drag-handle">⠿</span>
      </td>
      {serverInstances.length > 1 && (
        <td style={{ fontSize: "0.75rem", color: "#9ca3af" }}>
          {server?.displayName || item.protocolType}
        </td>
      )}
      <td>{area?.displayName || item.memoryArea}</td>
      <td>{area?.oneOrigin ? item.address + 1 : item.address}</td>
      <td>
        {!itemWithValue.isBit ? (
          <select
            value={item.bitWidth}
            onChange={(e) =>
              onSettingChange(
                itemWithValue,
                "bitWidth",
                parseInt(e.target.value),
              )
            }
            className="inline-select"
          >
            {BIT_WIDTHS.map((b) => (
              <option key={b.value} value={b.value}>
                {b.label}
              </option>
            ))}
          </select>
        ) : (
          "Bit"
        )}
      </td>
      <td>
        {!itemWithValue.isBit ? (
          <select
            value={item.endianness}
            onChange={(e) =>
              onSettingChange(itemWithValue, "endianness", e.target.value)
            }
            className="inline-select"
          >
            {ENDIANNESS_OPTIONS.map((e) => (
              <option key={e.value} value={e.value}>
                {e.label}
              </option>
            ))}
          </select>
        ) : (
          "-"
        )}
      </td>
      <td>
        {!itemWithValue.isBit ? (
          <select
            value={item.displayFormat}
            onChange={(e) =>
              onSettingChange(itemWithValue, "displayFormat", e.target.value)
            }
            className="inline-select"
          >
            {DISPLAY_FORMATS.map((f) => (
              <option key={f.value} value={f.value}>
                {f.label}
              </option>
            ))}
          </select>
        ) : (
          "-"
        )}
      </td>
      <td
        className="monitoring-value"
        onClick={() => onValueClick(itemWithValue)}
      >
        {itemWithValue.currentValue}
      </td>
      <td className="monitoring-actions">
        <button
          onClick={() => onDelete(item.id)}
          className="btn-small btn-danger"
        >
          削除
        </button>
      </td>
    </tr>
  );
}
