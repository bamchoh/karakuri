// 画面の中身の並べ方（1 画面か、左右の 2 画面か）。state に依存しない純粋な関数。
// どの関数も、渡されたものを書き換えずに、新しいものを返す

export type PageId = "server" | "variables" | "scripts";
export type PaneSide = "left" | "right";

export const PAGE_IDS: PageId[] = ["server", "variables", "scripts"];

// PaneLayout は、左右の欄に出すページ。right が null なら 1 画面（left だけを出す）
export interface PaneLayout {
  left: PageId;
  right: PageId | null;
  // 最後に使った欄（左の帯でページを選ぶと、この欄のページが替わる）。1 画面のときは left
  focused: PaneSide;
  // 左の欄の幅の割合（2 画面のとき）
  ratio: number;
}

export const MIN_RATIO = 0.2;
export const MAX_RATIO = 0.8;

export const DEFAULT_LAYOUT: PaneLayout = {
  left: "server",
  right: null,
  focused: "left",
  ratio: 0.5,
};

const isPage = (v: unknown): v is PageId =>
  typeof v === "string" && (PAGE_IDS as string[]).includes(v);

// clampRatio は、割合を、どちらの欄も狭くなりすぎない範囲に収める（数でなければ半分）
export function clampRatio(ratio: unknown): number {
  if (typeof ratio !== "number" || !Number.isFinite(ratio)) return 0.5;
  return Math.min(MAX_RATIO, Math.max(MIN_RATIO, ratio));
}

// isSplit は、2 画面かどうか
export function isSplit(layout: PaneLayout): boolean {
  return layout.right !== null;
}

// paneOf は、そのページを出している欄（出ていなければ null）
export function paneOf(layout: PaneLayout, page: PageId): PaneSide | null {
  if (layout.left === page) return "left";
  if (layout.right === page) return "right";
  return null;
}

// focusedPage は、最後に使った欄のページ
export function focusedPage(layout: PaneLayout): PageId {
  return layout.focused === "right" && layout.right !== null
    ? layout.right
    : layout.left;
}

// focusPane は、その欄を「最後に使った欄」にする（1 画面のときの右は、ないので左）
export function focusPane(layout: PaneLayout, side: PaneSide): PaneLayout {
  const focused = side === "right" && layout.right !== null ? "right" : "left";
  return focused === layout.focused ? layout : { ...layout, focused };
}

// showPage は、そのページを出す。もう出ていれば、その欄を「最後に使った欄」にするだけ。
// 出ていなければ、最後に使った欄のページを替える（同じページを両方の欄には出さない）
export function showPage(layout: PaneLayout, page: PageId): PaneLayout {
  const side = paneOf(layout, page);
  if (side) return focusPane(layout, side);
  if (layout.focused === "right" && layout.right !== null) {
    return { ...layout, right: page };
  }
  return { ...layout, left: page, focused: "left" };
}

// 2 画面にしたとき、右に出すページ（左のページから決める）。
// スクリプトとレジスタを並べて見ることが多いので、通信とスクリプトを組にする
function partnerOf(page: PageId): PageId {
  return page === "scripts" ? "server" : "scripts";
}

// toggleSplit は、1 画面と 2 画面を切り替える。
// 2 画面にするときは、右に preferred（前に右に出していたページ）を出す。左と同じ・なければ、左のページから決める。
// 1 画面にするときは、最後に使った欄のページを残す
export function toggleSplit(
  layout: PaneLayout,
  preferred?: PageId | null,
): PaneLayout {
  if (layout.right !== null) {
    return {
      ...layout,
      left: focusedPage(layout),
      right: null,
      focused: "left",
    };
  }
  const right =
    preferred && preferred !== layout.left ? preferred : partnerOf(layout.left);
  return { ...layout, right, focused: "left" };
}

// hiddenBy は、layout から next に変えると隠れるページ
export function hiddenBy(layout: PaneLayout, next: PaneLayout): PageId[] {
  return PAGE_IDS.filter(
    (p) => paneOf(layout, p) !== null && paneOf(next, p) === null,
  );
}

// parseLayout は、保存してあった並べ方（JSON の文字列）を読む。読めない・おかしいところは、既定の並べ方で補う
export function parseLayout(json: string | null | undefined): PaneLayout {
  let raw: any;
  try {
    raw = JSON.parse(json ?? "");
  } catch {
    return DEFAULT_LAYOUT;
  }
  if (!raw || typeof raw !== "object") return DEFAULT_LAYOUT;
  const left = isPage(raw.left) ? raw.left : DEFAULT_LAYOUT.left;
  const right = isPage(raw.right) && raw.right !== left ? raw.right : null;
  return {
    left,
    right,
    focused: raw.focused === "right" && right !== null ? "right" : "left",
    ratio: clampRatio(raw.ratio),
  };
}
