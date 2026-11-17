import { createContext, useContext } from "react";

// ページ（通信・変数・スクリプト）が、いま画面に出ているかどうか。
// ページは、ほかのページを見ている間も作り直さずに隠すだけ（選んでいたもの・入力中の内容を保つため）。
// 隠れている間は、周期的な読み直しを止める（usePageActive() が false の間はタイマーを動かさない）
export const PageActiveContext = createContext(true);

export function usePageActive(): boolean {
  return useContext(PageActiveContext);
}
