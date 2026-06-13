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
  GetStructTypes,
  RegisterStructType,
  DeleteStructType,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { application } from "../../wailsjs/go/models";

// 構造体型配列の要素エディタ（ローカルで展開/折りたたみ状態を管理）
const StructArrayElementEditor = ({
  elemType,
  elem,
  onChange,
  renderEditor,
}: {
  elemType: string;
  elem: any;
  onChange: (v: any) => void;
  renderEditor: (
    dataType: string,
    value: any,
    onChange: (v: any) => void,
  ) => JSX.Element;
}) => {
  const [isExpanded, setIsExpanded] = useState(false);
  return (
    <div
      style={{
        border: "1px solid #444",
        borderRadius: "3px",
        padding: "2px 4px",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          cursor: "pointer",
          gap: "4px",
        }}
        onClick={() => setIsExpanded(!isExpanded)}
      >
        <span style={{ fontSize: "0.8em" }}>
          {isExpanded ? "\u25BC" : "\u25B6"}
        </span>
        <span style={{ fontSize: "0.8em", color: "#aaa" }}>{elemType}</span>
      </div>
      {isExpanded && (
        <div style={{ marginTop: "4px" }}>
          {renderEditor(elemType, elem, onChange)}
        </div>
      )}
    </div>
  );
};

export function VariableView() {
  const [variables, setVariables] = useState<application.VariableDTO[]>([]);
  const [dataTypes, setDataTypes] = useState<application.DataTypeInfoDTO[]>([]);
  const [structTypes, setStructTypes] = useState<application.StructTypeDTO[]>(
    [],
  );
  const [isAddDialogOpen, setIsAddDialogOpen] = useState(false);
  const [isEditDialogOpen, setIsEditDialogOpen] = useState(false);
  const [isMappingDialogOpen, setIsMappingDialogOpen] = useState(false);
  const [isStructTypeDialogOpen, setIsStructTypeDialogOpen] = useState(false);
  const [editingVariable, setEditingVariable] =
    useState<application.VariableDTO | null>(null);

  // 新規変数フォーム
  const [newName, setNewName] = useState("");
  const [newDataType, setNewDataType] = useState("INT");
  const [newValue, setNewValue] = useState("");
  // 配列型追加用
  const [newArrayElemType, setNewArrayElemType] = useState("INT");
  const [newArrayElemCategory, setNewArrayElemCategory] = useState<
    "scalar" | "struct"
  >("scalar");
  const [newDimCount, setNewDimCount] = useState(1);
  const [newDimBounds, setNewDimBounds] = useState<
    Array<{ lower: number; upper: number }>
  >([
    { lower: 0, upper: 9 },
    { lower: 0, upper: 4 },
    { lower: 0, upper: 2 },
  ]);
  // STRING型のバイト長
  const [newStringLength, setNewStringLength] = useState(20);
  // 型カテゴリ: 'scalar' | 'array' | 'struct'
  const [newTypeCategory, setNewTypeCategory] = useState<
    "scalar" | "array" | "struct"
  >("scalar");

  // 変数メタデータ編集ダイアログ
  const [isMetaEditDialogOpen, setIsMetaEditDialogOpen] = useState(false);
  const [metaEditVariableId, setMetaEditVariableId] = useState("");
  const [metaEditName, setMetaEditName] = useState("");
  const [metaEditDataType, setMetaEditDataType] = useState("INT");
  const [metaEditTypeCategory, setMetaEditTypeCategory] = useState<
    "scalar" | "array" | "struct"
  >("scalar");
  const [metaEditArrayElemType, setMetaEditArrayElemType] = useState("INT");
  const [metaEditArrayElemCategory, setMetaEditArrayElemCategory] = useState<
    "scalar" | "struct"
  >("scalar");
  const [metaEditDimCount, setMetaEditDimCount] = useState(1);
  const [metaEditDimBounds, setMetaEditDimBounds] = useState<
    Array<{ lower: number; upper: number }>
  >([
    { lower: 0, upper: 9 },
    { lower: 0, upper: 4 },
    { lower: 0, upper: 2 },
  ]);
  const [metaEditStringLength, setMetaEditStringLength] = useState(20);

  // 構造体型定義フォーム
  const [structTypeName, setStructTypeName] = useState("");
  const [editingStructTypeName, setEditingStructTypeName] = useState<
    string | null
  >(null);
  const [structTypeFields, setStructTypeFields] = useState<
    {
      name: string;
      category: "scalar" | "struct" | "array";
      dataType: string;
      stringLength: number;
      arrayElemType: string;
      arrayElemCategory: "scalar" | "struct";
      arrayDimCount: number;
      arrayDimBounds: [
        { lower: number; upper: number },
        { lower: number; upper: number },
        { lower: number; upper: number },
      ];
    }[]
  >([
    {
      name: "",
      category: "scalar",
      dataType: "INT",
      stringLength: 20,
      arrayElemType: "INT",
      arrayElemCategory: "scalar",
      arrayDimCount: 1,
      arrayDimBounds: [
        { lower: 0, upper: 9 },
        { lower: 0, upper: 4 },
        { lower: 0, upper: 4 },
      ],
    },
  ]);

  // 展開/縮小状態（keyはFlatRowのヘッダーキー）
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());

  // キーボードナビゲーション用フォーカス状態
  const [focusedRowKey, setFocusedRowKey] = useState<string | null>(null);

  // 編集フォーム
  const [editData, setEditData] = useState<any>(null);
  const [editValue, setEditValue] = useState("");
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

  // サーバー一覧とメモリエリアを取得
  const loadServerInstancesAndAreas = useCallback(async () => {
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
    } catch (e) {
      console.error("Failed to load server instances:", e);
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

  // ESCキーでダイアログを閉じる
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      if (isBulkMappingOpen) {
        setIsBulkMappingOpen(false);
      } else if (isStructTypeDialogOpen) {
        setIsStructTypeDialogOpen(false);
        setEditingStructTypeName(null);
        setStructTypeName("");
        setStructTypeFields([
          {
            name: "",
            category: "scalar",
            dataType: "INT",
            stringLength: 20,
            arrayElemType: "INT",
            arrayElemCategory: "scalar",
            arrayDimCount: 1,
            arrayDimBounds: [
              { lower: 0, upper: 9 },
              { lower: 0, upper: 4 },
              { lower: 0, upper: 4 },
            ],
          },
        ]);
      } else if (isMappingDialogOpen) {
        setIsMappingDialogOpen(false);
      } else if (isMetaEditDialogOpen) {
        setIsMetaEditDialogOpen(false);
      } else if (isEditDialogOpen) {
        setIsEditDialogOpen(false);
        setEditData(null);
        setEditingRow(null);
      } else if (isAddDialogOpen) {
        setIsAddDialogOpen(false);
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [
    isAddDialogOpen,
    isEditDialogOpen,
    isMappingDialogOpen,
    isMetaEditDialogOpen,
    isStructTypeDialogOpen,
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

  // スカラー値のフォーマット
  const formatScalarValue = (value: any, dataType: string): string => {
    if (value === null || value === undefined) return "-";
    switch (dataType) {
      case "BOOL":
        return value ? "TRUE" : "FALSE";
      case "STRING":
        return `"${value}"`;
      case "REAL":
      case "LREAL":
        return typeof value === "number" ? String(value) : String(value);
      default:
        if (dataType.startsWith("STRING[")) return `"${value}"`;
        return String(value);
    }
  };

  // プロジェクトインポート完了イベントを購読して変数・構造体・サーバー情報をリロード
  useEffect(() => {
    const off = EventsOn("project:imported", () => {
      loadVariables();
      loadStructTypes();
      loadServerInstancesAndAreas();
    });

    return off;
  }, []);

  // 配列型かどうか判定（IEC 61131-3 形式および旧形式に対応）
  const isArrayType = (dataType: string): boolean => {
    if (!dataType.startsWith("ARRAY[")) return false;
    return dataType.includes("] OF ") || dataType.endsWith("]");
  };

  // 配列型文字列をパースして最初の次元の { elemType, size, lower, upper } を返す
  // IEC 61131-3 形式: "ARRAY[1..10] OF INT" → { elemType: "INT", size: 10, lower: 1, upper: 10 }
  //   多次元:          "ARRAY[0..2, 0..4] OF INT" → { elemType: "ARRAY[0..4] OF INT", size: 3, lower: 0, upper: 2 }
  // 旧形式（後方互換）: "ARRAY[INT;10]"  → { elemType: "INT", size: 10, lower: 0, upper: 9 }
  const parseArrayTypeFE = (
    dataType: string,
  ): {
    elemType: string;
    size: number;
    lower: number;
    upper: number;
  } | null => {
    if (!dataType.startsWith("ARRAY[")) return null;
    const ofIdx = dataType.indexOf("] OF ");
    if (ofIdx >= 0) {
      const dimsStr = dataType.slice(6, ofIdx);
      const elemStr = dataType.slice(ofIdx + 5);
      const dimParts = dimsStr.split(",");
      const firstDim = dimParts[0].trim();
      const rangeParts = firstDim.split("..");
      if (rangeParts.length !== 2) return null;
      const lower = parseInt(rangeParts[0].trim());
      const upper = parseInt(rangeParts[1].trim());
      if (isNaN(lower) || isNaN(upper) || upper < lower) return null;
      const size = upper - lower + 1;
      if (dimParts.length === 1) {
        return { elemType: elemStr, size, lower, upper };
      } else {
        const remaining = dimParts
          .slice(1)
          .map((d) => d.trim())
          .join(", ");
        return {
          elemType: `ARRAY[${remaining}] OF ${elemStr}`,
          size,
          lower,
          upper,
        };
      }
    }
    // 旧形式: ARRAY[ElementType;Size]
    if (!dataType.endsWith("]")) return null;
    const inner = dataType.slice(6, -1);
    const lastSemi = inner.lastIndexOf(";");
    if (lastSemi < 0) return null;
    const elemType = inner.slice(0, lastSemi).trim();
    const size = parseInt(inner.slice(lastSemi + 1).trim());
    if (isNaN(size) || size <= 0) return null;
    return { elemType, size, lower: 0, upper: size - 1 };
  };

  // 配列型文字列から全次元の境界とベース要素型を取得する（IEC 61131-3 形式のみ）
  // "ARRAY[1..3, 0..4] OF INT" → { dims: [{lower:1,upper:3},{lower:0,upper:4}], baseElemType:"INT" }
  const parseAllDimsFE = (
    dataType: string,
  ): {
    dims: Array<{ lower: number; upper: number }>;
    baseElemType: string;
  } | null => {
    if (!dataType.startsWith("ARRAY[")) return null;
    const ofIdx = dataType.indexOf("] OF ");
    if (ofIdx < 0) return null;
    const dimsStr = dataType.slice(6, ofIdx);
    const baseElemType = dataType.slice(ofIdx + 5);
    const dimParts = dimsStr.split(",").map((s) => s.trim());
    const dims: Array<{ lower: number; upper: number }> = [];
    for (const d of dimParts) {
      const rangeParts = d.split("..");
      if (rangeParts.length !== 2) return null;
      const lower = parseInt(rangeParts[0].trim());
      const upper = parseInt(rangeParts[1].trim());
      if (isNaN(lower) || isNaN(upper)) return null;
      dims.push({ lower, upper });
    }
    return { dims, baseElemType };
  };

  // 配列型文字列を IEC 61131-3 形式で構築する
  // buildArrayTypeFE("INT", [{lower:0,upper:9}])              → "ARRAY[0..9] OF INT"
  // buildArrayTypeFE("INT", [{lower:1,upper:3},{lower:0,upper:4}]) → "ARRAY[1..3, 0..4] OF INT"
  const buildArrayTypeFE = (
    baseElemType: string,
    dims: Array<{ lower: number; upper: number }>,
  ): string => {
    const dimsStr = dims.map((d) => `${d.lower}..${d.upper}`).join(", ");
    return `ARRAY[${dimsStr}] OF ${baseElemType}`;
  };

  // 構造体型かどうか判定
  const isStructType = (dataType: string): boolean => {
    const scalarTypes = [
      "BOOL",
      "SINT",
      "INT",
      "DINT",
      "LINT",
      "USINT",
      "UINT",
      "UDINT",
      "ULINT",
      "REAL",
      "LREAL",
      "STRING",
      "TIME",
      "DATE",
      "TIME_OF_DAY",
      "DATE_AND_TIME",
    ];
    return (
      !scalarTypes.includes(dataType) &&
      !isArrayType(dataType) &&
      !dataType.startsWith("STRING[")
    );
  };

  // データ型のワード数を取得
  const getWordCount = (dataType: string): number => {
    // STRING[n] 型
    const strMatch = dataType.match(/^STRING\[(\d+)\]$/);
    if (strMatch) {
      const maxBytes = parseInt(strMatch[1]);
      return Math.ceil(maxBytes / 2);
    }
    // スカラー型
    const dt = dataTypes.find((t) => t.id === dataType);
    if (dt) return dt.wordCount;
    // 構造体型
    const st = structTypes.find((s) => s.name === dataType);
    if (st) return st.wordCount;
    // 配列型（IEC 61131-3 形式 および 旧形式）
    const arr = parseArrayTypeFE(dataType);
    if (arr) {
      return getWordCount(arr.elemType) * arr.size;
    }
    return 1;
  };

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
    // Modbus系
    memoryArea: string;
    addressStr: string; // 空文字 = マッピングなし（削除）
    endianness: string;
    // OPC UA系（SupportsNodePublishing=true）
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
      isAddDialogOpen ||
      isMappingDialogOpen ||
      isMetaEditDialogOpen ||
      isStructTypeDialogOpen ||
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
        setIsAddDialogOpen(true);
        break;
      }
      case "F2": {
        e.preventDefault();
        handleOpenMetaEditDialog(row.variable);
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

  // デフォルト値を取得
  const getDefaultValue = (dataType: string): string => {
    if (dataType.startsWith("STRING[") || dataType === "STRING") return "";
    switch (dataType) {
      case "BOOL":
        return "false";
      case "REAL":
      case "LREAL":
        return "0.0";
      default:
        return "0";
    }
  };

  // 入力値をパース
  const parseValue = (input: string, dataType: string): any => {
    switch (dataType) {
      case "BOOL":
        return input.toLowerCase() === "true" || input === "1";
      case "STRING":
        return input;
      case "REAL":
      case "LREAL":
        return parseFloat(input) || 0;
      default:
        if (dataType.startsWith("STRING[")) return input;
        return parseInt(input, 10) || 0;
    }
  };

  // メモリエリアIDを短い表示名に変換（Modbusは番号、その他はそのまま）
  const areaShortName = (areaId: string): string => {
    const map: Record<string, string> = {
      coils: "0",
      discreteInputs: "1",
      inputRegisters: "3",
      holdingRegisters: "4",
    };
    return map[areaId] ?? areaId;
  };

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
        return `${m.protocolType}:${areaShortName(m.memoryArea)}:${addr}`;
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
        return `${areaShortName(m.memoryArea)}:${addr}`;
      })
      .join(", ");
  };

  // 構造体のデフォルト値を再帰的に生成
  const generateStructDefault = (typeName: string): any => {
    const st = structTypes.find((s) => s.name === typeName);
    if (!st) return {};
    const obj: Record<string, any> = {};
    for (const f of st.fields) {
      const arr = parseArrayTypeFE(f.dataType);
      if (arr) {
        obj[f.name] = Array.from({ length: arr.size }, () =>
          isStructType(arr.elemType)
            ? generateStructDefault(arr.elemType)
            : parseValue(getDefaultValue(arr.elemType), arr.elemType),
        );
      } else if (isStructType(f.dataType)) {
        obj[f.name] = generateStructDefault(f.dataType);
      } else {
        obj[f.name] = parseValue(getDefaultValue(f.dataType), f.dataType);
      }
    }
    return obj;
  };

  // 変数を追加
  const handleAddVariable = async () => {
    if (!newName.trim()) return;
    try {
      let dataType = newDataType;
      let value: any;

      if (newTypeCategory === "array") {
        const baseElemType = resolveBaseElemType(
          newArrayElemCategory,
          newArrayElemType,
          newStringLength,
        );
        dataType = buildArrayTypeFE(
          baseElemType,
          newDimBounds.slice(0, newDimCount),
        );
        value = generateDefaultForType(dataType);
      } else if (newTypeCategory === "struct") {
        // 構造体のデフォルト値を再帰的に生成
        value = generateStructDefault(newDataType);
      } else {
        // スカラー型
        // STRING選択時は STRING[n] 形式にする
        if (newDataType === "STRING") {
          dataType = `STRING[${newStringLength}]`;
        }
        value = parseValue(newValue || getDefaultValue(dataType), dataType);
      }

      await CreateVariable(newName.trim(), dataType, value);
      await loadVariables();
      setIsAddDialogOpen(false);
      setNewName("");
      setNewDataType("INT");
      setNewValue("");
      setNewTypeCategory("scalar");
      setNewArrayElemType("INT");
      setNewArrayElemCategory("scalar");
      setNewDimCount(1);
      setNewDimBounds([
        { lower: 0, upper: 9 },
        { lower: 0, upper: 4 },
        { lower: 0, upper: 2 },
      ]);
    } catch (e) {
      console.error("Failed to create variable:", e);
      alert("変数の作成に失敗しました: " + e);
    }
  };

  // 変数メタデータ編集ダイアログを開く
  const handleOpenMetaEditDialog = (v: application.VariableDTO) => {
    setMetaEditVariableId(v.id);
    setMetaEditName(v.name);

    const dt = v.dataType;
    if (isArrayType(dt)) {
      const allDims = parseAllDimsFE(dt);
      if (allDims) {
        setMetaEditTypeCategory("array");
        setMetaEditDimCount(allDims.dims.length);
        setMetaEditDimBounds([
          allDims.dims[0] ?? { lower: 0, upper: 9 },
          allDims.dims[1] ?? { lower: 0, upper: 4 },
          allDims.dims[2] ?? { lower: 0, upper: 2 },
        ]);
        const base = allDims.baseElemType;
        if (base.startsWith("STRING[")) {
          const sLen = base.match(/^STRING\[(\d+)\]$/);
          setMetaEditArrayElemCategory("scalar");
          setMetaEditArrayElemType("STRING");
          setMetaEditStringLength(sLen ? parseInt(sLen[1]) : 20);
        } else if (isStructType(base)) {
          setMetaEditArrayElemCategory("struct");
          setMetaEditArrayElemType(base);
        } else {
          setMetaEditArrayElemCategory("scalar");
          setMetaEditArrayElemType(base);
        }
      }
    } else if (dt.startsWith("STRING[")) {
      const sLen = dt.match(/^STRING\[(\d+)\]$/);
      setMetaEditTypeCategory("scalar");
      setMetaEditDataType("STRING");
      setMetaEditStringLength(sLen ? parseInt(sLen[1]) : 20);
    } else if (isStructType(dt)) {
      setMetaEditTypeCategory("struct");
      setMetaEditDataType(dt);
    } else {
      setMetaEditTypeCategory("scalar");
      setMetaEditDataType(dt);
    }

    setIsMetaEditDialogOpen(true);
  };

  // 変数メタデータ（名前・データタイプ）を保存する
  const handleSaveMetaEdit = async () => {
    if (!metaEditName.trim()) return;
    try {
      let dataType = metaEditDataType;
      if (metaEditTypeCategory === "array") {
        const baseElemType = resolveBaseElemType(
          metaEditArrayElemCategory,
          metaEditArrayElemType,
          metaEditStringLength,
        );
        dataType = buildArrayTypeFE(
          baseElemType,
          metaEditDimBounds.slice(0, metaEditDimCount),
        );
      } else if (
        metaEditTypeCategory === "scalar" &&
        metaEditDataType === "STRING"
      ) {
        dataType = `STRING[${metaEditStringLength}]`;
      }
      await UpdateVariable(metaEditVariableId, metaEditName.trim(), dataType);
      await loadVariables();
      setIsMetaEditDialogOpen(false);
    } catch (e) {
      console.error("Failed to update variable:", e);
      alert("変数の更新に失敗しました: " + e);
    }
  };

  // フィールドの実際のデータ型文字列を取得
  // カテゴリ・要素型・STRING長からベース要素型文字列を解決するヘルパー
  const resolveBaseElemType = (
    category: "scalar" | "struct",
    elemType: string,
    stringLength: number,
  ): string => {
    if (category === "struct") return elemType;
    return elemType === "STRING" ? `STRING[${stringLength}]` : elemType;
  };

  // 任意データ型のデフォルト値を再帰生成するヘルパー
  const generateDefaultForType = (dataType: string): any => {
    const arr = parseArrayTypeFE(dataType);
    if (arr) {
      return Array.from({ length: arr.size }, () =>
        generateDefaultForType(arr.elemType),
      );
    }
    if (isStructType(dataType)) return generateStructDefault(dataType);
    return parseValue(getDefaultValue(dataType), dataType);
  };

  const resolveFieldDataType = (
    field: (typeof structTypeFields)[0],
  ): string => {
    if (field.category === "scalar") {
      if (field.dataType === "STRING") return `STRING[${field.stringLength}]`;
      return field.dataType;
    }
    if (field.category === "struct") return field.dataType;
    // array: IEC 61131-3 形式で生成（構造体フィールドは0ベース）
    const baseElem = resolveBaseElemType(
      field.arrayElemCategory,
      field.arrayElemType,
      field.stringLength,
    );
    const dims = field.arrayDimBounds.slice(0, field.arrayDimCount);
    return buildArrayTypeFE(baseElem, dims);
  };

  // 構造体型を登録または更新
  const handleRegisterStructType = async () => {
    if (!structTypeName.trim()) return;
    const validFields = structTypeFields.filter((f) => f.name.trim());
    if (validFields.length === 0) {
      alert("少なくとも1つのフィールドを定義してください。");
      return;
    }
    try {
      // 編集モードの場合、既存の型を削除してから新しい型を登録
      if (editingStructTypeName) {
        await DeleteStructType(editingStructTypeName);
      }
      await RegisterStructType({
        name: structTypeName.trim(),
        fields: validFields.map((f) => ({
          name: f.name.trim(),
          dataType: resolveFieldDataType(f),
          offset: 0,
        })),
        wordCount: 0,
      } as application.StructTypeDTO);
      await loadStructTypes();
      setStructTypeName("");
      setEditingStructTypeName(null);
      setStructTypeFields([
        {
          name: "",
          category: "scalar",
          dataType: "INT",
          stringLength: 20,
          arrayElemType: "INT",
          arrayElemCategory: "scalar",
          arrayDimCount: 1,
          arrayDimBounds: [
            { lower: 0, upper: 9 },
            { lower: 0, upper: 4 },
            { lower: 0, upper: 4 },
          ],
        },
      ]);
    } catch (e) {
      console.error("Failed to register struct type:", e);
      alert("構造体型の登録に失敗しました: " + e);
    }
  };

  // 構造体型を編集
  const handleEditStructType = (st: application.StructTypeDTO) => {
    setEditingStructTypeName(st.name);
    setStructTypeName(st.name);

    // フィールドをフォーム形式に変換
    const formFields = st.fields.map((f) => {
      const field: (typeof structTypeFields)[0] = {
        name: f.name,
        category: "scalar",
        dataType: f.dataType,
        stringLength: 20,
        arrayElemType: "INT",
        arrayElemCategory: "scalar",
        arrayDimCount: 1,
        arrayDimBounds: [
          { lower: 0, upper: 9 },
          { lower: 0, upper: 4 },
          { lower: 0, upper: 4 },
        ],
      };

      // データ型を解析してカテゴリを判定
      if (isArrayType(f.dataType)) {
        field.category = "array";
        // 全次元とベース要素型を収集（フラット形式・入れ子形式両対応）
        const allDims: Array<{ lower: number; upper: number }> = [];
        let currentType = f.dataType;
        while (isArrayType(currentType)) {
          const flat = parseAllDimsFE(currentType);
          if (flat) {
            allDims.push(...flat.dims);
            currentType = flat.baseElemType;
          } else {
            break;
          }
        }
        field.arrayDimCount = Math.min(Math.max(allDims.length, 1), 3);
        field.arrayDimBounds = [
          allDims[0] ?? { lower: 0, upper: 9 },
          allDims[1] ?? { lower: 0, upper: 4 },
          allDims[2] ?? { lower: 0, upper: 4 },
        ] as [
          { lower: number; upper: number },
          { lower: number; upper: number },
          { lower: number; upper: number },
        ];
        // ベース要素型を解析
        if (currentType.startsWith("STRING[")) {
          const lenMatch = currentType.match(/^STRING\[(\d+)\]$/);
          field.arrayElemCategory = "scalar";
          field.arrayElemType = "STRING";
          if (lenMatch) field.stringLength = parseInt(lenMatch[1]);
        } else if (structTypes.some((s) => s.name === currentType)) {
          field.arrayElemCategory = "struct";
          field.arrayElemType = currentType;
        } else {
          field.arrayElemCategory = "scalar";
          field.arrayElemType = currentType;
        }
      } else if (f.dataType.startsWith("STRING[")) {
        field.category = "scalar";
        field.dataType = "STRING";
        const match = f.dataType.match(/^STRING\[(\d+)\]$/);
        if (match) field.stringLength = parseInt(match[1]);
      } else if (structTypes.some((s) => s.name === f.dataType)) {
        field.category = "struct";
        field.dataType = f.dataType;
      } else {
        field.category = "scalar";
        field.dataType = f.dataType;
      }

      return field;
    });

    setStructTypeFields(formFields);
  };

  // 構造体型を削除
  const handleDeleteStructType = async (name: string) => {
    if (!confirm(`構造体型 "${name}" を削除しますか?`)) return;
    try {
      await DeleteStructType(name);
      await loadStructTypes();
    } catch (e) {
      console.error("Failed to delete struct type:", e);
      alert("構造体型の削除に失敗しました: " + e);
    }
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

  // 文字列として保持されているREAL/LREAL値を数値に変換
  const coerceFloatIfNeeded = (value: any, dataType: string): any => {
    if (
      (dataType === "REAL" || dataType === "LREAL") &&
      typeof value === "string"
    ) {
      const f = parseFloat(value);
      return isNaN(f) ? 0 : f;
    }
    return value;
  };

  // valuePath (string | number)[] をバックエンドのフィールドパス文字列に変換する
  // 文字列セグメントは常に "." プレフィックスを付ける
  // 例: ["motor", "speed"] → ".motor.speed"
  //     ["items", 1]       → ".items[1]"
  //     [0, "name"]        → "[0].name"
  const valuePathToFieldPath = (valuePath: (string | number)[]): string => {
    let result = "";
    for (const seg of valuePath) {
      if (typeof seg === "number") {
        result += `[${seg}]`;
      } else {
        result += `.${seg}`;
      }
    }
    return result;
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

  // 数値入力のパース（接頭辞で自動判定）
  const parseNumericInput = (input: string): number => {
    const trimmed = input.trim();
    if (trimmed.startsWith("0x") || trimmed.startsWith("0X")) {
      return parseInt(trimmed, 16);
    }
    if (trimmed.startsWith("0b") || trimmed.startsWith("0B")) {
      return parseInt(trimmed.slice(2), 2);
    }
    return parseFloat(trimmed) || 0;
  };

  // 64ビット整数入力のパース（BigInt使用、精度損失なし）
  // 10進・16進（0x）・2進（0b）の入力に対応し、正規化された10進文字列を返す
  const parseBigIntInput = (input: string, dataType: string): string | null => {
    const trimmed = input.trim();
    if (trimmed === "" || trimmed === "-") return null;
    try {
      const bigVal =
        trimmed.startsWith("0x") ||
        trimmed.startsWith("0X") ||
        trimmed.startsWith("0b") ||
        trimmed.startsWith("0B")
          ? BigInt(trimmed)
          : BigInt(trimmed);
      if (dataType === "LINT") {
        const min = BigInt("-9223372036854775808");
        const max = BigInt("9223372036854775807");
        const clamped = bigVal < min ? min : bigVal > max ? max : bigVal;
        return clamped.toString(10);
      } else {
        // ULINT
        const max = BigInt("18446744073709551615");
        const clamped = bigVal < 0n ? 0n : bigVal > max ? max : bigVal;
        return clamped.toString(10);
      }
    } catch {
      return null;
    }
  };

  // 数値フォーマット
  const formatNumeric = (
    value: number,
    format: "dec" | "hex" | "bin",
  ): string => {
    switch (format) {
      case "hex":
        return "0x" + (value >>> 0).toString(16).toUpperCase();
      case "bin":
        return "0b" + (value >>> 0).toString(2);
      default:
        return String(value);
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
    await loadServerInstancesAndAreas();
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
    await loadServerInstancesAndAreas();
    if (variables.length === 0) return;
    const firstProtocol = serverInstances[0]?.protocolType ?? "";
    setBulkProtocol(firstProtocol);
    setBulkRows(buildBulkRows(variables, firstProtocol));
    setIsBulkMappingOpen(true);
  };

  // 一括編集ダイアログ内のプロトコル変更
  const handleBulkProtocolChange = (newProtocol: string) => {
    setBulkProtocol(newProtocol);
    setBulkRows(buildBulkRows(variables, newProtocol));
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

  // 一括編集ダイアログ内のアドレス競合を検出（Modbus系のみ）
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
          onClick={() => setIsAddDialogOpen(true)}
          className="btn-primary"
        >
          変数を追加
        </button>
        <button
          onClick={() => setIsStructTypeDialogOpen(true)}
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
              handleOpenMetaEditDialog(contextMenu.variable);
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

      {/* 変数追加ダイアログ */}
      {isAddDialogOpen && (
        <FocusTrap onConfirm={handleAddVariable}>
          <div className="dialog">
            <h3>変数を追加</h3>
            <div className="dialog-content">
              <div className="dialog-row">
                <label>変数名:</label>
                <input
                  type="text"
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  placeholder="例: Motor_Speed"
                  autoFocus
                />
              </div>
              <div className="dialog-row">
                <label>型カテゴリ:</label>
                <select
                  value={newTypeCategory}
                  onChange={(e) => {
                    const cat = e.target.value as "scalar" | "array" | "struct";
                    setNewTypeCategory(cat);
                    if (cat === "scalar") setNewDataType("INT");
                    else if (cat === "struct" && structTypes.length > 0)
                      setNewDataType(structTypes[0].name);
                  }}
                >
                  <option value="scalar">スカラー型</option>
                  <option value="array">配列型</option>
                  {structTypes.length > 0 && (
                    <option value="struct">構造体型</option>
                  )}
                </select>
              </div>

              {newTypeCategory === "scalar" && (
                <>
                  <div className="dialog-row">
                    <label>データ型:</label>
                    <select
                      value={newDataType}
                      onChange={(e) => {
                        setNewDataType(e.target.value);
                        setNewValue(getDefaultValue(e.target.value));
                      }}
                    >
                      {dataTypes.map((t) => (
                        <option key={t.id} value={t.id} title={t.description}>
                          {t.displayName} ({t.description})
                        </option>
                      ))}
                      <option value="STRING">STRING (文字列)</option>
                    </select>
                  </div>
                  {newDataType === "STRING" && (
                    <div className="dialog-row">
                      <label>バイト長:</label>
                      <input
                        type="number"
                        value={newStringLength}
                        onChange={(e) =>
                          setNewStringLength(parseInt(e.target.value) || 1)
                        }
                        min={1}
                        max={256}
                      />
                    </div>
                  )}
                  <div className="dialog-row">
                    <label>初期値:</label>
                    <input
                      type="text"
                      value={newValue}
                      onChange={(e) => setNewValue(e.target.value)}
                      placeholder={getDefaultValue(newDataType)}
                    />
                  </div>
                </>
              )}

              {newTypeCategory === "array" && (
                <>
                  <div className="dialog-row">
                    <label>要素型:</label>
                    <select
                      value={newArrayElemType}
                      onChange={(e) => {
                        const val = e.target.value;
                        setNewArrayElemType(val);
                        setNewArrayElemCategory(
                          structTypes.some((st) => st.name === val)
                            ? "struct"
                            : "scalar",
                        );
                      }}
                    >
                      <optgroup label="スカラー型">
                        {dataTypes.map((t) => (
                          <option key={t.id} value={t.id}>
                            {t.displayName}
                          </option>
                        ))}
                        <option value="STRING">STRING</option>
                      </optgroup>
                      {structTypes.length > 0 && (
                        <optgroup label="構造体型">
                          {structTypes.map((st) => (
                            <option key={st.name} value={st.name}>
                              {st.name} ({st.wordCount}W)
                            </option>
                          ))}
                        </optgroup>
                      )}
                    </select>
                  </div>
                  {newArrayElemCategory === "scalar" &&
                    newArrayElemType === "STRING" && (
                      <div className="dialog-row">
                        <label>バイト長:</label>
                        <input
                          type="number"
                          value={newStringLength}
                          onChange={(e) =>
                            setNewStringLength(parseInt(e.target.value) || 1)
                          }
                          min={1}
                          max={256}
                        />
                      </div>
                    )}
                  <div className="dialog-row">
                    <label>次元数:</label>
                    <select
                      value={newDimCount}
                      onChange={(e) => setNewDimCount(parseInt(e.target.value))}
                    >
                      <option value={1}>1</option>
                      <option value={2}>2</option>
                      <option value={3}>3</option>
                    </select>
                  </div>
                  {[0, 1, 2].slice(0, newDimCount).map((i) => (
                    <React.Fragment key={i}>
                      <div className="dialog-row">
                        <label>開始インデックス({i + 1}次元目):</label>
                        <input
                          type="number"
                          value={newDimBounds[i].lower}
                          onChange={(e) => {
                            const val = parseInt(e.target.value);
                            if (isNaN(val)) return;
                            setNewDimBounds((prev) => {
                              const next = [...prev];
                              next[i] = { ...next[i], lower: val };
                              return next;
                            });
                          }}
                        />
                      </div>
                      <div className="dialog-row">
                        <label>終了インデックス({i + 1}次元目):</label>
                        <input
                          type="number"
                          value={newDimBounds[i].upper}
                          onChange={(e) => {
                            const val = parseInt(e.target.value);
                            if (isNaN(val)) return;
                            setNewDimBounds((prev) => {
                              const next = [...prev];
                              next[i] = { ...next[i], upper: val };
                              return next;
                            });
                          }}
                        />
                      </div>
                    </React.Fragment>
                  ))}
                </>
              )}

              {newTypeCategory === "struct" && (
                <div className="dialog-row">
                  <label>構造体型:</label>
                  <select
                    value={newDataType}
                    onChange={(e) => setNewDataType(e.target.value)}
                  >
                    {structTypes.map((st) => (
                      <option key={st.name} value={st.name}>
                        {st.name} ({st.wordCount}ワード)
                      </option>
                    ))}
                  </select>
                </div>
              )}
            </div>
            <div className="dialog-buttons">
              <button
                onClick={() => setIsAddDialogOpen(false)}
                className="btn-secondary"
              >
                キャンセル
              </button>
              <button onClick={handleAddVariable} className="btn-primary">
                追加
              </button>
            </div>
          </div>
        </FocusTrap>
      )}

      {/* 変数メタデータ編集ダイアログ（名前・データタイプ変更） */}
      {isMetaEditDialogOpen && (
        <FocusTrap onConfirm={handleSaveMetaEdit}>
          <div className="dialog">
            <h3>変数を編集</h3>
            <div className="dialog-content">
              <div className="dialog-row">
                <label>変数名:</label>
                <input
                  type="text"
                  value={metaEditName}
                  onChange={(e) => setMetaEditName(e.target.value)}
                  autoFocus
                />
              </div>
              <div className="dialog-row">
                <label>型カテゴリ:</label>
                <select
                  value={metaEditTypeCategory}
                  onChange={(e) => {
                    const cat = e.target.value as "scalar" | "array" | "struct";
                    setMetaEditTypeCategory(cat);
                    if (cat === "scalar") setMetaEditDataType("INT");
                    else if (cat === "struct" && structTypes.length > 0)
                      setMetaEditDataType(structTypes[0].name);
                  }}
                >
                  <option value="scalar">スカラー型</option>
                  <option value="array">配列型</option>
                  {structTypes.length > 0 && (
                    <option value="struct">構造体型</option>
                  )}
                </select>
              </div>

              {metaEditTypeCategory === "scalar" && (
                <>
                  <div className="dialog-row">
                    <label>データ型:</label>
                    <select
                      value={metaEditDataType}
                      onChange={(e) => setMetaEditDataType(e.target.value)}
                    >
                      {dataTypes.map((t) => (
                        <option key={t.id} value={t.id} title={t.description}>
                          {t.displayName} ({t.description})
                        </option>
                      ))}
                      <option value="STRING">STRING (文字列)</option>
                    </select>
                  </div>
                  {metaEditDataType === "STRING" && (
                    <div className="dialog-row">
                      <label>バイト長:</label>
                      <input
                        type="number"
                        value={metaEditStringLength}
                        onChange={(e) =>
                          setMetaEditStringLength(parseInt(e.target.value) || 1)
                        }
                        min={1}
                        max={256}
                      />
                    </div>
                  )}
                </>
              )}

              {metaEditTypeCategory === "array" && (
                <>
                  <div className="dialog-row">
                    <label>要素型:</label>
                    <select
                      value={metaEditArrayElemType}
                      onChange={(e) => {
                        const val = e.target.value;
                        setMetaEditArrayElemType(val);
                        setMetaEditArrayElemCategory(
                          structTypes.some((st) => st.name === val)
                            ? "struct"
                            : "scalar",
                        );
                      }}
                    >
                      <optgroup label="スカラー型">
                        {dataTypes.map((t) => (
                          <option key={t.id} value={t.id}>
                            {t.displayName}
                          </option>
                        ))}
                        <option value="STRING">STRING</option>
                      </optgroup>
                      {structTypes.length > 0 && (
                        <optgroup label="構造体型">
                          {structTypes.map((st) => (
                            <option key={st.name} value={st.name}>
                              {st.name} ({st.wordCount}W)
                            </option>
                          ))}
                        </optgroup>
                      )}
                    </select>
                  </div>
                  {metaEditArrayElemCategory === "scalar" &&
                    metaEditArrayElemType === "STRING" && (
                      <div className="dialog-row">
                        <label>バイト長:</label>
                        <input
                          type="number"
                          value={metaEditStringLength}
                          onChange={(e) =>
                            setMetaEditStringLength(
                              parseInt(e.target.value) || 1,
                            )
                          }
                          min={1}
                          max={256}
                        />
                      </div>
                    )}
                  <div className="dialog-row">
                    <label>次元数:</label>
                    <select
                      value={metaEditDimCount}
                      onChange={(e) =>
                        setMetaEditDimCount(parseInt(e.target.value))
                      }
                    >
                      <option value={1}>1</option>
                      <option value={2}>2</option>
                      <option value={3}>3</option>
                    </select>
                  </div>
                  {[0, 1, 2].slice(0, metaEditDimCount).map((i) => (
                    <React.Fragment key={i}>
                      <div className="dialog-row">
                        <label>開始インデックス({i + 1}次元目):</label>
                        <input
                          type="number"
                          value={metaEditDimBounds[i].lower}
                          onChange={(e) => {
                            const val = parseInt(e.target.value);
                            if (isNaN(val)) return;
                            setMetaEditDimBounds((prev) => {
                              const next = [...prev];
                              next[i] = { ...next[i], lower: val };
                              return next;
                            });
                          }}
                        />
                      </div>
                      <div className="dialog-row">
                        <label>終了インデックス({i + 1}次元目):</label>
                        <input
                          type="number"
                          value={metaEditDimBounds[i].upper}
                          onChange={(e) => {
                            const val = parseInt(e.target.value);
                            if (isNaN(val)) return;
                            setMetaEditDimBounds((prev) => {
                              const next = [...prev];
                              next[i] = { ...next[i], upper: val };
                              return next;
                            });
                          }}
                        />
                      </div>
                    </React.Fragment>
                  ))}
                </>
              )}

              {metaEditTypeCategory === "struct" && (
                <div className="dialog-row">
                  <label>構造体型:</label>
                  <select
                    value={metaEditDataType}
                    onChange={(e) => setMetaEditDataType(e.target.value)}
                  >
                    {structTypes.map((st) => (
                      <option key={st.name} value={st.name}>
                        {st.name} ({st.wordCount}ワード)
                      </option>
                    ))}
                  </select>
                </div>
              )}

              <p
                style={{ fontSize: "0.85em", color: "#aaa", margin: "8px 0 0" }}
              >
                ※ データタイプを変更すると値はデフォルト値にリセットされます
              </p>
            </div>
            <div className="dialog-buttons">
              <button
                onClick={() => setIsMetaEditDialogOpen(false)}
                className="btn-secondary"
              >
                キャンセル
              </button>
              <button onClick={handleSaveMetaEdit} className="btn-primary">
                保存
              </button>
            </div>
          </div>
        </FocusTrap>
      )}

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
                          {pub.enabled && mappingVariable && (
                            <span style={{ fontSize: "11px", color: "#888" }}>
                              ns=1;s={mappingVariable.name}
                            </span>
                          )}
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
                /* OPC UA 系テーブル */
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
                          {row.nodeEnabled ? `ns=1;s=${row.variableName}` : "-"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : (
                /* Modbus 系テーブル */
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
      {/* 構造体型管理ダイアログ */}
      {isStructTypeDialogOpen && (
        <FocusTrap onConfirm={handleRegisterStructType}>
          <div
            className="dialog"
            style={{
              minWidth: "500px",
              maxHeight: "80vh",
              display: "flex",
              flexDirection: "column",
            }}
          >
            <h3>構造体型管理</h3>
            <div
              className="dialog-content"
              style={{ flex: 1, overflowY: "auto" }}
            >
              {/* 既存の構造体型一覧 */}
              {structTypes.length > 0 && (
                <div className="dialog-section">
                  <h4 className="dialog-section-title">登録済み構造体型</h4>
                  <table className="variable-table">
                    <thead>
                      <tr>
                        <th>名前</th>
                        <th>フィールド</th>
                        <th>ワード数</th>
                        <th>操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {structTypes.map((st) => (
                        <tr key={st.name}>
                          <td>{st.name}</td>
                          <td>
                            {st.fields
                              .map((f) => `${f.name}:${f.dataType}`)
                              .join(", ")}
                          </td>
                          <td>{st.wordCount}</td>
                          <td>
                            <button
                              onClick={() => handleEditStructType(st)}
                              className="btn-small btn-secondary"
                              style={{ marginRight: "0.5rem" }}
                            >
                              編集
                            </button>
                            <button
                              onClick={() => handleDeleteStructType(st.name)}
                              className="btn-small btn-danger"
                            >
                              削除
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {/* 新規構造体型登録 */}
              <h4 className="dialog-section-title">
                {editingStructTypeName ? "構造体型を編集" : "新規構造体型"}
              </h4>
              <div className="dialog-row">
                <label>型名:</label>
                <input
                  type="text"
                  value={structTypeName}
                  onChange={(e) => setStructTypeName(e.target.value)}
                  placeholder="例: MotorData"
                  disabled={!!editingStructTypeName}
                />
              </div>
              <div className="dialog-section">
                <label>フィールド:</label>
                {structTypeFields.map((field, index) => (
                  <div key={index} className="dialog-field-editor">
                    <div
                      style={{
                        display: "flex",
                        justifyContent: "space-between",
                        marginBottom: "0.5rem",
                      }}
                    >
                      <strong>フィールド {index + 1}</strong>
                      <button
                        onClick={() =>
                          setStructTypeFields(
                            structTypeFields.filter((_, i) => i !== index),
                          )
                        }
                        className="btn-small btn-danger"
                        disabled={structTypeFields.length <= 1}
                      >
                        X
                      </button>
                    </div>

                    <div className="dialog-row">
                      <label>名前:</label>
                      <input
                        type="text"
                        value={field.name}
                        onChange={(e) => {
                          const updated = [...structTypeFields];
                          updated[index] = {
                            ...updated[index],
                            name: e.target.value,
                          };
                          setStructTypeFields(updated);
                        }}
                        placeholder="フィールド名"
                      />
                    </div>

                    <div className="dialog-row">
                      <label>型カテゴリ:</label>
                      <select
                        value={field.category}
                        onChange={(e) => {
                          const updated = [...structTypeFields];
                          const cat = e.target.value as
                            | "scalar"
                            | "struct"
                            | "array";
                          updated[index] = {
                            ...updated[index],
                            category: cat,
                            dataType:
                              cat === "struct"
                                ? structTypes.length > 0
                                  ? structTypes[0].name
                                  : ""
                                : "INT",
                          };
                          setStructTypeFields(updated);
                        }}
                      >
                        <option value="scalar">スカラー</option>
                        <option value="array">配列</option>
                        {structTypes.length > 0 && (
                          <option value="struct">構造体</option>
                        )}
                      </select>
                    </div>

                    {field.category === "scalar" && (
                      <>
                        <div className="dialog-row">
                          <label>データ型:</label>
                          <select
                            value={field.dataType}
                            onChange={(e) => {
                              const updated = [...structTypeFields];
                              updated[index] = {
                                ...updated[index],
                                dataType: e.target.value,
                              };
                              setStructTypeFields(updated);
                            }}
                          >
                            {dataTypes.map((t) => (
                              <option
                                key={t.id}
                                value={t.id}
                                title={t.description}
                              >
                                {t.displayName} ({t.description})
                              </option>
                            ))}
                            <option value="STRING">STRING (文字列)</option>
                          </select>
                        </div>
                        {field.dataType === "STRING" && (
                          <div className="dialog-row">
                            <label>バイト長:</label>
                            <input
                              type="number"
                              value={field.stringLength}
                              onChange={(e) => {
                                const updated = [...structTypeFields];
                                updated[index] = {
                                  ...updated[index],
                                  stringLength: parseInt(e.target.value) || 1,
                                };
                                setStructTypeFields(updated);
                              }}
                              min={1}
                              max={256}
                            />
                          </div>
                        )}
                      </>
                    )}

                    {field.category === "struct" && (
                      <div className="dialog-row">
                        <label>構造体型:</label>
                        <select
                          value={field.dataType}
                          onChange={(e) => {
                            const updated = [...structTypeFields];
                            updated[index] = {
                              ...updated[index],
                              dataType: e.target.value,
                            };
                            setStructTypeFields(updated);
                          }}
                        >
                          {structTypes
                            .filter((st) => st.name !== structTypeName.trim())
                            .map((st) => (
                              <option key={st.name} value={st.name}>
                                {st.name} ({st.wordCount}W)
                              </option>
                            ))}
                        </select>
                      </div>
                    )}

                    {field.category === "array" && (
                      <>
                        <div className="dialog-row">
                          <label>要素型:</label>
                          <select
                            value={field.arrayElemType}
                            onChange={(e) => {
                              const updated = [...structTypeFields];
                              const val = e.target.value;
                              updated[index] = {
                                ...updated[index],
                                arrayElemType: val,
                                arrayElemCategory: structTypes
                                  .filter(
                                    (st) => st.name !== structTypeName.trim(),
                                  )
                                  .some((st) => st.name === val)
                                  ? "struct"
                                  : "scalar",
                              };
                              setStructTypeFields(updated);
                            }}
                          >
                            <optgroup label="スカラー型">
                              {dataTypes.map((t) => (
                                <option key={t.id} value={t.id}>
                                  {t.displayName}
                                </option>
                              ))}
                              <option value="STRING">STRING</option>
                            </optgroup>
                            {structTypes.filter(
                              (st) => st.name !== structTypeName.trim(),
                            ).length > 0 && (
                              <optgroup label="構造体型">
                                {structTypes
                                  .filter(
                                    (st) => st.name !== structTypeName.trim(),
                                  )
                                  .map((st) => (
                                    <option key={st.name} value={st.name}>
                                      {st.name} ({st.wordCount}W)
                                    </option>
                                  ))}
                              </optgroup>
                            )}
                          </select>
                        </div>
                        {field.arrayElemCategory === "scalar" &&
                          field.arrayElemType === "STRING" && (
                            <div className="dialog-row">
                              <label>バイト長:</label>
                              <input
                                type="number"
                                value={field.stringLength}
                                onChange={(e) => {
                                  const updated = [...structTypeFields];
                                  updated[index] = {
                                    ...updated[index],
                                    stringLength: parseInt(e.target.value) || 1,
                                  };
                                  setStructTypeFields(updated);
                                }}
                                min={1}
                                max={256}
                              />
                            </div>
                          )}
                        <div className="dialog-row">
                          <label>次元数:</label>
                          <select
                            value={field.arrayDimCount}
                            onChange={(e) => {
                              const updated = [...structTypeFields];
                              updated[index] = {
                                ...updated[index],
                                arrayDimCount: parseInt(e.target.value),
                              };
                              setStructTypeFields(updated);
                            }}
                          >
                            <option value={1}>1</option>
                            <option value={2}>2</option>
                            <option value={3}>3</option>
                          </select>
                        </div>
                        {([0, 1, 2] as const)
                          .slice(0, field.arrayDimCount)
                          .map((dimIdx) => (
                            <React.Fragment key={dimIdx}>
                              <div className="dialog-row">
                                <label>
                                  開始インデックス({dimIdx + 1}次元目):
                                </label>
                                <input
                                  type="number"
                                  value={field.arrayDimBounds[dimIdx].lower}
                                  onChange={(e) => {
                                    const val = parseInt(e.target.value);
                                    if (isNaN(val)) return;
                                    const updated = [...structTypeFields];
                                    const newBounds = [
                                      ...updated[index].arrayDimBounds,
                                    ] as typeof field.arrayDimBounds;
                                    newBounds[dimIdx] = {
                                      ...newBounds[dimIdx],
                                      lower: val,
                                    };
                                    updated[index] = {
                                      ...updated[index],
                                      arrayDimBounds: newBounds,
                                    };
                                    setStructTypeFields(updated);
                                  }}
                                />
                              </div>
                              <div className="dialog-row">
                                <label>
                                  終了インデックス({dimIdx + 1}次元目):
                                </label>
                                <input
                                  type="number"
                                  value={field.arrayDimBounds[dimIdx].upper}
                                  onChange={(e) => {
                                    const val = parseInt(e.target.value);
                                    if (isNaN(val)) return;
                                    const updated = [...structTypeFields];
                                    const newBounds = [
                                      ...updated[index].arrayDimBounds,
                                    ] as typeof field.arrayDimBounds;
                                    newBounds[dimIdx] = {
                                      ...newBounds[dimIdx],
                                      upper: val,
                                    };
                                    updated[index] = {
                                      ...updated[index],
                                      arrayDimBounds: newBounds,
                                    };
                                    setStructTypeFields(updated);
                                  }}
                                />
                              </div>
                            </React.Fragment>
                          ))}
                      </>
                    )}
                  </div>
                ))}
                <button
                  onClick={() =>
                    setStructTypeFields([
                      ...structTypeFields,
                      {
                        name: "",
                        category: "scalar",
                        dataType: "INT",
                        stringLength: 20,
                        arrayElemType: "INT",
                        arrayElemCategory: "scalar",
                        arrayDimCount: 1,
                        arrayDimBounds: [
                          { lower: 0, upper: 9 },
                          { lower: 0, upper: 4 },
                          { lower: 0, upper: 4 },
                        ],
                      },
                    ])
                  }
                  className="btn-secondary"
                  style={{ marginTop: "0.5rem" }}
                >
                  + フィールド追加
                </button>
              </div>
            </div>
            <div className="dialog-buttons">
              <button
                onClick={() => {
                  setIsStructTypeDialogOpen(false);
                  setEditingStructTypeName(null);
                  setStructTypeName("");
                  setStructTypeFields([
                    {
                      name: "",
                      category: "scalar",
                      dataType: "INT",
                      stringLength: 20,
                      arrayElemType: "INT",
                      arrayElemCategory: "scalar",
                      arrayDimCount: 1,
                      arrayDimBounds: [
                        { lower: 0, upper: 9 },
                        { lower: 0, upper: 4 },
                        { lower: 0, upper: 4 },
                      ],
                    },
                  ]);
                }}
                className="btn-secondary"
              >
                閉じる
              </button>
              <button
                onClick={handleRegisterStructType}
                className="btn-primary"
              >
                {editingStructTypeName ? "更新" : "型を登録"}
              </button>
            </div>
          </div>
        </FocusTrap>
      )}
    </div>
  );
}
