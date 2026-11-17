package opcua

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	uaclient "github.com/bamchoh/uastack/client"
	"github.com/bamchoh/uastack/ua"
	"github.com/bamchoh/uastack/uacp"

	"karakuri/internal/domain/protocol"
)

// startSecureServer は、cfg のセキュリティの設定で、証明書のストアを一時フォルダにしたサーバーを起動する
func startSecureServer(t *testing.T, cfg *Config) (*Server, *Factory) {
	t.Helper()
	f := &Factory{PKIDir: t.TempDir()}
	s, err := f.CreateServer(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := s.(*Server)
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv, f
}

// secureClient は、Karakuri のクライアント（証明書のストアは cf のもの）
func secureClient(t *testing.T, cf *ClientFactory, settings map[string]interface{}) protocol.Client {
	t.Helper()
	normalized, err := cf.NormalizeSettings(settings)
	if err != nil {
		t.Fatalf("NormalizeSettings: %v", err)
	}
	c, err := cf.NewClient(normalized)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// サーバーの時刻（i=2258）を読む。読めたら ok
func readServerTime(t *testing.T, c protocol.Client) protocol.CommandResult {
	t.Helper()
	req := buildRequest(t, cmdRead, map[string]interface{}{"nodeId": "i=2258", "attribute": "Value"})
	return c.Send(context.Background(), req)
}

// 設定したポリシーとモードの組だけのエンドポイントを出し、ユーザーのトークンも設定どおりにする
func TestServerSecurity_Endpoints(t *testing.T) {
	cfg := testConfig(freePort(t))
	cfg.Endpoints = []SecurityEndpoint{
		{Policy: policyNone},
		{Policy: "Basic256Sha256", Mode: modeBoth},
		{Policy: "ECC_nistP256", Mode: modeSign},
	}
	cfg.AllowAnonymous = false
	cfg.Users = []User{{Name: "op", Password: "pw"}}
	startSecureServer(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	eps, err := uaclient.GetEndpoints(ctx, fmt.Sprintf("opc.tcp://127.0.0.1:%d", cfg.Port), uacp.ClientLimits{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, ep := range eps {
		name := strings.TrimPrefix(ep.SecurityPolicyUri, "http://opcfoundation.org/UA/SecurityPolicy#")
		got[fmt.Sprintf("%s/%v", name, ep.SecurityMode)] = true
		for _, tok := range ep.UserIdentityTokens {
			if tok.TokenType == ua.UserTokenTypeAnonymous {
				t.Errorf("%s: 匿名を許さない設定なのに、匿名のトークンがある", name)
			}
		}
		if len(ep.UserIdentityTokens) == 0 {
			t.Errorf("%s: ユーザーのトークンがない", name)
		}
	}
	want := []string{
		fmt.Sprintf("None/%v", ua.MessageSecurityModeNone),
		fmt.Sprintf("Basic256Sha256/%v", ua.MessageSecurityModeSign),
		fmt.Sprintf("Basic256Sha256/%v", ua.MessageSecurityModeSignAndEncrypt),
		fmt.Sprintf("ECC_nistP256/%v", ua.MessageSecurityModeSign),
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("エンドポイント %s がない（%v）", w, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("エンドポイント = %v, want %v", got, want)
	}
}

// 暗号化したエンドポイントに、ユーザー名とパスワードでつなげる。違うパスワード・匿名・出していないポリシーではつながらない
func TestServerSecurity_EncryptedLoginWithUser(t *testing.T) {
	cfg := testConfig(freePort(t))
	cfg.Endpoints = []SecurityEndpoint{
		{Policy: policyNone},
		{Policy: "Basic256Sha256", Mode: modeSignEn},
		{Policy: "Aes256_Sha256_RsaPss", Mode: modeBoth},
		{Policy: "ECC_nistP384", Mode: modeSignEn},
	}
	cfg.AllowAnonymous = false
	cfg.Users = []User{{Name: "operator", Password: "secret"}, {Name: "viewer", Password: "v"}}
	startSecureServer(t, cfg)
	cf := &ClientFactory{PKIDir: t.TempDir()}
	url := fmt.Sprintf("opc.tcp://127.0.0.1:%d", cfg.Port)
	settings := func(policy, mode, user, password string) map[string]interface{} {
		return map[string]interface{}{"endpointUrl": url, "securityPolicy": policy, "securityMode": mode, "userName": user, "password": password, "timeoutMs": 10000}
	}

	for _, s := range []map[string]interface{}{
		settings("Basic256Sha256", modeSignEn, "operator", "secret"),
		settings("Aes256_Sha256_RsaPss", modeSign, "viewer", "v"),
		settings("Aes256_Sha256_RsaPss", modeSignEn, "operator", "secret"),
		settings("ECC_nistP384", modeSignEn, "operator", "secret"),
		settings(policyNone, modeSignEn, "operator", "secret"), // None でも、パスワードはサーバーの証明書で暗号化して送る
	} {
		if res := readServerTime(t, secureClient(t, cf, s)); res.Status != protocol.CommandOK {
			t.Errorf("%s/%s/%s: %s %s", s["securityPolicy"], s["securityMode"], s["userName"], res.Status, res.Error)
		}
	}

	for name, s := range map[string]map[string]interface{}{
		"パスワードが違う":   settings("Basic256Sha256", modeSignEn, "operator", "wrong"),
		"いないユーザー":    settings("Basic256Sha256", modeSignEn, "nobody", "secret"),
		"匿名":         settings("Basic256Sha256", modeSignEn, "", ""),
		"出していないモード":  settings("Basic256Sha256", modeSign, "operator", "secret"),
		"出していないポリシー": settings("Aes128_Sha256_RsaOaep", modeSignEn, "operator", "secret"),
		"None で匿名":   settings(policyNone, modeSignEn, "", ""),
		"ECC の違うカーブ": settings("ECC_nistP256", modeSignEn, "operator", "secret"),
	} {
		c := secureClient(t, cf, s)
		if res := readServerTime(t, c); res.Status != protocol.CommandError || c.Connected() {
			t.Errorf("%s: つながってしまった（%s %s）", name, res.Status, res.Error)
		}
	}
}

// 「信頼したものだけ」のサーバーは、知らないクライアントの証明書を拒否し、拒否の一覧に置く。信頼に移すと、つながる。
// クライアントも「信頼したものだけ」なら、知らないサーバーにはつながず、信頼に移すとつながる
func TestServerSecurity_TrustListOnBothSides(t *testing.T) {
	cfg := testConfig(freePort(t))
	cfg.Endpoints = []SecurityEndpoint{{Policy: policyNone}, {Policy: "Basic256Sha256", Mode: modeSignEn}}
	cfg.TrustAllClients = false
	_, sf := startSecureServer(t, cfg)
	cf := &ClientFactory{PKIDir: t.TempDir()}
	settings := map[string]interface{}{
		"endpointUrl": fmt.Sprintf("opc.tcp://127.0.0.1:%d", cfg.Port), "securityPolicy": "Basic256Sha256",
		"securityMode": modeSignEn, "trustServers": trustTrusted, "timeoutMs": 10000,
	}

	only := func(t *testing.T, m protocol.CertificateManager, status string) protocol.CertificateInfo {
		t.Helper()
		list, err := m.Certificates()
		if err != nil {
			t.Fatal(err)
		}
		var hits []protocol.CertificateInfo
		for _, c := range list.Certificates {
			if c.Status == status {
				hits = append(hits, c)
			}
		}
		if len(hits) != 1 {
			t.Fatalf("%s の証明書が %d 個（want 1）: %+v", status, len(hits), list.Certificates)
		}
		return hits[0]
	}

	// 1. クライアントが、サーバーの証明書を知らないので、つながない（サーバーの証明書を拒否の一覧に置く）
	if res := readServerTime(t, secureClient(t, cf, settings)); res.Status != protocol.CommandError {
		t.Fatalf("知らないサーバーにつながった: %+v", res)
	}
	serverCert := only(t, cf, protocol.CertificateRejected)
	if serverCert.ApplicationURI == "" || !strings.HasPrefix(serverCert.KeyType, "RSA") {
		t.Errorf("サーバーの証明書 = %+v", serverCert)
	}
	// サーバーの自分の証明書と同じもの
	if own := only(t, sf, protocol.CertificateOwn); own.Thumbprint != serverCert.Thumbprint {
		t.Errorf("拒否した証明書 %s が、サーバーの証明書 %s と違う", serverCert.Thumbprint, own.Thumbprint)
	}
	must(t, cf.SetCertificateTrusted(serverCert.Thumbprint, true))

	// 2. 次は、サーバーがクライアントの証明書を知らないので、断る（クライアントの証明書を拒否の一覧に置く）
	if res := readServerTime(t, secureClient(t, cf, settings)); res.Status != protocol.CommandError {
		t.Fatalf("知らないクライアントがつながった: %+v", res)
	}
	clientCert := only(t, sf, protocol.CertificateRejected)
	must(t, sf.SetCertificateTrusted(clientCert.Thumbprint, true))
	if c := only(t, sf, protocol.CertificateTrusted); c.Thumbprint != clientCert.Thumbprint {
		t.Errorf("信頼に移した証明書 = %+v", c)
	}

	// 3. 両方が信頼したので、つながる（起動し直さなくても効く）
	if res := readServerTime(t, secureClient(t, cf, settings)); res.Status != protocol.CommandOK {
		t.Fatalf("信頼したあと: %s %s", res.Status, res.Error)
	}

	// 4. 拒否に戻すと、またつながらない。消すと一覧からなくなる
	must(t, sf.SetCertificateTrusted(clientCert.Thumbprint, false))
	if res := readServerTime(t, secureClient(t, cf, settings)); res.Status != protocol.CommandError {
		t.Fatalf("拒否に戻したのにつながった: %+v", res)
	}
	only(t, sf, protocol.CertificateRejected)
	must(t, sf.DeleteCertificate(clientCert.Thumbprint))
	list, _ := sf.Certificates()
	for _, c := range list.Certificates {
		if c.Status != protocol.CertificateOwn {
			t.Errorf("消したのに残っている: %+v", c)
		}
	}
	if err := sf.DeleteCertificate(clientCert.Thumbprint); err == nil {
		t.Error("ない証明書を消せた")
	}
	if err := sf.SetCertificateTrusted("0000", true); err == nil {
		t.Error("ない証明書を信頼できた")
	}
	// 自分の証明書は、信頼・削除の対象にしない
	own := only(t, sf, protocol.CertificateOwn)
	if err := sf.DeleteCertificate(own.Thumbprint); err == nil {
		t.Error("自分の証明書を消せた")
	}
}

// 既定（すべて受け入れる）のサーバーとクライアントは、何も設定しなくても暗号化してつながる
func TestServerSecurity_DefaultAcceptsAll(t *testing.T) {
	cfg := testConfig(freePort(t))
	cfg.Endpoints = []SecurityEndpoint{{Policy: policyNone}, {Policy: "Aes128_Sha256_RsaOaep", Mode: modeSignEn}}
	startSecureServer(t, cfg)
	cf := &ClientFactory{PKIDir: t.TempDir()}
	res := readServerTime(t, secureClient(t, cf, map[string]interface{}{
		"endpointUrl": fmt.Sprintf("opc.tcp://127.0.0.1:%d", cfg.Port), "securityPolicy": "Aes128_Sha256_RsaOaep", "timeoutMs": 10000,
	}))
	if res.Status != protocol.CommandOK {
		t.Fatalf("%s %s", res.Status, res.Error)
	}
}

func TestConfig_ValidateSecurity(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(c *Config)
		want   string
	}{
		"エンドポイントがない":    {func(c *Config) { c.Endpoints = nil }, "エンドポイントを 1 つ以上"},
		"不明なポリシー":       {func(c *Config) { c.Endpoints = []SecurityEndpoint{{Policy: "Basic128Rsa15"}} }, "不明なセキュリティポリシー"},
		"同じポリシーが 2 つ":   {func(c *Config) { c.Endpoints = append(c.Endpoints, SecurityEndpoint{Policy: policyNone}) }, "2 つあります"},
		"不明なモード":        {func(c *Config) { c.Endpoints = []SecurityEndpoint{{Policy: "Basic256Sha256", Mode: "x"}} }, "不明なセキュリティモード"},
		"空のユーザー名":       {func(c *Config) { c.Users = []User{{Name: ""}} }, "ユーザー名が空"},
		"同じユーザー名":       {func(c *Config) { c.Users = []User{{Name: "a"}, {Name: "a"}} }, "ユーザー名 a が 2 つ"},
		"匿名もユーザーもない":    {func(c *Config) { c.AllowAnonymous = false }, "ユーザーを 1 人以上"},
		"None のモードは見ない": {func(c *Config) { c.Endpoints = []SecurityEndpoint{{Policy: policyNone, Mode: ""}} }, ""},
	} {
		c := DefaultConfig()
		tc.mutate(c)
		err := c.Validate()
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
