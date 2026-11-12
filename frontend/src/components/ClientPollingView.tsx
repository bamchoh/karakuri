import { useState, useEffect, useCallback } from "react";
import {
  GetClientPolling,
  AddClientPollItem,
  UpdateClientPollItem,
  RemoveClientPollItem,
  SetClientPollInterval,
  SetClientFailThreshold,
  StartClientPolling,
  StopClientPolling,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { Play, Square, Trash2, SquarePlus } from "lucide-react";
import { DynamicField } from "./DynamicField";
import { DISPLAY_FORMATS } from "../constants/register";
import { formatSingleWord } from "../utils/register";
import type { DisplayFormat } from "../types/register";
import {
  clientStatusLabel,
  formatClientElapsed,
  formatClientTime,
} from "./clientFormat";

interface ClientPollingViewProps {
  client: application.ClientDTO;
  protocol: application.ClientProtocolDTO;
  // 開始する前に呼ぶ（保存していない接続先の設定を保存する）。false なら開始しない
  beforeStart: () => Promise<boolean>;
  onClientsChanged: () => Promise<unknown>;
}

// 画面が読み直す間隔（ポーリングの周期とは別）
const REFRESH_MS = 200;

const noPorts: string[] = [];
const noop = () => {};

// 読み取った値の表示形式。ホストは値の意味を持たない（項目に保存するだけ）
const SIGNED = "signed";
const FORMATS = [
  ...DISPLAY_FORMATS.slice(0, 1),
  { value: SIGNED, label: "10進数（符号付き）" },
  ...DISPLAY_FORMATS.slice(1),
];

function formatValue(v: application.ClientResultValueDTO, format: string) {
  if (v.bit !== undefined && v.bit !== null) return v.bit ? "ON" : "OFF";
  if (v.word !== undefined && v.word !== null) {
    if (format === SIGNED)
      return String(v.word >= 0x8000 ? v.word - 0x10000 : v.word);
    const f = (
      FORMATS.some((x) => x.value === format) ? format : "decimal"
    ) as DisplayFormat;
    return formatSingleWord(v.word, f);
  }
  return v.value;
}

function defaultParams(cmd: application.ClientCommandDTO): Record<string, any> {
  const params: Record<string, any> = {};
  for (const f of cmd.fields) params[f.name] = f.default;
  return params;
}

// ポーリング（周期的な読み取り）: 読み取り項目を登録して、周期ごとに上から順に読む。
// 読めなかった項目は、最後に読めた値を薄く表示する
export function ClientPollingView({
  client,
  protocol,
  beforeStart,
  onClientsChanged,
}: ClientPollingViewProps) {
  const pollable = (protocol.commands || []).filter((c) => c.pollable);
  // 値をビット・ワードで返すコマンド（Modbus の読み取りなど）だけ、表示形式を選べる
  const wordValueCommands = new Set(
    pollable.filter((c) => c.wordValues).map((c) => c.id),
  );
  const [polling, setPolling] = useState<application.ClientPollingDTO | null>(
    null,
  );
  const [intervalInput, setIntervalInput] = useState<string>("");
  // 周期の欄を編集している間は、読み直した値で上書きしない
  const [editingInterval, setEditingInterval] = useState(false);
  const [thresholdInput, setThresholdInput] = useState<string>("");
  const [editingThreshold, setEditingThreshold] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [addCommandId, setAddCommandId] = useState(pollable[0]?.id ?? "");
  const [addParams, setAddParams] = useState<
    Record<string, Record<string, any>>
  >({});

  const load = useCallback(async () => {
    try {
      const p = await GetClientPolling(client.id);
      setPolling(p);
      return p;
    } catch (e) {
      setError(String(e));
      return null;
    }
  }, [client.id]);

  useEffect(() => {
    load();
    const timer = setInterval(load, REFRESH_MS);
    return () => clearInterval(timer);
  }, [load]);

  // 周期の欄は、編集していなければ実際の周期を表示する（API など、ほかから変えられたときも追う）
  const actualInterval = polling?.intervalMs;
  useEffect(() => {
    if (!editingInterval && actualInterval !== undefined) {
      setIntervalInput(String(actualInterval));
    }
  }, [editingInterval, actualInterval]);

  const run = async (fn: () => Promise<unknown>) => {
    try {
      setError(null);
      await fn();
    } catch (e) {
      setError(String(e));
    }
    await load();
  };

  const actualThreshold = polling?.failThreshold;
  useEffect(() => {
    if (!editingThreshold && actualThreshold !== undefined) {
      setThresholdInput(String(actualThreshold));
    }
  }, [editingThreshold, actualThreshold]);

  const applyThreshold = async () => {
    const n = parseInt(thresholdInput, 10);
    if (!isNaN(n) && n !== polling?.failThreshold) {
      await run(() => SetClientFailThreshold(client.id, n));
    }
    setEditingThreshold(false);
  };

  // 入れた周期を反映する。反映できなかったら（範囲外など）、エラーを出して実際の周期に戻す
  const applyInterval = async () => {
    const ms = parseInt(intervalInput, 10);
    if (!isNaN(ms) && ms !== polling?.intervalMs) {
      await run(() => SetClientPollInterval(client.id, ms));
    }
    setEditingInterval(false);
  };

  const handleStartStop = () =>
    run(async () => {
      if (polling?.running) await StopClientPolling(client.id);
      else if (await beforeStart()) await StartClientPolling(client.id);
      await onClientsChanged();
    });

  const addCommand = pollable.find((c) => c.id === addCommandId);
  const params = addCommand
    ? (addParams[addCommand.id] ?? defaultParams(addCommand))
    : {};

  const handleAdd = () => {
    if (!addCommand) return;
    run(() => AddClientPollItem(client.id, addCommand.id, params, "decimal"));
  };

  const handleFormat = (item: application.ClientPollItemDTO, format: string) =>
    run(() =>
      UpdateClientPollItem(
        client.id,
        item.id,
        item.commandId,
        item.params,
        format,
      ),
    );

  const items = polling?.items || [];
  const mapping = polling?.mapping;
  // 読み取り項目も、マッピングした変数もなければ、開始しても何もしない
  const nothingToDo = items.length === 0 && !(mapping && mapping.variables > 0);
  const overrun =
    polling?.running &&
    polling.cycles > 0 &&
    polling.lastCycleMs > polling.intervalMs;

  return (
    <div className="client-polling">
      {error && <div className="error-message config-error">{error}</div>}

      <div className="vscode-setting-item client-polling-controls">
        <label className="client-polling-interval">
          周期（ms）
          <input
            type="number"
            min={50}
            value={intervalInput}
            onChange={(e) => {
              setEditingInterval(true);
              setIntervalInput(e.target.value);
            }}
            onBlur={applyInterval}
            onKeyDown={(e) => {
              if (e.key === "Enter") applyInterval();
            }}
            aria-label="周期（ms）"
          />
        </label>
        <button
          className={polling?.running ? "btn-danger" : "btn-primary"}
          onClick={handleStartStop}
          disabled={!polling || (!polling.running && nothingToDo)}
        >
          {polling?.running ? (
            <>
              <Square className="icon-left" size={14} />
              停止
            </>
          ) : (
            <>
              <Play className="icon-left" size={14} />
              開始
            </>
          )}
        </button>
        {polling?.running && (
          <span className="client-note" data-testid="client-polling-cycles">
            {polling.cycles} 周 / 1 周{" "}
            {formatClientElapsed(polling.lastCycleMs)}
          </span>
        )}
        {overrun && (
          <span className="client-polling-overrun">
            1 周が周期より長くかかっています
          </span>
        )}
      </div>

      {mapping && (
        <>
          <div className="vscode-setting-category">変数のマッピング</div>
          <div className="vscode-setting-item client-mapping-summary">
            {mapping.variables === 0 ? (
              <div className="client-note">
                マッピングした変数はありません。変数画面のマッピングで、この接続先（
                {client.name}）を選ぶと、ポーリング中に変数と同期します。
              </div>
            ) : (
              <div
                className="client-mapping-counts"
                data-testid="client-mapping-summary"
              >
                <span>変数 {mapping.variables}</span>
                <span className="client-status client-status-ok">
                  正常 {mapping.ok}
                </span>
                {mapping.commError > 0 && (
                  <span className="client-status client-status-error">
                    通信エラー {mapping.commError}
                  </span>
                )}
                {mapping.writePending > 0 && (
                  <span className="client-status client-status-exception">
                    書き込み未反映 {mapping.writePending}
                  </span>
                )}
                {mapping.invalid > 0 && (
                  <span className="client-status client-status-error">
                    マッピング不可 {mapping.invalid}
                  </span>
                )}
              </div>
            )}
            <label className="client-polling-interval">
              続けて
              <input
                type="number"
                min={1}
                max={100}
                value={thresholdInput}
                onChange={(e) => {
                  setEditingThreshold(true);
                  setThresholdInput(e.target.value);
                }}
                onBlur={applyThreshold}
                onKeyDown={(e) => {
                  if (e.key === "Enter") applyThreshold();
                }}
                aria-label="通信エラーにするまでの失敗回数"
              />
              回読めなかったら「通信エラー」にする（値は最後に読めたまま残す）
            </label>
          </div>
        </>
      )}

      <div className="vscode-setting-category">読み取り項目の追加</div>
      <div className="vscode-setting-item">
        <div className="vscode-setting-label">コマンド</div>
        <div className="vscode-setting-control">
          <select
            value={addCommandId}
            onChange={(e) => setAddCommandId(e.target.value)}
            aria-label="読み取りのコマンド"
          >
            {pollable.map((c) => (
              <option key={c.id} value={c.id}>
                {c.displayName}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className="client-polling-add-fields">
        {addCommand?.fields.map((field) => (
          <DynamicField
            key={`${addCommand.id}:${field.name}`}
            field={field}
            value={params[field.name]}
            onChange={(v) =>
              setAddParams((prev) => ({
                ...prev,
                [addCommand.id]: { ...params, [field.name]: v },
              }))
            }
            disabled={false}
            serialPorts={noPorts}
            onRefreshPorts={noop}
          />
        ))}
      </div>
      <div className="client-send-row">
        <button
          className="btn-secondary"
          onClick={handleAdd}
          disabled={!addCommand}
        >
          <SquarePlus className="icon-left" size={14} />
          項目を追加
        </button>
      </div>

      <div className="vscode-setting-category">
        読み取り項目（{items.length}）
      </div>
      {items.length === 0 ? (
        <div className="client-note client-polling-empty">
          読み取り項目がありません。上の欄から追加してください。
        </div>
      ) : (
        <div className="client-poll-items">
          {items.map((item) => {
            const stale = item.status !== "ok";
            return (
              <div
                key={item.id}
                className="client-poll-item"
                data-testid="client-poll-item"
              >
                <div className="client-result-header">
                  <span
                    className={`client-status client-status-${item.status || "none"}`}
                  >
                    {item.status ? clientStatusLabel(item.status) : "未送信"}
                  </span>
                  <span className="client-result-summary">{item.summary}</span>
                  <span className="client-poll-counts">
                    成功 {item.okCount} / 失敗 {item.errorCount}
                  </span>
                  {item.at && (
                    <span className="client-result-elapsed">
                      {formatClientTime(item.at)}
                      {item.status !== "skipped" &&
                        `（${formatClientElapsed(item.elapsedMs)}）`}
                    </span>
                  )}
                  {wordValueCommands.has(item.commandId) && (
                    <select
                      className="client-poll-format"
                      value={item.format || "decimal"}
                      onChange={(e) => handleFormat(item, e.target.value)}
                      aria-label="表示形式"
                    >
                      {FORMATS.map((f) => (
                        <option key={f.value} value={f.value}>
                          {f.label}
                        </option>
                      ))}
                    </select>
                  )}
                  <button
                    className="btn-secondary btn-small"
                    onClick={() =>
                      run(() => RemoveClientPollItem(client.id, item.id))
                    }
                    title="読み取り項目を削除"
                    aria-label="読み取り項目を削除"
                  >
                    <Trash2 size={12} />
                  </button>
                </div>
                {item.error && (
                  <div className="client-result-error">{item.error}</div>
                )}
                {item.values && item.values.length > 0 && (
                  <>
                    {stale && item.valuesAt && (
                      <div className="client-note">
                        {formatClientTime(item.valuesAt)} に読めた値
                      </div>
                    )}
                    <div
                      className={`client-poll-values${stale ? " stale" : ""}`}
                    >
                      {item.values.map((v, i) => (
                        <div key={i} className="client-poll-value">
                          <span className="client-poll-value-label">
                            {v.label}
                          </span>
                          <span className="client-poll-value-text">
                            {formatValue(v, item.format)}
                          </span>
                        </div>
                      ))}
                    </div>
                  </>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
