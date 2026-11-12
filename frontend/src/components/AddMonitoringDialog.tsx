import { useState } from "react";
import { FocusTrap } from "./FocusTrap";
import { application } from "../../wailsjs/go/models";

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
  const [formEndianness, setFormEndianness] = useState<Endianness>("big");
  const [formDisplayFormat, setFormDisplayFormat] =
    useState<DisplayFormat>("decimal");

  const formProtocolType =
    selectedProtocolType || serverInstances[0]?.protocolType || "";
  const formAreas = memoryAreasByProtocol[formProtocolType] || [];
  const formArea =
    selectedArea ||
    formAreas.find((a) => !a.isBit)?.id ||
    formAreas[0]?.id ||
    "";

  // フォームのプロトコル変更（エリアは、そのプロトコルの既定に戻す）
  const handleFormProtocolChange = (protocolType: string) => {
    setSelectedProtocolType(protocolType);
    setSelectedArea("");
  };

  // 選択されたエリアがビットタイプかどうか
  const selectedAreaIsBit =
    formAreas.find((a) => a.id === formArea)?.isBit ?? false;
  const isOneOriginFormArea =
    formAreas.find((a) => a.id === formArea)?.oneOrigin ?? false;

  return (
    <FocusTrap
      onConfirm={() =>
        onSave(
          formProtocolType,
          formArea,
          formAddress,
          formCount,
          formBitWidth,
          formEndianness,
          formDisplayFormat,
        )
      }
    >
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
              onChange={(e) => setSelectedArea(e.target.value)}
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
            <input
              type="number"
              min={isOneOriginFormArea ? "1" : "0"}
              max="65535"
              value={isOneOriginFormArea ? formAddress + 1 : formAddress}
              onChange={(e) => {
                const v =
                  parseInt(e.target.value) || (isOneOriginFormArea ? 1 : 0);
                setFormAddress(isOneOriginFormArea ? Math.max(0, v - 1) : v);
              }}
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

          {!selectedAreaIsBit && (
            <>
              <div className="dialog-row">
                <label>ビット幅:</label>
                <select
                  value={formBitWidth}
                  onChange={(e) =>
                    setFormBitWidth(parseInt(e.target.value) as BitWidth)
                  }
                >
                  {BIT_WIDTHS.map((b) => (
                    <option key={b.value} value={b.value}>
                      {b.label}
                    </option>
                  ))}
                </select>
              </div>

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
            onClick={() =>
              onSave(
                formProtocolType,
                formArea,
                formAddress,
                formCount,
                formBitWidth,
                formEndianness,
                formDisplayFormat,
              )
            }
            className="btn-primary"
          >
            追加
          </button>
        </div>
      </div>
    </FocusTrap>
  );
}
