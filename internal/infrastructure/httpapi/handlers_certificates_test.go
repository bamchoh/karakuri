package httpapi

import (
	"net/http"
	"testing"

	"karakuri/internal/application"
	"karakuri/internal/domain/protocol/protocoltest"
)

type certServerFactory struct {
	*protocoltest.Factory
	*protocoltest.CertificateStore
}

func TestCertificates_API(t *testing.T) {
	api := newTestAPI(t)
	api.svc.RegisterFactory(certServerFactory{protocoltest.NewFactory("secure", "v", "Secure"), protocoltest.NewCertificateStore()})

	var list application.CertificateListDTO
	api.do("GET", "/api/certificates/server/secure", "").status(http.StatusOK).json(&list)
	if len(list.Certificates) != 2 || list.Certificates[1].Thumbprint != "PEER" || list.Certificates[1].Status != "rejected" {
		t.Fatalf("一覧 = %+v", list)
	}

	api.do("PUT", "/api/certificates/server/secure/PEER", `{"trusted":true}`).status(http.StatusNoContent)
	api.do("GET", "/api/certificates/server/secure", "").status(http.StatusOK).json(&list)
	if list.Certificates[1].Status != "trusted" {
		t.Errorf("信頼したあと = %+v", list.Certificates[1])
	}
	api.do("PUT", "/api/certificates/server/secure/PEER", `{}`).isError(http.StatusBadRequest)
	api.do("PUT", "/api/certificates/server/secure/NONE", `{"trusted":false}`).isError(http.StatusBadRequest)

	api.do("DELETE", "/api/certificates/server/secure/PEER", "").status(http.StatusNoContent)
	api.do("GET", "/api/certificates/server/secure", "").status(http.StatusOK).json(&list)
	if len(list.Certificates) != 1 {
		t.Errorf("消したあと = %+v", list.Certificates)
	}

	api.do("GET", "/api/certificates/server/fake-a", "").isError(http.StatusBadRequest) // 証明書を使わないプロトコル
	api.do("GET", "/api/certificates/other/secure", "").isError(http.StatusBadRequest)
}
