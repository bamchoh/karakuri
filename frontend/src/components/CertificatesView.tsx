import { ReactNode, useCallback, useEffect, useState } from "react";
import {
  DeleteCertificate,
  GetCertificates,
  SetCertificateTrusted,
} from "../../wailsjs/go/main/App";
import { application } from "../../wailsjs/go/models";
import {
  formatDate,
  groupCertificates,
  shortThumbprint,
} from "../utils/certificates";

// 証明書のタブ（サーバーと、クライアントの接続先で共通）。
// 証明書のストア（自分の証明書・信頼した相手・拒否した相手）を一覧にし、相手の証明書を信頼・拒否・削除する。
// 「信頼したものだけ」にしていると、知らない相手とつながろうとするたびに拒否の一覧に増えるので、開いている間は読み直す

const RELOAD_MS = 2000;

interface CertificatesViewProps {
  kind: "server" | "client";
  protocolType: string;
  note?: string; // 一覧の上に出す説明（クライアントは、全部の接続先で共通であること）
}

export function CertificatesView({
  kind,
  protocolType,
  note,
}: CertificatesViewProps) {
  const [list, setList] = useState<application.CertificateListDTO | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setList(await GetCertificates(kind, protocolType));
    } catch (e) {
      setError(String(e));
    }
  }, [kind, protocolType]);

  useEffect(() => {
    load();
    const timer = setInterval(load, RELOAD_MS);
    return () => clearInterval(timer);
  }, [load]);

  const run = async (action: () => Promise<void>) => {
    setBusy(true);
    try {
      await action();
      setError(null);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
      await load();
    }
  };

  const groups = groupCertificates(list?.certificates ?? []);

  const table = (
    certs: application.CertificateDTO[],
    actions?: (c: application.CertificateDTO) => ReactNode,
  ) => (
    <table className="variable-table certificate-table">
      <thead>
        <tr>
          <th>名前（CN）</th>
          <th>アプリケーション URI</th>
          <th>鍵</th>
          <th>有効期限</th>
          <th>拇印</th>
          {actions && <th className="certificate-actions-col" />}
        </tr>
      </thead>
      <tbody>
        {certs.map((c) => (
          <tr key={`${c.status}:${c.thumbprint}`} title={c.file}>
            <td>{c.subject || "（なし）"}</td>
            <td className="certificate-uri">{c.applicationUri}</td>
            <td>{c.keyType}</td>
            <td className={c.expired ? "certificate-expired" : undefined}>
              {formatDate(c.notAfter)}
              {c.expired ? "（期限切れ）" : ""}
            </td>
            <td className="certificate-thumbprint" title={c.thumbprint}>
              {shortThumbprint(c.thumbprint)}
            </td>
            {actions && <td className="certificate-actions">{actions(c)}</td>}
          </tr>
        ))}
      </tbody>
    </table>
  );

  const removeButton = (c: application.CertificateDTO) => (
    <button
      className="btn-secondary btn-small"
      disabled={busy}
      onClick={() =>
        run(() => DeleteCertificate(kind, protocolType, c.thumbprint))
      }
      aria-label={`${c.subject} を削除`}
    >
      削除
    </button>
  );

  const trustButton = (c: application.CertificateDTO, trusted: boolean) => (
    <button
      className={`${trusted ? "btn-primary" : "btn-secondary"} btn-small`}
      disabled={busy}
      onClick={() =>
        run(() =>
          SetCertificateTrusted(kind, protocolType, c.thumbprint, trusted),
        )
      }
      aria-label={`${c.subject} ${trusted ? "を信頼する" : "の信頼をやめる"}`}
    >
      {trusted ? "信頼する" : "信頼をやめる"}
    </button>
  );

  return (
    <div className="certificates-view">
      {error && <div className="error-message config-error">{error}</div>}
      {list && (
        <div className="client-note certificate-dir">
          {note && <div>{note}</div>}
          <div>
            証明書のフォルダ: <code>{list.dir}</code>
          </div>
        </div>
      )}

      <div className="vscode-setting-category">
        このアプリの証明書（{groups.own.length}）
      </div>
      {groups.own.length === 0 ? (
        <div className="client-note">
          {kind === "server"
            ? "まだありません。暗号化したエンドポイントを設定して起動すると作ります。"
            : "まだありません。None 以外のセキュリティポリシーでつなぐと作ります。"}
        </div>
      ) : (
        <>
          <div className="client-note">
            相手の側で「信頼したものだけ」にしているときは、フォルダの own/certs
            にあるファイルを相手に渡して、信頼してもらいます。
          </div>
          {table(groups.own)}
        </>
      )}

      <div className="vscode-setting-category">
        拒否した証明書（{groups.rejected.length}）
      </div>
      {groups.rejected.length === 0 ? (
        <div className="client-note">
          ありません（「信頼したものだけ」にしていると、知らない相手の証明書がここに出ます）。
        </div>
      ) : (
        table(groups.rejected, (c) => (
          <>
            {trustButton(c, true)}
            {removeButton(c)}
          </>
        ))
      )}

      <div className="vscode-setting-category">
        信頼した証明書（{groups.trusted.length}）
      </div>
      {groups.trusted.length === 0 ? (
        <div className="client-note">ありません。</div>
      ) : (
        table(groups.trusted, (c) => (
          <>
            {trustButton(c, false)}
            {removeButton(c)}
          </>
        ))
      )}
    </div>
  );
}
