package application

import (
	"fmt"
	"time"

	"karakuri/internal/domain/protocol"
)

// 証明書のストア（protocol.CertificateManager）の一覧と、相手の証明書の信頼・拒否・削除。
// ストアはファクトリーごとに 1 つなので、サーバー（kind = "server"）かクライアント（"client"）と、プロトコルタイプで指す。
// クライアントのストアは、そのプロトコルの全部の接続先で共通

const (
	CertificateKindServer = "server"
	CertificateKindClient = "client"
)

// CertificateListDTO は、証明書のストアの中身
type CertificateListDTO struct {
	Dir          string           `json:"dir"` // ストアのフォルダ
	Certificates []CertificateDTO `json:"certificates"`
}

// CertificateDTO は、証明書 1 つ
type CertificateDTO struct {
	Thumbprint     string `json:"thumbprint"`
	Status         string `json:"status"` // own / trusted / rejected
	Subject        string `json:"subject"`
	ApplicationURI string `json:"applicationUri"`
	KeyType        string `json:"keyType"`
	NotBefore      string `json:"notBefore"` // RFC 3339
	NotAfter       string `json:"notAfter"`
	Expired        bool   `json:"expired"` // 有効期間の外
	File           string `json:"file"`
}

// certificateManager は、kind と protocolType で指したストアを返す
func (s *PLCService) certificateManager(kind, protocolType string) (protocol.CertificateManager, error) {
	var f interface{}
	switch kind {
	case CertificateKindServer:
		s.mu.RLock()
		sf, ok := s.factories[protocol.ProtocolType(protocolType)]
		s.mu.RUnlock()
		if ok {
			f = sf
		}
	case CertificateKindClient:
		m := s.clients
		m.mu.Lock()
		cf, ok := m.factories[protocol.ProtocolType(protocolType)]
		m.mu.Unlock()
		if ok {
			f = cf
		}
	default:
		return nil, fmt.Errorf("証明書の種類は server か client です: %s", kind)
	}
	if f == nil {
		return nil, fmt.Errorf("プロトコル %s がありません", protocolType)
	}
	cm, ok := f.(protocol.CertificateManager)
	if !ok {
		return nil, fmt.Errorf("プロトコル %s は証明書を使いません", protocolType)
	}
	return cm, nil
}

// GetCertificates は、証明書のストアの一覧を返す
func (s *PLCService) GetCertificates(kind, protocolType string) (*CertificateListDTO, error) {
	cm, err := s.certificateManager(kind, protocolType)
	if err != nil {
		return nil, err
	}
	list, err := cm.Certificates()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	dto := &CertificateListDTO{Dir: list.Dir, Certificates: make([]CertificateDTO, len(list.Certificates))}
	for i, c := range list.Certificates {
		dto.Certificates[i] = CertificateDTO{
			Thumbprint: c.Thumbprint, Status: c.Status, Subject: c.Subject, ApplicationURI: c.ApplicationURI,
			KeyType: c.KeyType, NotBefore: c.NotBefore.Format(time.RFC3339), NotAfter: c.NotAfter.Format(time.RFC3339),
			Expired: now.Before(c.NotBefore) || now.After(c.NotAfter), File: c.File,
		}
	}
	return dto, nil
}

// certificateLogSource は、証明書の操作のログの出どころ
func (s *PLCService) certificateLogSource(kind, protocolType string) (source, target string) {
	if kind == CertificateKindServer {
		return s.serverLogSource(protocolType), "server:" + protocolType
	}
	return "クライアントの証明書（" + protocolType + "）", ""
}

// SetCertificateTrusted は、相手の証明書を信頼する（trusted = true）か、拒否の一覧に戻す
func (s *PLCService) SetCertificateTrusted(kind, protocolType, thumbprint string, trusted bool) (err error) {
	defer func() {
		source, target := s.certificateLogSource(kind, protocolType)
		ok, fail := "証明書 "+thumbprint+" を信頼しました", "証明書を信頼できませんでした"
		if !trusted {
			ok, fail = "証明書 "+thumbprint+" を拒否の一覧に戻しました", "証明書を拒否の一覧に戻せませんでした"
		}
		s.appLog.Scope(source, target).Result(ok, fail, err)
	}()
	cm, err := s.certificateManager(kind, protocolType)
	if err != nil {
		return err
	}
	return cm.SetCertificateTrusted(thumbprint, trusted)
}

// DeleteCertificate は、相手の証明書をストアから消す
func (s *PLCService) DeleteCertificate(kind, protocolType, thumbprint string) (err error) {
	defer func() {
		source, target := s.certificateLogSource(kind, protocolType)
		s.appLog.Scope(source, target).Result("証明書 "+thumbprint+" を消しました", "証明書を消せませんでした", err)
	}()
	cm, err := s.certificateManager(kind, protocolType)
	if err != nil {
		return err
	}
	return cm.DeleteCertificate(thumbprint)
}

func isCertificateManager(f interface{}) bool {
	_, ok := f.(protocol.CertificateManager)
	return ok
}
