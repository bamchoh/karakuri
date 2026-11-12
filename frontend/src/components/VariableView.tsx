import React, { useState, useEffect, useCallback, useRef } from "react";
import {
  GetVariables,
  GetDataTypes,
  DeleteVariable,
  GetServerInstances,
  GetMemoryAreas,
  GetStructTypes,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { application } from "../../wailsjs/go/models";
import {
  AddVariableDialog,
  AddVariableDialogHandle,
} from "./AddVariableDialog";
import {
  BulkMappingDialog,
  BulkMappingDialogHandle,
} from "./BulkMappingDialog";
import { MappingDialog, MappingDialogHandle } from "./MappingDialog";
import { MetaEditDialog, MetaEditDialogHandle } from "./MetaEditDialog";
import { StructTypeDialog, StructTypeDialogHandle } from "./StructTypeDialog";
import { ValueEditDialog, ValueEditDialogHandle } from "./ValueEditDialog";
import {
  formatScalarValue,
  isArrayType,
  parseArrayTypeFE,
  isStructType,
  getWordCount as getWordCountOf,
  FlatRow,
} from "./variableTypes";

export function VariableView() {
  const [variables, setVariables] = useState<application.VariableDTO[]>([]);
  const [dataTypes, setDataTypes] = useState<application.DataTypeInfoDTO[]>([]);
  const [structTypes, setStructTypes] = useState<application.StructTypeDTO[]>(
    [],
  );
  const [isEditDialogOpen, setIsEditDialogOpen] = useState(false);

  // 展開/縮小状態（keyはFlatRowのヘッダーキー）
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());

  // キーボードナビゲーション用フォーカス状態
  const [focusedRowKey, setFocusedRowKey] = useState<string | null>(null);

  const addDialogRef = useRef<AddVariableDialogHandle>(null);
  const metaEditDialogRef = useRef<MetaEditDialogHandle>(null);
  const structTypeDialogRef = useRef<StructTypeDialogHandle>(null);
  const mappingDialogRef = useRef<MappingDialogHandle>(null);
  const bulkMappingDialogRef = useRef<BulkMappingDialogHandle>(null);
  const valueEditDialogRef = useRef<ValueEditDialogHandle>(null);
  // 行DOM要素のref map
  const rowRefs = useRef<Map<string, HTMLTableRowElement>>(new Map());

  // サーバー一覧とメモリエリア（マッピングの表示とダイアログで使う）
  const [serverInstances, setServerInstances] = useState<
    application.ServerInstanceDTO[]
  >([]);
  const [memoryAreasByProtocol, setMemoryAreasByProtocol] = useState<
    Record<string, application.MemoryAreaDTO[]>
  >({});

  // コンテキストメニュー
  const [contextMenu, setContextMenu] = useState<{
    x: number;
    y: number;
    variable: application.VariableDTO;
  } | null>(null);

  // 変数一覧を取得
  const loadVariables = useCallback(async () => {
    try {
      const vars = await GetVariables();
      if (vars) {
        setVariables(vars);
      }
    } catch (e) {
      console.error("Failed to load variables:", e);
    }
  }, []);

  // 構造体型一覧を取得
  const loadStructTypes = useCallback(async () => {
    try {
      const types = await GetStructTypes();
      setStructTypes(types || []);
    } catch (e) {
      console.error("Failed to load struct types:", e);
    }
  }, []);

  // サーバー一覧とメモリエリアを取得（取得したサーバー一覧を返す）
  const loadServerInstancesAndAreas = useCallback(async (): Promise<
    application.ServerInstanceDTO[]
  > => {
    try {
      const instances = await GetServerInstances();
      setServerInstances(instances || []);
      const areasMap: Record<string, application.MemoryAreaDTO[]> = {};
      for (const inst of instances || []) {
        try {
          const areas = await GetMemoryAreas(inst.protocolType);
          areasMap[inst.protocolType] = areas || [];
        } catch {
          areasMap[inst.protocolType] = [];
        }
      }
      setMemoryAreasByProtocol(areasMap);
      return instances || [];
    } catch (e) {
      console.error("Failed to load server instances:", e);
      return [];
    }
  }, []);

  // データ型一覧を取得
  useEffect(() => {
    const loadDataTypes = async () => {
      try {
        const types = await GetDataTypes();
        if (types && types.types) {
          setDataTypes(types.types);
        }
      } catch (e) {
        console.error("Failed to load data types:", e);
      }
    };
    loadDataTypes();
    loadStructTypes();
    loadServerInstancesAndAreas();
  }, [loadStructTypes, loadServerInstancesAndAreas]);

  // 変数一覧の初回読み込みとイベント購読
  useEffect(() => {
    loadVariables();
    const off = EventsOn(
      "plc:variables-changed",
      (variables: application.VariableDTO[]) => {
        setVariables(variables || []);
      },
    );
    return off;
  }, [loadVariables]);

  // キーボードナビゲーション: focusedRowKey が変わったらその行にフォーカスを移す
  useEffect(() => {
    if (focusedRowKey) {
      const el = rowRefs.current.get(focusedRowKey);
      if (el && document.activeElement !== el) {
        el.focus({ preventScroll: false });
        el.scrollIntoView({ block: "nearest" });
      }
    }
  }, [focusedRowKey]);

  // キーボードナビゲーション: 値編集ダイアログが閉じたら行にフォーカスを戻す
  useEffect(() => {
    if (!isEditDialogOpen && focusedRowKey) {
      requestAnimationFrame(() => {
        rowRefs.current.get(focusedRowKey)?.focus({ preventScroll: false });
      });
    }
    // isEditDialogOpen の変化にのみ反応（focusedRowKey は最新値を参照したいため除外）
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isEditDialogOpen]);

  // プロジェクトインポート完了イベントを購読して変数・構造体・サーバー情報をリロード
  useEffect(() => {
    const off = EventsOn("project:imported", () => {
      loadVariables();
      loadStructTypes();
      loadServerInstancesAndAreas();
    });

    return off;
  }, []);

  // データ型のワード数（読み込み済みのデータ型・構造体型から求める）
  const getWordCount = (dataType: string): number =>
    getWordCountOf(dataType, dataTypes, structTypes);

  // 変数をフラットな行に展開
  const flattenVariable = (v: application.VariableDTO): FlatRow[] => {
    const rows: FlatRow[] = [];

    const expand = (
      displayName: string,
      dataType: string,
      value: any,
      depth: number,
      valuePath: (string | number)[],
      wordOffset: number,
      // バックエンドに渡すフィールドパス（配列の添字は表示と同じ外部インデックス）
      extPath: string,
    ) => {
      // 配列型
      if (isArrayType(dataType)) {
        const arr = parseArrayTypeFE(dataType);
        const elemType = arr ? arr.elemType : "";
        const elemWordCount = getWordCount(elemType);
        // 親ヘッダー行
        rows.push({
          key: `${v.id}:${valuePath.join(".")}:header`,
          displayName,
          dataType,
          value,
          depth,
          variable: v,
          valuePath,
          fieldPath: extPath,
          isHeader: true,
          wordOffset,
        });
        // 各要素を行として展開
        if (Array.isArray(value)) {
          const lowerBound = arr ? arr.lower : 0;
          value.forEach((elem: any, i: number) => {
            const elemPath = [...valuePath, i];
            const elemName = `${displayName}[${lowerBound + i}]`;
            const elemExtPath = `${extPath}[${lowerBound + i}]`;
            const elemOffset = wordOffset + i * elemWordCount;
            if (isStructType(elemType) || isArrayType(elemType)) {
              expand(
                elemName,
                elemType,
                elem,
                depth + 1,
                elemPath,
                elemOffset,
                elemExtPath,
              );
            } else {
              rows.push({
                key: `${v.id}:${elemPath.join(".")}`,
                displayName: elemName,
                dataType: elemType,
                value: elem,
                depth: depth + 1,
                variable: v,
                valuePath: elemPath,
                fieldPath: elemExtPath,
                isHeader: false,
                wordOffset: elemOffset,
              });
            }
          });
        }
        return;
      }

      // 構造体型
      if (
        isStructType(dataType) &&
        typeof value === "object" &&
        value !== null &&
        !Array.isArray(value)
      ) {
        const st = structTypes.find((s) => s.name === dataType);
        // 親ヘッダー行
        rows.push({
          key: `${v.id}:${valuePath.join(".")}:header`,
          displayName,
          dataType,
          value,
          depth,
          variable: v,
          valuePath,
          fieldPath: extPath,
          isHeader: true,
          wordOffset,
        });
        if (st) {
          st.fields.forEach((field) => {
            const fieldPath = [...valuePath, field.name];
            const fieldName = `${displayName}.${field.name}`;
            const fieldExtPath = `${extPath}.${field.name}`;
            const fieldOffset = wordOffset + field.offset;
            if (isStructType(field.dataType) || isArrayType(field.dataType)) {
              expand(
                fieldName,
                field.dataType,
                value[field.name],
                depth + 1,
                fieldPath,
                fieldOffset,
                fieldExtPath,
              );
            } else {
              rows.push({
                key: `${v.id}:${fieldPath.join(".")}`,
                displayName: fieldName,
                dataType: field.dataType,
                value: value[field.name],
                depth: depth + 1,
                variable: v,
                valuePath: fieldPath,
                fieldPath: fieldExtPath,
                isHeader: false,
                wordOffset: fieldOffset,
              });
            }
          });
        }
        return;
      }

      // スカラー型
      rows.push({
        key: `${v.id}:${valuePath.join(".")}`,
        displayName,
        dataType,
        value,
        depth,
        variable: v,
        valuePath,
        fieldPath: extPath,
        isHeader: false,
        wordOffset,
      });
    };

    expand(v.name, v.dataType, v.value, 0, [], 0, "");
    return rows;
  };

  // すべての変数をフラット化
  const allFlatRows = variables.flatMap(flattenVariable);

  // 展開/縮小を考慮して表示する行をフィルタリング
  // 各行の親ヘッダーがすべて展開中であれば表示
  const flatRows = (() => {
    const visible: FlatRow[] = [];
    // 縮小中のヘッダーの depth を追跡（この depth 以下の行を非表示）
    let collapseDepth = -1; // -1 = 非表示なし
    let collapseVarId = "";

    for (const row of allFlatRows) {
      // 別の変数に移ったらリセット
      if (row.variable.id !== collapseVarId) {
        collapseDepth = -1;
      }

      // 縮小中のヘッダーより深い行は非表示
      if (
        collapseDepth >= 0 &&
        row.variable.id === collapseVarId &&
        row.depth > collapseDepth
      ) {
        continue;
      } else {
        // 抜けたのでリセット
        collapseDepth = -1;
      }

      visible.push(row);

      // ヘッダー行で縮小中なら、以降のこのヘッダーの深さより深い行を非表示にする
      if (row.isHeader && !expandedRows.has(row.key)) {
        collapseDepth = row.depth;
        collapseVarId = row.variable.id;
      }
    }
    return visible;
  })();

  // ヘッダー行の展開/縮小をトグル
  const toggleExpand = (headerKey: string) => {
    setExpandedRows((prev) => {
      const next = new Set(prev);
      if (next.has(headerKey)) {
        next.delete(headerKey);
      } else {
        next.add(headerKey);
      }
      return next;
    });
  };

  // キーボードナビゲーション: 行のキー押下ハンドラ
  const handleRowKeyDown = (
    e: React.KeyboardEvent<HTMLTableRowElement>,
    row: FlatRow,
    idx: number,
  ) => {
    const anyDialogOpen =
      isEditDialogOpen ||
      !!addDialogRef.current?.isOpen() ||
      !!mappingDialogRef.current?.isOpen() ||
      !!metaEditDialogRef.current?.isOpen() ||
      !!structTypeDialogRef.current?.isOpen() ||
      !!bulkMappingDialogRef.current?.isOpen();
    if (anyDialogOpen) return;

    switch (e.key) {
      case "ArrowDown": {
        e.preventDefault();
        if (idx < flatRows.length - 1) {
          setFocusedRowKey(flatRows[idx + 1].key);
        }
        break;
      }
      case "ArrowUp": {
        e.preventDefault();
        if (idx > 0) {
          setFocusedRowKey(flatRows[idx - 1].key);
        }
        break;
      }
      case "ArrowRight": {
        e.preventDefault();
        if (row.isHeader) {
          if (!expandedRows.has(row.key)) {
            // 折りたたみ中 → 展開（フォーカスは移動しない）
            toggleExpand(row.key);
          } else if (idx < flatRows.length - 1) {
            // 展開済み → 最初の子にフォーカス
            setFocusedRowKey(flatRows[idx + 1].key);
          }
        }
        break;
      }
      case "ArrowLeft": {
        e.preventDefault();
        if (row.isHeader && expandedRows.has(row.key)) {
          // 展開中のヘッダー → 折りたたむ
          toggleExpand(row.key);
        } else {
          // 親ヘッダーを探してフォーカス
          for (let i = idx - 1; i >= 0; i--) {
            const candidate = flatRows[i];
            if (candidate.variable.id !== row.variable.id) break;
            if (candidate.isHeader && candidate.depth < row.depth) {
              setFocusedRowKey(candidate.key);
              break;
            }
          }
        }
        break;
      }
      case "Enter": {
        // ボタンにフォーカスがある場合はネイティブのクリック動作に任せる
        if ((e.target as HTMLElement).tagName === "BUTTON") break;
        e.preventDefault();
        if (row.isHeader) {
          toggleExpand(row.key);
        } else {
          valueEditDialogRef.current?.openRow(row);
        }
        break;
      }
      case " ": {
        e.preventDefault();
        if (row.isHeader) {
          toggleExpand(row.key);
        }
        break;
      }
      case "Insert": {
        e.preventDefault();
        addDialogRef.current?.open();
        break;
      }
      case "F2": {
        e.preventDefault();
        metaEditDialogRef.current?.open(row.variable);
        break;
      }
      case "Delete": {
        e.preventDefault();
        handleDeleteVariable(row.variable.id, row.variable.name);
        break;
      }
      case "m":
      case "M": {
        e.preventDefault();
        mappingDialogRef.current?.open(row.variable);
        break;
      }
    }
  };

  // メモリエリアIDを短い表示名に変換（プラグインが shortName を返さなければ ID のまま）
  const areaShortName = (protocolType: string, areaId: string): string =>
    (memoryAreasByProtocol[protocolType] || []).find((a) => a.id === areaId)
      ?.shortName || areaId;

  const isOneOriginArea = (protocolType: string, areaId: string): boolean =>
    (memoryAreasByProtocol[protocolType] || []).find((a) => a.id === areaId)
      ?.oneOrigin ?? false;

  // 変数のマッピングが他の変数と重複しているか確認し、変数名一覧を返す（テーブル表示用）
  const getVariableMappingConflicts = (
    v: application.VariableDTO,
  ): string[] => {
    if (!v.mappings || v.mappings.length === 0) return [];
    const currWordCount = getWordCount(v.dataType);
    const conflictNames = new Set<string>();

    for (const m of v.mappings) {
      const currStart = m.address;
      const currEnd = currStart + currWordCount;
      for (const other of variables) {
        if (other.id === v.id || !other.mappings) continue;
        const otherWordCount = getWordCount(other.dataType);
        for (const vm of other.mappings) {
          if (
            vm.protocolType !== m.protocolType ||
            vm.memoryArea !== m.memoryArea
          )
            continue;
          if (currStart < vm.address + otherWordCount && currEnd > vm.address) {
            conflictNames.add(other.name);
            break;
          }
        }
      }
    }
    return [...conflictNames];
  };

  // マッピングのフォーマット
  const formatMappings = (
    mappings: application.ProtocolMappingDTO[] | undefined,
  ): string => {
    if (!mappings || mappings.length === 0) return "-";
    return mappings
      .map((m) => {
        const addr = isOneOriginArea(m.protocolType, m.memoryArea)
          ? m.address + 1
          : m.address;
        return `${m.protocolType}:${areaShortName(m.protocolType, m.memoryArea)}:${addr}`;
      })
      .join(", ");
  };

  // ノード公開設定のフォーマット（有効なものだけ、OPC UA 等）
  const formatNodePublishings = (
    publishings: application.NodePublishingDTO[] | undefined,
  ): string => {
    if (!publishings) return "";
    const enabled = publishings.filter((p) => p.enabled);
    if (enabled.length === 0) return "";
    return enabled
      .map((p) => {
        const accessLabel =
          p.accessMode === "read"
            ? "RO"
            : p.accessMode === "write"
              ? "WO"
              : "R/W";
        return `${p.protocolType}(${accessLabel})`;
      })
      .join(", ");
  };

  // オフセット付きマッピングのフォーマット（フィールド/要素用）
  const formatMappingsWithOffset = (
    mappings: application.ProtocolMappingDTO[] | undefined,
    offset: number,
  ): string => {
    if (!mappings || mappings.length === 0) return "-";
    return mappings
      .map((m) => {
        const addr = isOneOriginArea(m.protocolType, m.memoryArea)
          ? m.address + offset + 1
          : m.address + offset;
        return `${areaShortName(m.protocolType, m.memoryArea)}:${addr}`;
      })
      .join(", ");
  };

  // 変数を削除
  const handleDeleteVariable = async (id: string, name: string) => {
    if (!confirm(`変数 "${name}" を削除しますか?`)) return;
    try {
      await DeleteVariable(id);
      await loadVariables();
    } catch (e) {
      console.error("Failed to delete variable:", e);
      alert("変数の削除に失敗しました: " + e);
    }
  };

  // コンテキストメニューを開く
  const handleContextMenu = (
    e: React.MouseEvent,
    variable: application.VariableDTO,
  ) => {
    e.preventDefault();
    setContextMenu({ x: e.clientX, y: e.clientY, variable });
  };

  return (
    <div className="variable-view">
      <div className="variable-toolbar">
        <button
          onClick={() => addDialogRef.current?.open()}
          className="btn-primary"
        >
          変数を追加
        </button>
        <button
          onClick={() => structTypeDialogRef.current?.open()}
          className="btn-secondary"
        >
          構造体型管理
        </button>
        <button
          onClick={() => bulkMappingDialogRef.current?.open()}
          className="btn-secondary"
          disabled={serverInstances.length === 0}
          title={
            serverInstances.length === 0 ? "サーバーが追加されていません" : ""
          }
        >
          一括マッピング編集
        </button>
      </div>

      <table className="variable-table">
        <thead>
          <tr>
            <th>名前</th>
            <th>データ型</th>
            <th>値</th>
            <th>マッピング</th>
          </tr>
        </thead>
        <tbody>
          {flatRows.map((row, idx) => (
            <tr
              key={row.key}
              ref={(el) => {
                if (el) rowRefs.current.set(row.key, el);
                else rowRefs.current.delete(row.key);
              }}
              tabIndex={0}
              onFocus={() => setFocusedRowKey(row.key)}
              onKeyDown={(e) => handleRowKeyDown(e, row, idx)}
              onContextMenu={
                row.depth === 0
                  ? (e) => handleContextMenu(e, row.variable)
                  : undefined
              }
              style={{
                backgroundColor:
                  focusedRowKey === row.key
                    ? "rgba(100, 160, 255, 0.12)"
                    : row.isHeader
                      ? "rgba(255,255,255,0.03)"
                      : undefined,
                outline:
                  focusedRowKey === row.key
                    ? "1px solid rgba(100,160,255,0.4)"
                    : undefined,
                fontWeight:
                  row.depth === 0 && row.isHeader ? "bold" : undefined,
              }}
            >
              <td
                className="var-name"
                style={{
                  paddingLeft: `${8 + row.depth * 16}px`,
                  cursor: row.isHeader ? "pointer" : undefined,
                }}
                onClick={row.isHeader ? () => toggleExpand(row.key) : undefined}
              >
                {row.isHeader ? (
                  <span
                    style={{
                      display: "inline-flex",
                      alignItems: "center",
                      gap: "4px",
                    }}
                  >
                    <span
                      style={{
                        fontSize: "0.7em",
                        width: "12px",
                        display: "inline-block",
                      }}
                    >
                      {expandedRows.has(row.key) ? "\u25BC" : "\u25B6"}
                    </span>
                    {row.displayName}
                  </span>
                ) : (
                  <span style={{ paddingLeft: "16px" }}>{row.displayName}</span>
                )}
              </td>
              <td
                className="var-type"
                style={{ fontSize: row.isHeader ? undefined : "0.85em" }}
              >
                <span>{row.dataType}</span>
              </td>
              <td
                className="var-value"
                onClick={() => valueEditDialogRef.current?.openRow(row)}
                style={{ cursor: "pointer" }}
              >
                {row.isHeader ? (
                  <span style={{ color: "#888", fontSize: "0.85em" }}>
                    {isArrayType(row.dataType) && Array.isArray(row.value)
                      ? `(${row.value.length} 要素)`
                      : `{${row.dataType}}`}
                  </span>
                ) : (
                  <span>{formatScalarValue(row.value, row.dataType)}</span>
                )}
              </td>
              <td
                className="var-mapping"
                onClick={
                  row.depth === 0
                    ? () => mappingDialogRef.current?.open(row.variable)
                    : undefined
                }
                style={{ cursor: row.depth === 0 ? "pointer" : undefined }}
              >
                {row.depth === 0 ? (
                  <span>
                    {(() => {
                      const conflicts = getVariableMappingConflicts(
                        row.variable,
                      );
                      return conflicts.length > 0 ? (
                        <span
                          className="mapping-conflict-icon"
                          title={`以下の変数と重複: ${conflicts.join(", ")}`}
                        >
                          ⚠
                        </span>
                      ) : null;
                    })()}
                    {(() => {
                      const parts: string[] = [];
                      if (
                        row.variable.mappings &&
                        row.variable.mappings.length > 0
                      ) {
                        parts.push(formatMappings(row.variable.mappings));
                      }
                      const npStr = formatNodePublishings(
                        row.variable.nodePublishings,
                      );
                      if (npStr) parts.push(npStr);
                      return parts.length > 0 ? parts.join(", ") : "-";
                    })()}
                  </span>
                ) : (
                  <span style={{ fontSize: "0.85em", color: "#aaa" }}>
                    {formatMappingsWithOffset(
                      row.variable.mappings,
                      row.wordOffset,
                    )}
                  </span>
                )}
              </td>
            </tr>
          ))}
          {variables.length === 0 && (
            <tr>
              <td colSpan={4} className="empty-message">
                変数がありません。「変数を追加」ボタンで変数を作成してください。
              </td>
            </tr>
          )}
        </tbody>
      </table>

      {/* コンテキストメニュー */}
      {contextMenu && (
        <div
          className="context-menu"
          style={{ top: contextMenu.y, left: contextMenu.x }}
          onContextMenu={(e) => e.preventDefault()}
        >
          <button
            onClick={() => {
              metaEditDialogRef.current?.open(contextMenu.variable);
              setContextMenu(null);
            }}
          >
            編集
          </button>
          <button
            onClick={() => {
              mappingDialogRef.current?.open(contextMenu.variable);
              setContextMenu(null);
            }}
          >
            マッピング
          </button>
          <button
            className="context-menu-danger"
            onClick={() => {
              handleDeleteVariable(
                contextMenu.variable.id,
                contextMenu.variable.name,
              );
              setContextMenu(null);
            }}
          >
            削除
          </button>
        </div>
      )}
      {contextMenu && (
        <div
          className="context-menu-backdrop"
          onClick={() => setContextMenu(null)}
          onContextMenu={(e) => {
            e.preventDefault();
            setContextMenu(null);
          }}
        />
      )}

      <AddVariableDialog
        ref={addDialogRef}
        dataTypes={dataTypes}
        structTypes={structTypes}
        onChanged={loadVariables}
      />

      <MetaEditDialog
        ref={metaEditDialogRef}
        dataTypes={dataTypes}
        structTypes={structTypes}
        onChanged={loadVariables}
      />

      <ValueEditDialog
        ref={valueEditDialogRef}
        structTypes={structTypes}
        onChanged={loadVariables}
        onOpenChange={setIsEditDialogOpen}
      />

      <MappingDialog
        ref={mappingDialogRef}
        variables={variables}
        serverInstances={serverInstances}
        memoryAreasByProtocol={memoryAreasByProtocol}
        getWordCount={getWordCount}
        loadServerInstancesAndAreas={loadServerInstancesAndAreas}
        onChanged={loadVariables}
      />

      <BulkMappingDialog
        ref={bulkMappingDialogRef}
        variables={variables}
        serverInstances={serverInstances}
        memoryAreasByProtocol={memoryAreasByProtocol}
        getWordCount={getWordCount}
        loadServerInstancesAndAreas={loadServerInstancesAndAreas}
        onChanged={loadVariables}
      />
      <StructTypeDialog
        ref={structTypeDialogRef}
        dataTypes={dataTypes}
        structTypes={structTypes}
        onChanged={loadStructTypes}
      />
    </div>
  );
}
