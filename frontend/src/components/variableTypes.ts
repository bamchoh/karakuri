// 変数のデータ型・値に関する純粋関数（コンポーネントの state に依存しない）
import { application } from "../../wailsjs/go/models";

// スカラー値のフォーマット
export const formatScalarValue = (value: any, dataType: string): string => {
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

// 配列型かどうか判定（IEC 61131-3 形式および旧形式に対応）
export const isArrayType = (dataType: string): boolean => {
  if (!dataType.startsWith("ARRAY[")) return false;
  return dataType.includes("] OF ") || dataType.endsWith("]");
};

// 配列型文字列をパースして最初の次元の { elemType, size, lower, upper } を返す
// IEC 61131-3 形式: "ARRAY[1..10] OF INT" → { elemType: "INT", size: 10, lower: 1, upper: 10 }
//   多次元:          "ARRAY[0..2, 0..4] OF INT" → { elemType: "ARRAY[0..4] OF INT", size: 3, lower: 0, upper: 2 }
// 旧形式（後方互換）: "ARRAY[INT;10]"  → { elemType: "INT", size: 10, lower: 0, upper: 9 }
export const parseArrayTypeFE = (
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
export const parseAllDimsFE = (
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
export const buildArrayTypeFE = (
  baseElemType: string,
  dims: Array<{ lower: number; upper: number }>,
): string => {
  const dimsStr = dims.map((d) => `${d.lower}..${d.upper}`).join(", ");
  return `ARRAY[${dimsStr}] OF ${baseElemType}`;
};

// 構造体型かどうか判定
export const isStructType = (dataType: string): boolean => {
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

// デフォルト値を取得
export const getDefaultValue = (dataType: string): string => {
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
export const parseValue = (input: string, dataType: string): any => {
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

// 文字列として保持されているREAL/LREAL値を数値に変換
export const coerceFloatIfNeeded = (value: any, dataType: string): any => {
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
export const valuePathToFieldPath = (valuePath: (string | number)[]): string => {
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

// 数値入力のパース（接頭辞で自動判定）
export const parseNumericInput = (input: string): number => {
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
export const parseBigIntInput = (input: string, dataType: string): string | null => {
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
export const formatNumeric = (
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

// データ型のワード数を取得
export const getWordCount = (
  dataType: string,
  dataTypes: application.DataTypeInfoDTO[],
  structTypes: application.StructTypeDTO[],
): number => {
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
    return getWordCount(arr.elemType, dataTypes, structTypes) * arr.size;
  }
  return 1;
};

// 構造体のデフォルト値を再帰的に生成
export const generateStructDefault = (
  typeName: string,
  structTypes: application.StructTypeDTO[],
): any => {
  const st = structTypes.find((s) => s.name === typeName);
  if (!st) return {};
  const obj: Record<string, any> = {};
  for (const f of st.fields) {
    const arr = parseArrayTypeFE(f.dataType);
    if (arr) {
      obj[f.name] = Array.from({ length: arr.size }, () =>
        isStructType(arr.elemType)
          ? generateStructDefault(arr.elemType, structTypes)
          : parseValue(getDefaultValue(arr.elemType), arr.elemType),
      );
    } else if (isStructType(f.dataType)) {
      obj[f.name] = generateStructDefault(f.dataType, structTypes);
    } else {
      obj[f.name] = parseValue(getDefaultValue(f.dataType), f.dataType);
    }
  }
  return obj;
};

// カテゴリ・要素型・STRING長からベース要素型文字列を解決するヘルパー
export const resolveBaseElemType = (
  category: "scalar" | "struct",
  elemType: string,
  stringLength: number,
): string => {
  if (category === "struct") return elemType;
  return elemType === "STRING" ? `STRING[${stringLength}]` : elemType;
};

// 任意データ型のデフォルト値を再帰生成するヘルパー
export const generateDefaultForType = (
  dataType: string,
  structTypes: application.StructTypeDTO[],
): any => {
  const arr = parseArrayTypeFE(dataType);
  if (arr) {
    return Array.from({ length: arr.size }, () =>
      generateDefaultForType(arr.elemType, structTypes),
    );
  }
  if (isStructType(dataType)) return generateStructDefault(dataType, structTypes);
  return parseValue(getDefaultValue(dataType), dataType);
};
