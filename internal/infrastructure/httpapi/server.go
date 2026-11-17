package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"karakuri/internal/application"
)

// Server はREST HTTP APIサーバー
type Server struct {
	svc    *application.PLCService
	server *http.Server
}

// NewServer は新しいHTTP APIサーバーを作成する
func NewServer(svc *application.PLCService, port int) *Server {
	s := &Server{svc: svc}
	s.server = s.newHTTPServer(port)
	return s
}

func (s *Server) newHTTPServer(port int) *http.Server {
	mux := http.NewServeMux()
	for _, rt := range s.routes() {
		mux.HandleFunc(rt.method+" "+rt.path, rt.handler)
	}
	return &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: corsMiddleware(mux),
	}
}

// Start はHTTPサーバーをバックグラウンドで起動する
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("HTTP API サーバーのポートを開けません %s: %w", s.server.Addr, err)
	}
	go s.server.Serve(ln) //nolint:errcheck
	return nil
}

// Shutdown はHTTPサーバーをグレースフルに停止する
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Restart は新しいポートでHTTPサーバーを再起動する
func (s *Server) Restart(port int) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.server.Shutdown(shutdownCtx)

	s.server = s.newHTTPServer(port)
	return s.Start()
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// route はエンドポイント 1 つ。一覧は openapi.json と突き合わせてテストする（足したら両方に書く）
type route struct {
	method  string
	path    string
	handler http.HandlerFunc
}

func (s *Server) routes() []route {
	return []route{
		{"GET", "/api/openapi.json", s.handleOpenAPI},

		// === サーバー管理 ===
		{"GET", "/api/servers", s.handleGetServers},
		{"POST", "/api/servers", s.handleAddServer},
		{"DELETE", "/api/servers/{protocolType}", s.handleRemoveServer},
		{"POST", "/api/servers/{protocolType}/start", s.handleStartServer},
		{"POST", "/api/servers/{protocolType}/stop", s.handleStopServer},
		{"GET", "/api/servers/{protocolType}/status", s.handleGetServerStatus},
		{"GET", "/api/servers/{protocolType}/config", s.handleGetServerConfig},
		{"PUT", "/api/servers/{protocolType}/config", s.handleUpdateServerConfig},

		// === メモリ操作 ===
		{"GET", "/api/memory/{protocolType}/areas", s.handleGetMemoryAreas},
		{"GET", "/api/memory/{protocolType}/words", s.handleReadWordsAt},
		{"PUT", "/api/memory/{protocolType}/words", s.handleWriteWordsAt},
		{"GET", "/api/memory/{protocolType}/bits", s.handleReadBitsAt},
		{"PUT", "/api/memory/{protocolType}/bits", s.handleWriteBitsAt},
		{"GET", "/api/memory/{protocolType}/{area}/words", s.handleReadWords},
		{"PUT", "/api/memory/{protocolType}/{area}/words", s.handleWriteWords},
		{"PUT", "/api/memory/{protocolType}/{area}/words/{address}", s.handleWriteWord},
		{"GET", "/api/memory/{protocolType}/{area}/bits", s.handleReadBits},
		{"PUT", "/api/memory/{protocolType}/{area}/bits", s.handleWriteBits},
		{"PUT", "/api/memory/{protocolType}/{area}/bits/{address}", s.handleWriteBit},

		// === 変数管理 ===
		{"GET", "/api/variables", s.handleGetVariables},
		{"POST", "/api/variables", s.handleCreateVariable},
		{"GET", "/api/variables/{ref}", s.handleGetVariable},
		{"PUT", "/api/variables/{ref}", s.handleUpdateVariable},
		{"DELETE", "/api/variables/{ref}", s.handleDeleteVariable},
		{"GET", "/api/variables/{ref}/value", s.handleReadVariableValue},
		{"PUT", "/api/variables/{ref}/value", s.handleUpdateVariableValue},
		{"GET", "/api/variables/{ref}/mappings", s.handleGetVariableMappings},
		{"PUT", "/api/variables/{ref}/mappings", s.handleUpdateVariableMappings},
		{"PUT", "/api/variables/{ref}/node-publishings/{protocolType}", s.handleUpdateNodePublishing},
		{"GET", "/api/values", s.handleReadValues},
		{"PUT", "/api/values", s.handleWriteValues},
		{"GET", "/api/data-types", s.handleGetDataTypes},
		{"GET", "/api/struct-types", s.handleGetStructTypes},
		{"POST", "/api/struct-types", s.handleRegisterStructType},
		{"DELETE", "/api/struct-types/{name}", s.handleDeleteStructType},
		{"GET", "/api/mapping-targets", s.handleGetMappingTargets},
		{"GET", "/api/variable-statuses", s.handleGetVariableStatuses},

		// === クライアント ===
		{"GET", "/api/client-protocols", s.handleGetClientProtocols},
		{"GET", "/api/clients", s.handleGetClients},
		{"POST", "/api/clients", s.handleAddClient},
		{"GET", "/api/clients/{ref}", s.handleGetClient},
		{"PUT", "/api/clients/{ref}", s.handleUpdateClient},
		{"DELETE", "/api/clients/{ref}", s.handleRemoveClient},
		{"POST", "/api/clients/{ref}/disconnect", s.handleDisconnectClient},
		{"POST", "/api/clients/{ref}/build", s.handleBuildClientRequest},
		{"POST", "/api/clients/{ref}/send", s.handleSendClientRequest},
		{"GET", "/api/clients/{ref}/browse", s.handleBrowseClient},
		{"GET", "/api/clients/{ref}/commands", s.handleGetSavedCommands},
		{"POST", "/api/clients/{ref}/commands", s.handleAddSavedCommand},
		{"PUT", "/api/clients/{ref}/commands/{cmd}", s.handleUpdateSavedCommand},
		{"DELETE", "/api/clients/{ref}/commands/{cmd}", s.handleRemoveSavedCommand},
		{"POST", "/api/clients/{ref}/commands/{cmd}/send", s.handleSendSavedCommand},
		{"GET", "/api/clients/{ref}/polling", s.handleGetPolling},
		{"PUT", "/api/clients/{ref}/polling", s.handleUpdatePolling},
		{"POST", "/api/clients/{ref}/polling/start", s.handleStartPolling},
		{"POST", "/api/clients/{ref}/polling/stop", s.handleStopPolling},
		{"POST", "/api/clients/{ref}/polling/items", s.handleAddPollItem},
		{"PUT", "/api/clients/{ref}/polling/items/{itemId}", s.handleUpdatePollItem},
		{"DELETE", "/api/clients/{ref}/polling/items/{itemId}", s.handleRemovePollItem},
		{"GET", "/api/clients/{ref}/subscription", s.handleGetSubscription},
		{"PUT", "/api/clients/{ref}/subscription", s.handleUpdateSubscription},
		{"POST", "/api/clients/{ref}/subscription/start", s.handleStartSubscription},
		{"POST", "/api/clients/{ref}/subscription/stop", s.handleStopSubscription},
		{"POST", "/api/clients/{ref}/subscription/items", s.handleAddSubscriptionItems},
		{"DELETE", "/api/clients/{ref}/subscription/items/{itemId}", s.handleRemoveSubscriptionItem},
		{"GET", "/api/client-logs", s.handleGetClientLogs},
		{"DELETE", "/api/client-logs", s.handleClearClientLogs},

		// === 証明書（kind は server / client） ===
		{"GET", "/api/certificates/{kind}/{protocolType}", s.handleGetCertificates},
		{"PUT", "/api/certificates/{kind}/{protocolType}/{thumbprint}", s.handleSetCertificateTrusted},
		{"DELETE", "/api/certificates/{kind}/{protocolType}/{thumbprint}", s.handleDeleteCertificate},

		// === プロジェクトエクスポート/インポート ===
		{"GET", "/api/project/export", s.handleExportProject},
		{"POST", "/api/project/import", s.handleImportProject},
	}
}

// --- ヘルパー ---

// maxBodyBytes はリクエストボディの上限（プロジェクトの読み込みが入る大きさ）
const maxBodyBytes = 64 << 20

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeBody はリクエストボディの JSON を v に読む。読めなければ 400 を返して false
func decodeBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	return decode(w, r, v, false)
}

// decodeValueBody は、変数の値を含むボディを読む。数値は桁を落とさないよう、
// 整数なら int64（収まらなければ uint64）、それ以外は float64 にする（LINT / ULINT の全範囲を書けるようにするため）。
// v の中の interface{} の値は、あとで normalizeNumbers を通すこと
func decodeValueBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	return decode(w, r, v, true)
}

func decode(w http.ResponseWriter, r *http.Request, v interface{}, useNumber bool) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if useNumber {
		dec.UseNumber()
	}
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "リクエストボディがありません")
		} else {
			writeError(w, http.StatusBadRequest, "リクエストボディが不正です: "+err.Error())
		}
		return false
	}
	return true
}

// normalizeNumbers は json.Number を int64 / uint64 / float64 に直す（配列・オブジェクトの中も）
func normalizeNumbers(v interface{}) interface{} {
	switch t := v.(type) {
	case json.Number:
		s := t.String()
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i
		}
		if u, err := strconv.ParseUint(s, 10, 64); err == nil {
			return u
		}
		f, _ := t.Float64()
		return f
	case []interface{}:
		for i, e := range t {
			t[i] = normalizeNumbers(e)
		}
		return t
	case map[string]interface{}:
		for k, e := range t {
			t[k] = normalizeNumbers(e)
		}
		return t
	}
	return v
}

// queryInt はクエリの整数を読む。なければ def。数値として読めなければ error
func queryInt(r *http.Request, name string, def int) (int, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s が整数ではありません: %q", name, s)
	}
	return n, nil
}

// noContent は err が nil なら 204、そうでなければ 400 を返す
func noContent(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// jsonOr400 は err が nil なら v を 200 で、そうでなければ 400 を返す
func jsonOr400(w http.ResponseWriter, v interface{}, err error) {
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}
