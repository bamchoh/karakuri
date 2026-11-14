import { forwardRef, useImperativeHandle, useRef, useState } from "react";
import { FocusTrap } from "./FocusTrap";
import {
  UpdateVariableMappings,
  UpdateVariableNodePublishing,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import {
  addressRangeError,
  addressRangeText,
  convertAddress,
  formatAddress,
  mappingRange,
  isBitDataType,
  isWordBitArea,
  mappingAreaFor,
  mappingWidthLabel,
  parseAddress,
  selectableAreas,
  selectedAreaId,
} from "../utils/address";
import { useEscapeKey } from "./useEscapeKey";
import { useNodeAddresses } from "./useNodeAddresses";
import { BulkEditRow } from "./variableTypes";

export interface BulkMappingDialogHandle {
  // サーバー一覧を読み直してから、全変数のマッピングをまとめて編集するダイアログを開く
  open: () => Promise<void>;
  isOpen: () => boolean;
}

interface Props {
  variables: application.VariableDTO[];
  serverInstances: application.MappingTargetDTO[];
  memoryAreasByProtocol: Record<string, application.MemoryAreaDTO[]>;
  getWordCount: (dataType: string) => number;
  // サーバー一覧とメモリエリアを読み直す（ダイアログを開く直前に呼ぶ）
  loadServerInstancesAndAreas: () => Promise<application.MappingTargetDTO[]>;
  // 保存したあとに呼ぶ（一覧の再読み込み）
  onChanged: () => Promise<void>;
}

// 全変数のマッピング（または公開設定）を、プロトコルごとにまとめて編集するダイアログ。
// 行の memoryArea は、変数の型に合ったエリア（BOOL なら、ビットごとに見るエリア）の ID。
// エリアの選択肢には、ワードのエリアと同じメモリをビットごとに見るエリア（area.bitsOf）を出さない
export const BulkMappingDialog = forwardRef<BulkMappingDialogHandle, Props>(
  function BulkMappingDialog(
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
    const [bulkProtocol, setBulkProtocol] = useState<string>("");
    const [bulkRows, setBulkRows] = useState<BulkEditRow[]>([]);
    const [bulkIsSaving, setBulkIsSaving] = useState(false);
    const { nodeAddresses, loadNodeAddresses } = useNodeAddresses();

    const areaOf = (protocolType: string, areaId: string) =>
      (memoryAreasByProtocol[protocolType] || []).find((a) => a.id === areaId);

    // 指定プロトコルが SupportsNodePublishing かどうか
    const isNodePublishingProtocol = (protocolType: string): boolean =>
      serverInstances.find((i) => i.protocolType === protocolType)
        ?.supportsNodePublishing ?? false;

    // 変数一覧とプロトコルから BulkEditRow を構築
    const buildBulkRows = (
      vars: application.VariableDTO[],
      protocolType: string,
    ): BulkEditRow[] => {
      const isNP = isNodePublishingProtocol(protocolType);
      const areas = memoryAreasByProtocol[protocolType] || [];
      const firstArea = selectableAreas(areas)[0]?.id ?? "";
      return vars.map((v) => {
        const wc = getWordCount(v.dataType);
        const isBit = isBitDataType(v.dataType);
        const defaultArea =
          mappingAreaFor(areas, firstArea, isBit)?.id ?? firstArea;
        if (isNP) {
          const existing = (v.nodePublishings || []).find(
            (np) => np.protocolType === protocolType,
          );
          return {
            variableId: v.id,
            variableName: v.name,
            dataType: v.dataType,
            wordCount: wc,
            memoryArea: "",
            addressStr: "",
            endianness: "big",
            nodeEnabled: existing?.enabled ?? false,
            accessMode: existing?.accessMode ?? "readwrite",
          } as BulkEditRow;
        } else {
          const existing = (v.mappings || []).find(
            (m) => m.protocolType === protocolType,
          );
          // 保存してあるエリアを、変数の型に合ったエリアに直す。
          // BOOL をワードのエリアに保存してあった場合、アドレスはもともとビットの番号なので、そのまま使う
          const existingArea = existing
            ? areaOf(protocolType, existing.memoryArea)
            : undefined;
          const targetArea = existing
            ? (mappingAreaFor(areas, existing.memoryArea, isBit) ??
              existingArea)
            : undefined;
          const displayAddr =
            existing !== undefined
              ? formatAddress(
                  targetArea,
                  isBit
                    ? existing.address
                    : convertAddress(
                        existingArea,
                        targetArea,
                        existing.address,
                      ),
                )
              : "";
          return {
            variableId: v.id,
            variableName: v.name,
            dataType: v.dataType,
            wordCount: wc,
            memoryArea: targetArea?.id ?? existing?.memoryArea ?? defaultArea,
            addressStr: displayAddr,
            endianness: existing?.endianness ?? "big",
            nodeEnabled: false,
            accessMode: "readwrite",
          } as BulkEditRow;
        }
      });
    };

    // 一括マッピング編集ダイアログを開く
    const open = async () => {
      const instances = await loadServerInstancesAndAreas();
      if (variables.length === 0) return;
      const first = instances[0];
      const firstProtocol = first?.protocolType ?? "";
      setBulkProtocol(firstProtocol);
      setBulkRows(buildBulkRows(variables, firstProtocol));
      if (first?.supportsNodePublishing) {
        loadNodeAddresses(
          firstProtocol,
          variables.map((v) => v.id),
        );
      }
      setIsOpen(true);
    };

    const isOpenRef = useRef(isOpen);
    isOpenRef.current = isOpen;
    useImperativeHandle(ref, () => ({ open, isOpen: () => isOpenRef.current }));
    useEscapeKey(isOpen, () => setIsOpen(false));

    // 一括編集ダイアログ内のプロトコル変更
    const handleBulkProtocolChange = (newProtocol: string) => {
      setBulkProtocol(newProtocol);
      setBulkRows(buildBulkRows(variables, newProtocol));
      if (isNodePublishingProtocol(newProtocol)) {
        loadNodeAddresses(
          newProtocol,
          variables.map((v) => v.id),
        );
      }
    };

    // 一括編集テーブルのセル更新
    const handleBulkRowChange = (
      variableId: string,
      patch: Partial<BulkEditRow>,
    ) => {
      setBulkRows((prev) =>
        prev.map((row) =>
          row.variableId === variableId ? { ...row, ...patch } : row,
        ),
      );
    };

    // エリア変更。選んだエリアを、変数の型に合ったエリアにする
    const handleBulkAreaChange = (variableId: string, selected: string) => {
      setBulkRows((prev) =>
        prev.map((row) => {
          if (row.variableId !== variableId) return row;
          const oldArea = areaOf(bulkProtocol, row.memoryArea);
          const target = mappingAreaFor(
            memoryAreasByProtocol[bulkProtocol] || [],
            selected,
            isBitDataType(row.dataType),
          );
          const newArea = target?.id ?? selected;
          // 入力済みのアドレスは、同じワードを指すようにして、新しいエリアの書き方に直す
          let newAddrStr = row.addressStr;
          const addr0based = parseAddress(oldArea, row.addressStr);
          if (addr0based !== null) {
            newAddrStr = formatAddress(
              target,
              convertAddress(oldArea, target, addr0based),
            );
          }
          return { ...row, memoryArea: newArea, addressStr: newAddrStr };
        }),
      );
    };

    // 一括編集ダイアログ内のアドレス競合を検出（レジスタ型プロトコルのみ）
    const findBulkRowConflicts = (
      rows: BulkEditRow[],
      protocolType: string,
    ): Set<string> => {
      const conflictIds = new Set<string>();
      const activeRows = rows
        .filter((row) => row.addressStr.trim() !== "")
        .map((row) => {
          const addr0 = parseAddress(
            areaOf(protocolType, row.memoryArea),
            row.addressStr,
          );
          if (addr0 === null) return null;
          const wc = Math.max(1, row.wordCount);
          return {
            variableId: row.variableId,
            memoryArea: row.memoryArea,
            addr0,
            addrEnd: addr0 + wc - 1,
          };
        })
        .filter((r): r is NonNullable<typeof r> => r !== null);
      for (let i = 0; i < activeRows.length; i++) {
        for (let j = i + 1; j < activeRows.length; j++) {
          const a = activeRows[i];
          const b = activeRows[j];
          if (a.memoryArea !== b.memoryArea) continue;
          if (a.addr0 <= b.addrEnd && b.addr0 <= a.addrEnd) {
            conflictIds.add(a.variableId);
            conflictIds.add(b.variableId);
          }
        }
      }
      return conflictIds;
    };

    // 行のアドレスの範囲と、範囲外のときの理由（アドレスが空・読めない行は、検査しない）
    const rowRange = (row: BulkEditRow, protocolType: string) => {
      const rowArea = areaOf(protocolType, row.memoryArea);
      const { area, count } = mappingRange(
        rowArea,
        row.dataType,
        row.wordCount,
      );
      const addr0 = parseAddress(rowArea, row.addressStr);
      return {
        range: addressRangeText(area),
        error:
          row.addressStr.trim() === "" || addr0 === null
            ? null
            : addressRangeError(area, addr0, count),
      };
    };
    const rangeErrorCount = isNodePublishingProtocol(bulkProtocol)
      ? 0
      : bulkRows.filter((row) => rowRange(row, bulkProtocol).error !== null)
          .length;

    // 一括マッピング保存
    const handleBulkMappingSave = async () => {
      if (bulkIsSaving) return;
      // 範囲外のアドレスは、保存しても動かないので止める
      if (rangeErrorCount > 0) return;
      if (isNodePublishingProtocol(bulkProtocol)) {
        // 公開できない変数があれば、途中まで保存される前に止める
        const blocked = bulkRows.filter(
          (row) =>
            row.nodeEnabled &&
            nodeAddresses[bulkProtocol]?.[row.variableId]?.error,
        );
        if (blocked.length > 0) {
          alert(
            "公開できない変数があります:\n" +
              blocked
                .map(
                  (row) =>
                    `${row.variableName}: ${nodeAddresses[bulkProtocol][row.variableId].error}`,
                )
                .join("\n"),
          );
          return;
        }
      }
      setBulkIsSaving(true);
      try {
        const isNP = isNodePublishingProtocol(bulkProtocol);
        for (const row of bulkRows) {
          const variable = variables.find((v) => v.id === row.variableId);
          if (!variable) continue;
          if (isNP) {
            await UpdateVariableNodePublishing(row.variableId, bulkProtocol, {
              protocolType: bulkProtocol,
              enabled: row.nodeEnabled,
              accessMode: row.accessMode,
            } as application.NodePublishingDTO);
          } else {
            const otherMappings = (variable.mappings || []).filter(
              (m) => m.protocolType !== bulkProtocol,
            );
            if (row.addressStr.trim() === "") {
              await UpdateVariableMappings(row.variableId, otherMappings);
            } else {
              const addr0based = parseAddress(
                areaOf(bulkProtocol, row.memoryArea),
                row.addressStr,
              );
              if (addr0based === null) continue;
              await UpdateVariableMappings(row.variableId, [
                ...otherMappings,
                {
                  protocolType: bulkProtocol,
                  memoryArea: row.memoryArea,
                  address: addr0based,
                  endianness: row.endianness,
                } as application.ProtocolMappingDTO,
              ]);
            }
          }
        }
        await onChanged();
        setIsOpen(false);
      } catch (e) {
        alert("一括マッピングの保存に失敗しました: " + e);
      } finally {
        setBulkIsSaving(false);
      }
    };

    if (!isOpen) return null;
    return (
      <FocusTrap>
        <div className="dialog dialog-scrollable dialog-bulk">
          <h3>一括マッピング編集</h3>
          {/* プロトコル選択 */}
          <div className="dialog-content dialog-content-fixed">
            <div className="dialog-row">
              <label>プロトコル:</label>
              <select
                value={bulkProtocol}
                onChange={(e) => handleBulkProtocolChange(e.target.value)}
              >
                {serverInstances.map((inst) => (
                  <option key={inst.protocolType} value={inst.protocolType}>
                    {inst.displayName}
                  </option>
                ))}
              </select>
            </div>
          </div>
          {/* テーブル本体 */}
          <div className="dialog-content dialog-content-scroll bulk-table-area">
            {bulkRows.length === 0 ? (
              <p className="empty-note">変数がありません</p>
            ) : isNodePublishingProtocol(bulkProtocol) ? (
              /* ノード公開型プロトコルのテーブル */
              <table className="variable-table">
                <thead>
                  <tr>
                    <th className="col-name">変数名</th>
                    <th className="col-type">データ型</th>
                    <th className="col-words">W数</th>
                    <th className="col-publish">公開する</th>
                    <th className="col-access">アクセスモード</th>
                    <th className="col-nodeid">NodeID</th>
                  </tr>
                </thead>
                <tbody>
                  {bulkRows.map((row) => (
                    <tr key={row.variableId}>
                      <td className="cell-name">{row.variableName}</td>
                      <td className="cell-type">{row.dataType}</td>
                      <td className="cell-center">{row.wordCount}</td>
                      <td className="cell-center">
                        <input
                          type="checkbox"
                          checked={row.nodeEnabled}
                          onChange={(e) =>
                            handleBulkRowChange(row.variableId, {
                              nodeEnabled: e.target.checked,
                            })
                          }
                        />
                      </td>
                      <td>
                        <select
                          value={row.accessMode}
                          disabled={!row.nodeEnabled}
                          onChange={(e) =>
                            handleBulkRowChange(row.variableId, {
                              accessMode: e.target.value,
                            })
                          }
                        >
                          <option value="read">Read Only</option>
                          <option value="write">Write Only</option>
                          <option value="readwrite">Read / Write</option>
                        </select>
                      </td>
                      <td className="cell-nodeid">
                        {(() => {
                          if (!row.nodeEnabled) return "-";
                          const addr =
                            nodeAddresses[bulkProtocol]?.[row.variableId];
                          return addr?.error ? (
                            <span className="node-address-error">
                              ⚠ {addr.error}
                            </span>
                          ) : (
                            (addr?.address ?? "-")
                          );
                        })()}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              /* レジスタ型プロトコルのテーブル */
              (() => {
                const conflictIds = findBulkRowConflicts(
                  bulkRows,
                  bulkProtocol,
                );
                return (
                  <>
                    <table className="variable-table">
                      <thead>
                        <tr>
                          <th className="col-name">変数名</th>
                          <th className="col-type">データ型</th>
                          <th className="col-words">ビット幅</th>
                          <th className="col-area">メモリエリア</th>
                          <th className="col-address">
                            アドレス
                            <span className="th-note">(空=削除)</span>
                          </th>
                          <th className="col-endian">エンディアン</th>
                        </tr>
                      </thead>
                      <tbody>
                        {bulkRows.map((row) => {
                          const areas =
                            memoryAreasByProtocol[bulkProtocol] || [];
                          const rowArea = areaOf(bulkProtocol, row.memoryArea);
                          const wordBit = isWordBitArea(rowArea);
                          const hasConflict = conflictIds.has(row.variableId);
                          const range = rowRange(row, bulkProtocol);
                          return (
                            <tr
                              key={row.variableId}
                              className={
                                hasConflict ? "bulk-row-conflict" : undefined
                              }
                            >
                              <td className="cell-name">{row.variableName}</td>
                              <td className="cell-type">{row.dataType}</td>
                              <td className="cell-center">
                                {mappingWidthLabel(row.dataType, row.wordCount)}
                              </td>
                              <td>
                                <select
                                  value={selectedAreaId(areas, row.memoryArea)}
                                  onChange={(e) =>
                                    handleBulkAreaChange(
                                      row.variableId,
                                      e.target.value,
                                    )
                                  }
                                >
                                  {selectableAreas(areas).map((a) => (
                                    <option key={a.id} value={a.id}>
                                      {a.displayName}
                                    </option>
                                  ))}
                                </select>
                              </td>
                              <td>
                                <div className="bulk-address-cell">
                                  <input
                                    type={wordBit ? "text" : "number"}
                                    value={row.addressStr}
                                    min={rowArea?.oneOrigin ? 1 : 0}
                                    placeholder={
                                      range.range
                                        ? `未設定（${range.range}）`
                                        : "未設定"
                                    }
                                    title={
                                      range.error ??
                                      (range.range
                                        ? `範囲: ${range.range}`
                                        : undefined)
                                    }
                                    aria-invalid={!!range.error || undefined}
                                    onChange={(e) =>
                                      handleBulkRowChange(row.variableId, {
                                        addressStr: e.target.value,
                                      })
                                    }
                                    className={`bulk-address-input${hasConflict ? " conflict" : ""}${range.error ? " address-invalid" : ""}`}
                                  />
                                  {hasConflict && (
                                    <span
                                      className="bulk-conflict-icon"
                                      title="アドレスが他の変数と重複しています"
                                    >
                                      ⚠
                                    </span>
                                  )}
                                </div>
                              </td>
                              <td>
                                <select
                                  value={row.endianness}
                                  onChange={(e) =>
                                    handleBulkRowChange(row.variableId, {
                                      endianness: e.target.value,
                                    })
                                  }
                                >
                                  <option value="big">Big</option>
                                  <option value="little">Little</option>
                                </select>
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </>
                );
              })()
            )}
          </div>
          {!isNodePublishingProtocol(bulkProtocol) &&
            (() => {
              const conflictIds = findBulkRowConflicts(bulkRows, bulkProtocol);
              return conflictIds.size > 0 ? (
                <div className="bulk-conflict-banner">
                  ⚠ アドレスが重複している変数が {conflictIds.size} 件あります
                </div>
              ) : null;
            })()}
          {rangeErrorCount > 0 && (
            <div className="bulk-range-banner">
              ⚠ アドレスが範囲外の変数が {rangeErrorCount}{" "}
              件あります（保存できません。入力欄にマウスを乗せると、範囲が出ます）
            </div>
          )}
          <div className="dialog-buttons">
            <button
              onClick={() => setIsOpen(false)}
              className="btn-secondary"
              disabled={bulkIsSaving}
            >
              キャンセル
            </button>
            <button
              onClick={handleBulkMappingSave}
              className="btn-primary"
              disabled={bulkIsSaving || rangeErrorCount > 0}
            >
              {bulkIsSaving ? "保存中..." : "一括保存"}
            </button>
          </div>
        </div>
      </FocusTrap>
    );
  },
);
