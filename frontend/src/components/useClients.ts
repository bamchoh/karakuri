import { usePageActive } from "./usePageActive";
import { useState, useEffect, useCallback } from "react";
import {
  GetClientProtocols,
  GetClients,
  GetClientCommandLogs,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";

// 一覧（接続中・ポーリング中の表示）を読み直す間隔
const CLIENTS_REFRESH_MS = 1000;

// クライアント（外部の機器へつなぐ接続先）の一覧・使えるプロトコル・コマンドの履歴を読み込んで持つ。
// ポーリング中は接続の状態が変わるので、一覧は 1 秒ごとに読み直す
export function useClients() {
  const [protocols, setProtocols] = useState<application.ClientProtocolDTO[]>(
    [],
  );
  const [clients, setClients] = useState<application.ClientDTO[]>([]);
  const [logs, setLogs] = useState<application.ClientCommandLogDTO[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadClients = useCallback(async () => {
    const list = (await GetClients()) || [];
    setClients(list);
    return list;
  }, []);

  const loadLogs = useCallback(async () => {
    setLogs((await GetClientCommandLogs()) || []);
  }, []);

  useEffect(() => {
    (async () => {
      try {
        setProtocols((await GetClientProtocols()) || []);
        await loadClients();
        await loadLogs();
      } catch (e) {
        setError(String(e));
      } finally {
        setLoaded(true);
      }
    })();
  }, [loadClients, loadLogs]);

  // 一覧の読み直しは、ページが出ている間だけ（戻ってきたら、すぐ読み直す）
  const pageActive = usePageActive();
  useEffect(() => {
    if (!pageActive) return;
    loadClients().catch(() => {});
    // 履歴は、隠れている間にスクリプトが送った分も出す
    loadLogs().catch(() => {});
    const timer = setInterval(() => {
      loadClients().catch(() => {});
    }, CLIENTS_REFRESH_MS);
    return () => clearInterval(timer);
  }, [loadClients, loadLogs, pageActive]);

  return { protocols, clients, logs, loaded, error, loadClients, loadLogs };
}
