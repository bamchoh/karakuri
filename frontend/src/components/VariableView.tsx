import React, { useState, useEffect, useCallback, useRef } from "react";
import { FocusTrap } from "./FocusTrap";
import {
  GetVariables,
  GetDataTypes,
  CreateVariable,
  UpdateVariable,
  UpdateVariableValue,
  UpdateVariableFieldValue,
  DeleteVariable,
  UpdateVariableMappings,
  UpdateVariableNodePublishing,
  GetServerInstances,
  GetMemoryAreas,
  GetNodeAddresses,
  GetStructTypes,
  RegisterStructType,
  DeleteStructType,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { application } from "../../wailsjs/go/models";
import {
  AddVariableDialog,
  AddVariableDialogHandle,
} from "./AddVariableDialog";
import { MetaEditDialog, MetaEditDialogHandle } from "./MetaEditDialog";
import { StructArrayElementEditor } from "./StructArrayElementEditor";
import { StructTypeDialog, StructTypeDialogHandle } from "./StructTypeDialog";
import {
  formatScalarValue,
  isArrayType,
  parseArrayTypeFE,
  parseAllDimsFE,
  buildArrayTypeFE,
  isStructType,
  getDefaultValue,
  parseValue,
  coerceFloatIfNeeded,
  valuePathToFieldPath,
  parseNumericInput,
  parseBigIntInput,
  formatNumeric,
  resolveBaseElemType,
  getWordCount as getWordCountOf,
  generateStructDefault as generateStructDefaultOf,
  generateDefaultForType as generateDefaultForTypeOf,
} from "./variableTypes";

export function VariableView() {
  const [variables, setVariables] = useState<application.VariableDTO[]>([]);
  const [dataTypes, setDataTypes] = useState<application.DataTypeInfoDTO[]>([]);
  const [structTypes, setStructTypes] = useState<application.StructTypeDTO[]>(
    [],
  );
  const [isEditDialogOpen, setIsEditDialogOpen] = useState(false);
  const [isMappingDialogOpen, setIsMappingDialogOpen] = useState(false);
  const [editingVariable, setEditingVariable] =
    useState<application.VariableDTO | null>(null);

  // 展開/縮小状態（keyはFlatRowのヘッダーキー）
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());

  // キーボードナビゲーション用フォーカス状態
  const [focusedRowKey, setFocusedRowKey] = useState<string | null>(null);

  // 編集フォーム
  const [editData, setEditData] = useState<any>(null);
  const [editValue, setEditValue] = useState("");
  const addDialogRef = useRef<AddVariableDialogHandle>(null);
  const metaEditDialogRef = useRef<MetaEditDialogHandle>(null);
  const structTypeDialogRef = useRef<StructTypeDialogHandle>(null);
  const editDialogRef = useRef<HTMLDivElement>(null);
  // 行DOM要素のref map
  const rowRefs = useRef<Map<string, HTMLTableRowElement>>(new Map());

  // マッピング編集
  const [mappingVariable, setMappingVariable] =
    useState<application.VariableDTO | null>(null);
  const [editMappings, setEditMappings] = useState<
    application.ProtocolMappingDTO[]
  >([]);
  const [editNodePublishings, setEditNodePublishings] = useState<
    application.NodePublishingDTO[]
  >([]);
  const [serverInstances, setServerInstances] = useState<
    application.ServerInstanceDTO[]
  >([]);
  const [memoryAreasByProtocol, setMemoryAreasByProtocol] = useState<
    Record<string, application.MemoryAreaDTO[]>
  >({});

  // 一括マッピング編集ダイアログ
  const [isBulkMappingOpen, setIsBulkMappingOpen] = useState(false);
  const [bulkProtocol, setBulkProtocol] = useState<string>("");
  // プロトコル -> 変数 ID -> ノードアドレス（OPC UA の NodeID 等）。
  // 書式と、公開できない変数の判定（error）はプラグインが決める
  const [nodeAddresses, setNodeAddresses] = useState<
    Record<string, Record<string, application.NodeAddressDTO>>
  >({});
  const [bulkRows, setBulkRows] = useState<BulkEditRow[]>([]);
  const [bulkIsSaving, setBulkIsSaving] = useState(false);

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

  // ノード公開型プロトコルのサーバーから、変数のノードアドレスを取得する
  const loadNodeAddresses = useCallback(
    async (protocolType: string, variableIds: string[]) => {
      if (!protocolType || variableIds.length === 0) return;
      try {
        const addrs = await GetNodeAddresses(protocolType, variableIds);
        setNodeAddresses((prev) => {
          const byId = { ...(prev[protocolType] || {}) };
          for (const a of addrs || []) byId[a.variableId] = a;
          return { ...prev, [protocolType]: byId };
        });
      } catch (e) {
        console.error("Failed to load node addresses:", e);
      }
    },
    [],
  );

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

  // ESCキーでダイアログを閉じる
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      if (isBulkMappingOpen) {
        setIsBulkMappingOpen(false);
      } else if (isMappingDialogOpen) {
        setIsMappingDialogOpen(false);
      } else if (isEditDialogOpen) {
        setIsEditDialogOpen(false);
        setEditData(null);
        setEditingRow(null);
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [
    isEditDialogOpen,
    isMappingDialogOpen,
    isBulkMappingOpen,
  ]);

  // 値編集ダイアログが開いたら最初の入力にフォーカス
  useEffect(() => {
    if (isEditDialogOpen && editDialogRef.current) {
      const first =
        editDialogRef.current.querySelector<HTMLElement>("input, select");
      first?.focus();
    }
  }, [isEditDialogOpen]);

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

  // 型の補助関数に、読み込み済みのデータ型・構造体型を渡す
  const getWordCount = (dataType: string): number =>
    getWordCountOf(dataType, dataTypes, structTypes);
  const generateStructDefault = (typeName: string): any =>
    generateStructDefaultOf(typeName, structTypes);
  const generateDefaultForType = (dataType: string): any =>
    generateDefaultForTypeOf(dataType, structTypes);

  // フラット化された行の型定義
  interface FlatRow {
    key: string;
    displayName: string;
    dataType: string;
    value: any;
    depth: number;
    // 親変数への参照（編集・削除・マッピング用）
    variable: application.VariableDTO;
    // 値のパスを表すキー配列（例: ["fieldName"] や [0, "fieldName"]）
    valuePath: (string | number)[];
    // この行がヘッダー行（構造体/配列の親）かどうか
    isHeader: boolean;
    // 変数のベースアドレスからのワードオフセット
    wordOffset: number;
  }

  // 一括マッピング編集用の行データ
  interface BulkEditRow {
    variableId: string;
    variableName: string;
    dataType: string;
    wordCount: number;
    // レジスタ型プロトコル（メモリエリアを持つ）
    memoryArea: string;
    addressStr: string; // 空文字 = マッピングなし（削除）
    endianness: string;
    // ノード公開型プロトコル（SupportsNodePublishing=true）
    nodeEnabled: boolean;
    accessMode: string;
  }

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
          isHeader: true,
          wordOffset,
        });
        // 各要素を行として展開
        if (Array.isArray(value)) {
          const lowerBound = arr ? arr.lower : 0;
          value.forEach((elem: any, i: number) => {
            const elemPath = [...valuePath, i];
            const elemName = `${displayName}[${lowerBound + i}]`;
            const elemOffset = wordOffset + i * elemWordCount;
            if (isStructType(elemType) || isArrayType(elemType)) {
              expand(elemName, elemType, elem, depth + 1, elemPath, elemOffset);
            } else {
              rows.push({
                key: `${v.id}:${elemPath.join(".")}`,
                displayName: elemName,
                dataType: elemType,
                value: elem,
                depth: depth + 1,
                variable: v,
                valuePath: elemPath,
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
          isHeader: true,
          wordOffset,
        });
        if (st) {
          st.fields.forEach((field) => {
            const fieldPath = [...valuePath, field.name];
            const fieldName = `${displayName}.${field.name}`;
            const fieldOffset = wordOffset + field.offset;
            if (isStructType(field.dataType) || isArrayType(field.dataType)) {
              expand(
                fieldName,
                field.dataType,
                value[field.name],
                depth + 1,
                fieldPath,
                fieldOffset,
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
        isHeader: false,
        wordOffset,
      });
    };

    expand(v.name, v.dataType, v.value, 0, [], 0);
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
      isMappingDialogOpen ||
      !!metaEditDialogRef.current?.isOpen() ||
      !!structTypeDialogRef.current?.isOpen() ||
      isBulkMappingOpen;
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
          handleRowEditClick(row);
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
        handleMappingClick(row.variable);
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

  // 指定プロトコルが SupportsNodePublishing かどうか
  const isNodePublishingProtocol = (protocolType: string): boolean =>
    serverInstances.find((i) => i.protocolType === protocolType)
      ?.supportsNodePublishing ?? false;

  // 指定したマッピングが他の変数のマッピングと重複しているか確認し、変数名一覧を返す
  const findMappingConflicts = (
    mapping: application.ProtocolMappingDTO,
  ): string[] => {
    if (!mappingVariable) return [];
    const currStart = mapping.address;
    const currEnd = currStart + getWordCount(mappingVariable.dataType);
    const conflicts: string[] = [];

    for (const v of variables) {
      if (v.id === mappingVariable.id || !v.mappings) continue;
      const otherWordCount = getWordCount(v.dataType);
      for (const vm of v.mappings) {
        if (
          vm.protocolType !== mapping.protocolType ||
          vm.memoryArea !== mapping.memoryArea
        )
          continue;
        const otherStart = vm.address;
        const otherEnd = otherStart + otherWordCount;
        // アドレス範囲の重複チェック
        if (currStart < otherEnd && currEnd > otherStart) {
          conflicts.push(v.name);
          break;
        }
      }
    }
    return conflicts;
  };

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

  // 編集用の行情報
  const [editingRow, setEditingRow] = useState<FlatRow | null>(null);

  // 編集ダイアログを開く（行単位）
  const handleEditClick = (v: application.VariableDTO) => {
    setEditingVariable(v);
    // 構造体・配列はディープコピーして編集用データを作る
    if (isArrayType(v.dataType) || isStructType(v.dataType)) {
      setEditData(JSON.parse(JSON.stringify(v.value ?? null)));
    } else {
      setEditData(v.value);
    }
    setEditingRow(null);

    setIsEditDialogOpen(true);
  };

  // フラット行から個別の値を編集
  const handleRowEditClick = (row: FlatRow) => {
    if (row.isHeader) {
      // ヘッダー行の場合は変数全体の編集
      handleEditClick(row.variable);
      return;
    }
    setEditingVariable(row.variable);
    setEditingRow(row);
    setEditData(
      row.value != null ? JSON.parse(JSON.stringify(row.value)) : row.value,
    );

    setIsEditDialogOpen(true);
  };

  // 行単位の値更新
  const handleUpdateRow = async () => {
    if (!editingVariable) return;
    try {
      if (editingRow && editingRow.valuePath.length > 0) {
        // フィールドパスを使って部分更新
        const fieldPath = valuePathToFieldPath(editingRow.valuePath);
        await UpdateVariableFieldValue(
          editingVariable.id,
          fieldPath,
          coerceFloatIfNeeded(editData, editingRow.dataType),
        );
      } else {
        const dataType = editingVariable.dataType;
        await UpdateVariableValue(
          editingVariable.id,
          coerceFloatIfNeeded(editData, dataType),
        );
      }
      await loadVariables();
      setIsEditDialogOpen(false);
      setEditingVariable(null);
      setEditingRow(null);
      setEditData(null);
    } catch (e) {
      console.error("Failed to update variable:", e);
      alert("変数の更新に失敗しました: " + e);
    }
  };

  // スカラー値エディタ
  const renderScalarEditor = (
    dataType: string,
    value: any,
    onChange: (v: any) => void,
  ) => {
    if (dataType === "BOOL") {
      return (
        <select
          value={value ? "true" : "false"}
          onChange={(e) => onChange(e.target.value === "true")}
          style={{ flex: 1 }}
        >
          <option value="true">TRUE</option>
          <option value="false">FALSE</option>
        </select>
      );
    }
    if (dataType === "STRING" || dataType.startsWith("STRING[")) {
      const maxLen = dataType.startsWith("STRING[")
        ? parseInt(dataType.match(/\[(\d+)\]/)?.[1] || "0")
        : 0;
      return (
        <input
          type="text"
          value={value ?? ""}
          onChange={(e) =>
            onChange(
              maxLen > 0 ? e.target.value.slice(0, maxLen) : e.target.value,
            )
          }
          onFocus={(e) => e.target.select()}
          style={{ flex: 1 }}
          maxLength={maxLen > 0 ? maxLen : undefined}
          placeholder={maxLen > 0 ? `最大${maxLen}バイト` : undefined}
        />
      );
    }
    // 64ビット整数型（LINT/ULINT）- 文字列で精度を保持
    if (dataType === "LINT" || dataType === "ULINT") {
      const placeholder =
        dataType === "LINT"
          ? "-9223372036854775808〜9223372036854775807 (0x/0b対応)"
          : "0〜18446744073709551615 (0x/0b対応)";
      return (
        <input
          type="text"
          value={String(value ?? "0")}
          onChange={(e) => {
            // 入力中は文字列のまま保持（確定時に正規化）
            onChange(e.target.value);
          }}
          onFocus={(e) => e.target.select()}
          onBlur={(e) => {
            const normalized = parseBigIntInput(e.target.value, dataType);
            if (normalized !== null) {
              onChange(normalized);
            }
          }}
          style={{ flex: 1 }}
          placeholder={placeholder}
        />
      );
    }
    // 浮動小数点型（REAL/LREAL）- 入力中は文字列のまま保持してドット入力を可能にする
    if (dataType === "REAL" || dataType === "LREAL") {
      return (
        <input
          type="text"
          value={String(value ?? "0")}
          onChange={(e) => {
            // 入力中は文字列のまま保持（"1." のようなドット末尾も維持）
            onChange(e.target.value);
          }}
          onFocus={(e) => e.target.select()}
          onBlur={(e) => {
            const parsed = parseFloat(e.target.value);
            onChange(isNaN(parsed) ? 0 : parsed);
          }}
          style={{ flex: 1 }}
          placeholder="例: 3.14"
        />
      );
    }
    // 時間・日付型（文字列として編集）
    if (
      dataType === "TIME" ||
      dataType === "DATE" ||
      dataType === "TIME_OF_DAY" ||
      dataType === "DATE_AND_TIME"
    ) {
      const placeholders: { [key: string]: string } = {
        TIME: "T#1s, T#100ms, T#1h30m",
        DATE: "D#2024-01-01",
        TIME_OF_DAY: "TOD#12:30:15",
        DATE_AND_TIME: "DT#2024-01-01-12:30:15",
      };
      return (
        <input
          type="text"
          value={value ?? ""}
          onChange={(e) => onChange(e.target.value)}
          onFocus={(e) => e.target.select()}
          style={{ flex: 1 }}
          placeholder={placeholders[dataType] || ""}
        />
      );
    }
    // 数値型
    return (
      <input
        type="text"
        value={value ?? 0}
        onChange={(e) => {
          const parsed = parseNumericInput(e.target.value);
          if (!isNaN(parsed)) {
            onChange(parsed);
          }
        }}
        onFocus={(e) => e.target.select()}
        onBlur={(e) => {
          const parsed = parseNumericInput(e.target.value);
          if (!isNaN(parsed)) {
            onChange(parsed);
          }
        }}
        style={{ flex: 1 }}
        placeholder="10進, 0x(16進), 0b(2進)"
      />
    );
  };

  // 再帰的値エディタ
  const renderValueEditor = (
    dataType: string,
    value: any,
    onChange: (v: any) => void,
    depth: number = 0,
  ): JSX.Element => {
    const indent = depth * 16;

    // 配列型
    if (isArrayType(dataType)) {
      const arr = parseArrayTypeFE(dataType);
      if (!arr || !Array.isArray(value)) {
        return <span>-</span>;
      }
      const elemType = arr.elemType;
      const elemIsStruct = isStructType(elemType) || isArrayType(elemType);

      return (
        <div style={{ marginLeft: indent }}>
          {value.map((elem: any, i: number) => (
            <div
              key={i}
              style={{
                display: "flex",
                gap: "4px",
                alignItems: "center",
                marginBottom: "2px",
              }}
            >
              <span
                style={{ fontSize: "0.8em", color: "#888", minWidth: "30px" }}
              >
                [{i}]
              </span>
              {elemIsStruct ? (
                <StructArrayElementEditor
                  elemType={elemType}
                  elem={elem}
                  onChange={(newElem) => {
                    const newArr = [...value];
                    newArr[i] = newElem;
                    onChange(newArr);
                  }}
                  renderEditor={renderValueEditor}
                />
              ) : (
                renderScalarEditor(elemType, elem, (newVal) => {
                  const newArr = [...value];
                  newArr[i] = newVal;
                  onChange(newArr);
                })
              )}
            </div>
          ))}
        </div>
      );
    }

    // 構造体型
    if (isStructType(dataType)) {
      const st = structTypes.find((s) => s.name === dataType);
      if (!st || typeof value !== "object" || value === null) {
        return <span>-</span>;
      }
      return (
        <div style={{ marginLeft: indent }}>
          {st.fields.map((field) => (
            <div key={field.name} style={{ marginBottom: "4px" }}>
              <div
                style={{ display: "flex", gap: "4px", alignItems: "center" }}
              >
                <span
                  style={{
                    fontSize: "0.85em",
                    fontWeight: "bold",
                    minWidth: "80px",
                  }}
                >
                  {field.name}
                </span>
                <span style={{ fontSize: "0.75em", color: "#888" }}>
                  ({field.dataType})
                </span>
              </div>
              <div style={{ marginLeft: "8px" }}>
                {renderValueEditor(
                  field.dataType,
                  value[field.name],
                  (newVal) => {
                    onChange({ ...value, [field.name]: newVal });
                  },
                  depth + 1,
                )}
              </div>
            </div>
          ))}
        </div>
      );
    }

    // スカラー型
    return renderScalarEditor(dataType, value, onChange);
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

  // マッピングダイアログを開く
  const handleMappingClick = async (v: application.VariableDTO) => {
    const instances = await loadServerInstancesAndAreas();
    for (const inst of instances) {
      if (inst.supportsNodePublishing) {
        loadNodeAddresses(inst.protocolType, [v.id]);
      }
    }
    setMappingVariable(v);
    setEditMappings(v.mappings ? [...v.mappings] : []);
    setEditNodePublishings(v.nodePublishings ? [...v.nodePublishings] : []);
    setIsMappingDialogOpen(true);
  };

  // 変数一覧とプロトコルから BulkEditRow を構築
  const buildBulkRows = (
    vars: application.VariableDTO[],
    protocolType: string,
  ): BulkEditRow[] => {
    const isNP = isNodePublishingProtocol(protocolType);
    const defaultArea =
      (memoryAreasByProtocol[protocolType] || [])[0]?.id ?? "";
    return vars.map((v) => {
      const wc = getWordCount(v.dataType);
      if (isNP) {
        const existing = (v.nodePublishings || []).find(
          (np) => np.protocolType === protocolType,
        );
        return {
          variableId: v.id,
          variableName: v.name,
          dataType: v.dataType,
          wordCount: wc,
          memoryArea: "",
          addressStr: "",
          endianness: "big",
          nodeEnabled: existing?.enabled ?? false,
          accessMode: existing?.accessMode ?? "readwrite",
        } as BulkEditRow;
      } else {
        const existing = (v.mappings || []).find(
          (m) => m.protocolType === protocolType,
        );
        const oneOrigin = isOneOriginArea(
          protocolType,
          existing?.memoryArea ?? defaultArea,
        );
        const displayAddr =
          existing !== undefined
            ? oneOrigin
              ? existing.address + 1
              : existing.address
            : "";
        return {
          variableId: v.id,
          variableName: v.name,
          dataType: v.dataType,
          wordCount: wc,
          memoryArea: existing?.memoryArea ?? defaultArea,
          addressStr: existing !== undefined ? String(displayAddr) : "",
          endianness: existing?.endianness ?? "big",
          nodeEnabled: false,
          accessMode: "readwrite",
        } as BulkEditRow;
      }
    });
  };

  // 一括マッピング編集ダイアログを開く
  const handleBulkMappingOpen = async () => {
    const instances = await loadServerInstancesAndAreas();
    if (variables.length === 0) return;
    const first = instances[0];
    const firstProtocol = first?.protocolType ?? "";
    setBulkProtocol(firstProtocol);
    setBulkRows(buildBulkRows(variables, firstProtocol));
    if (first?.supportsNodePublishing) {
      loadNodeAddresses(firstProtocol, variables.map((v) => v.id));
    }
    setIsBulkMappingOpen(true);
  };

  // 一括編集ダイアログ内のプロトコル変更
  const handleBulkProtocolChange = (newProtocol: string) => {
    setBulkProtocol(newProtocol);
    setBulkRows(buildBulkRows(variables, newProtocol));
    if (isNodePublishingProtocol(newProtocol)) {
      loadNodeAddresses(newProtocol, variables.map((v) => v.id));
    }
  };

  // 一括編集テーブルのセル更新
  const handleBulkRowChange = (
    variableId: string,
    patch: Partial<BulkEditRow>,
  ) => {
    setBulkRows((prev) =>
      prev.map((row) =>
        row.variableId === variableId ? { ...row, ...patch } : row,
      ),
    );
  };

  // エリア変更（1オリジン変換を維持しながらアドレス文字列を再計算）
  const handleBulkAreaChange = (variableId: string, newArea: string) => {
    setBulkRows((prev) =>
      prev.map((row) => {
        if (row.variableId !== variableId) return row;
        const oldOneOrigin = isOneOriginArea(bulkProtocol, row.memoryArea);
        const newOneOrigin = isOneOriginArea(bulkProtocol, newArea);
        let newAddrStr = row.addressStr;
        if (row.addressStr !== "") {
          const parsed = parseInt(row.addressStr, 10);
          if (!isNaN(parsed)) {
            const addr0based = oldOneOrigin ? parsed - 1 : parsed;
            newAddrStr = String(newOneOrigin ? addr0based + 1 : addr0based);
          }
        }
        return { ...row, memoryArea: newArea, addressStr: newAddrStr };
      }),
    );
  };

  // 一括編集ダイアログ内のアドレス競合を検出（レジスタ型プロトコルのみ）
  const findBulkRowConflicts = (
    rows: BulkEditRow[],
    protocolType: string,
  ): Set<string> => {
    const conflictIds = new Set<string>();
    const activeRows = rows
      .filter((row) => row.addressStr.trim() !== "")
      .map((row) => {
        const parsed = parseInt(row.addressStr, 10);
        if (isNaN(parsed)) return null;
        const oneOrigin = isOneOriginArea(protocolType, row.memoryArea);
        const addr0 = oneOrigin ? Math.max(0, parsed - 1) : parsed;
        const wc = Math.max(1, row.wordCount);
        return {
          variableId: row.variableId,
          memoryArea: row.memoryArea,
          addr0,
          addrEnd: addr0 + wc - 1,
        };
      })
      .filter((r): r is NonNullable<typeof r> => r !== null);
    for (let i = 0; i < activeRows.length; i++) {
      for (let j = i + 1; j < activeRows.length; j++) {
        const a = activeRows[i];
        const b = activeRows[j];
        if (a.memoryArea !== b.memoryArea) continue;
        if (a.addr0 <= b.addrEnd && b.addr0 <= a.addrEnd) {
          conflictIds.add(a.variableId);
          conflictIds.add(b.variableId);
        }
      }
    }
    return conflictIds;
  };

  // 一括マッピング保存
  const handleBulkMappingSave = async () => {
    if (bulkIsSaving) return;
    if (isNodePublishingProtocol(bulkProtocol)) {
      // 公開できない変数があれば、途中まで保存される前に止める
      const blocked = bulkRows.filter(
        (row) =>
          row.nodeEnabled &&
          nodeAddresses[bulkProtocol]?.[row.variableId]?.error,
      );
      if (blocked.length > 0) {
        alert(
          "公開できない変数があります:\n" +
            blocked
              .map(
                (row) =>
                  `${row.variableName}: ${nodeAddresses[bulkProtocol][row.variableId].error}`,
              )
              .join("\n"),
        );
        return;
      }
    }
    setBulkIsSaving(true);
    try {
      const isNP = isNodePublishingProtocol(bulkProtocol);
      for (const row of bulkRows) {
        const variable = variables.find((v) => v.id === row.variableId);
        if (!variable) continue;
        if (isNP) {
          await UpdateVariableNodePublishing(row.variableId, bulkProtocol, {
            protocolType: bulkProtocol,
            enabled: row.nodeEnabled,
            accessMode: row.accessMode,
          } as application.NodePublishingDTO);
        } else {
          const otherMappings = (variable.mappings || []).filter(
            (m) => m.protocolType !== bulkProtocol,
          );
          if (row.addressStr.trim() === "") {
            await UpdateVariableMappings(row.variableId, otherMappings);
          } else {
            const parsed = parseInt(row.addressStr, 10);
            if (isNaN(parsed)) continue;
            const oneOrigin = isOneOriginArea(bulkProtocol, row.memoryArea);
            const addr0based = oneOrigin ? Math.max(0, parsed - 1) : parsed;
            await UpdateVariableMappings(row.variableId, [
              ...otherMappings,
              {
                protocolType: bulkProtocol,
                memoryArea: row.memoryArea,
                address: addr0based,
                endianness: row.endianness,
              } as application.ProtocolMappingDTO,
            ]);
          }
        }
      }
      await loadVariables();
      setIsBulkMappingOpen(false);
    } catch (e) {
      alert("一括マッピングの保存に失敗しました: " + e);
    } finally {
      setBulkIsSaving(false);
    }
  };

  // マッピングを追加
  const handleAddMapping = () => {
    const firstProtocol = serverInstances[0]?.protocolType || "";
    const firstArea = (memoryAreasByProtocol[firstProtocol] || [])[0]?.id || "";
    setEditMappings([
      ...editMappings,
      {
        protocolType: firstProtocol,
        memoryArea: firstArea,
        address: 0,
        endianness: "big",
      } as application.ProtocolMappingDTO,
    ]);
  };

  // マッピングを削除
  const handleRemoveMapping = (index: number) => {
    setEditMappings(editMappings.filter((_, i) => i !== index));
  };

  // コンテキストメニューを開く
  const handleContextMenu = (
    e: React.MouseEvent,
    variable: application.VariableDTO,
  ) => {
    e.preventDefault();
    setContextMenu({ x: e.clientX, y: e.clientY, variable });
  };

  // マッピングを保存
  const handleSaveMappings = async () => {
    if (!mappingVariable) return;
    // 公開できない変数は、マッピングだけ保存される前に止める
    for (const pub of editNodePublishings) {
      const error =
        nodeAddresses[pub.protocolType]?.[mappingVariable.id]?.error;
      if (pub.enabled && error) {
        alert(error);
        return;
      }
    }
    try {
      await UpdateVariableMappings(mappingVariable.id, editMappings);
      for (const pub of editNodePublishings) {
        await UpdateVariableNodePublishing(
          mappingVariable.id,
          pub.protocolType,
          pub,
        );
      }
      await loadVariables();
      setIsMappingDialogOpen(false);
      setMappingVariable(null);
    } catch (e) {
      console.error("Failed to update mappings:", e);
      alert("マッピングの更新に失敗しました: " + e);
    }
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
          onClick={handleBulkMappingOpen}
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
                onClick={() => handleRowEditClick(row)}
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
                    ? () => handleMappingClick(row.variable)
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
              handleMappingClick(contextMenu.variable);
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

      {/* 変数編集ダイアログ */}
      {isEditDialogOpen && editingVariable && (
        <FocusTrap onConfirm={handleUpdateRow}>
          <div
            ref={editDialogRef}
            className="dialog"
            style={{
              minWidth: "450px",
              maxHeight: "80vh",
              display: "flex",
              flexDirection: "column",
            }}
          >
            <h3>値を編集</h3>
            <div
              className="dialog-content"
              style={{ flex: 1, overflowY: "auto" }}
            >
              <div className="dialog-row">
                <label>変数名:</label>
                <span className="dialog-value">
                  {editingRow ? editingRow.displayName : editingVariable.name}
                </span>
              </div>
              <div className="dialog-row">
                <label>データ型:</label>
                <span className="dialog-value">
                  {editingRow ? editingRow.dataType : editingVariable.dataType}
                </span>
              </div>
              <div
                className="dialog-row"
                style={{ flexDirection: "column", alignItems: "flex-start" }}
              >
                <label>値:</label>
                <div style={{ width: "100%" }}>
                  {editingRow
                    ? renderValueEditor(
                        editingRow.dataType,
                        editData,
                        setEditData,
                      )
                    : renderValueEditor(
                        editingVariable.dataType,
                        editData,
                        setEditData,
                      )}
                </div>
              </div>
            </div>
            <div className="dialog-buttons">
              <button
                onClick={() => {
                  setIsEditDialogOpen(false);
                  setEditData(null);
                  setEditingRow(null);
                }}
                className="btn-secondary"
              >
                キャンセル
              </button>
              <button onClick={handleUpdateRow} className="btn-primary">
                更新
              </button>
            </div>
          </div>
        </FocusTrap>
      )}

      {/* マッピング編集ダイアログ */}
      {isMappingDialogOpen && mappingVariable && (
        <FocusTrap onConfirm={handleSaveMappings}>
          <div className="dialog" style={{ minWidth: "500px" }}>
            <h3>マッピング設定: {mappingVariable.name}</h3>
            <div className="dialog-content">
              <p className="dialog-description">
                この変数をプロトコルのアドレスにマッピングします。
              </p>

              {editMappings.map((m, index) => (
                <div key={index} className="dialog-section">
                  {/* ヘッダ行 */}
                  <div className="dialog-row">
                    <label style={{ flex: 1 }}>プロトコル</label>
                    <label style={{ flex: 1 }}>メモリエリア</label>
                    <label style={{ flex: 1 }}>アドレス</label>
                    <label style={{ flex: 1 }}>エンディアン</label>
                    <span style={{ width: "60px" }}></span>
                  </div>
                  {/* コントロール行 */}
                  <div className="dialog-row">
                    <select
                      value={m.protocolType}
                      onChange={(e) => {
                        const updated = [...editMappings];
                        const newProtocol = e.target.value;
                        const firstArea =
                          (memoryAreasByProtocol[newProtocol] || [])[0]?.id ||
                          "";
                        updated[index] = {
                          ...updated[index],
                          protocolType: newProtocol,
                          memoryArea: firstArea,
                        };
                        setEditMappings(updated);
                      }}
                      style={{ flex: 1 }}
                    >
                      {serverInstances.map((inst) => (
                        <option
                          key={inst.protocolType}
                          value={inst.protocolType}
                        >
                          {inst.displayName}
                        </option>
                      ))}
                    </select>

                    <select
                      value={m.memoryArea}
                      onChange={(e) => {
                        const updated = [...editMappings];
                        updated[index] = {
                          ...updated[index],
                          memoryArea: e.target.value,
                        };
                        setEditMappings(updated);
                      }}
                      style={{ flex: 1 }}
                    >
                      {(memoryAreasByProtocol[m.protocolType] || []).map(
                        (a) => (
                          <option key={a.id} value={a.id}>
                            {a.displayName}
                          </option>
                        ),
                      )}
                    </select>

                    <input
                      type="number"
                      value={
                        isOneOriginArea(m.protocolType, m.memoryArea)
                          ? m.address + 1
                          : m.address
                      }
                      onChange={(e) => {
                        const oneOrigin = isOneOriginArea(
                          m.protocolType,
                          m.memoryArea,
                        );
                        const v =
                          parseInt(e.target.value) || (oneOrigin ? 1 : 0);
                        const updated = [...editMappings];
                        updated[index] = {
                          ...updated[index],
                          address: oneOrigin ? Math.max(0, v - 1) : v,
                        };
                        setEditMappings(updated);
                      }}
                      min={
                        isOneOriginArea(m.protocolType, m.memoryArea) ? 1 : 0
                      }
                      style={{ flex: 1 }}
                    />

                    <select
                      value={m.endianness}
                      onChange={(e) => {
                        const updated = [...editMappings];
                        updated[index] = {
                          ...updated[index],
                          endianness: e.target.value,
                        };
                        setEditMappings(updated);
                      }}
                      style={{ flex: 1 }}
                    >
                      <option value="big">Big</option>
                      <option value="little">Little</option>
                    </select>

                    <button
                      onClick={() => handleRemoveMapping(index)}
                      className="btn-small btn-danger"
                    >
                      X
                    </button>
                  </div>
                  {(() => {
                    const conflicts = findMappingConflicts(m);
                    if (conflicts.length === 0) return null;
                    return (
                      <div className="mapping-conflict-warning">
                        ⚠ 以下の変数と重複しています: {conflicts.join(", ")}
                      </div>
                    );
                  })()}
                </div>
              ))}

              <button onClick={handleAddMapping} className="btn-secondary">
                + マッピング追加
              </button>

              {/* プロトコル公開設定（SupportsNodePublishing が true のサーバーのみ表示）*/}
              {(() => {
                const publishableServers = serverInstances.filter(
                  (i) => i.supportsNodePublishing,
                );
                if (publishableServers.length === 0) return null;
                return (
                  <div className="dialog-section" style={{ marginTop: "16px" }}>
                    <h4 style={{ margin: "0 0 8px 0" }}>プロトコル公開設定</h4>
                    {publishableServers.map((server) => {
                      const pub = editNodePublishings.find(
                        (p) => p.protocolType === server.protocolType,
                      ) ?? {
                        protocolType: server.protocolType,
                        enabled: false,
                        accessMode: "readwrite",
                      };
                      const updatePub = (
                        patch: Partial<application.NodePublishingDTO>,
                      ) => {
                        const updated = editNodePublishings.filter(
                          (p) => p.protocolType !== server.protocolType,
                        );
                        setEditNodePublishings([
                          ...updated,
                          { ...pub, ...patch },
                        ]);
                      };
                      return (
                        <div
                          key={server.protocolType}
                          className="dialog-row"
                          style={{ alignItems: "center", gap: "8px" }}
                        >
                          <label style={{ minWidth: "80px" }}>
                            {server.displayName}
                          </label>
                          <label
                            style={{
                              display: "flex",
                              alignItems: "center",
                              gap: "4px",
                            }}
                          >
                            <input
                              type="checkbox"
                              checked={pub.enabled}
                              onChange={(e) =>
                                updatePub({ enabled: e.target.checked })
                              }
                            />
                            公開する
                          </label>
                          <select
                            value={pub.accessMode}
                            disabled={!pub.enabled}
                            onChange={(e) =>
                              updatePub({ accessMode: e.target.value })
                            }
                          >
                            <option value="read">Read Only</option>
                            <option value="write">Write Only</option>
                            <option value="readwrite">Read / Write</option>
                          </select>
                          {pub.enabled &&
                            mappingVariable &&
                            (() => {
                              const addr =
                                nodeAddresses[server.protocolType]?.[
                                  mappingVariable.id
                                ];
                              return addr?.error ? (
                                <span className="node-address-error">
                                  ⚠ {addr.error}
                                </span>
                              ) : (
                                <span
                                  style={{ fontSize: "11px", color: "#888" }}
                                >
                                  {addr?.address ?? ""}
                                </span>
                              );
                            })()}
                        </div>
                      );
                    })}
                  </div>
                );
              })()}
            </div>
            <div className="dialog-buttons">
              <button
                onClick={() => setIsMappingDialogOpen(false)}
                className="btn-secondary"
              >
                キャンセル
              </button>
              <button onClick={handleSaveMappings} className="btn-primary">
                保存
              </button>
            </div>
          </div>
        </FocusTrap>
      )}
      {/* 一括マッピング編集ダイアログ */}
      {isBulkMappingOpen && (
        <FocusTrap>
          <div
            className="dialog"
            style={{
              minWidth: "700px",
              maxWidth: "95vw",
              maxHeight: "80vh",
              display: "flex",
              flexDirection: "column",
            }}
          >
            <h3>一括マッピング編集</h3>
            {/* プロトコル選択 */}
            <div
              className="dialog-content"
              style={{ flex: "none", paddingBottom: 0 }}
            >
              <div className="dialog-row">
                <label>プロトコル:</label>
                <select
                  value={bulkProtocol}
                  onChange={(e) => handleBulkProtocolChange(e.target.value)}
                >
                  {serverInstances.map((inst) => (
                    <option key={inst.protocolType} value={inst.protocolType}>
                      {inst.displayName}
                    </option>
                  ))}
                </select>
              </div>
            </div>
            {/* テーブル本体 */}
            <div
              className="dialog-content"
              style={{ flex: 1, overflowY: "auto", paddingTop: "8px" }}
            >
              {bulkRows.length === 0 ? (
                <p style={{ color: "#888", textAlign: "center" }}>
                  変数がありません
                </p>
              ) : isNodePublishingProtocol(bulkProtocol) ? (
                /* ノード公開型プロトコルのテーブル */
                <table className="variable-table">
                  <thead>
                    <tr>
                      <th style={{ textAlign: "left", minWidth: "140px" }}>
                        変数名
                      </th>
                      <th style={{ textAlign: "left", minWidth: "100px" }}>
                        データ型
                      </th>
                      <th style={{ textAlign: "center", minWidth: "60px" }}>
                        W数
                      </th>
                      <th style={{ textAlign: "center", minWidth: "80px" }}>
                        公開する
                      </th>
                      <th style={{ minWidth: "130px" }}>アクセスモード</th>
                      <th
                        style={{
                          minWidth: "100px",
                          fontSize: "0.8em",
                          color: "#888",
                        }}
                      >
                        NodeID
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {bulkRows.map((row) => (
                      <tr key={row.variableId}>
                        <td style={{ fontWeight: "bold" }}>
                          {row.variableName}
                        </td>
                        <td style={{ fontSize: "0.85em", color: "#aaa" }}>
                          {row.dataType}
                        </td>
                        <td style={{ textAlign: "center" }}>{row.wordCount}</td>
                        <td style={{ textAlign: "center" }}>
                          <input
                            type="checkbox"
                            checked={row.nodeEnabled}
                            onChange={(e) =>
                              handleBulkRowChange(row.variableId, {
                                nodeEnabled: e.target.checked,
                              })
                            }
                          />
                        </td>
                        <td>
                          <select
                            value={row.accessMode}
                            disabled={!row.nodeEnabled}
                            onChange={(e) =>
                              handleBulkRowChange(row.variableId, {
                                accessMode: e.target.value,
                              })
                            }
                            style={{ width: "100%" }}
                          >
                            <option value="read">Read Only</option>
                            <option value="write">Write Only</option>
                            <option value="readwrite">Read / Write</option>
                          </select>
                        </td>
                        <td style={{ fontSize: "0.8em", color: "#888" }}>
                          {(() => {
                            if (!row.nodeEnabled) return "-";
                            const addr =
                              nodeAddresses[bulkProtocol]?.[row.variableId];
                            return addr?.error ? (
                              <span className="node-address-error">
                                ⚠ {addr.error}
                              </span>
                            ) : (
                              (addr?.address ?? "-")
                            );
                          })()}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : (
                /* レジスタ型プロトコルのテーブル */
                (() => {
                  const conflictIds = findBulkRowConflicts(
                    bulkRows,
                    bulkProtocol,
                  );
                  return (
                    <>
                      <table className="variable-table">
                        <thead>
                          <tr>
                            <th
                              style={{ textAlign: "left", minWidth: "140px" }}
                            >
                              変数名
                            </th>
                            <th
                              style={{ textAlign: "left", minWidth: "100px" }}
                            >
                              データ型
                            </th>
                            <th
                              style={{ textAlign: "center", minWidth: "60px" }}
                            >
                              W数
                            </th>
                            <th style={{ minWidth: "140px" }}>メモリエリア</th>
                            <th style={{ minWidth: "110px" }}>
                              アドレス
                              <span
                                style={{
                                  fontSize: "0.75em",
                                  color: "#888",
                                  marginLeft: "4px",
                                }}
                              >
                                (空=削除)
                              </span>
                            </th>
                            <th style={{ minWidth: "100px" }}>エンディアン</th>
                          </tr>
                        </thead>
                        <tbody>
                          {bulkRows.map((row) => {
                            const areas =
                              memoryAreasByProtocol[bulkProtocol] || [];
                            const oneOrigin = isOneOriginArea(
                              bulkProtocol,
                              row.memoryArea,
                            );
                            const hasConflict = conflictIds.has(row.variableId);
                            return (
                              <tr
                                key={row.variableId}
                                style={
                                  hasConflict
                                    ? { background: "rgba(255, 150, 0, 0.08)" }
                                    : undefined
                                }
                              >
                                <td style={{ fontWeight: "bold" }}>
                                  {row.variableName}
                                </td>
                                <td
                                  style={{ fontSize: "0.85em", color: "#aaa" }}
                                >
                                  {row.dataType}
                                </td>
                                <td style={{ textAlign: "center" }}>
                                  {row.wordCount}
                                </td>
                                <td>
                                  <select
                                    value={row.memoryArea}
                                    onChange={(e) =>
                                      handleBulkAreaChange(
                                        row.variableId,
                                        e.target.value,
                                      )
                                    }
                                    style={{ width: "100%" }}
                                  >
                                    {areas.map((a) => (
                                      <option key={a.id} value={a.id}>
                                        {a.displayName}
                                      </option>
                                    ))}
                                  </select>
                                </td>
                                <td>
                                  <div
                                    style={{
                                      display: "flex",
                                      alignItems: "center",
                                      gap: "4px",
                                    }}
                                  >
                                    <input
                                      type="number"
                                      value={row.addressStr}
                                      min={oneOrigin ? 1 : 0}
                                      placeholder="未設定"
                                      onChange={(e) =>
                                        handleBulkRowChange(row.variableId, {
                                          addressStr: e.target.value,
                                        })
                                      }
                                      style={{
                                        width: "80px",
                                        ...(hasConflict
                                          ? { borderColor: "#ffa500" }
                                          : {}),
                                      }}
                                    />
                                    {hasConflict && (
                                      <span
                                        title="アドレスが他の変数と重複しています"
                                        style={{ color: "#ffa500" }}
                                      >
                                        ⚠
                                      </span>
                                    )}
                                  </div>
                                </td>
                                <td>
                                  <select
                                    value={row.endianness}
                                    onChange={(e) =>
                                      handleBulkRowChange(row.variableId, {
                                        endianness: e.target.value,
                                      })
                                    }
                                    style={{ width: "100%" }}
                                  >
                                    <option value="big">Big</option>
                                    <option value="little">Little</option>
                                  </select>
                                </td>
                              </tr>
                            );
                          })}
                        </tbody>
                      </table>
                    </>
                  );
                })()
              )}
            </div>
            {!isNodePublishingProtocol(bulkProtocol) &&
              (() => {
                const conflictIds = findBulkRowConflicts(
                  bulkRows,
                  bulkProtocol,
                );
                return conflictIds.size > 0 ? (
                  <div
                    style={{
                      padding: "6px 16px",
                      background: "rgba(255, 150, 0, 0.15)",
                      borderTop: "1px solid rgba(255, 150, 0, 0.4)",
                      color: "#ffa500",
                      fontSize: "0.85em",
                      flexShrink: 0,
                    }}
                  >
                    ⚠ アドレスが重複している変数が {conflictIds.size} 件あります
                  </div>
                ) : null;
              })()}
            <div className="dialog-buttons">
              <button
                onClick={() => setIsBulkMappingOpen(false)}
                className="btn-secondary"
                disabled={bulkIsSaving}
              >
                キャンセル
              </button>
              <button
                onClick={handleBulkMappingSave}
                className="btn-primary"
                disabled={bulkIsSaving}
              >
                {bulkIsSaving ? "保存中..." : "一括保存"}
              </button>
            </div>
          </div>
        </FocusTrap>
      )}
      <StructTypeDialog
        ref={structTypeDialogRef}
        dataTypes={dataTypes}
        structTypes={structTypes}
        onChanged={loadStructTypes}
      />
    </div>
  );
}
