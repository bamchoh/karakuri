import { useState, useEffect, useCallback, useRef } from "react";
import { GetMemoryAreas, ReadBits, ReadWords } from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { RegisterWriteDialog } from "./RegisterWriteDialog";
import {
  BIT_WIDTHS,
  DISPLAY_FORMATS,
  ENDIANNESS_LONG_OPTIONS,
} from "../constants/register";
import { getWordCount } from "../utils/register";
import type { BitWidth, DisplayFormat, Endianness } from "../types/register";
import {
  buildRows,
  columnsPerRow,
  formatRegisterValue,
  moveSelection,
  PAGE_SIZE,
  RegisterValue,
  WORD_BIT_COLUMNS,
  WORD_BIT_PAGE_SIZE,
} from "./registerList";
import {
  BITS_PER_WORD,
  bitViewAreaOf,
  formatAddress,
  isWordBitArea,
  selectableAreas,
} from "../utils/address";

interface Props {
  serverInstances: application.ServerInstanceDTO[];
  // 「一覧表示」のタブが選ばれているか。
  // 選ばれていない間も state（選んだエリア・表示形式など）を保つため、常にマウントしておき、表示と読み取りだけを止める
  active: boolean;
}

// 書き込みダイアログを開いたセルの情報
interface EditingCell {
  address: number; // 0 始まり
  originalValue: RegisterValue;
  initialValue: string;
  format: DisplayFormat;
}

// レジスタの一覧表示: メモリエリアの値を 10 列の表で表示し、セルを選んで書き込む。
// ワードのエリアと同じメモリをビットごとに見るエリア（area.bitsOf）は、エリアの選択肢には出さず、
// ワードのエリアの「ビット幅: 1bit」として見る。1 行に 1 ワード分の 16 ビットを並べ、行の見出しをワードの番号にする
export function RegisterListView({ serverInstances, active }: Props) {
  const [selectedProtocol, setSelectedProtocol] = useState<string>("");

  const [memoryAreas, setMemoryAreas] = useState<application.MemoryAreaDTO[]>(
    [],
  );
  const [selectedArea, setSelectedArea] = useState<string>("");

  // レジスタ表示の状態
  const [startAddressText, setStartAddressText] = useState("0");
  const [startAddress, setStartAddress] = useState(0);

  const [values, setValues] = useState<RegisterValue[]>([]);
  const [displayFormat, setDisplayFormat] = useState<DisplayFormat>("decimal");
  const [bitWidth, setBitWidth] = useState<BitWidth>(16);
  // ビット幅で「1bit」を選んでいるか（選べないエリアでは無視する）
  const [oneBitSelected, setOneBitSelected] = useState(false);
  const [endianness, setEndianness] = useState<Endianness>("big");

  // カーソル（選択セル）の状態
  const [selectedIndex, setSelectedIndex] = useState<number>(0);
  const tableContainerRef = useRef<HTMLDivElement>(null);

  // 書き込みダイアログ（開いていなければ null）
  const [editing, setEditing] = useState<EditingCell | null>(null);

  // レジスタを持つサーバー（ノード公開型のプロトコルは除く）
  const registerInstances = serverInstances.filter(
    (i) => !i.supportsNodePublishing,
  );

  // 選んでいたプロトコルのサーバーがなくなったら、先頭のサーバーを選ぶ
  useEffect(() => {
    const candidates = serverInstances.filter((i) => !i.supportsNodePublishing);
    if (candidates.length > 0) {
      setSelectedProtocol((prev) => {
        if (!prev || !candidates.find((i) => i.protocolType === prev)) {
          return candidates[0].protocolType;
        }
        return prev;
      });
    }
  }, [serverInstances]);

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

  // 「1bit」を選べるエリアなら、ビットごとに見るエリア
  const bitViewArea = bitViewAreaOf(memoryAreas, selectedArea);
  const oneBit = oneBitSelected && !!bitViewArea;
  // 実際に読み書きするエリア（「1bit」のときは、ビットごとに見るエリア）
  const currentArea = oneBit
    ? bitViewArea
    : memoryAreas.find((a) => a.id === selectedArea);
  const currentAreaId = currentArea?.id ?? selectedArea;
  const isBitType = currentArea?.isBit ?? false;
  const isByteAddr = currentArea?.byteAddressing ?? false;
  const isOneOriginArea = currentArea?.oneOrigin ?? false;
  const isWordBit = isWordBitArea(currentArea);
  const columns = isWordBit ? WORD_BIT_COLUMNS : undefined;
  const pageSize = isWordBit ? WORD_BIT_PAGE_SIZE : PAGE_SIZE;
  const maxStartAddress = currentArea
    ? Math.max(0, currentArea.size - pageSize)
    : 0;

  const remaining = currentArea
    ? Math.max(0, currentArea.size - startAddress)
    : 0;

  const readCount = Math.min(pageSize, remaining);

  // 開始アドレスを、入力欄に出す番号にする（1 始まりのエリアは +1、「ワード.ビット」のエリアはワードの番号）
  const startAddressDisplay = (address: number) =>
    String(
      isWordBit
        ? Math.floor(address / BITS_PER_WORD)
        : isOneOriginArea
          ? address + 1
          : address,
    );

  useEffect(() => {
    setStartAddressText(startAddressDisplay(startAddress));
  }, [startAddress, isOneOriginArea, isWordBit]);

  // 「ワード.ビット」のエリアでは、開始アドレスをワードの境目にそろえる（行の先頭を .00 にするため）
  useEffect(() => {
    if (isWordBit && startAddress % BITS_PER_WORD !== 0) {
      setStartAddress(startAddress - (startAddress % BITS_PER_WORD));
    }
  }, [isWordBit, startAddress]);

  // ワードアドレスを表示用アドレスに変換する（バイトアドレスは*2、oneOrigin のエリアは+1）
  const toDisplayAddr = (wordAddr: number) => {
    if (isByteAddr) return wordAddr * 2;
    if (isOneOriginArea) return wordAddr + 1;
    return wordAddr;
  };

  const loadRegisters = useCallback(async () => {
    if (!currentAreaId || !selectedProtocol) return;

    try {
      let data: RegisterValue[];
      if (isBitType) {
        data = await ReadBits(
          selectedProtocol,
          currentAreaId,
          startAddress,
          readCount,
        );
      } else {
        data = await ReadWords(
          selectedProtocol,
          currentAreaId,
          startAddress,
          readCount,
        );
      }
      setValues(data || []);
    } catch (e) {
      console.error("Failed to load registers:", e);
    }
  }, [
    selectedProtocol,
    currentAreaId,
    startAddress,
    isBitType,
    currentArea?.size,
  ]);

  // 表示している間、100ms ごとに読み直す
  useEffect(() => {
    if (!active) return;
    loadRegisters();
    const interval = setInterval(loadRegisters, 100);
    return () => clearInterval(interval);
  }, [loadRegisters, active]);

  // コンポーネントマウント時にテーブルにフォーカスを設定
  useEffect(() => {
    tableContainerRef.current?.focus();
  }, []);

  // 値を、表示形式・ビット幅・エンディアンの設定に従って文字列にする
  const formatValue = (value: RegisterValue, index: number) =>
    formatRegisterValue(
      values,
      value,
      index,
      displayFormat,
      bitWidth,
      endianness,
    );

  // テーブルにフォーカスを戻す（ダイアログを閉じたあと、続けてキーで操作できるようにする）
  const focusTable = () => {
    setTimeout(() => {
      tableContainerRef.current?.focus();
    }, 0);
  };

  // セルクリック時にダイアログを開く
  const handleCellClick = (index: number) => {
    const value = values[index];
    setEditing({
      address: startAddress + index,
      originalValue: value,
      // 入力欄の初期値: ビットは 1 / 0、ワードは一覧と同じ表示
      initialValue:
        typeof value === "boolean"
          ? value
            ? "1"
            : "0"
          : formatValue(value, index),
      format: displayFormat,
    });
  };

  // ダイアログを閉じる
  const handleDialogClose = () => {
    setEditing(null);
    focusTable();
  };

  // 書き込んだあと: 読み直してから閉じる
  const handleWritten = async () => {
    await loadRegisters();
    setEditing(null);
    focusTable();
  };

  // テーブルのキーハンドラ（カーソル移動・Enter で書き込みダイアログ）
  const handleTableKeyDown = (e: React.KeyboardEvent) => {
    if (editing) return;

    if (e.key === "Enter") {
      e.preventDefault();
      handleCellClick(selectedIndex);
      return;
    }
    const newIndex = moveSelection(
      e.key,
      selectedIndex,
      values.length - 1,
      columns,
    );
    if (newIndex === null) return;
    e.preventDefault();
    setSelectedIndex(newIndex);
  };

  // セルクリック時に選択も更新
  const handleCellClickWithSelect = (index: number) => {
    setSelectedIndex(index);
    handleCellClick(index);
  };

  // ビット幅の変更。「1bit」との切り替えでは、同じワードが先頭に来るように開始アドレスを直す
  const handleBitWidthChange = (value: string) => {
    const nextOneBit = value === "1";
    if (nextOneBit !== oneBit) {
      setStartAddress(
        nextOneBit
          ? startAddress * BITS_PER_WORD
          : Math.floor(startAddress / BITS_PER_WORD),
      );
      setValues([]);
      setSelectedIndex(0);
    }
    setOneBitSelected(nextOneBit);
    if (!nextOneBit) setBitWidth(parseInt(value) as BitWidth);
  };

  // エリアの変更。「1bit」で見ていて、次のエリアでは選べないときは、開始アドレスをワードの番号に戻す
  const handleAreaChange = (areaId: string) => {
    if (oneBit && !bitViewAreaOf(memoryAreas, areaId)) {
      setStartAddress(Math.floor(startAddress / BITS_PER_WORD));
      setOneBitSelected(false);
      setValues([]);
      setSelectedIndex(0);
    }
    setSelectedArea(areaId);
  };

  // 現在のビット幅に応じたワード数
  const wordCount = isBitType ? 1 : getWordCount(bitWidth);

  // 実効的な列数（32bit/64bitの場合は列をまとめる）
  const effectiveColumns = columnsPerRow(wordCount, columns);

  if (!active) return null;

  return (
    <>
      <div className="register-controls">
        {/* プロトコル選択（レジスタを持つサーバーが複数の場合） */}
        {registerInstances.length > 1 && (
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
              {registerInstances.map((inst) => (
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
            onChange={(e) => handleAreaChange(e.target.value)}
          >
            {selectableAreas(memoryAreas).map((area) => (
              <option key={area.id} value={area.id}>
                {area.displayName}
              </option>
            ))}
          </select>
        </div>

        <div className="form-group">
          <label>開始アドレス</label>
          <input
            value={startAddressText}
            onChange={(e) => {
              setStartAddressText(e.target.value);
            }}
            onBlur={() => {
              const v = parseInt(startAddressText);

              if (!isNaN(v)) {
                setStartAddress(
                  isWordBit
                    ? Math.max(0, v) * BITS_PER_WORD
                    : isOneOriginArea
                      ? Math.max(0, v - 1)
                      : Math.max(0, v),
                );
              }

              setStartAddressText(startAddressDisplay(startAddress));
            }}
            onFocus={(e) => e.target.select()}
          />
        </div>

        {!isBitType && (
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
        )}

        {(!isBitType || oneBit) && (
          <div className="form-group">
            <label>ビット幅</label>
            <select
              value={oneBit ? "1" : bitWidth}
              onChange={(e) => handleBitWidthChange(e.target.value)}
            >
              {bitViewArea && <option value="1">1bit</option>}
              {BIT_WIDTHS.map((b) => (
                <option key={b.value} value={b.value}>
                  {b.label}
                </option>
              ))}
            </select>
          </div>
        )}

        {!isBitType && (
          <>
            <div className="form-group">
              <label>エンディアン</label>
              <select
                value={endianness}
                onChange={(e) => setEndianness(e.target.value as Endianness)}
              >
                {ENDIANNESS_LONG_OPTIONS.map((e) => (
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
                  {isWordBit ? (
                    `.${String(i).padStart(2, "0")}`
                  ) : (
                    <>
                      +{toDisplayAddr(i * wordCount)}
                      {wordCount > 1
                        ? `~+${toDisplayAddr(i * wordCount + wordCount - 1)}`
                        : ""}
                    </>
                  )}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {buildRows(values, startAddress, wordCount, columns).map((row) => (
              <tr key={row.rowStart}>
                <td className="row-header">
                  {isWordBit
                    ? Math.floor(row.rowStart / BITS_PER_WORD)
                    : toDisplayAddr(row.rowStart)}
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
          onClick={() => setStartAddress(Math.max(0, startAddress - pageSize))}
          disabled={startAddress === 0}
          className="btn-secondary"
        >
          前へ
        </button>
        <span>
          {isWordBit
            ? `${startAddressDisplay(startAddress)} - ${startAddressDisplay(startAddress + pageSize - 1)}`
            : `${toDisplayAddr(startAddress)} - ${toDisplayAddr(startAddress + PAGE_SIZE - 1)}`}
        </span>
        <button
          onClick={() => setStartAddress(startAddress + pageSize)}
          className="btn-secondary"
          disabled={startAddress >= maxStartAddress}
        >
          次へ
        </button>
      </div>

      {/* 書き込みダイアログ */}
      {editing && (
        <RegisterWriteDialog
          title="書き込み"
          protocolType={selectedProtocol}
          area={currentAreaId}
          address={editing.address}
          displayAddress={
            isWordBit
              ? formatAddress(currentArea, editing.address)
              : toDisplayAddr(editing.address)
          }
          currentValueText={formatValue(
            editing.originalValue,
            editing.address - startAddress,
          )}
          isBit={isBitType}
          bitInput="buttons"
          bitWidth={bitWidth}
          endianness={endianness}
          initialFormat={editing.format}
          initialValue={editing.initialValue}
          onClose={handleDialogClose}
          onWritten={handleWritten}
        />
      )}
    </>
  );
}
