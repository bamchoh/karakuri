// 証明書の一覧（GetCertificates）の表示の組み立て。state に依存しない純粋な関数

// 表示に使う証明書の項目（application.CertificateDTO の一部）
export interface CertificateLike {
  thumbprint: string;
  status: string; // own / trusted / rejected
}

export interface CertificateGroups<T> {
  own: T[];
  trusted: T[];
  rejected: T[];
}

// groupCertificates は、証明書を自分・信頼・拒否に分ける（それぞれ、もとの順のまま）
export function groupCertificates<T extends CertificateLike>(
  certs: T[],
): CertificateGroups<T> {
  const groups: CertificateGroups<T> = { own: [], trusted: [], rejected: [] };
  for (const c of certs) {
    if (c.status === "own") groups.own.push(c);
    else if (c.status === "trusted") groups.trusted.push(c);
    else if (c.status === "rejected") groups.rejected.push(c);
  }
  return groups;
}

// formatDate は、RFC 3339 の時刻を「2031-11-16」の形（その PC の時刻での日付）にする。読めなければそのまま
export function formatDate(rfc3339: string): string {
  const d = new Date(rfc3339);
  if (Number.isNaN(d.getTime())) return rfc3339;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

// shortThumbprint は、拇印を 4 文字ずつ区切った先頭 4 組（表に収めるため。全部はホバーで出す）
export function shortThumbprint(thumbprint: string): string {
  const groups = thumbprint.match(/.{1,4}/g) ?? [];
  return groups.slice(0, 4).join(" ") + (groups.length > 4 ? " …" : "");
}
