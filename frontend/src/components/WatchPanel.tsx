import { Fragment, useCallback, useEffect, useState } from "react";
import { ChevronDown, ChevronRight, X } from "lucide-react";
import {
  AddMonitoringItem,
  DeleteMonitoringItem,
  GetClientPolling,
  GetClientSubscription,
  GetClients,
  GetMemoryAreas,
  GetMonitoringItems,
  GetMonitoringValues,
  GetServerInstances,
  GetVariables,
  UpdateVariableFieldValue,
  UpdateVariableValue,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { application } from "../../wailsjs/go/models";
import { AddMonitoringDialog } from "./AddMonitoringDialog";
import { RegisterWriteDialog } from "./RegisterWriteDialog";
import { VariableWriteDialog, WatchPickDialog } from "./WatchDialogs";
import { useEscapeKey } from "./useEscapeKey";
import { MonitoringItemWithValue, readItemValue } from "./useMonitoringItems";
import { formatAddress } from "../utils/address";
import { getWordCount } from "../utils/register";
import {
  WatchCandidate,
  isWritableValue,
  variableNameOf,
  variablePathLabel,
  visibleRows,
} from "../utils/watch";
import type { BitWidth, DisplayFormat, Endianness } from "../types/register";

// 値を読み直す間隔
const REFRESH_MS = 200;

type AreasByProtocol = Record<string, application.MemoryAreaDTO[]>;

// 1 行に出すもの
interface WatchRow {
  item: application.MonitoringItemDTO;
  source: string;
  label: string;
  text: string;
  // "ok" / "stale"（最後に届いた値）/ "error"
  status: string;
  error?: string;
  // 押すと書けるか
  writable: boolean;
  register?: MonitoringItemWithValue;
  value?: application.MonitoringValueDTO;
  // 中身の行（構造体のフィールド・配列の要素・接続先の値の 1 つずつ）。なければ空
  children: application.MonitoringValueRowDTO[];
}

// 畳んだ行（項目は項目の ID、中身の構造体・配列の行は「項目の ID:パス」）。欄を閉じても覚えておく
let rememberedCollapsed = new Set<string>();

// 中身を畳む・開くボタン
function CollapseButton({
  collapsed,
  onClick,
}: {
  collapsed: boolean;
  onClick: () => void;
}) {
  return (
    <button
      className="watch-collapse"
      onClick={onClick}
      title={collapsed ? "中身を開く" : "中身を畳む"}
      aria-label={collapsed ? "中身を開く" : "中身を畳む"}
      aria-expanded={!collapsed}
    >
      {collapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
    </button>
  );
}

// 接続先の値の候補のキー（種類・接続先・要約）
const clientKey = (kind: string, clientId: string, summary: string) =>
  JSON.stringify([kind, clientId, summary]);

// 下の帯の「モニタリング」: 登録したレジスタ・変数・接続先の値（サブスクリプションの監視項目・ポーリングの読み取り項目）を、
// どのページにいても見られる。値を押すと、レジスタと変数（数値・真偽・文字列）には書ける。
// 項目は、サーバーの「モニタリング」のタブと同じもの（そちらには、そのサーバーのレジスタだけが出る）
export function WatchPanel() {
  const [servers, setServers] = useState<application.ServerInstanceDTO[]>([]);
  const [areas, setAreas] = useState<AreasByProtocol>({});
  const [rows, setRows] = useState<WatchRow[]>([]);
  const [adding, setAdding] = useState<
    "register" | "variable" | "client" | null
  >(null);
  const [candidates, setCandidates] = useState<WatchCandidate[]>([]);
  const [writing, setWriting] = useState<WatchRow | null>(null);
  // 中身の行（構造体のフィールド・配列の要素）に書くときの、その行
  const [writingChild, setWritingChild] = useState<{
    row: WatchRow;
    child: application.MonitoringValueRowDTO;
  } | null>(null);
  const [collapsed, setCollapsed] = useState(rememberedCollapsed);
  const toggleCollapsed = (key: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      rememberedCollapsed = next;
      return next;
    });
  useEscapeKey(adding === "register", () => setAdding(null));

  // サーバーとエリア（レジスタの項目の名前と値に使う）。サーバーが増減したら読み直す
  const loadServers = useCallback(
    async (instances?: application.ServerInstanceDTO[]) => {
      try {
        const list = instances ?? (await GetServerInstances()) ?? [];
        const map: AreasByProtocol = {};
        for (const inst of list) {
          try {
            map[inst.protocolType] =
              (await GetMemoryAreas(inst.protocolType)) || [];
          } catch {
            map[inst.protocolType] = [];
          }
        }
        setServers(list);
        setAreas(map);
      } catch (e) {
        console.error("Failed to load servers:", e);
      }
    },
    [],
  );
  useEffect(() => {
    loadServers();
    return EventsOn(
      "plc:server-changed",
      (instances: application.ServerInstanceDTO[]) =>
        loadServers(instances || []),
    );
  }, [loadServers]);

  const load = useCallback(async () => {
    try {
      const items = (await GetMonitoringItems()) || [];
      const values = new Map(
        ((await GetMonitoringValues()) || []).map((v) => [v.id, v]),
      );
      const next = await Promise.all(
        items.map(async (item): Promise<WatchRow> => {
          if (item.kind) {
            const v = values.get(item.id);
            return {
              item,
              source: v?.source ?? "",
              label: v?.label ?? "",
              text: v?.text ?? "",
              status: v?.status ?? "error",
              error: v?.error,
              writable:
                item.kind === "variable" &&
                v?.status === "ok" &&
                isWritableValue(v.value),
              value: v,
              children: v?.rows || [],
            };
          }
          const server = servers.find(
            (s) => s.protocolType === item.protocolType,
          );
          const area = (areas[item.protocolType] || []).find(
            (a) => a.id === item.memoryArea,
          );
          if (!server || !area) {
            return {
              item,
              source: server?.displayName ?? item.protocolType,
              label: `${item.memoryArea} ${item.address}`,
              text: "",
              status: "error",
              error: server ? "エリアがありません" : "サーバーがありません",
              writable: false,
              children: [],
            };
          }
          const register = await readItemValue(item, areas);
          const failed = register.currentValue === "Error";
          return {
            item,
            source: server.displayName,
            label: `${area.displayName} ${formatAddress(area, item.address)}`,
            text: failed ? "" : register.currentValue,
            status: failed ? "error" : "ok",
            error: failed ? "読めません" : undefined,
            writable: !failed,
            register,
            children: [],
          };
        }),
      );
      setRows(next);
    } catch (e) {
      console.error("Failed to load monitoring values:", e);
    }
  }, [servers, areas]);

  useEffect(() => {
    load();
    const timer = setInterval(load, REFRESH_MS);
    return () => clearInterval(timer);
  }, [load]);

  const handleDelete = async (id: string) => {
    try {
      await DeleteMonitoringItem(id);
      await load();
    } catch (e) {
      console.error("Failed to delete monitoring item:", e);
    }
  };

  const newItem = (
    fields: Partial<application.MonitoringItemDTO>,
  ): application.MonitoringItemDTO =>
    ({
      id: "",
      order: 0,
      protocolType: "",
      memoryArea: "",
      address: 0,
      bitWidth: 0,
      endianness: "",
      displayFormat: "",
      ...fields,
    }) as application.MonitoringItemDTO;

  // 変数の候補（いちばん上の変数。フィールド・要素は、まるごとの値の中に出る）
  const openVariables = async () => {
    try {
      const vars = (await GetVariables()) || [];
      setCandidates(
        vars.map((v) => ({ key: v.id, label: v.name, detail: v.dataType })),
      );
      setAdding("variable");
    } catch (e) {
      console.error("Failed to load variables:", e);
    }
  };

  // 接続先の値の候補（サブスクリプションの監視項目と、ポーリングの読み取り項目）
  const openClientValues = async () => {
    try {
      const list: WatchCandidate[] = [];
      for (const c of (await GetClients()) || []) {
        try {
          const sub = await GetClientSubscription(c.id);
          for (const it of sub?.items || []) {
            list.push({
              key: clientKey("subscription", c.id, it.summary),
              label: it.summary,
              detail: `${c.name}（サブスクリプション）`,
            });
          }
        } catch {
          // サブスクリプションに対応していない接続先
        }
        try {
          const poll = await GetClientPolling(c.id);
          for (const it of poll?.items || []) {
            list.push({
              key: clientKey("poll", c.id, it.summary),
              label: it.summary,
              detail: `${c.name}（ポーリング）`,
            });
          }
        } catch {
          // 読めなければ、その接続先の分は出さない
        }
      }
      // 同じ要約の項目が 2 つあっても、候補は 1 つ（要約で指すため）
      const seen = new Set<string>();
      setCandidates(
        list.filter((c) => (seen.has(c.key) ? false : seen.add(c.key))),
      );
      setAdding("client");
    } catch (e) {
      console.error("Failed to load client values:", e);
    }
  };

  const addPicked = async (keys: string[]) => {
    try {
      for (const key of keys) {
        if (adding === "variable") {
          await AddMonitoringItem(
            newItem({ kind: "variable", variableId: key }),
          );
        } else {
          const [kind, clientId, summary] = JSON.parse(key) as string[];
          await AddMonitoringItem(newItem({ kind, clientId, summary }));
        }
      }
      setAdding(null);
      await load();
    } catch (e) {
      console.error("Failed to add monitoring items:", e);
    }
  };

  const addRegisters = async (
    protocolType: string,
    memoryArea: string,
    address: number,
    count: number,
    bitWidth: BitWidth,
    endianness: Endianness,
    displayFormat: DisplayFormat,
  ) => {
    try {
      const area = (areas[protocolType] || []).find((a) => a.id === memoryArea);
      const step = area?.isBit ? 1 : getWordCount(bitWidth);
      for (let i = 0; i < count; i++) {
        await AddMonitoringItem(
          newItem({
            protocolType,
            memoryArea,
            address: address + i * step,
            bitWidth,
            endianness,
            displayFormat,
          }),
        );
      }
      setAdding(null);
      await load();
    } catch (e) {
      console.error("Failed to add monitoring items:", e);
    }
  };

  const writeVariable = async (row: WatchRow, value: unknown) => {
    if (row.item.fieldPath) {
      await UpdateVariableFieldValue(
        row.item.variableId!,
        row.item.fieldPath,
        value,
      );
    } else {
      await UpdateVariableValue(row.item.variableId!, value);
    }
    setWriting(null);
    await load();
  };

  // 中身の行（構造体のフィールド・配列の要素）に書く。パスは、変数の先頭から
  const writeVariableField = async (
    row: WatchRow,
    child: application.MonitoringValueRowDTO,
    value: unknown,
  ) => {
    await UpdateVariableFieldValue(row.item.variableId!, child.path!, value);
    setWritingChild(null);
    await load();
  };

  // 中身の行に書けるか（変数の、数値・真偽・文字列の行）
  const childWritable = (
    row: WatchRow,
    child: application.MonitoringValueRowDTO,
  ) =>
    row.item.kind === "variable" &&
    row.status === "ok" &&
    !child.composite &&
    !child.more &&
    !!child.path &&
    isWritableValue(child.value);

  const writingArea = writing?.register
    ? (areas[writing.item.protocolType] || []).find(
        (a) => a.id === writing.item.memoryArea,
      )
    : undefined;

  return (
    <div className="watch-panel">
      <div className="watch-toolbar">
        <span className="watch-toolbar-label">追加:</span>
        <button
          className="btn-small"
          onClick={() => setAdding("register")}
          disabled={servers.length === 0}
          title={
            servers.length === 0
              ? "サーバーがありません"
              : "サーバーのレジスタを足す"
          }
        >
          レジスタ
        </button>
        <button className="btn-small" onClick={openVariables}>
          変数
        </button>
        <button
          className="btn-small"
          onClick={openClientValues}
          title="接続先のサブスクリプションの監視項目・ポーリングの読み取り項目を足す"
        >
          接続先の値
        </button>
      </div>

      {rows.length === 0 ? (
        <div className="watch-empty">
          モニタリングの項目がありません。上のボタンで、見たいレジスタ・変数・接続先の値を足してください。
        </div>
      ) : (
        <table className="watch-table">
          <tbody>
            {rows.map((row) => {
              const hasChildren = row.children.length > 0;
              const itemCollapsed = collapsed.has(row.item.id);
              const childKey = (c: application.MonitoringValueRowDTO) =>
                `${row.item.id}:${c.path ?? ""}`;
              const shownChildren = itemCollapsed
                ? []
                : visibleRows(row.children, (c) => collapsed.has(childKey(c)));
              return (
                <Fragment key={row.item.id}>
                  <tr
                    className={`watch-row watch-${row.status}`}
                    data-testid="watch-row"
                  >
                    <td className="watch-source">{row.source}</td>
                    <td className="watch-label">
                      {hasChildren && (
                        <CollapseButton
                          collapsed={itemCollapsed}
                          onClick={() => toggleCollapsed(row.item.id)}
                        />
                      )}
                      {row.label}
                    </td>
                    <td
                      className={`watch-value${row.writable ? " writable" : ""}`}
                      onClick={row.writable ? () => setWriting(row) : undefined}
                      title={row.writable ? "押すと、値を書ける" : undefined}
                    >
                      {hasChildren ? "" : row.text || "---"}
                    </td>
                    <td className="watch-note">{row.error}</td>
                    <td className="watch-actions">
                      <button
                        className="watch-delete"
                        onClick={() => handleDelete(row.item.id)}
                        title="モニタリングから外す"
                        aria-label="モニタリングから外す"
                      >
                        <X size={14} />
                      </button>
                    </td>
                  </tr>
                  {/* 中身（構造体のフィールド・配列の要素・接続先の値の 1 つずつ）は、字下げして 1 行ずつ */}
                  {shownChildren.map((child, i) => {
                    const writable = childWritable(row, child);
                    return (
                      <tr
                        key={`${childKey(child)}:${i}`}
                        className={`watch-row watch-child watch-${row.status}`}
                        data-testid="watch-child"
                      >
                        <td className="watch-source" />
                        <td
                          className={`watch-label${child.more ? " watch-more" : ""}`}
                          // 字下げは、行の深さで決まる
                          style={{ paddingLeft: `${child.depth * 1.1}rem` }}
                        >
                          {child.composite && (
                            <CollapseButton
                              collapsed={collapsed.has(childKey(child))}
                              onClick={() => toggleCollapsed(childKey(child))}
                            />
                          )}
                          {child.label}
                        </td>
                        <td
                          className={`watch-value${writable ? " writable" : ""}`}
                          onClick={
                            writable
                              ? () => setWritingChild({ row, child })
                              : undefined
                          }
                          title={writable ? "押すと、値を書ける" : undefined}
                        >
                          {child.composite || child.more ? "" : child.text}
                        </td>
                        <td className="watch-note" />
                        <td className="watch-actions" />
                      </tr>
                    );
                  })}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      )}

      {adding === "register" && (
        <AddMonitoringDialog
          onSave={addRegisters}
          onClose={() => setAdding(null)}
          serverInstances={servers}
          memoryAreasByProtocol={areas}
        />
      )}
      {adding === "variable" && (
        <WatchPickDialog
          title="変数をモニタリングに追加"
          emptyText="変数がありません。変数のページで作ってください。"
          candidates={candidates}
          onAdd={addPicked}
          onClose={() => setAdding(null)}
        />
      )}
      {adding === "client" && (
        <WatchPickDialog
          title="接続先の値をモニタリングに追加"
          emptyText="接続先に、サブスクリプションの監視項目もポーリングの読み取り項目もありません。通信画面の接続先で足してください。"
          candidates={candidates}
          onAdd={addPicked}
          onClose={() => setAdding(null)}
        />
      )}

      {writing?.register && (
        <RegisterWriteDialog
          title="レジスタ書き込み"
          protocolType={writing.item.protocolType}
          area={writing.item.memoryArea}
          address={writing.item.address}
          displayAddress={formatAddress(writingArea, writing.item.address)}
          currentValueText={writing.register.currentValue}
          isBit={writingArea?.isBit ?? false}
          bitInput="text"
          bitWidth={(writing.item.bitWidth as BitWidth) || 16}
          endianness={(writing.item.endianness as Endianness) || "big"}
          initialFormat={
            (writing.item.displayFormat as DisplayFormat) || "decimal"
          }
          initialValue={writing.register.currentValue}
          onClose={() => setWriting(null)}
          onWritten={async () => {
            setWriting(null);
            await load();
          }}
        />
      )}
      {writingChild && (
        <VariableWriteDialog
          label={variablePathLabel(
            variableNameOf(
              writingChild.row.label,
              writingChild.row.item.fieldPath ?? "",
            ),
            writingChild.child.path ?? "",
          )}
          current={writingChild.child.value}
          currentText={writingChild.child.text}
          onWrite={(value) =>
            writeVariableField(writingChild.row, writingChild.child, value)
          }
          onClose={() => setWritingChild(null)}
        />
      )}
      {writing && !writing.register && (
        <VariableWriteDialog
          label={writing.label}
          current={writing.value?.value}
          currentText={writing.text}
          onWrite={(value) => writeVariable(writing, value)}
          onClose={() => setWriting(null)}
        />
      )}
    </div>
  );
}
