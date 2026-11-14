import { useState, useEffect, useCallback } from "react";
import {
  GetClientSubscription,
  AddClientSubscriptionItems,
  RemoveClientSubscriptionItem,
  SetClientSubscriptionInterval,
  StartClientSubscription,
  StopClientSubscription,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import { Play, Square, Trash2, SquarePlus } from "lucide-react";
import { DynamicField, visibleFields } from "./DynamicField";
import { clientStatusLabel, formatClientTime } from "./clientFormat";
import { ClientBrowseDialog, applyBrowsePick } from "./ClientBrowseDialog";

interface ClientSubscriptionViewProps {
  client: application.ClientDTO;
  protocol: application.ClientProtocolDTO;
  // 開始する前に呼ぶ（保存していない接続先の設定を保存する）。false なら開始しない
  beforeStart: () => Promise<boolean>;
  onClientsChanged: () => Promise<unknown>;
}

// 画面が読み直す間隔（通知の間隔とは別）
const REFRESH_MS = 200;

const noPorts: string[] = [];
const noop = () => {};

// 監視全体の状態の表示
const stateLabels: Record<string, { text: string; status: string }> = {
  connecting: { text: "接続中…", status: "none" },
  ok: { text: "監視中", status: "ok" },
  error: { text: "つながっていません", status: "error" },
};

function defaultParams(fields: application.FieldDTO[]): Record<string, any> {
  const params: Record<string, any> = {};
  for (const f of fields) params[f.name] = f.default;
  return params;
}

// サブスクリプション（値の変化の通知）: 監視項目を登録しておくと、値が変わったときに機器の側から知らせが届く。
// ポーリングと違い、こちらからは周期的に読まない。切れている間は、最後に届いた値を薄く表示する
export function ClientSubscriptionView({
  client,
  protocol,
  beforeStart,
  onClientsChanged,
}: ClientSubscriptionViewProps) {
  const fields = protocol.monitoredItemFields || [];
  const [subscription, setSubscription] =
    useState<application.ClientSubscriptionDTO | null>(null);
  const [intervalInput, setIntervalInput] = useState<string>("");
  // 間隔の欄を編集している間は、読み直した値で上書きしない
  const [editingInterval, setEditingInterval] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [addParams, setAddParams] = useState<Record<string, any> | null>(null);
  // アドレスの一覧（参照のダイアログ）を開いている項目
  const [browseField, setBrowseField] = useState<application.FieldDTO | null>(
    null,
  );

  const load = useCallback(async () => {
    try {
      const s = await GetClientSubscription(client.id);
      setSubscription(s);
      return s;
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

  const actualInterval = subscription?.intervalMs;
  useEffect(() => {
    if (!editingInterval && actualInterval !== undefined) {
      setIntervalInput(String(actualInterval));
    }
  }, [editingInterval, actualInterval]);

  const run = async (fn: () => Promise<unknown>) => {
    let ok = true;
    try {
      setError(null);
      await fn();
    } catch (e) {
      setError(String(e));
      ok = false;
    }
    await load();
    return ok;
  };

  // 入れた間隔を反映する。反映できなかったら（範囲外など）、エラーを出して実際の間隔に戻す
  const applyInterval = async () => {
    const ms = parseInt(intervalInput, 10);
    if (!isNaN(ms) && ms !== subscription?.intervalMs) {
      await run(() => SetClientSubscriptionInterval(client.id, ms));
    }
    setEditingInterval(false);
  };

  const handleStartStop = () =>
    run(async () => {
      if (subscription?.running) await StopClientSubscription(client.id);
      else if (await beforeStart()) await StartClientSubscription(client.id);
      await onClientsChanged();
    });

  const params = addParams ?? defaultParams(fields);

  const handleAdd = async () => {
    // 追加できたら、入力欄を空に戻す（同じ項目を続けて足さないように）
    if (await run(() => AddClientSubscriptionItems(client.id, params))) {
      setAddParams(null);
    }
  };

  const items = subscription?.items || [];
  const running = !!subscription?.running;
  const state = running ? stateLabels[subscription?.state ?? ""] : undefined;
  // 監視できていない間（停止中・つながっていない）は、届いていた値を薄く表示する
  const live = running && subscription?.state === "ok";

  return (
    <div className="client-polling client-subscription">
      {error && <div className="error-message config-error">{error}</div>}

      <div className="vscode-setting-item client-polling-controls">
        <label
          className="client-polling-interval"
          title="機器が値の変化を調べて、通知をまとめて送る間隔（OPC UA の PublishingInterval）"
        >
          通知の間隔（ms）
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
            aria-label="通知の間隔（ms）"
          />
        </label>
        <button
          className={running ? "btn-danger" : "btn-primary"}
          onClick={handleStartStop}
          disabled={!subscription || (!running && items.length === 0)}
        >
          {running ? (
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
        {state && (
          <span
            className={`client-status client-status-${state.status}`}
            data-testid="client-subscription-state"
          >
            {state.text}
          </span>
        )}
      </div>
      {running && subscription?.error && (
        <div className="client-result-error client-subscription-error">
          {subscription.error}
        </div>
      )}

      <div className="vscode-setting-category">監視項目の追加</div>
      <div className="client-polling-add-fields">
        {visibleFields(fields, params).map((field) => (
          <DynamicField
            key={field.name}
            field={field}
            value={params[field.name]}
            onChange={(v) => setAddParams({ ...params, [field.name]: v })}
            disabled={false}
            serialPorts={noPorts}
            onRefreshPorts={noop}
            onBrowse={async () => {
              if (await beforeStart()) setBrowseField(field);
            }}
          />
        ))}
      </div>
      {browseField && (
        <ClientBrowseDialog
          clientId={client.id}
          field={browseField}
          onPick={(picked) =>
            setAddParams(applyBrowsePick(browseField, fields, params, picked))
          }
          onClose={() => setBrowseField(null)}
        />
      )}
      <div className="client-send-row">
        <button className="btn-secondary" onClick={handleAdd}>
          <SquarePlus className="icon-left" size={14} />
          項目を追加
        </button>
      </div>

      <div className="vscode-setting-category">監視項目（{items.length}）</div>
      {items.length === 0 ? (
        <div className="client-note client-polling-empty">
          監視項目がありません。上の欄から追加してください。
        </div>
      ) : (
        <div className="client-poll-items">
          {items.map((item) => {
            const failed =
              item.status === "exception" || item.status === "error";
            const stale = !live || failed;
            return (
              <div
                key={item.id}
                className="client-poll-item"
                data-testid="client-subscription-item"
              >
                <div className="client-result-header">
                  <span
                    className={`client-status client-status-${item.status || "none"}`}
                  >
                    {item.status
                      ? clientStatusLabel(item.status)
                      : running
                        ? "待機中"
                        : "停止中"}
                  </span>
                  <span className="client-result-summary">{item.summary}</span>
                  <span className="client-poll-counts">
                    通知 {item.count} 回
                  </span>
                  {item.valuesAt && (
                    <span className="client-result-elapsed">
                      {formatClientTime(item.valuesAt)}
                    </span>
                  )}
                  <button
                    className="btn-secondary btn-small"
                    onClick={() =>
                      run(async () => {
                        await RemoveClientSubscriptionItem(client.id, item.id);
                        await onClientsChanged();
                      })
                    }
                    title="監視項目を削除"
                    aria-label="監視項目を削除"
                  >
                    <Trash2 size={12} />
                  </button>
                </div>
                {item.error && (
                  <div className="client-result-error">{item.error}</div>
                )}
                {item.values && item.values.length > 0 && (
                  <div className={`client-poll-values${stale ? " stale" : ""}`}>
                    {item.values.map((v, i) => (
                      <div key={i} className="client-poll-value">
                        <span className="client-poll-value-label">
                          {v.label}
                        </span>
                        <span className="client-poll-value-text">
                          {v.value}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
