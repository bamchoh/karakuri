package opcua

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/bamchoh/uastack/uacert"

	"karakuri/internal/domain/protocol"
)

// 証明書のストア（uastack の uacert.Store と同じフォルダの形）の一覧と、信頼・拒否の切り替え。
//
//	<root>/own/certs       自分の証明書（サーバーは起動したとき、クライアントはつないだときに作る）
//	<root>/trusted/certs   信頼した相手の証明書
//	<root>/rejected/certs  拒否した相手の証明書（「信頼したものだけ」にしているとき、知らない相手がつないでくると置かれる）
//
// uastack は、つなぐたびに trusted を読み直すので、ここで移せば、起動し直さなくても効く

var (
	_ protocol.CertificateManager = (*Factory)(nil)
	_ protocol.CertificateManager = (*ClientFactory)(nil)
)

// certStore は、1 つのストアのフォルダ
type certStore struct{ root string }

type storedCert struct {
	cert *x509.Certificate
	file string
}

func (s certStore) dir(name string) string { return filepath.Join(s.root, name, "certs") }

// read は、name（own / trusted / rejected）のフォルダの証明書を読む（読めないファイルは飛ばす）
func (s certStore) read(name string) []storedCert {
	entries, err := os.ReadDir(s.dir(name))
	if err != nil {
		return nil
	}
	var out []storedCert
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		file := filepath.Join(s.dir(name), e.Name())
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if blk, _ := pem.Decode(b); blk != nil {
			b = blk.Bytes
		}
		certs, err := x509.ParseCertificates(b)
		if err != nil {
			continue
		}
		for _, c := range certs {
			out = append(out, storedCert{cert: c, file: file})
		}
	}
	return out
}

func (s certStore) list() (protocol.CertificateList, error) {
	list := protocol.CertificateList{Dir: s.root, Certificates: []protocol.CertificateInfo{}}
	for _, status := range []string{protocol.CertificateOwn, protocol.CertificateTrusted, protocol.CertificateRejected} {
		certs := s.read(status)
		sort.Slice(certs, func(i, j int) bool { return certs[i].cert.Subject.CommonName < certs[j].cert.Subject.CommonName })
		for _, c := range certs {
			list.Certificates = append(list.Certificates, protocol.CertificateInfo{
				Thumbprint:     uacert.Thumbprint(c.cert.Raw),
				Status:         status,
				Subject:        c.cert.Subject.CommonName,
				ApplicationURI: uacert.CertificateURI(c.cert),
				KeyType:        keyType(c.cert),
				NotBefore:      c.cert.NotBefore,
				NotAfter:       c.cert.NotAfter,
				File:           c.file,
			})
		}
	}
	return list, nil
}

func keyType(c *x509.Certificate) string {
	switch k := c.PublicKey.(type) {
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA %d", k.N.BitLen())
	case *ecdsa.PublicKey:
		return "ECC " + k.Curve.Params().Name
	}
	return c.PublicKeyAlgorithm.String()
}

// find は、相手の証明書（trusted / rejected）から拇印で探す
func (s certStore) find(thumbprint string) (found []storedCert, cert *x509.Certificate) {
	for _, name := range []string{protocol.CertificateTrusted, protocol.CertificateRejected} {
		for _, c := range s.read(name) {
			if uacert.Thumbprint(c.cert.Raw) == thumbprint {
				found = append(found, c)
				cert = c.cert
			}
		}
	}
	return found, cert
}

func (s certStore) setTrusted(thumbprint string, trusted bool) error {
	found, cert := s.find(thumbprint)
	if cert == nil {
		return fmt.Errorf("証明書 %s がありません", thumbprint)
	}
	st := &uacert.Store{Root: s.root}
	if trusted {
		if err := st.Trust(cert); err != nil {
			return fmt.Errorf("証明書を信頼の一覧に置けません: %w", err)
		}
	} else {
		st.Reject(cert)
	}
	// 移す前のファイルを消す（移した先のファイルは残す）
	want := s.dir(protocol.CertificateRejected)
	if !trusted {
		want = s.dir(protocol.CertificateTrusted)
	}
	for _, c := range found {
		if filepath.Dir(c.file) == want {
			if err := os.Remove(c.file); err != nil {
				return fmt.Errorf("証明書のファイルを消せません: %w", err)
			}
		}
	}
	return nil
}

func (s certStore) delete(thumbprint string) error {
	found, _ := s.find(thumbprint)
	if len(found) == 0 {
		return fmt.Errorf("証明書 %s がありません", thumbprint)
	}
	for _, c := range found {
		if err := os.Remove(c.file); err != nil {
			return fmt.Errorf("証明書のファイルを消せません: %w", err)
		}
	}
	return nil
}

// --- サーバー（pki） ---

func (f *Factory) certStore() certStore { return certStore{root: f.pkiRoot()} }

// pkiRoot は、サーバーの証明書のストアのフォルダ（PKIDir がなければ、exe と同じフォルダの pki）
func (f *Factory) pkiRoot() string {
	if f.PKIDir != "" {
		return f.PKIDir
	}
	return pkiDir()
}

func (f *Factory) Certificates() (protocol.CertificateList, error) { return f.certStore().list() }
func (f *Factory) SetCertificateTrusted(thumbprint string, trusted bool) error {
	return f.certStore().setTrusted(thumbprint, trusted)
}
func (f *Factory) DeleteCertificate(thumbprint string) error {
	return f.certStore().delete(thumbprint)
}

// --- クライアント（pki-client。全部の接続先で共通） ---

func (f *ClientFactory) certStore() certStore { return certStore{root: f.pkiRoot()} }

// pkiRoot は、クライアントの証明書のストアのフォルダ（PKIDir がなければ、exe と同じフォルダの pki-client）
func (f *ClientFactory) pkiRoot() string {
	if f.PKIDir != "" {
		return f.PKIDir
	}
	return clientPKIDir()
}

func (f *ClientFactory) Certificates() (protocol.CertificateList, error) {
	return f.certStore().list()
}
func (f *ClientFactory) SetCertificateTrusted(thumbprint string, trusted bool) error {
	return f.certStore().setTrusted(thumbprint, trusted)
}
func (f *ClientFactory) DeleteCertificate(thumbprint string) error {
	return f.certStore().delete(thumbprint)
}
