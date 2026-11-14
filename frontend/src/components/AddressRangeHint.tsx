// アドレスの入力欄の近くに出す、エリアの範囲（「範囲: 0〜6143」）。
// 入力したアドレスが範囲外のときは、その理由を赤く出す
export function AddressRangeHint({
  range,
  error,
  className,
}: {
  // addressRangeText() の結果（分からなければ空。空で、エラーもなければ何も出さない）
  range: string;
  // addressRangeError() の結果（範囲に収まっていれば null）
  error: string | null;
  className?: string;
}) {
  if (!error && !range) return null;
  return (
    <span
      className={`address-range-hint${error ? " error" : ""}${className ? " " + className : ""}`}
    >
      {error ?? `範囲: ${range}`}
    </span>
  );
}
