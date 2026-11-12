import { useCallback, useState } from "react";
import { GetNodeAddresses } from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";

// ノード公開型プロトコル（OPC UA 等）での、変数のノードアドレスを取得して保持する
export function useNodeAddresses() {
  // プロトコル -> 変数 ID -> ノードアドレス（OPC UA の NodeID 等）。
  // 書式と、公開できない変数の判定（error）はプラグインが決める
  const [nodeAddresses, setNodeAddresses] = useState<
    Record<string, Record<string, application.NodeAddressDTO>>
  >({});

  // ノード公開型プロトコルのサーバーから、変数のノードアドレスを取得する
  const loadNodeAddresses = useCallback(
    async (protocolType: string, variableIds: string[]) => {
      if (!protocolType || variableIds.length === 0) return;
      try {
        const addrs = await GetNodeAddresses(protocolType, variableIds);
        setNodeAddresses((prev) => {
          const byId = { ...(prev[protocolType] || {}) };
          for (const a of addrs || []) byId[a.variableId] = a;
          return { ...prev, [protocolType]: byId };
        });
      } catch (e) {
        console.error("Failed to load node addresses:", e);
      }
    },
    [],
  );

  return { nodeAddresses, loadNodeAddresses };
}
