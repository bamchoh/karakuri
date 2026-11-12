// クライアント画面（コマンドの結果・履歴・ポーリング）で共通の表示の書式

const statusLabels: Record<string, string> = {
  ok: "正常",
  exception: "例外応答",
  timeout: "タイムアウト",
  error: "エラー",
  skipped: "送っていない",
};

export function clientStatusLabel(status: string): string {
  return statusLabels[status] ?? status;
}

// 時刻（RFC 3339）を「時:分:秒.ミリ秒」にする
export function formatClientTime(at: string): string {
  const d = new Date(at);
  if (isNaN(d.getTime())) return at;
  const pad = (n: number, w = 2) => String(n).padStart(w, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
}

// かかった時間。Windows の Go の時計は 0.5 ms 程度より短い時間を測れず 0 になるので、0 は「1 ms 未満」と出す
export function formatClientElapsed(ms: number): string {
  if (ms <= 0) return "1 ms 未満";
  return ms < 10 ? `${ms.toFixed(2)} ms` : `${Math.round(ms)} ms`;
}
