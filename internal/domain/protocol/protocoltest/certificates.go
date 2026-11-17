package protocoltest

import (
	"fmt"
	"sync"
	"time"

	"karakuri/internal/domain/protocol"
)

// CertificateStore は、メモリの上の証明書のストア（protocol.CertificateManager のフェイク）。
// ファクトリーに埋め込んで使う（struct{ *protocoltest.Factory; *protocoltest.CertificateStore }）
type CertificateStore struct {
	mu    sync.Mutex
	Dir   string
	Items []protocol.CertificateInfo
}

// NewCertificateStore は、自分の証明書 1 つと、拒否した相手の証明書 1 つ（拇印 "PEER"）を持つストアを作る
func NewCertificateStore() *CertificateStore {
	now := time.Now()
	return &CertificateStore{Dir: "fake-pki", Items: []protocol.CertificateInfo{
		{Thumbprint: "OWN", Status: protocol.CertificateOwn, Subject: "me", KeyType: "RSA 2048", NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)},
		{Thumbprint: "PEER", Status: protocol.CertificateRejected, Subject: "peer", KeyType: "RSA 2048", NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)},
	}}
}

func (s *CertificateStore) Certificates() (protocol.CertificateList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return protocol.CertificateList{Dir: s.Dir, Certificates: append([]protocol.CertificateInfo(nil), s.Items...)}, nil
}

func (s *CertificateStore) peer(thumbprint string) (int, error) {
	for i, c := range s.Items {
		if c.Thumbprint == thumbprint && c.Status != protocol.CertificateOwn {
			return i, nil
		}
	}
	return -1, fmt.Errorf("証明書 %s がありません", thumbprint)
}

func (s *CertificateStore) SetCertificateTrusted(thumbprint string, trusted bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.peer(thumbprint)
	if err != nil {
		return err
	}
	s.Items[i].Status = protocol.CertificateRejected
	if trusted {
		s.Items[i].Status = protocol.CertificateTrusted
	}
	return nil
}

func (s *CertificateStore) DeleteCertificate(thumbprint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.peer(thumbprint)
	if err != nil {
		return err
	}
	s.Items = append(s.Items[:i], s.Items[i+1:]...)
	return nil
}
