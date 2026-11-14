import { useState } from "react";
import { FocusTrap } from "./FocusTrap";
import { application } from "../../wailsjs/go/models";
import { AddressInput } from "./AddressInput";
import { AddressRangeHint } from "./AddressRangeHint";
import { getWordCount } from "../utils/register";
import {
  addressRangeError,
  addressRangeText,
  BITS_PER_WORD,
  bitViewAreaOf,
  selectableAreas,
} from "../utils/address";

import {
  BIT_WIDTHS,
  DISPLAY_FORMATS,
  ENDIANNESS_OPTIONS,
} from "../constants/register";

import type { DisplayFormat, BitWidth, Endianness } from "../types/register";

interface AddMonitoringDialogProps {
  onSave: (
    formProtocolType: string,
    formArea: string,
    formAddress: number,
    formCount: number,
    formBitWidth: BitWidth,
    formEndianness: Endianness,
    formDisplayFormat: DisplayFormat,
  ) => void;
  onClose: () => void;
  serverInstances: application.ServerInstanceDTO[];
  memoryAreasByProtocol: {
    [protocolType: string]: application.MemoryAreaDTO[];
  };
}

export function AddMonitoringDialog({
  onSave,
  onClose,
  serverInstances,
  memoryAreasByProtocol,
}: AddMonitoringDialogProps) {
  // 選んだプロトコルとエリア。空文字は「まだ選んでいない」で、その間は既定のもの
  // （先頭のサーバー、その最初のワードのエリア）を使う。
  // 既定を effect で state に入れると、開いた直後の一瞬だけエリアが決まらず、
  // 1 始まりのアドレスの換算がずれる
  const [selectedProtocolType, setSelectedProtocolType] = useState("");
  const [selectedArea, setSelectedArea] = useState("");
  const [formAddress, setFormAddress] = useState(0);
  const [formCount, setFormCount] = useState(1);
  const [formBitWidth, setFormBitWidth] = useState<BitWidth>(16);
  // ビット幅で「1bit」を選んでいるか（ワードのエリアを、ビットごとに見る）
  const [formOneBit, setFormOneBit] = useState(false);
  const [formEndianness, setFormEndianness] = useState<Endianness>("big");
  const [formDisplayFormat, setFormDisplayFormat] =
    useState<DisplayFormat>("decimal");

  const formProtocolType =
    selectedProtocolType || serverInstances[0]?.protocolType || "";
  const allAreas = memoryAreasByProtocol[formProtocolType] || [];
  // ワードのエリアと同じメモリを見るビットのエリアは、選択肢に出さない（ビット幅の「1bit」で選ぶ）
  const formAreas = selectableAreas(allAreas);
  const formArea =
    selectedArea ||
    formAreas.find((a) => !a.isBit)?.id ||
    formAreas[0]?.id ||
    "";

  // 「1bit」を選べるエリアなら、ビットごとに見るエリア
  const bitViewArea = bitViewAreaOf(allAreas, formArea);
  const oneBit = formOneBit && !!bitViewArea;
  // 実際に読み書きするエリア（「1bit」のときは、ビットごとに見るエリア）
  const targetArea = oneBit
    ? bitViewArea
    : formAreas.find((a) => a.id === formArea);
  const targetAreaId = targetArea?.id ?? formArea;

  // フォームのプロトコル変更（エリアは、そのプロトコルの既定に戻す）
  const handleFormProtocolChange = (protocolType: string) => {
    setSelectedProtocolType(protocolType);
    setSelectedArea("");
  };

  // ビット幅の変更。「1bit」との切り替えでは、同じワードを指すようにアドレスを直す
  const handleBitWidthChange = (value: string) => {
    const nextOneBit = value === "1";
    if (nextOneBit !== oneBit) {
      setFormAddress(
        nextOneBit
          ? formAddress * BITS_PER_WORD
          : Math.floor(formAddress / BITS_PER_WORD),
      );
    }
    setFormOneBit(nextOneBit);
    if (!nextOneBit) setFormBitWidth(parseInt(value) as BitWidth);
  };

  // エリアの変更。「1bit」を選べないエリアに変えたら、アドレスをワードの番号に戻す
  const handleAreaChange = (areaId: string) => {
    if (oneBit && !bitViewAreaOf(allAreas, areaId)) {
      setFormAddress(Math.floor(formAddress / BITS_PER_WORD));
      setFormOneBit(false);
    }
    setSelectedArea(areaId);
  };

  const targetIsBit = targetArea?.isBit ?? false;

  // 追加する項目が、最後の 1 つまでエリアに収まるか（項目は、1 つ分の大きさずつ並べて追加する）
  const unitsPerItem = targetIsBit ? 1 : getWordCount(formBitWidth);
  const rangeError = addressRangeError(
    targetArea,
    formAddress,
    Math.max(1, formCount) * unitsPerItem,
  );

  const save = () => {
    if (rangeError) return;
    onSave(
      formProtocolType,
      targetAreaId,
      formAddress,
      formCount,
      formBitWidth,
      formEndianness,
      formDisplayFormat,
    );
  };

  return (
    <FocusTrap onConfirm={save}>
      <div className="dialog">
        <h3>モニタリング項目を追加</h3>

        <div className="dialog-content">
          {serverInstances.length > 1 && (
            <div className="dialog-row">
              <label>プロトコル:</label>
              <select
                value={formProtocolType}
                onChange={(e) => handleFormProtocolChange(e.target.value)}
              >
                {serverInstances.map((inst) => (
                  <option key={inst.protocolType} value={inst.protocolType}>
                    {inst.displayName} ({inst.variant})
                  </option>
                ))}
              </select>
            </div>
          )}

          <div className="dialog-row">
            <label>メモリエリア:</label>
            <select
              value={formArea}
              onChange={(e) => handleAreaChange(e.target.value)}
            >
              {formAreas.map((area) => (
                <option key={area.id} value={area.id}>
                  {area.displayName}
                </option>
              ))}
            </select>
          </div>

          <div className="dialog-row">
            <label>開始アドレス:</label>
            <AddressInput
              area={targetArea}
              value={formAddress}
              onChange={setFormAddress}
              invalid={!!rangeError}
            />
          </div>
          <div className="dialog-row">
            <AddressRangeHint
              className="monitoring-range"
              range={addressRangeText(targetArea)}
              error={rangeError}
            />
          </div>

          <div className="dialog-row">
            <label>個数:</label>
            <input
              type="number"
              min="1"
              max="100"
              value={formCount}
              onChange={(e) => setFormCount(parseInt(e.target.value) || 1)}
            />
          </div>

          {(!targetIsBit || oneBit) && (
            <div className="dialog-row">
              <label>ビット幅:</label>
              <select
                value={oneBit ? "1" : formBitWidth}
                onChange={(e) => handleBitWidthChange(e.target.value)}
              >
                {bitViewArea && <option value="1">1bit</option>}
                {BIT_WIDTHS.map((b) => (
                  <option key={b.value} value={b.value}>
                    {b.label}
                  </option>
                ))}
              </select>
            </div>
          )}

          {!targetIsBit && (
            <>
              <div className="dialog-row">
                <label>エンディアン:</label>
                <select
                  value={formEndianness}
                  onChange={(e) =>
                    setFormEndianness(e.target.value as Endianness)
                  }
                >
                  {ENDIANNESS_OPTIONS.map((e) => (
                    <option key={e.value} value={e.value}>
                      {e.label}
                    </option>
                  ))}
                </select>
              </div>

              <div className="dialog-row">
                <label>表示形式:</label>
                <select
                  value={formDisplayFormat}
                  onChange={(e) =>
                    setFormDisplayFormat(e.target.value as DisplayFormat)
                  }
                >
                  {DISPLAY_FORMATS.map((f) => (
                    <option key={f.value} value={f.value}>
                      {f.label}
                    </option>
                  ))}
                </select>
              </div>
            </>
          )}
        </div>

        <div className="dialog-buttons">
          <button onClick={onClose} className="btn-secondary">
            キャンセル
          </button>
          <button
            onClick={save}
            className="btn-primary"
            disabled={!!rangeError}
            title={rangeError ?? undefined}
          >
            追加
          </button>
        </div>
      </div>
    </FocusTrap>
  );
}
