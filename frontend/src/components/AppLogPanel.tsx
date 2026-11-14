import { useEffect, useRef, useState } from "react";
import { ChevronDown, ChevronUp, Eraser } from "lucide-react";
import { ClearAppLogs, GetAppLogs } from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { application } from "../../wailsjs/go/models";

// 画面に持っておくログの件数（Go 側が持つ件数と同じ）
const MAX_LOGS = 1000;

const LEVEL_LABELS: Record<string, string> = {
  info: "情報",
  warn: "警告",
  error: "エラー",
};

// ログの行き先（AppLogDTO.target）。"server:<プロトコルタイプ>" / "client:<接続先の ID>" / "script:<スクリプトの ID>"
export type LogTargetKind = "server" | "client" | "script";
export interface LogTarget {
  kind: LogTargetKind;
  id: string;
}

export function parseLogTarget(target: string | undefined): LogTarget | null {
  const i = (target ?? "").indexOf(":");
  if (i <= 0) return null;
  const kind = target!.slice(0, i);
  const id = target!.slice(i + 1);
  if ((kind !== "server" && kind !== "client" && kind !== "script") || !id) {
    return null;
  }
  return { kind, id };
}

const TARGET_TITLES: Record<LogTargetKind, string> = {
  server: "通信画面で、このサーバーを開く",
  client: "通信画面で、この接続先を開く",
  script: "スクリプトのページで、このスクリプトを開く",
};

interface Props {
  // ログの行の出どころを押したときに呼ぶ（該当のサーバー・接続先・スクリプトの画面に移る）
  onNavigate?: (target: LogTarget) => void;
}

function formatTime(at: number): string {
  const d = new Date(at);
  const p = (n: number, width = 2) => String(n).padStart(width, "0");
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
}

// ログの出どころ。行き先があれば、押すとその画面に移るボタンにする
function LogSource({
  log,
  onNavigate,
}: {
  log: application.AppLogDTO;
  onNavigate?: (target: LogTarget) => void;
}) {
  const target = parseLogTarget(log.target);
  if (!target || !onNavigate) {
    return <span className="app-log-source">[{log.source}]</span>;
  }
  return (
    <button
      className="app-log-source app-log-link"
      title={TARGET_TITLES[target.kind]}
      onClick={() => onNavigate(target)}
    >
      [{log.source}]
    </button>
  );
}

// アプリ全体のログの欄。どのページでも画面の下に出す（開閉できる）。
// サーバーの起動・停止、クライアントの通信エラー、プロジェクトの保存・読み込みなどが出る。
// スクリプトの出力（console.log など）は、スクリプトのページのコンソールに出る（ここには出ない）。
//
// 開いたときに持っているログを読み、以後は Wails イベント plc:app-log-added で 1 件ずつ受け取る
// （消されたときは plc:app-log-cleared）。
// 閉じている間に届いた警告・エラーは、見出しに件数を出す。
// 行き先のあるログ（サーバー・接続先・スクリプトのもの）は、出どころを押すと、その画面に移る
export function AppLogPanel({ onNavigate }: Props) {
  const [logs, setLogs] = useState<application.AppLogDTO[]>([]);
  const [open, setOpen] = useState(false);
  // 閉じている間に届いた警告・エラーの件数（開くと 0 に戻す）
  const [unseen, setUnseen] = useState({ warn: 0, error: 0 });
  const openRef = useRef(open);
  openRef.current = open;
  const outputRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let cancelled = false;
    // イベントの登録を先にして、読み込みとの間に届いたログを取りこぼさない（重なった分は番号で除く）
    const off = EventsOn(
      "plc:app-log-added",
      (entry: application.AppLogDTO) => {
        setLogs((prev) =>
          prev.some((l) => l.id === entry.id)
            ? prev
            : [...prev, entry].slice(-MAX_LOGS),
        );
        if (
          !openRef.current &&
          (entry.level === "warn" || entry.level === "error")
        ) {
          setUnseen((prev) => ({
            ...prev,
            [entry.level]: prev[entry.level as "warn" | "error"] + 1,
          }));
        }
      },
    );
    GetAppLogs()
      .then((loaded) => {
        if (cancelled) return;
        setLogs((prev) => {
          const known = new Set((loaded || []).map((l) => l.id));
          return [...(loaded || []), ...prev.filter((l) => !known.has(l.id))]
            .sort((a, b) => a.id - b.id)
            .slice(-MAX_LOGS);
        });
      })
      .catch((e) => console.error("Failed to load app logs:", e));
    // 画面の外（REST API など）から消されたときも、表示を空にする
    const offCleared = EventsOn("plc:app-log-cleared", () => {
      setLogs([]);
      setUnseen({ warn: 0, error: 0 });
    });
    return () => {
      cancelled = true;
      off();
      offCleared();
    };
  }, []);

  // 開いている間は、新しいログが見えるよう下までスクロールする
  useEffect(() => {
    if (open && outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [logs, open]);

  const toggle = () => {
    setOpen(!open);
    setUnseen({ warn: 0, error: 0 });
  };

  const handleClear = async () => {
    try {
      await ClearAppLogs();
      setLogs([]);
      setUnseen({ warn: 0, error: 0 });
    } catch (e) {
      console.error("Failed to clear app logs:", e);
    }
  };

  const last = logs[logs.length - 1];

  return (
    <div className={`app-log${open ? " open" : ""}`}>
      <div className="app-log-header">
        <button
          className="app-log-toggle"
          onClick={toggle}
          title={open ? "ログを閉じる" : "ログを開く"}
          // 見出しに出す最後のログの文面を、ボタンの名前に含めない（ほかのボタンと名前が重なるため）
          aria-label={open ? "ログを閉じる" : "ログを開く"}
          aria-expanded={open}
        >
          {open ? <ChevronDown size={14} /> : <ChevronUp size={14} />}
          <span className="app-log-title">ログ</span>
          <span className="app-log-count">{logs.length} 件</span>
          {unseen.error > 0 && (
            <span className="app-log-badge error">エラー {unseen.error}</span>
          )}
          {unseen.warn > 0 && (
            <span className="app-log-badge warn">警告 {unseen.warn}</span>
          )}
          {/* 閉じている間は、最後の 1 件を見出しに出す */}
          {!open && last && (
            <span className={`app-log-last ${last.level}`}>
              {formatTime(last.at)} [{last.source}] {last.message}
            </span>
          )}
        </button>
        {open && (
          <button
            className="app-log-clear"
            onClick={handleClear}
            title="ログをすべて消す"
            aria-label="ログをすべて消す"
          >
            <Eraser size={14} />
          </button>
        )}
      </div>
      {open && (
        <div className="app-log-output" ref={outputRef}>
          {logs.length === 0 ? (
            <span className="console-empty">ログはありません</span>
          ) : (
            logs.map((log) => (
              <div key={log.id} className={`app-log-entry ${log.level}`}>
                <span className="app-log-time">{formatTime(log.at)}</span>
                <span className="app-log-level">
                  {LEVEL_LABELS[log.level] ?? log.level}
                </span>
                <LogSource log={log} onNavigate={onNavigate} />
                <span className="app-log-message">{log.message}</span>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}
