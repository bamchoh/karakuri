import { useEffect, useRef } from "react";

// active の間だけ、Esc キーで onEscape を呼ぶ（ダイアログを閉じるために使う）
export function useEscapeKey(active: boolean, onEscape: () => void) {
  // 毎回の描画で作り直される関数でも、リスナーを登録し直さずに最新のものを呼ぶ
  const handler = useRef(onEscape);
  handler.current = onEscape;

  useEffect(() => {
    if (!active) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") handler.current();
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [active]);
}
