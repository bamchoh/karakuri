package protocol

import "time"

// CertificateManager は、証明書のストア（自分の証明書と、相手の証明書の信頼・拒否の一覧）を持つプロトコルが実装する。
// サーバーのファクトリー（ServerFactory）とクライアントのファクトリー（ClientFactory）のどちらにも付けられる。
// ストアはファクトリーごとに 1 つ（OPC UA のクライアントなら、全部の接続先で共通）。
//
// ホストは、実装していればサーバー・接続先の画面に「証明書」のタブを出す。証明書の中身の意味は持たない
type CertificateManager interface {
	// Certificates は、ストアの証明書の一覧を返す
	Certificates() (CertificateList, error)
	// SetCertificateTrusted は、相手の証明書を信頼する（trusted = true）か、拒否の一覧に戻す
	SetCertificateTrusted(thumbprint string, trusted bool) error
	// DeleteCertificate は、相手の証明書をストアから消す（自分の証明書は消せない）
	DeleteCertificate(thumbprint string) error
}

// 証明書の状態（CertificateInfo.Status）
const (
	CertificateOwn      = "own"      // 自分の証明書
	CertificateTrusted  = "trusted"  // 信頼した相手の証明書
	CertificateRejected = "rejected" // 拒否した（まだ信頼していない）相手の証明書
)

// CertificateList は、証明書のストアの中身
type CertificateList struct {
	Dir          string            // ストアのフォルダ（利用者が、自分の証明書を相手に渡すときに開く）
	Certificates []CertificateInfo // 自分の証明書・信頼・拒否の順
}

// CertificateInfo は、証明書 1 つ
type CertificateInfo struct {
	Thumbprint     string    // SHA-1 の拇印（16 進の大文字）。操作で証明書を指すのに使う
	Status         string    // CertificateOwn / CertificateTrusted / CertificateRejected
	Subject        string    // 主体の CN
	ApplicationURI string    // 証明書の SAN の URI（アプリケーション URI）
	KeyType        string    // 鍵の種類（RSA 2048 / ECC P-256 など）
	NotBefore      time.Time // 有効期間の始まり
	NotAfter       time.Time // 有効期間の終わり
	File           string    // ストアの中のファイルのパス
}
