package opcua

import (
	"fmt"
	"strings"

	"github.com/bamchoh/uastack/server"
	"github.com/bamchoh/uastack/ua"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/fields"
)

// サーバーのセキュリティとユーザー認証の設定。
//
//   - エンドポイント: セキュリティポリシーとモードの組を、行を足せる表で並べる（行の数だけエンドポイントを出す）。
//     既定は None だけ（以前と同じ。設定のない古いプロジェクトも、そのまま動く）
//   - クライアントの証明書: 既定は、すべて受け入れる（テストの道具なので、つなぐまでの手間を少なくする）。
//     「信頼したものだけ」にすると、知らない証明書は拒否して pki/rejected に置く（証明書のタブで信頼に移せる）
//   - ユーザー: 匿名を許すか・ユーザー名とパスワードの一覧。パスワードはプロジェクトにそのまま保存する（クライアントと同じ）

const (
	keySecurityEndpoints = "securityEndpoints"
	keyTrustClients      = "trustClients"
	keyAllowAnonymous    = "allowAnonymous"
	keyUsers             = "users"

	colPolicy   = "policy"
	colMode     = "mode"
	colUserName = "userName"
	colPassword = "password"

	modeBoth = "Both" // Sign と SignAndEncrypt の両方のエンドポイントを出す

	trustAll     = "all"
	trustTrusted = "trusted"

	yes = "yes"
	no  = "no"

	maxUsers = 50

	categorySecurity = "セキュリティ"
	categoryUsers    = "ユーザー認証"
)

// SecurityEndpoint は、サーバーが出すエンドポイントの 1 行（ポリシーとモード）
type SecurityEndpoint struct {
	Policy string `json:"policy"` // securityPolicies の name
	Mode   string `json:"mode"`   // Sign / SignAndEncrypt / Both（None では使わない）
}

// User は、サーバーにログインできるユーザー
type User struct {
	Name     string `json:"userName"`
	Password string `json:"password"`
}

func defaultSecurityEndpoints() []SecurityEndpoint {
	return []SecurityEndpoint{{Policy: policyNone, Mode: modeSignEn}}
}

func securityConfigFields() []protocol.ConfigField {
	policies := make([]protocol.FieldOption, len(securityPolicies))
	for i, p := range securityPolicies {
		policies[i] = protocol.FieldOption{Value: p.name, Label: p.name}
	}
	return []protocol.ConfigField{
		{
			Name:  keySecurityEndpoints,
			Label: "エンドポイント",
			Description: "クライアントに出すエンドポイント（セキュリティポリシーとモードの組）。1 行が 1 つのポリシーです。" +
				"None 以外では、サーバーの証明書を exe と同じフォルダの pki に作ります（起動したとき）。モードは None では使いません。",
			Type:     "rows",
			Required: true,
			Default:  endpointsToRows(defaultSecurityEndpoints()),
			Max:      intPtr(len(securityPolicies)),
			Category: categorySecurity,
			Columns: []protocol.ConfigField{
				{Name: colPolicy, Label: "セキュリティポリシー", Type: "select", Default: policyNone, Options: policies},
				{Name: colMode, Label: "モード", Type: "select", Default: modeSignEn, Options: []protocol.FieldOption{
					{Value: modeSignEn, Label: "SignAndEncrypt"},
					{Value: modeSign, Label: "Sign"},
					{Value: modeBoth, Label: "両方"},
				}},
			},
		},
		{
			Name:  keyTrustClients,
			Label: "クライアントの証明書",
			Description: "暗号化したエンドポイントにつないできたクライアントの証明書を、どう扱うか。" +
				"「信頼したものだけ」では、知らない証明書を拒否して、証明書のタブの一覧に出します（そこで信頼すると、つなげるようになります）。",
			Type:     "select",
			Required: true,
			Default:  trustAll,
			Category: categorySecurity,
			Options: []protocol.FieldOption{
				{Value: trustAll, Label: "すべて受け入れる"},
				{Value: trustTrusted, Label: "信頼したものだけ"},
			},
		},
		{
			Name:        keyAllowAnonymous,
			Label:       "匿名のログイン",
			Description: "ユーザー名なし（Anonymous）でのログインを許すか。",
			Type:        "select",
			Required:    true,
			Default:     yes,
			Category:    categoryUsers,
			Options: []protocol.FieldOption{
				{Value: yes, Label: "許す"},
				{Value: no, Label: "許さない"},
			},
		},
		{
			Name:  keyUsers,
			Label: "ユーザー",
			Description: "ユーザー名とパスワードでログインできるユーザー。1 人もいなければ、ユーザー名でのログインはできません。" +
				"パスワードはプロジェクトのファイルに、そのまま（暗号化せずに）保存します。",
			Type:     "rows",
			Default:  []map[string]interface{}{},
			Max:      intPtr(maxUsers),
			Category: categoryUsers,
			Columns: []protocol.ConfigField{
				{Name: colUserName, Label: "ユーザー名", Type: "text", Default: ""},
				{Name: colPassword, Label: "パスワード", Type: "password", Default: ""},
			},
		},
	}
}

func endpointsToRows(eps []SecurityEndpoint) []map[string]interface{} {
	rows := make([]map[string]interface{}, len(eps))
	for i, e := range eps {
		rows[i] = map[string]interface{}{colPolicy: e.Policy, colMode: e.Mode}
	}
	return rows
}

func usersToRows(users []User) []map[string]interface{} {
	rows := make([]map[string]interface{}, len(users))
	for i, u := range users {
		rows[i] = map[string]interface{}{colUserName: u.Name, colPassword: u.Password}
	}
	return rows
}

// securityToMap は、セキュリティとユーザーの設定を、設定のマップに入れる
func securityToMap(cfg *Config, m map[string]interface{}) {
	m[keySecurityEndpoints] = endpointsToRows(cfg.Endpoints)
	m[keyTrustClients] = trustAll
	if !cfg.TrustAllClients {
		m[keyTrustClients] = trustTrusted
	}
	m[keyAllowAnonymous] = yes
	if !cfg.AllowAnonymous {
		m[keyAllowAnonymous] = no
	}
	m[keyUsers] = usersToRows(cfg.Users)
}

// securityFromMap は、設定のマップからセキュリティとユーザーの設定を読む（ないキーは既定値のまま）
func securityFromMap(settings map[string]interface{}, cfg *Config) {
	if _, ok := settings[keySecurityEndpoints]; ok {
		cfg.Endpoints = nil
		for _, r := range fields.Rows(settings, keySecurityEndpoints) {
			cfg.Endpoints = append(cfg.Endpoints, SecurityEndpoint{
				Policy: strings.TrimSpace(fields.String(r, colPolicy)),
				Mode:   strings.TrimSpace(fields.String(r, colMode)),
			})
		}
	}
	if v, ok := settings[keyTrustClients].(string); ok {
		cfg.TrustAllClients = v != trustTrusted
	}
	if v, ok := settings[keyAllowAnonymous].(string); ok {
		cfg.AllowAnonymous = v != no
	}
	if _, ok := settings[keyUsers]; ok {
		cfg.Users = nil
		for _, r := range fields.Rows(settings, keyUsers) {
			cfg.Users = append(cfg.Users, User{
				Name:     strings.TrimSpace(fields.String(r, colUserName)),
				Password: fields.String(r, colPassword),
			})
		}
	}
}

// validateSecurity は、セキュリティとユーザーの設定を確かめる
func (c *Config) validateSecurity() error {
	if len(c.Endpoints) == 0 {
		return fmt.Errorf("エンドポイントを 1 つ以上指定してください")
	}
	seen := map[string]bool{}
	for _, e := range c.Endpoints {
		if policyURI(e.Policy) == "" {
			return fmt.Errorf("不明なセキュリティポリシーです: %s", e.Policy)
		}
		if seen[e.Policy] {
			return fmt.Errorf("セキュリティポリシー %s が 2 つあります", e.Policy)
		}
		seen[e.Policy] = true
		if e.Policy != policyNone && e.Mode != modeSign && e.Mode != modeSignEn && e.Mode != modeBoth {
			return fmt.Errorf("不明なセキュリティモードです: %s", e.Mode)
		}
	}
	names := map[string]bool{}
	for _, u := range c.Users {
		if u.Name == "" {
			return fmt.Errorf("ユーザー名が空の行があります")
		}
		if names[u.Name] {
			return fmt.Errorf("ユーザー名 %s が 2 つあります", u.Name)
		}
		names[u.Name] = true
	}
	if len(c.Users) > maxUsers {
		return fmt.Errorf("ユーザーは %d 人までです", maxUsers)
	}
	if !c.AllowAnonymous && len(c.Users) == 0 {
		return fmt.Errorf("匿名のログインを許さないときは、ユーザーを 1 人以上指定してください")
	}
	return nil
}

func policyURI(name string) string {
	for _, p := range securityPolicies {
		if p.name == name {
			return p.uri
		}
	}
	return ""
}

// applySecurity は、設定を uastack のサーバーの設定（エンドポイント・ユーザーのトークン・認証）にする
func (c *Config) applySecurity(sc *server.Config) {
	for _, e := range c.Endpoints {
		var modes []ua.MessageSecurityMode
		switch {
		case e.Policy == policyNone:
			modes = []ua.MessageSecurityMode{ua.MessageSecurityModeNone}
		case e.Mode == modeSign:
			modes = []ua.MessageSecurityMode{ua.MessageSecurityModeSign}
		case e.Mode == modeBoth:
			modes = []ua.MessageSecurityMode{ua.MessageSecurityModeSign, ua.MessageSecurityModeSignAndEncrypt}
		default:
			modes = []ua.MessageSecurityMode{ua.MessageSecurityModeSignAndEncrypt}
		}
		sc.Security = append(sc.Security, server.SecurityConfig{PolicyURI: policyURI(e.Policy), Modes: modes})
	}
	if c.AllowAnonymous {
		sc.UserTokens = append(sc.UserTokens, server.UserTokenAnonymous)
	}
	if len(c.Users) > 0 {
		sc.UserTokens = append(sc.UserTokens, server.UserTokenUserName)
		users := make(map[string]string, len(c.Users))
		for _, u := range c.Users {
			users[u.Name] = u.Password
		}
		sc.AuthenticateUser = func(user, password string) bool {
			want, ok := users[user]
			return ok && want == password
		}
	}
	sc.AutoAcceptUntrustedCertificates = c.TrustAllClients
}
