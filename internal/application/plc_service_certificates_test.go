package application

import (
	"strings"
	"testing"

	"karakuri/internal/domain/protocol/protocoltest"
)

// 証明書のストアを持つ、フェイクのサーバーとクライアントのファクトリー
type certServerFactory struct {
	*protocoltest.Factory
	*protocoltest.CertificateStore
}

type certClientFactory struct {
	*protocoltest.ClientFactory
	*protocoltest.CertificateStore
}

func TestCertificates_ServerAndClient(t *testing.T) {
	svc := newTestService(t)
	serverStore, clientStore := protocoltest.NewCertificateStore(), protocoltest.NewCertificateStore()
	svc.RegisterFactory(certServerFactory{protocoltest.NewFactory("secure", "v", "Secure"), serverStore})
	svc.RegisterClientFactory(certClientFactory{protocoltest.NewClientFactory("secure-client"), clientStore})
	if err := svc.AddServer("secure", ""); err != nil {
		t.Fatal(err)
	}

	// 証明書のストアを持つものだけに、フラグが立つ
	for _, inst := range svc.GetServerInstances() {
		if inst.SupportsCertificates != (inst.ProtocolType == "secure") {
			t.Errorf("%s: SupportsCertificates = %v", inst.ProtocolType, inst.SupportsCertificates)
		}
	}
	for _, p := range svc.GetClientProtocols() {
		if p.SupportsCertificates != (p.ProtocolType == "secure-client") {
			t.Errorf("%s: SupportsCertificates = %v", p.ProtocolType, p.SupportsCertificates)
		}
	}

	list, err := svc.GetCertificates(CertificateKindServer, "secure")
	if err != nil {
		t.Fatal(err)
	}
	if list.Dir != "fake-pki" || len(list.Certificates) != 2 || list.Certificates[1].Status != "rejected" || list.Certificates[1].Expired {
		t.Fatalf("一覧 = %+v", list)
	}

	// サーバーのストアで信頼しても、クライアントのストアは変わらない
	if err := svc.SetCertificateTrusted(CertificateKindServer, "secure", "PEER", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.GetCertificates(CertificateKindServer, "secure"); got.Certificates[1].Status != "trusted" {
		t.Errorf("サーバー: %+v", got.Certificates[1])
	}
	if got, _ := svc.GetCertificates(CertificateKindClient, "secure-client"); got.Certificates[1].Status != "rejected" {
		t.Errorf("クライアント: %+v", got.Certificates[1])
	}
	if err := svc.DeleteCertificate(CertificateKindClient, "secure-client", "PEER"); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.GetCertificates(CertificateKindClient, "secure-client"); len(got.Certificates) != 1 {
		t.Errorf("消したあと: %+v", got.Certificates)
	}

	// ログに残る（サーバーの操作は、そのサーバーへの行き先つき）
	found := false
	for _, e := range svc.GetAppLogs() {
		if strings.Contains(e.Message, "証明書 PEER を信頼しました") && e.Target == "server:secure" {
			found = true
		}
	}
	if !found {
		t.Errorf("信頼したログがない: %+v", svc.GetAppLogs())
	}

	for name, err := range map[string]error{
		"種類が違う":     func() error { _, err := svc.GetCertificates("x", "secure"); return err }(),
		"ないプロトコル":   func() error { _, err := svc.GetCertificates(CertificateKindServer, "nope"); return err }(),
		"ストアを持たない":  func() error { _, err := svc.GetCertificates(CertificateKindServer, "modbus-tcp"); return err }(),
		"ない証明書":     svc.SetCertificateTrusted(CertificateKindServer, "secure", "NONE", true),
		"自分の証明書を消す": svc.DeleteCertificate(CertificateKindServer, "secure", "OWN"),
	} {
		if err == nil {
			t.Errorf("%s: エラーにならない", name)
		}
	}
}
