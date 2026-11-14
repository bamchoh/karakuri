import { useState, useEffect } from "react";
import { AddMonitoringDialog } from "./AddMonitoringDialog";
import { MonitoringRow } from "./MonitoringRow";
import { RegisterWriteDialog } from "./RegisterWriteDialog";
import { useEscapeKey } from "./useEscapeKey";
import {
  MonitoringItemWithValue,
  useMonitoringItems,
} from "./useMonitoringItems";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  DragEndEvent,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import {
  GetMemoryAreas,
  AddMonitoringItem,
  UpdateMonitoringItem,
  DeleteMonitoringItem,
  ReorderMonitoringItem,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { formatAddress } from "../utils/address";

import { getWordCount } from "../utils/register";

import type { DisplayFormat, BitWidth, Endianness } from "../types/register";

interface Props {
  serverInstances: application.ServerInstanceDTO[];
}

// モニタリング: 登録したレジスタの現在値を一覧で表示し、行ごとの設定変更・書き込み・並べ替えができる
export function MonitoringView({ serverInstances }: Props) {
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [memoryAreasByProtocol, setMemoryAreasByProtocol] = useState<
    Record<string, application.MemoryAreaDTO[]>
  >({});
  const { items, setItems, loadItems } = useMonitoringItems(
    memoryAreasByProtocol,
    autoRefresh,
  );

  // ドラッグ＆ドロップ用センサー
  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  // 追加ダイアログの状態
  const [isAddDialogOpen, setIsAddDialogOpen] = useState(false);
  useEscapeKey(isAddDialogOpen, () => setIsAddDialogOpen(false));

  // 書き込みダイアログ（開いていなければ null）
  const [writingItem, setWritingItem] =
    useState<MonitoringItemWithValue | null>(null);

  // プロトコルセットが変わった時だけメモリエリアを再ロード
  const protocolTypesKey = serverInstances.map((i) => i.protocolType).join(",");
  useEffect(() => {
    const loadMemoryAreas = async () => {
      const areasMap: Record<string, application.MemoryAreaDTO[]> = {};
      for (const inst of serverInstances) {
        try {
          const areas = await GetMemoryAreas(inst.protocolType);
          areasMap[inst.protocolType] = areas || [];
        } catch {
          areasMap[inst.protocolType] = [];
        }
      }
      setMemoryAreasByProtocol(areasMap);
    };
    if (serverInstances.length > 0) {
      loadMemoryAreas();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [protocolTypesKey]);

  // 追加ダイアログを開く
  const handleAdd = () => {
    setIsAddDialogOpen(true);
  };

  // 保存（複数追加対応）
  const handleSave = async (
    formProtocolType: string,
    formArea: string,
    formAddress: number,
    formCount: number,
    formBitWidth: BitWidth,
    formEndianness: Endianness,
    formDisplayFormat: DisplayFormat,
  ) => {
    try {
      const formAreas = memoryAreasByProtocol[formProtocolType] || [];
      for (let i = 0; i < formCount; i++) {
        const area = formAreas.find((a) => a.id === formArea);
        const isBit = area?.isBit ?? false;
        const addressIncrement = isBit ? 1 : getWordCount(formBitWidth);

        const itemData: application.MonitoringItemDTO = {
          id: "",
          order: 0,
          protocolType: formProtocolType,
          memoryArea: formArea,
          address: formAddress + i * addressIncrement,
          bitWidth: formBitWidth,
          endianness: formEndianness,
          displayFormat: formDisplayFormat,
        };

        await AddMonitoringItem(itemData);
      }

      setIsAddDialogOpen(false);
      await loadItems();
    } catch (e) {
      console.error("Failed to save monitoring item:", e);
    }
  };

  // 設定変更（表示形式、ビット幅、エンディアン）
  const handleSettingChange = async (
    itemWithValue: MonitoringItemWithValue,
    field: "displayFormat" | "bitWidth" | "endianness",
    value: string | number,
  ) => {
    const item = { ...itemWithValue.item };
    if (field === "displayFormat") {
      item.displayFormat = value as string;
    } else if (field === "bitWidth") {
      item.bitWidth = value as number;
    } else if (field === "endianness") {
      item.endianness = value as string;
    }

    try {
      await UpdateMonitoringItem(item);
      await loadItems();
    } catch (e) {
      console.error("Failed to update monitoring item:", e);
    }
  };

  // 削除
  const handleDelete = async (id: string) => {
    try {
      await DeleteMonitoringItem(id);
      await loadItems();
    } catch (e) {
      console.error("Failed to delete monitoring item:", e);
    }
  };

  // ドラッグ＆ドロップによる並び替え
  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event;
    if (over && active.id !== over.id) {
      const oldIndex = items.findIndex((item) => item.item.id === active.id);
      const newIndex = items.findIndex((item) => item.item.id === over.id);

      // UIを先に更新（楽観的更新）
      setItems((items) => arrayMove(items, oldIndex, newIndex));

      // バックエンドに通知
      try {
        await ReorderMonitoringItem(active.id as string, newIndex);
      } catch (e) {
        console.error("Failed to reorder monitoring item:", e);
        // エラー時はリロード
        await loadItems();
      }
    }
  };

  // 書き込んだあと: 閉じてから読み直す
  const handleWritten = async () => {
    setWritingItem(null);
    await loadItems();
  };

  // 書き込みダイアログ用エリア情報
  const writingArea = writingItem
    ? (memoryAreasByProtocol[writingItem.item.protocolType] || []).find(
        (a) => a.id === writingItem.item.memoryArea,
      )
    : undefined;

  return (
    <div className="monitoring-view">
      <div className="monitoring-controls">
        <button onClick={handleAdd} className="btn-primary">
          追加
        </button>
        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={autoRefresh}
            onChange={(e) => setAutoRefresh(e.target.checked)}
          />
          自動更新
        </label>
        <button onClick={loadItems} className="btn-secondary">
          更新
        </button>
      </div>

      {items.length === 0 ? (
        <div className="monitoring-empty">
          モニタリング項目がありません。「追加」ボタンで項目を登録してください。
        </div>
      ) : (
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragEnd={handleDragEnd}
        >
          <table className="monitoring-table">
            <thead>
              <tr>
                <th></th>
                {serverInstances.length > 1 && <th>プロトコル</th>}
                <th>エリア</th>
                <th>アドレス</th>
                <th>ビット幅</th>
                <th>エンディアン</th>
                <th>表示形式</th>
                <th>現在値</th>
                <th>操作</th>
              </tr>
            </thead>
            <SortableContext
              items={items.map((i) => i.item.id)}
              strategy={verticalListSortingStrategy}
            >
              <tbody>
                {items.map((itemWithValue) => (
                  <MonitoringRow
                    key={itemWithValue.item.id}
                    itemWithValue={itemWithValue}
                    memoryAreasByProtocol={memoryAreasByProtocol}
                    serverInstances={serverInstances}
                    onSettingChange={handleSettingChange}
                    onValueClick={setWritingItem}
                    onDelete={handleDelete}
                  />
                ))}
              </tbody>
            </SortableContext>
          </table>
        </DndContext>
      )}

      {/* 追加ダイアログ */}
      {isAddDialogOpen && (
        <AddMonitoringDialog
          onSave={handleSave}
          onClose={() => setIsAddDialogOpen(false)}
          serverInstances={serverInstances}
          memoryAreasByProtocol={memoryAreasByProtocol}
        />
      )}

      {/* 書き込みダイアログ */}
      {writingItem && (
        <RegisterWriteDialog
          title="レジスタ書き込み"
          protocolType={writingItem.item.protocolType}
          area={writingItem.item.memoryArea}
          address={writingItem.item.address}
          displayAddress={formatAddress(writingArea, writingItem.item.address)}
          currentValueText={writingItem.currentValue}
          isBit={writingArea?.isBit ?? false}
          bitInput="text"
          bitWidth={(writingItem.item.bitWidth as BitWidth) || 16}
          endianness={(writingItem.item.endianness as Endianness) || "big"}
          initialFormat={
            (writingItem.item.displayFormat as DisplayFormat) || "decimal"
          }
          initialValue={writingItem.currentValue}
          onClose={() => setWritingItem(null)}
          onWritten={handleWritten}
        />
      )}
    </div>
  );
}
