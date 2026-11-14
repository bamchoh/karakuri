import { useEffect, useRef, useState } from "react";
import { FocusTrap } from "./FocusTrap";
import { WriteBit, WriteWord } from "../../wailsjs/go/main/App";
import { DISPLAY_FORMATS } from "../constants/register";
import {
  formatBigInt,
  formatSingleWord,
  getWordCount,
  parseBigIntInput,
  parseInputValue,
  splitToWords,
} from "../utils/register";
import type { BitWidth, DisplayFormat, Endianness } from "../types/register";

interface Props {
  // ダイアログの見出し
  title: string;
  // 書き込み先（address は 0 始まり）
  protocolType: string;
  area: string;
  address: number;
  // 画面に出すアドレス（1 始まりのエリアなら +1 したもの）と現在の値
  // 表示用のアドレス（1 始まりのエリアは +1 した番号、「ワード.ビット」のエリアは "100.05"）
  displayAddress: number | string;
  currentValueText: string;
  isBit: boolean;
  // ビットの値の入れ方: "buttons" は ON / OFF のボタン、"text" は文字（0, 1, ON, OFF）
  bitInput: "buttons" | "text";
  bitWidth: BitWidth;
  endianness: Endianness;
  // 開いたときの入力形式と入力欄の値
  initialFormat: DisplayFormat;
  initialValue: string;
  // キャンセル・Esc で閉じるとき
  onClose: () => void;
  // 書き込みに成功したあと（閉じる・読み直すなどは呼び出し側が行う）
  onWritten: () => void | Promise<void>;
}

// レジスタに値を書き込むダイアログ（レジスタの一覧とモニタリングで共通）。
// 開くたびにマウントする。16bit は 1 ワード、32bit / 64bit はエンディアンに従って複数ワードに分けて書く。
export function RegisterWriteDialog({
  title,
  protocolType,
  area,
  address,
  displayAddress,
  currentValueText,
  isBit,
  bitInput,
  bitWidth,
  endianness,
  initialFormat,
  initialValue,
  onClose,
  onWritten,
}: Props) {
  const [value, setValue] = useState(initialValue);
  const [inputFormat, setInputFormat] = useState<DisplayFormat>(initialFormat);
  const inputRef = useRef<HTMLInputElement>(null);

  // 開いたときに入力欄へフォーカスし、値を全選択する（そのまま打てば置き換わる）
  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  // 入力形式を変えたら、入力中の値をその形式に変換する
  const handleInputFormatChange = (newFormat: DisplayFormat) => {
    if (!isBit && value) {
      try {
        if (bitWidth === 16) {
          const numValue = parseInputValue(value, inputFormat);
          if (!isNaN(numValue)) {
            setValue(formatSingleWord(numValue, newFormat));
          }
        } else {
          // 32bit / 64bit は bigint で変換する
          const bigValue = parseBigIntInput(value, inputFormat);
          setValue(formatBigInt(bigValue, newFormat, bitWidth));
        }
      } catch {
        // 読めない値はそのままにする
      }
    }
    setInputFormat(newFormat);
  };

  // 入力欄の値を書き込む
  const handleSave = async () => {
    try {
      if (isBit) {
        const newValue =
          value === "true" || value === "1" || value.toLowerCase() === "on";
        await WriteBit(protocolType, area, address, newValue);
      } else if (bitWidth === 16) {
        const newValue = parseInputValue(value, inputFormat);
        if (isNaN(newValue)) {
          console.error("Invalid number format");
          return;
        }
        await WriteWord(protocolType, area, address, newValue);
      } else {
        // 32bit / 64bit: 複数ワードに分けて書き込む
        const bigValue = parseBigIntInput(value, inputFormat);
        const words = splitToWords(
          bigValue,
          getWordCount(bitWidth),
          endianness,
        );
        for (let i = 0; i < words.length; i++) {
          await WriteWord(protocolType, area, address + i, words[i]);
        }
      }
      await onWritten();
    } catch (e) {
      console.error("Failed to write value:", e);
    }
  };

  // ON / OFF のボタンで書き込む
  const handleBitWrite = async (bit: boolean) => {
    try {
      await WriteBit(protocolType, area, address, bit);
      await onWritten();
    } catch (e) {
      console.error("Failed to set bit:", e);
    }
  };

  const useBitButtons = isBit && bitInput === "buttons";

  return (
    <FocusTrap onConfirm={handleSave}>
      <div className="dialog">
        <h3>{title}</h3>

        <div className="dialog-content">
          <div className="dialog-row">
            <label>アドレス:</label>
            <span className="dialog-value">{displayAddress}</span>
          </div>
          <div className="dialog-row">
            <label>現在の値:</label>
            <span className="dialog-value">{currentValueText}</span>
          </div>
          {!isBit && (
            <div className="dialog-row">
              <label>入力形式:</label>
              <select
                value={inputFormat}
                onChange={(e) =>
                  handleInputFormatChange(e.target.value as DisplayFormat)
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
          {useBitButtons ? (
            <div className="dialog-row">
              <label>新しい値:</label>

              <div className="bit-buttons">
                <button
                  className="btn-primary bit-on-button"
                  onClick={() => handleBitWrite(true)}
                >
                  ON
                </button>

                <button
                  className="btn-secondary"
                  onClick={() => handleBitWrite(false)}
                >
                  OFF
                </button>
              </div>
            </div>
          ) : (
            <div className="dialog-row">
              <label>新しい値:</label>
              <input
                ref={inputRef}
                type="text"
                value={value}
                onChange={(e) => setValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Escape") onClose();
                }}
                className="dialog-input"
                placeholder={isBit ? "0, 1, ON, OFF" : ""}
              />
            </div>
          )}
        </div>

        <div className="dialog-buttons">
          <button onClick={onClose} className="btn-secondary">
            キャンセル
          </button>
          {!useBitButtons && (
            <button onClick={handleSave} className="btn-primary">
              書き込み
            </button>
          )}
        </div>
      </div>
    </FocusTrap>
  );
}
