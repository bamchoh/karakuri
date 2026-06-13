import { useState, useEffect, useCallback, useRef } from "react";
import { FocusTrap } from "./FocusTrap";
import {
  GetServerInstances,
  GetMemoryAreas,
  ReadBits,
  ReadWords,
  WriteBit,
  WriteWord,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { application } from "../../wailsjs/go/models";
import { MonitoringView } from "./MonitoringView";

export type RegisterTab = "list" | "monitoring";

type DisplayFormat = "decimal" | "hex" | "octal" | "binary";
type BitWidth = 16 | 32 | 64;
type Endianness = "little" | "big";

const DISPLAY_FORMATS: { value: DisplayFormat; label: string }[] = [
  { value: "decimal", label: "10進数" },
  { value: "hex", label: "16進数" },
  { value: "octal", label: "8進数" },
  { value: "binary", label: "2進数" },
];

const BIT_WIDTHS: { value: BitWidth; label: string; wordCount: number }[] = [
  { value: 16, label: "16bit", wordCount: 1 },
  { value: 32, label: "32bit", wordCount: 2 },
  { value: 64, label: "64bit", wordCount: 4 },
];

const ENDIANNESS_OPTIONS: { value: Endianness; label: string }[] = [
  { value: "big", label: "ビッグ(BE)" },
  { value: "little", label: "リトル(LE)" },
];

// ビット幅に応じたワード数を取得
const getWordCount = (bitWidth: BitWidth): number => {
  return BIT_WIDTHS.find((b) => b.value === bitWidth)?.wordCount ?? 1;
};

// 複数ワードを結合して数値に変換
const combineWords = (words: number[], endianness: Endianness): bigint => {
  if (words.length === 0) return BigInt(0);

  // ビッグエンディアン: 最初のワードが上位
  // リトルエンディアン: 最初のワードが下位
  const orderedWords = endianness === "little" ? words : [...words].reverse();

  let result = BigInt(0);
  for (let i = orderedWords.length - 1; i >= 0; i--) {
    result = (result << BigInt(16)) | BigInt(orderedWords[i] & 0xffff);
  }
  return result;
};

// bigintを指定形式でフォーマット
const formatBigInt = (
  value: bigint,
  format: DisplayFormat,
  bitWidth: BitWidth,
): string => {
  const isNegative = value < 0;
  const absValue = isNegative ? -value : value;

  switch (format) {
    case "hex": {
      const hexDigits = bitWidth / 4;
      return (
        "0x" + absValue.toString(16).toUpperCase().padStart(hexDigits, "0")
      );
    }
    case "octal": {
      return "0o" + absValue.toString(8);
    }
    case "binary": {
      return absValue.toString(2).padStart(bitWidth, "0");
    }
    default:
      return value.toString();
  }
};

// bigintを複数ワードに分解
const splitToWords = (
  value: bigint,
  wordCount: number,
  endianness: Endianness,
): number[] => {
  const words: number[] = [];
  let remaining = value < 0 ? -value : value;
  const mask = BigInt(0xffff);

  for (let i = 0; i < wordCount; i++) {
    words.push(Number(remaining & mask));
    remaining = remaining >> BigInt(16);
  }

  // ビッグエンディアンの場合は逆順にする
  return endianness === "little" ? words : words.reverse();
};

// 文字列入力をbigintにパース
const parseBigIntInput = (input: string, format: DisplayFormat): bigint => {
  const trimmed = input.trim();
  switch (format) {
    case "hex": {
      const hexStr = trimmed.replace(/^0x/i, "");
      return BigInt("0x" + hexStr);
    }
    case "octal": {
      const octStr = trimmed.replace(/^0o/i, "");
      return BigInt("0o" + octStr);
    }
    case "binary": {
      const binStr = trimmed.replace(/^0b/i, "");
      return BigInt("0b" + binStr);
    }
    default:
      return BigInt(trimmed);
  }
};

const COLUMNS = 10;
const PAGE_SIZE = 100;

interface RegisterPanelProps {
  activeSubTab: RegisterTab;
  onSubTabChange: (tab: RegisterTab) => void;
}

export function RegisterPanel({
  activeSubTab,
  onSubTabChange,
}: RegisterPanelProps) {
  // サブタブ
  const activeTab = activeSubTab;
  const setActiveTab = onSubTabChange;

  // サーバーインスタンス
  const [serverInstances, setServerInstances] = useState<
    application.ServerInstanceDTO[]
  >([]);
  const [selectedProtocol, setSelectedProtocol] = useState<string>("");

  const [memoryAreas, setMemoryAreas] = useState<application.MemoryAreaDTO[]>(
    [],
  );
  const [selectedArea, setSelectedArea] = useState<string>("");
  const [startAddress, setStartAddress] = useState(0);
  const [values, setValues] = useState<(boolean | number)[]>([]);
  const [displayFormat, setDisplayFormat] = useState<DisplayFormat>("decimal");
  const [bitWidth, setBitWidth] = useState<BitWidth>(16);
  const [endianness, setEndianness] = useState<Endianness>("big");

  // カーソル（選択セル）の状態
  const [selectedIndex, setSelectedIndex] = useState<number>(0);
  const tableContainerRef = useRef<HTMLDivElement>(null);
  const dialogInputRef = useRef<HTMLInputElement>(null);

  // モーダルダイアログの状態
  const [isDialogOpen, setIsDialogOpen] = useState(false);
  const [editingAddress, setEditingAddress] = useState<number>(0);
  const [editingOriginalValue, setEditingOriginalValue] = useState<
    boolean | number
  >(0);
  const [editInputFormat, setEditInputFormat] =
    useState<DisplayFormat>("decimal");
  const [editValue, setEditValue] = useState("");

  // サーバーインスタンスをポーリング
  useEffect(() => {
    const loadInstances = async () => {
      try {
        const instances = await GetServerInstances();
        setServerInstances(instances || []);
        const registerInstances = (instances || []).filter(
          (i) => !i.supportsNodePublishing,
        );
        if (registerInstances.length > 0) {
          setSelectedProtocol((prev) => {
            if (
              !prev ||
              !registerInstances.find((i) => i.protocolType === prev)
            ) {
              return registerInstances[0].protocolType;
            }
            return prev;
          });
        }
      } catch (e) {
        console.error("Failed to load server instances:", e);
      }
    };
    loadInstances();
    const interval = setInterval(loadInstances, 1000);
    return () => clearInterval(interval);
  }, []);

  // メモリエリア一覧を取得（プロトコルが変更されたら再ロード）
  useEffect(() => {
    if (!selectedProtocol) {
      setMemoryAreas([]);
      setSelectedArea("");
      return;
    }

    const loadAreas = async () => {
      try {
        const areas = await GetMemoryAreas(selectedProtocol);
        setMemoryAreas(areas || []);
        // デフォルトで最初のワードエリアを選択
        const defaultArea = areas?.find((a) => !a.isBit) || areas?.[0];
        if (defaultArea) {
          setSelectedArea(defaultArea.id);
        }
      } catch (e) {
        console.error("Failed to load memory areas:", e);
      }
    };
    loadAreas();
  }, [selectedProtocol]);

  useEffect(() => {
    const off = EventsOn("project:imported", () => {
      loadRegisters();
    });

    return off;
  }, []);

  const currentArea = memoryAreas.find((a) => a.id === selectedArea);
  const isBitType = currentArea?.isBit ?? false;
  const isByteAddr = currentArea?.byteAddressing ?? false;
  const isModbusArea = currentArea?.oneOrigin ?? false;

  // ワードアドレスを表示用アドレスに変換する（バイトアドレスは*2、Modbusは1オリジン）
  const toDisplayAddr = (wordAddr: number) => {
    if (isByteAddr) return wordAddr * 2;
    if (isModbusArea) return wordAddr + 1;
    return wordAddr;
  };

  const loadRegisters = useCallback(async () => {
    if (!selectedArea || !selectedProtocol) return;

    try {
      let data: (boolean | number)[];
      if (isBitType) {
        data = await ReadBits(
          selectedProtocol,
          selectedArea,
          startAddress,
          PAGE_SIZE,
        );
      } else {
        data = await ReadWords(
          selectedProtocol,
          selectedArea,
          startAddress,
          PAGE_SIZE,
        );
      }
      setValues(data || []);
    } catch (e) {
      console.error("Failed to load registers:", e);
    }
  }, [selectedProtocol, selectedArea, startAddress, isBitType]);

  useEffect(() => {
    loadRegisters();
  }, [loadRegisters]);

  useEffect(() => {
    const interval = setInterval(loadRegisters, 100);
    return () => clearInterval(interval);
  }, [loadRegisters]);

  // コンポーネントマウント時にテーブルにフォーカスを設定
  useEffect(() => {
    tableContainerRef.current?.focus();
  }, []);

  // ダイアログが開いた時に入力値を全選択
  useEffect(() => {
    if (isDialogOpen && dialogInputRef.current) {
      dialogInputRef.current.focus();
      dialogInputRef.current.select();
    }
  }, [isDialogOpen]);

  // 指定した形式で単一ワードをフォーマット（16bit）
  const formatSingleWord = (value: number, format: DisplayFormat) => {
    switch (format) {
      case "hex":
        return "0x" + value.toString(16).toUpperCase().padStart(4, "0");
      case "octal":
        return "0o" + value.toString(8).padStart(6, "0");
      case "binary":
        return value.toString(2).padStart(16, "0");
      default:
        return value.toString();
    }
  };

  // 指定した形式で値をフォーマット（16bit用）
  const formatValueWithFormat = (
    value: boolean | number,
    format: DisplayFormat,
  ) => {
    if (typeof value === "boolean") {
      return value ? "ON" : "OFF";
    }
    return formatSingleWord(value, format);
  };

  // 複数ワードをまとめてフォーマット
  const formatMultiWordValue = (
    startIndex: number,
    format: DisplayFormat,
    width: BitWidth,
    endian: Endianness,
  ): string => {
    const wordCount = getWordCount(width);
    const words: number[] = [];
    for (let i = 0; i < wordCount; i++) {
      const val = values[startIndex + i];
      if (val === undefined || typeof val === "boolean") {
        return "---";
      }
      words.push(val as number);
    }
    const combined = combineWords(words, endian);
    return formatBigInt(combined, format, width);
  };

  // 表示用フォーマット（現在の表示形式を使用）
  const formatValue = (value: boolean | number, index: number) => {
    if (typeof value === "boolean") {
      return value ? "ON" : "OFF";
    }
    if (bitWidth === 16) {
      return formatValueWithFormat(value, displayFormat);
    }
    return formatMultiWordValue(index, displayFormat, bitWidth, endianness);
  };

  // 入力値をパース
  const parseInputValue = (input: string, format: DisplayFormat): number => {
    const trimmed = input.trim();
    switch (format) {
      case "hex":
        // 0x プレフィックスを除去
        const hexStr = trimmed.replace(/^0x/i, "");
        return parseInt(hexStr, 16);
      case "octal":
        // 0o プレフィックスを除去
        const octStr = trimmed.replace(/^0o/i, "");
        return parseInt(octStr, 8);
      case "binary":
        // 0b プレフィックスを除去
        const binStr = trimmed.replace(/^0b/i, "");
        return parseInt(binStr, 2);
      default:
        return parseInt(trimmed, 10);
    }
  };

  // セルクリック時にダイアログを開く
  const handleCellClick = (index: number) => {
    const address = startAddress + index;
    const value = values[index];
    setEditingAddress(address);
    setEditingOriginalValue(value);
    setEditInputFormat(displayFormat);

    if (typeof value === "boolean") {
      setEditValue(value ? "1" : "0");
    } else if (bitWidth === 16) {
      // 16bit: 現在の表示形式で初期値を設定
      setEditValue(formatValueWithFormat(value, displayFormat));
    } else {
      // 32bit/64bit: マルチワード値を初期値として設定
      setEditValue(
        formatMultiWordValue(index, displayFormat, bitWidth, endianness),
      );
    }
    setIsDialogOpen(true);
  };

  // ダイアログを閉じる
  const handleDialogClose = () => {
    setIsDialogOpen(false);
    // テーブルにフォーカスを戻す
    setTimeout(() => {
      tableContainerRef.current?.focus();
    }, 0);
  };

  // 入力形式変更時に値を変換
  const handleInputFormatChange = (newFormat: DisplayFormat) => {
    if (!isBitType && editValue) {
      try {
        if (bitWidth === 16) {
          const numValue = parseInputValue(editValue, editInputFormat);
          if (!isNaN(numValue)) {
            setEditValue(formatSingleWord(numValue, newFormat));
          }
        } else {
          // 32bit/64bit: bigintで変換
          const bigValue = parseBigIntInput(editValue, editInputFormat);
          setEditValue(formatBigInt(bigValue, newFormat, bitWidth));
        }
      } catch {
        // パース失敗時は値をそのまま
      }
    }
    setEditInputFormat(newFormat);
  };

  // 保存処理
  const handleSave = async () => {
    try {
      if (isBitType) {
        const newValue =
          editValue === "true" ||
          editValue === "1" ||
          editValue.toLowerCase() === "on";
        await WriteBit(
          selectedProtocol,
          selectedArea,
          editingAddress,
          newValue,
        );
      } else if (bitWidth === 16) {
        const newValue = parseInputValue(editValue, editInputFormat);
        if (isNaN(newValue)) {
          console.error("Invalid number format");
          return;
        }
        await WriteWord(
          selectedProtocol,
          selectedArea,
          editingAddress,
          newValue,
        );
      } else {
        // 32bit/64bit: bigintをパースして複数ワードに分解して書き込み
        const bigValue = parseBigIntInput(editValue, editInputFormat);
        const words = splitToWords(bigValue, wordCount, endianness);
        for (let i = 0; i < words.length; i++) {
          await WriteWord(
            selectedProtocol,
            selectedArea,
            editingAddress + i,
            words[i],
          );
        }
      }
      await loadRegisters();
      setIsDialogOpen(false);
      // テーブルにフォーカスを戻す
      setTimeout(() => {
        tableContainerRef.current?.focus();
      }, 0);
    } catch (e) {
      console.error("Failed to set register:", e);
    }
  };

  // ダイアログ内のキーハンドラ
  const handleDialogKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      handleDialogClose();
    }
  };

  // テーブルのキーハンドラ（カーソル移動）
  const handleTableKeyDown = (e: React.KeyboardEvent) => {
    if (isDialogOpen) return;

    const maxIndex = values.length - 1;
    let newIndex = selectedIndex;

    switch (e.key) {
      case "ArrowUp":
        e.preventDefault();
        newIndex = Math.max(0, selectedIndex - COLUMNS);
        break;
      case "ArrowDown":
        e.preventDefault();
        newIndex = Math.min(maxIndex, selectedIndex + COLUMNS);
        break;
      case "ArrowLeft":
        e.preventDefault();
        newIndex = Math.max(0, selectedIndex - 1);
        break;
      case "ArrowRight":
        e.preventDefault();
        newIndex = Math.min(maxIndex, selectedIndex + 1);
        break;
      case "Enter":
        e.preventDefault();
        handleCellClick(selectedIndex);
        return;
      case "Home":
        e.preventDefault();
        newIndex = 0;
        break;
      case "End":
        e.preventDefault();
        newIndex = maxIndex;
        break;
      case "PageUp":
        e.preventDefault();
        newIndex = Math.max(0, selectedIndex - COLUMNS * 5);
        break;
      case "PageDown":
        e.preventDefault();
        newIndex = Math.min(maxIndex, selectedIndex + COLUMNS * 5);
        break;
      default:
        return;
    }

    setSelectedIndex(newIndex);
  };

  // セルクリック時に選択も更新
  const handleCellClickWithSelect = (index: number) => {
    setSelectedIndex(index);
    handleCellClick(index);
  };

  // 現在のビット幅に応じたワード数
  const wordCount = isBitType ? 1 : getWordCount(bitWidth);

  // 実効的な列数（32bit/64bitの場合は列をまとめる）
  const effectiveColumns = Math.max(1, Math.floor(COLUMNS / wordCount));

  const getRows = () => {
    const rows: {
      rowStart: number;
      cells: { index: number; value: boolean | number; wordSpan: number }[];
    }[] = [];
    const step = effectiveColumns * wordCount;

    for (let i = 0; i < values.length; i += step) {
      const rowCells: {
        index: number;
        value: boolean | number;
        wordSpan: number;
      }[] = [];
      for (let j = 0; j < effectiveColumns; j++) {
        const idx = i + j * wordCount;
        if (idx < values.length) {
          rowCells.push({
            index: idx,
            value: values[idx],
            wordSpan: wordCount,
          });
        }
      }
      rows.push({
        rowStart: startAddress + i,
        cells: rowCells,
      });
    }
    return rows;
  };

  return (
    <div className="panel">
      <h2>レジスタ</h2>

      {/* サブタブナビゲーション */}
      <div className="sub-tab-nav">
        <button
          className={`sub-tab-button ${activeTab === "list" ? "active" : ""}`}
          onClick={() => setActiveTab("list")}
        >
          一覧表示
        </button>
        <button
          className={`sub-tab-button ${activeTab === "monitoring" ? "active" : ""}`}
          onClick={() => setActiveTab("monitoring")}
        >
          モニタリング
        </button>
      </div>

      {/* モニタリングタブ */}
      {activeTab === "monitoring" && (
        <MonitoringView serverInstances={serverInstances} />
      )}

      {/* 一覧表示タブ */}
      {activeTab === "list" && (
        <>
          <div className="register-controls">
            {/* プロトコル選択（レジスタを持つサーバーが複数の場合） */}
            {serverInstances.filter((i) => !i.supportsNodePublishing).length >
              1 && (
              <div className="form-group">
                <label>プロトコル</label>
                <select
                  value={selectedProtocol}
                  onChange={(e) => {
                    setSelectedProtocol(e.target.value);
                    setMemoryAreas([]);
                    setSelectedArea("");
                    setValues([]);
                  }}
                >
                  {serverInstances
                    .filter((i) => !i.supportsNodePublishing)
                    .map((inst) => (
                      <option key={inst.protocolType} value={inst.protocolType}>
                        {inst.displayName}
                      </option>
                    ))}
                </select>
              </div>
            )}

            <div className="form-group">
              <label>メモリエリア</label>
              <select
                value={selectedArea}
                onChange={(e) => setSelectedArea(e.target.value)}
              >
                {memoryAreas.map((area) => (
                  <option key={area.id} value={area.id}>
                    {area.displayName}
                  </option>
                ))}
              </select>
            </div>

            <div className="form-group">
              <label>開始アドレス</label>
              <input
                type="number"
                min={isModbusArea ? "1" : "0"}
                max="65535"
                value={isModbusArea ? startAddress + 1 : startAddress}
                onChange={(e) => {
                  const v = parseInt(e.target.value) || (isModbusArea ? 1 : 0);
                  setStartAddress(isModbusArea ? Math.max(0, v - 1) : v);
                }}
              />
            </div>

            {!isBitType && (
              <>
                <div className="form-group">
                  <label>表示形式</label>
                  <select
                    value={displayFormat}
                    onChange={(e) =>
                      setDisplayFormat(e.target.value as DisplayFormat)
                    }
                  >
                    {DISPLAY_FORMATS.map((f) => (
                      <option key={f.value} value={f.value}>
                        {f.label}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="form-group">
                  <label>ビット幅</label>
                  <select
                    value={bitWidth}
                    onChange={(e) =>
                      setBitWidth(parseInt(e.target.value) as BitWidth)
                    }
                  >
                    {BIT_WIDTHS.map((b) => (
                      <option key={b.value} value={b.value}>
                        {b.label}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="form-group">
                  <label>エンディアン</label>
                  <select
                    value={endianness}
                    onChange={(e) =>
                      setEndianness(e.target.value as Endianness)
                    }
                  >
                    {ENDIANNESS_OPTIONS.map((e) => (
                      <option key={e.value} value={e.value}>
                        {e.label}
                      </option>
                    ))}
                  </select>
                </div>
              </>
            )}
          </div>

          <div
            className="register-matrix-container"
            ref={tableContainerRef}
            tabIndex={0}
            onKeyDown={handleTableKeyDown}
          >
            <table className="register-matrix">
              <thead>
                <tr>
                  <th className="row-header"></th>
                  {Array.from({ length: effectiveColumns }, (_, i) => (
                    <th key={i} className="col-header" colSpan={wordCount}>
                      +{toDisplayAddr(i * wordCount)}
                      {wordCount > 1
                        ? `~+${toDisplayAddr(i * wordCount + wordCount - 1)}`
                        : ""}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {getRows().map((row) => (
                  <tr key={row.rowStart}>
                    <td className="row-header">
                      {toDisplayAddr(row.rowStart)}
                    </td>
                    {row.cells.map((cell) => (
                      <td
                        key={cell.index}
                        colSpan={cell.wordSpan}
                        onClick={() => handleCellClickWithSelect(cell.index)}
                        className={`matrix-cell ${typeof cell.value === "boolean" && cell.value ? "cell-on" : ""} ${selectedIndex === cell.index ? "cell-selected" : ""}`}
                      >
                        <span className="matrix-value">
                          {formatValue(cell.value, cell.index)}
                        </span>
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="register-navigation">
            <button
              onClick={() =>
                setStartAddress(Math.max(0, startAddress - PAGE_SIZE))
              }
              disabled={startAddress === 0}
              className="btn-secondary"
            >
              前へ
            </button>
            <span>
              {toDisplayAddr(startAddress)} -{" "}
              {toDisplayAddr(startAddress + PAGE_SIZE - 1)}
            </span>
            <button
              onClick={() => setStartAddress(startAddress + PAGE_SIZE)}
              className="btn-secondary"
            >
              次へ
            </button>
          </div>

          {/* 書き込みダイアログ */}
          {isDialogOpen && (
            <FocusTrap onConfirm={handleSave}>
              <div className="dialog">
                <h3>レジスタ書き込み</h3>

                <div className="dialog-content">
                  <div className="dialog-row">
                    <label>アドレス:</label>
                    <span className="dialog-value">
                      {toDisplayAddr(editingAddress)}
                    </span>
                  </div>

                  <div className="dialog-row">
                    <label>現在の値:</label>
                    <span className="dialog-value">
                      {formatValue(
                        editingOriginalValue,
                        editingAddress - startAddress,
                      )}
                    </span>
                  </div>

                  {!isBitType && (
                    <div className="dialog-row">
                      <label>入力形式:</label>
                      <select
                        value={editInputFormat}
                        onChange={(e) =>
                          handleInputFormatChange(
                            e.target.value as DisplayFormat,
                          )
                        }
                      >
                        {DISPLAY_FORMATS.map((f) => (
                          <option key={f.value} value={f.value}>
                            {f.label}
                          </option>
                        ))}
                      </select>
                    </div>
                  )}

                  <div className="dialog-row">
                    <label>新しい値:</label>
                    <input
                      ref={dialogInputRef}
                      type="text"
                      value={editValue}
                      onChange={(e) => setEditValue(e.target.value)}
                      onKeyDown={handleDialogKeyDown}
                      className="dialog-input"
                      placeholder={isBitType ? "0, 1, ON, OFF" : ""}
                    />
                  </div>
                </div>

                <div className="dialog-buttons">
                  <button onClick={handleDialogClose} className="btn-secondary">
                    キャンセル
                  </button>
                  <button onClick={handleSave} className="btn-primary">
                    書き込み
                  </button>
                </div>
              </div>
            </FocusTrap>
          )}
        </>
      )}
    </div>
  );
}
