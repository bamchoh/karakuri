import { useEffect, useState } from "react";
import {
  AddressArea,
  addressPlaceholder,
  formatAddress,
  isWordBitArea,
  parseAddress,
} from "../utils/address";

interface Props {
  // アドレスを入れるエリア（表示のしかたを決める）
  area: AddressArea | undefined;
  // 0 始まりのアドレス
  value: number;
  onChange: (address: number) => void;
  className?: string;
  max?: number;
}

// メモリエリアのアドレスの入力欄。
// ふつうのエリアは数値の欄（1 始まりのエリアは +1 して表示）、「ワード.ビット」のエリアは文字の欄（100.05）にする
export function AddressInput({ area, value, onChange, className, max }: Props) {
  const wordBit = isWordBitArea(area);
  const oneOrigin = area?.oneOrigin ?? false;

  // 「ワード.ビット」の欄は、入力の途中（"100." など）を保つため、文字列を自分で持つ
  const [text, setText] = useState(() => formatAddress(area, value));
  useEffect(() => {
    setText((prev) =>
      parseAddress(area, prev) === value ? prev : formatAddress(area, value),
    );
    // area は描画のたびに別のオブジェクトになることがあるので、中身で比べる
  }, [value, wordBit, oneOrigin]);
  // アドレスの書き方が変わったら（エリアを変えた・ビット幅を 1bit にした）、その書き方で出し直す
  useEffect(() => {
    setText(formatAddress(area, value));
  }, [wordBit, oneOrigin]);

  if (wordBit) {
    return (
      <input
        className={className}
        type="text"
        value={text}
        placeholder={addressPlaceholder(area)}
        onChange={(e) => {
          setText(e.target.value);
          const parsed = parseAddress(area, e.target.value);
          if (parsed !== null) onChange(parsed);
        }}
        onBlur={() => setText(formatAddress(area, value))}
      />
    );
  }

  return (
    <input
      className={className}
      type="number"
      min={oneOrigin ? 1 : 0}
      max={max}
      value={oneOrigin ? value + 1 : value}
      onChange={(e) => {
        const v = parseInt(e.target.value) || (oneOrigin ? 1 : 0);
        onChange(oneOrigin ? Math.max(0, v - 1) : v);
      }}
    />
  );
}
