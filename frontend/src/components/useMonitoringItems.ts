import { useCallback, useEffect, useState } from "react";
import {
  GetMonitoringItems,
  ReadBits,
  ReadWords,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import {
  combineWords,
  formatBigInt,
  formatSingleWord,
  getWordCount,
} from "../utils/register";
import type { BitWidth, DisplayFormat, Endianness } from "../types/register";

// モニタリング項目と、その現在値（表示用の文字列）
export interface MonitoringItemWithValue {
  item: application.MonitoringItemDTO;
  currentValue: string;
  rawValues: number[];
  isBit: boolean;
  bitValue?: boolean;
}

type AreasByProtocol = Record<string, application.MemoryAreaDTO[]>;

// 1 項目の現在値を読み、項目の設定（ビット幅・エンディアン・表示形式）に従って文字列にする
async function readItemValue(
  item: application.MonitoringItemDTO,
  memoryAreasByProtocol: AreasByProtocol,
): Promise<MonitoringItemWithValue> {
  const areas = memoryAreasByProtocol[item.protocolType] || [];
  const area = areas.find((a) => a.id === item.memoryArea);
  const isBit = area?.isBit ?? false;
  const bitWidth = (item.bitWidth as BitWidth) || 16;
  const endianness = (item.endianness as Endianness) || "big";
  const displayFormat = (item.displayFormat as DisplayFormat) || "decimal";

  try {
    if (isBit) {
      const bits = await ReadBits(
        item.protocolType,
        item.memoryArea,
        item.address,
        1,
      );
      const bitValue = bits?.[0] ?? false;
      return {
        item,
        currentValue: bitValue ? "ON" : "OFF",
        rawValues: [],
        isBit: true,
        bitValue,
      };
    }

    const wordCount = getWordCount(bitWidth);
    const words = await ReadWords(
      item.protocolType,
      item.memoryArea,
      item.address,
      wordCount,
    );
    if (!words || words.length < wordCount) {
      return { item, currentValue: "---", rawValues: [], isBit: false };
    }

    const currentValue =
      bitWidth === 16
        ? formatSingleWord(words[0], displayFormat)
        : formatBigInt(
            combineWords(words, endianness),
            displayFormat,
            bitWidth,
          );
    return { item, currentValue, rawValues: words, isBit: false };
  } catch {
    return { item, currentValue: "Error", rawValues: [], isBit };
  }
}

// モニタリング項目の一覧と現在値を持つ。autoRefresh が true の間は 100ms ごとに読み直す。
// setItems は、並べ替えの結果を先に画面へ反映するために公開している
export function useMonitoringItems(
  memoryAreasByProtocol: AreasByProtocol,
  autoRefresh: boolean,
) {
  const [items, setItems] = useState<MonitoringItemWithValue[]>([]);

  // 項目一覧を読み込み、各項目の現在値を取得する
  const loadItems = useCallback(async () => {
    try {
      const monitoringItems = await GetMonitoringItems();
      if (!monitoringItems || monitoringItems.length === 0) {
        setItems([]);
        return;
      }
      setItems(
        await Promise.all(
          monitoringItems.map((item) =>
            readItemValue(item, memoryAreasByProtocol),
          ),
        ),
      );
    } catch (e) {
      console.error("Failed to load monitoring items:", e);
    }
  }, [memoryAreasByProtocol]);

  // 初回読み込み
  useEffect(() => {
    loadItems();
  }, [loadItems]);

  // 自動更新
  useEffect(() => {
    if (autoRefresh) {
      const interval = setInterval(loadItems, 100);
      return () => clearInterval(interval);
    }
  }, [autoRefresh, loadItems]);

  return { items, setItems, loadItems };
}
