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

// エリアの選択肢に出すエリア。ワードのエリアと同じメモリを見るビットのエリア（bitsOf）は出さない
// （ワードのエリアを選んで、ビット幅の「1bit」で見る）
export function selectableAreas<T extends AddressArea>(areas: T[]): T[] {
  return areas.filter((a) => !a.bitsOf);
}

// areaId のワードのエリアを、ビットごとに見るエリアを返す（なければ undefined）。
// これがあるエリアは、ビット幅で「1bit」を選べる
export function bitViewAreaOf<T extends AddressArea>(
  areas: T[],
  areaId: string,
): T | undefined {
  return areaId ? areas.find((a) => a.bitsOf === areaId) : undefined;
}

// 変数をマッピングするときに、実際に読み書きするエリアを、変数の型に合わせて決める。
// ビットの型（BOOL）は、ビットごとに見るエリアがあればそれ（アドレスは「ワード.ビット」）、ほかの型はワードのエリア。
// areaId は、ワードのエリアの ID でも、ビットごとに見るエリアの ID でもよい
export function mappingAreaFor<T extends AddressArea & { id: string }>(
  areas: T[],
  areaId: string,
  isBitType: boolean,
): T | undefined {
  const area = areas.find((a) => a.id === areaId);
  if (!area) return undefined;
  const word = area.bitsOf
    ? (areas.find((a) => a.id === area.bitsOf) ?? area)
    : area;
  return isBitType ? (bitViewAreaOf(areas, word.id) ?? word) : word;
}

// エリアの選択肢で選ばれているものの ID（ビットごとに見るエリアなら、もとのワードのエリア）
export function selectedAreaId(
  areas: (AddressArea & { id: string })[],
  areaId: string,
): string {
  return areas.find((a) => a.id === areaId)?.bitsOf ?? areaId;
}

// エリアを変えたときに、同じワードを指すようにアドレスを直す
// （「ワード.ビット」のエリアのアドレスは ワード × 16 + ビット、ほかはワードの番号）
export function convertAddress(
  from: AddressArea | undefined | null,
  to: AddressArea | undefined | null,
  address: number,
): number {
  const fromWordBit = isWordBitArea(from);
  const toWordBit = isWordBitArea(to);
  if (fromWordBit && !toWordBit) return Math.floor(address / BITS_PER_WORD);
  if (!fromWordBit && toWordBit) return address * BITS_PER_WORD;
  return address;
}

// ビットの型（1 ビットで読み書きする型）かどうか
export function isBitDataType(dataType: string): boolean {
  return dataType === "BOOL";
}

// 変数がマッピング先で占める大きさの表示（"1bit"・"16bit"・"32bit"・"64bit"・"5 ワード"）
export function mappingWidthLabel(dataType: string, wordCount: number): string {
  if (isBitDataType(dataType)) return "1bit";
  if (wordCount === 1 || wordCount === 2 || wordCount === 4) {
    return `${wordCount * BITS_PER_WORD}bit`;
  }
  return `${wordCount} ワード`;
}

// --- アドレスの範囲 ---

// 範囲の検査に使うエリアの属性（MemoryAreaDTO の一部）。size は、エリアの大きさ（ワードのエリアはワード数、ビットのエリアはビット数）
export interface RangeArea extends AddressArea {
  size: number;
  isBit?: boolean;
}

// エリアのアドレスの範囲を、画面の書き方で返す（"0〜6143"・"1〜65536"・"0.00〜6143.15"）。大きさが分からなければ空
export function addressRangeText(area: RangeArea | undefined | null): string {
  if (!area || !(area.size > 0)) return "";
  return `${formatAddress(area, 0)}〜${formatAddress(area, area.size - 1)}`;
}

// address から count 個（ワードのエリアはワード、ビットのエリアはビット）が、エリアに収まらないときの理由を返す。
// 収まれば null。大きさが分からないエリアは、検査しない（null）
export function addressRangeError(
  area: RangeArea | undefined | null,
  address: number,
  count = 1,
): string | null {
  if (!area || !(area.size > 0)) return null;
  const range = addressRangeText(area);
  if (address < 0 || address >= area.size) {
    return `アドレスが範囲外です（範囲: ${range}）`;
  }
  if (address + count > area.size) {
    return `末尾が範囲を超えます（${formatAddress(area, address)} から ${count} 個。範囲: ${range}）`;
  }
  return null;
}

// 変数をマッピングするときの、範囲の検査の対象（エリアと、占める個数）を返す。
//   - ビットの型（BOOL）は 1 ビット。ワードのエリアに直接マッピングするとき（対になるビットのエリアがないとき）は、
//     アドレスがビットの番号になるので、エリアの大きさを 16 倍して見る
//   - ほかの型は、ワードのエリアなら wordCount ワード、ビットのエリアなら wordCount × 16 ビット
export function mappingRange(
  area: RangeArea | undefined | null,
  dataType: string,
  wordCount: number,
): { area: RangeArea | undefined | null; count: number } {
  if (!area) return { area, count: 1 };
  if (isBitDataType(dataType)) {
    return area.isBit
      ? { area, count: 1 }
      : { area: { ...area, size: area.size * BITS_PER_WORD }, count: 1 };
  }
  const words = Math.max(1, wordCount);
  return { area, count: area.isBit ? words * BITS_PER_WORD : words };
}

// アドレスの入力欄に出す例
export function addressPlaceholder(
  area: AddressArea | undefined | null,
): string {
  return isWordBitArea(area) ? "100.05" : "";
}
