import { forwardRef, useImperativeHandle, useRef, useState } from "react";
import { FocusTrap } from "./FocusTrap";
import {
  UpdateVariableMappings,
  UpdateVariableNodePublishing,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { useEscapeKey } from "./useEscapeKey";
import { useNodeAddresses } from "./useNodeAddresses";

export interface MappingDialogHandle {
  // サーバー一覧を読み直してから、変数のマッピングを編集するダイアログを開く
  open: (v: application.VariableDTO) => Promise<void>;
  isOpen: () => boolean;
}

interface Props {
  variables: application.VariableDTO[];
  serverInstances: application.ServerInstanceDTO[];
  memoryAreasByProtocol: Record<string, application.MemoryAreaDTO[]>;
  getWordCount: (dataType: string) => number;
  // サーバー一覧とメモリエリアを読み直す（ダイアログを開く直前に呼ぶ）
  loadServerInstancesAndAreas: () => Promise<application.ServerInstanceDTO[]>;
  // 保存したあとに呼ぶ（一覧の再読み込み）
  onChanged: () => Promise<void>;
}

// 1 つの変数のマッピング（レジスタ型プロトコル）と公開設定（ノード公開型プロトコル）を編集するダイアログ
export const MappingDialog = forwardRef<MappingDialogHandle, Props>(
  function MappingDialog(
    {
      variables,
      serverInstances,
      memoryAreasByProtocol,
      getWordCount,
      loadServerInstancesAndAreas,
      onChanged,
    },
    ref,
  ) {
    const [isOpen, setIsOpen] = useState(false);
    const [mappingVariable, setMappingVariable] =
      useState<application.VariableDTO | null>(null);
    const [editMappings, setEditMappings] = useState<
      application.ProtocolMappingDTO[]
    >([]);
    const [editNodePublishings, setEditNodePublishings] = useState<
      application.NodePublishingDTO[]
    >([]);
    const { nodeAddresses, loadNodeAddresses } = useNodeAddresses();

    const isOneOriginArea = (protocolType: string, areaId: string): boolean =>
      (memoryAreasByProtocol[protocolType] || []).find((a) => a.id === areaId)
        ?.oneOrigin ?? false;

    // マッピングダイアログを開く
    const open = async (v: application.VariableDTO) => {
      const instances = await loadServerInstancesAndAreas();
      for (const inst of instances) {
        if (inst.supportsNodePublishing) {
          loadNodeAddresses(inst.protocolType, [v.id]);
        }
      }
      setMappingVariable(v);
      setEditMappings(v.mappings ? [...v.mappings] : []);
      setEditNodePublishings(v.nodePublishings ? [...v.nodePublishings] : []);
      setIsOpen(true);
    };

    const isOpenRef = useRef(isOpen);
    isOpenRef.current = isOpen;
    useImperativeHandle(ref, () => ({ open, isOpen: () => isOpenRef.current }));
    useEscapeKey(isOpen, () => setIsOpen(false));

    // 指定したマッピングが他の変数のマッピングと重複しているか確認し、変数名一覧を返す
    const findMappingConflicts = (
      mapping: application.ProtocolMappingDTO,
    ): string[] => {
      if (!mappingVariable) return [];
      const currStart = mapping.address;
      const currEnd = currStart + getWordCount(mappingVariable.dataType);
      const conflicts: string[] = [];

      for (const v of variables) {
        if (v.id === mappingVariable.id || !v.mappings) continue;
        const otherWordCount = getWordCount(v.dataType);
        for (const vm of v.mappings) {
          if (
            vm.protocolType !== mapping.protocolType ||
            vm.memoryArea !== mapping.memoryArea
          )
            continue;
          const otherStart = vm.address;
          const otherEnd = otherStart + otherWordCount;
          // アドレス範囲の重複チェック
          if (currStart < otherEnd && currEnd > otherStart) {
            conflicts.push(v.name);
            break;
          }
        }
      }
      return conflicts;
    };

    // マッピングを追加
    const handleAddMapping = () => {
      const firstProtocol = serverInstances[0]?.protocolType || "";
      const firstArea = (memoryAreasByProtocol[firstProtocol] || [])[0]?.id || "";
      setEditMappings([
        ...editMappings,
        {
          protocolType: firstProtocol,
          memoryArea: firstArea,
          address: 0,
          endianness: "big",
        } as application.ProtocolMappingDTO,
      ]);
    };

    // マッピングを削除
    const handleRemoveMapping = (index: number) => {
      setEditMappings(editMappings.filter((_, i) => i !== index));
    };

    // マッピングを保存
    const handleSaveMappings = async () => {
      if (!mappingVariable) return;
      // 公開できない変数は、マッピングだけ保存される前に止める
      for (const pub of editNodePublishings) {
        const error =
          nodeAddresses[pub.protocolType]?.[mappingVariable.id]?.error;
        if (pub.enabled && error) {
          alert(error);
          return;
        }
      }
      try {
        await UpdateVariableMappings(mappingVariable.id, editMappings);
        for (const pub of editNodePublishings) {
          await UpdateVariableNodePublishing(
            mappingVariable.id,
            pub.protocolType,
            pub,
          );
        }
        await onChanged();
        setIsOpen(false);
        setMappingVariable(null);
      } catch (e) {
        console.error("Failed to update mappings:", e);
        alert("マッピングの更新に失敗しました: " + e);
      }
    };

    if (!isOpen || !mappingVariable) return null;
    return (
      <FocusTrap onConfirm={handleSaveMappings}>
        <div className="dialog dialog-w500">
          <h3>マッピング設定: {mappingVariable.name}</h3>
          <div className="dialog-content">
            <p className="dialog-description">
              この変数をプロトコルのアドレスにマッピングします。
            </p>

            {editMappings.map((m, index) => (
              <div key={index} className="dialog-section">
                {/* ヘッダ行 */}
                <div className="dialog-row">
                  <label className="mapping-col">プロトコル</label>
                  <label className="mapping-col">メモリエリア</label>
                  <label className="mapping-col">アドレス</label>
                  <label className="mapping-col">エンディアン</label>
                  <span className="mapping-col-spacer"></span>
                </div>
                {/* コントロール行 */}
                <div className="dialog-row">
                  <select
                    className="mapping-col"
                    value={m.protocolType}
                    onChange={(e) => {
                      const updated = [...editMappings];
                      const newProtocol = e.target.value;
                      const firstArea =
                        (memoryAreasByProtocol[newProtocol] || [])[0]?.id ||
                        "";
                      updated[index] = {
                        ...updated[index],
                        protocolType: newProtocol,
                        memoryArea: firstArea,
                      };
                      setEditMappings(updated);
                    }}
                  >
                    {serverInstances.map((inst) => (
                      <option
                        key={inst.protocolType}
                        value={inst.protocolType}
                      >
                        {inst.displayName}
                      </option>
                    ))}
                  </select>

                  <select
                    className="mapping-col"
                    value={m.memoryArea}
                    onChange={(e) => {
                      const updated = [...editMappings];
                      updated[index] = {
                        ...updated[index],
                        memoryArea: e.target.value,
                      };
                      setEditMappings(updated);
                    }}
                  >
                    {(memoryAreasByProtocol[m.protocolType] || []).map(
                      (a) => (
                        <option key={a.id} value={a.id}>
                          {a.displayName}
                        </option>
                      ),
                    )}
                  </select>

                  <input
                    className="mapping-col"
                    type="number"
                    value={
                      isOneOriginArea(m.protocolType, m.memoryArea)
                        ? m.address + 1
                        : m.address
                    }
                    onChange={(e) => {
                      const oneOrigin = isOneOriginArea(
                        m.protocolType,
                        m.memoryArea,
                      );
                      const v =
                        parseInt(e.target.value) || (oneOrigin ? 1 : 0);
                      const updated = [...editMappings];
                      updated[index] = {
                        ...updated[index],
                        address: oneOrigin ? Math.max(0, v - 1) : v,
                      };
                      setEditMappings(updated);
                    }}
                    min={
                      isOneOriginArea(m.protocolType, m.memoryArea) ? 1 : 0
                    }
                  />

                  <select
                    className="mapping-col"
                    value={m.endianness}
                    onChange={(e) => {
                      const updated = [...editMappings];
                      updated[index] = {
                        ...updated[index],
                        endianness: e.target.value,
                      };
                      setEditMappings(updated);
                    }}
                  >
                    <option value="big">Big</option>
                    <option value="little">Little</option>
                  </select>

                  <button
                    onClick={() => handleRemoveMapping(index)}
                    className="btn-small btn-danger"
                  >
                    X
                  </button>
                </div>
                {(() => {
                  const conflicts = findMappingConflicts(m);
                  if (conflicts.length === 0) return null;
                  return (
                    <div className="mapping-conflict-warning">
                      ⚠ 以下の変数と重複しています: {conflicts.join(", ")}
                    </div>
                  );
                })()}
              </div>
            ))}

            <button onClick={handleAddMapping} className="btn-secondary">
              + マッピング追加
            </button>

            {/* プロトコル公開設定（SupportsNodePublishing が true のサーバーのみ表示）*/}
            {(() => {
              const publishableServers = serverInstances.filter(
                (i) => i.supportsNodePublishing,
              );
              if (publishableServers.length === 0) return null;
              return (
                <div className="dialog-section publish-section">
                  <h4 className="publish-title">プロトコル公開設定</h4>
                  {publishableServers.map((server) => {
                    const pub = editNodePublishings.find(
                      (p) => p.protocolType === server.protocolType,
                    ) ?? {
                      protocolType: server.protocolType,
                      enabled: false,
                      accessMode: "readwrite",
                    };
                    const updatePub = (
                      patch: Partial<application.NodePublishingDTO>,
                    ) => {
                      const updated = editNodePublishings.filter(
                        (p) => p.protocolType !== server.protocolType,
                      );
                      setEditNodePublishings([
                        ...updated,
                        { ...pub, ...patch },
                      ]);
                    };
                    return (
                      <div
                        key={server.protocolType}
                        className="dialog-row publish-row"
                      >
                        <label className="publish-protocol">
                          {server.displayName}
                        </label>
                        <label className="publish-check">
                          <input
                            type="checkbox"
                            checked={pub.enabled}
                            onChange={(e) =>
                              updatePub({ enabled: e.target.checked })
                            }
                          />
                          公開する
                        </label>
                        <select
                          value={pub.accessMode}
                          disabled={!pub.enabled}
                          onChange={(e) =>
                            updatePub({ accessMode: e.target.value })
                          }
                        >
                          <option value="read">Read Only</option>
                          <option value="write">Write Only</option>
                          <option value="readwrite">Read / Write</option>
                        </select>
                        {pub.enabled &&
                          mappingVariable &&
                          (() => {
                            const addr =
                              nodeAddresses[server.protocolType]?.[
                                mappingVariable.id
                              ];
                            return addr?.error ? (
                              <span className="node-address-error">
                                ⚠ {addr.error}
                              </span>
                            ) : (
                              <span className="publish-nodeid">
                                {addr?.address ?? ""}
                              </span>
                            );
                          })()}
                      </div>
                    );
                  })}
                </div>
              );
            })()}
          </div>
          <div className="dialog-buttons">
            <button
              onClick={() => setIsOpen(false)}
              className="btn-secondary"
            >
              キャンセル
            </button>
            <button onClick={handleSaveMappings} className="btn-primary">
              保存
            </button>
          </div>
        </div>
      </FocusTrap>
    );
  },
);
