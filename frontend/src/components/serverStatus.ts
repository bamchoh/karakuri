// サーバーの状態（Running / Stopped / Error）に対応する CSS クラス
export function serverStatusClass(status: string): string {
  if (status === "Running") return "running";
  if (status === "Error") return "error";
  return "stopped";
}
