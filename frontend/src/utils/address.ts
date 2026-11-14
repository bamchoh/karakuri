// メモリエリアのアドレスの、表示と入力の変換。
//
// 内部のアドレスは常に 0 始まりの番号。表示のしかたはエリアの属性（バックエンドが返す）で決まる:
//   - oneOrigin: 1 始まりで表示する（Modbus）
//   - bitsOf: ワードのエリアと同じメモリをビットごとに見たエリア。内部のアドレスは「ワード × 16 + ビット」で、
//     「ワード.ビット」（100.05）の形で表示する（FINS の CIO など）
// 画面ごとに変換を書かず、ここの関数を使う。

// 変換に使うエリアの属性（MemoryAreaDTO の一部）
export interface AddressArea {
  oneOrigin?: boolean;
  bitsOf?: string;
}

export const BITS_PER_WORD = 16;

// 「ワード.ビット」の形でアドレスを表示するエリアかどうか
export function isWordBitArea(area: AddressArea | undefined | null): boolean {
  return !!area?.bitsOf;
}

// 0 始まりのアドレスを、表示用の文字列にする
export function formatAddress(
  area: AddressArea | undefined | null,
  address: number,
): string {
  if (isWordBitArea(area)) {
    const word = Math.floor(address / BITS_PER_WORD);
    const bit = address % BITS_PER_WORD;
    return `${word}.${String(bit).padStart(2, "0")}`;
  }
  return String(area?.oneOrigin ? address + 1 : address);
}

// 表示用の文字列を、0 始まりのアドレスにする。読めなければ null。
// 「ワード.ビット」のエリアでは、ビットを省くと 0 番のビットにする（"100" は 100.00）
export function parseAddress(
  area: AddressArea | undefined | null,
  text: string,
): number | null {
  const s = text.trim();
  if (isWordBitArea(area)) {
    const m = /^(\d+)(?:\.(\d{1,2}))?$/.exec(s);
    if (!m) return null;
    const word = parseInt(m[1], 10);
    const bit = m[2] === undefined ? 0 : parseInt(m[2], 10);
    if (bit >= BITS_PER_WORD) return null;
    return word * BITS_PER_WORD + bit;
  }
  if (!/^\d+$/.test(s)) return null;
  const n = parseInt(s, 10);
  if (area?.oneOrigin) {
    return n >= 1 ? n - 1 : null;
  }
  return n;
}

// アドレスの入力欄に出す例
export function addressPlaceholder(
  area: AddressArea | undefined | null,
): string {
  return isWordBitArea(area) ? "100.05" : "";
}
