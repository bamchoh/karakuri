package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
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
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: corsMiddleware(mux),
	}
	return s
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

	mux := http.NewServeMux()
	s.registerRoutes(mux)
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: corsMiddleware(mux),
	}
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

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// === サーバー管理 ===
	mux.HandleFunc("GET /api/servers", s.handleGetServers)
	mux.HandleFunc("POST /api/servers", s.handleAddServer)
	mux.HandleFunc("DELETE /api/servers/{protocolType}", s.handleRemoveServer)
	mux.HandleFunc("POST /api/servers/{protocolType}/start", s.handleStartServer)
	mux.HandleFunc("POST /api/servers/{protocolType}/stop", s.handleStopServer)
	mux.HandleFunc("GET /api/servers/{protocolType}/status", s.handleGetServerStatus)
	mux.HandleFunc("GET /api/servers/{protocolType}/config", s.handleGetServerConfig)
	mux.HandleFunc("PUT /api/servers/{protocolType}/config", s.handleUpdateServerConfig)

	// === メモリ操作 ===
	mux.HandleFunc("GET /api/memory/{protocolType}/areas", s.handleGetMemoryAreas)
	mux.HandleFunc("GET /api/memory/{protocolType}/{area}/words", s.handleReadWords)
	mux.HandleFunc("PUT /api/memory/{protocolType}/{area}/words/{address}", s.handleWriteWord)
	mux.HandleFunc("GET /api/memory/{protocolType}/{area}/bits", s.handleReadBits)
	mux.HandleFunc("PUT /api/memory/{protocolType}/{area}/bits/{address}", s.handleWriteBit)

	// === 変数管理 ===
	mux.HandleFunc("GET /api/variables", s.handleGetVariables)
	mux.HandleFunc("POST /api/variables", s.handleCreateVariable)
	mux.HandleFunc("PUT /api/variables/{id}/value", s.handleUpdateVariableValue)
	mux.HandleFunc("DELETE /api/variables/{id}", s.handleDeleteVariable)

	// === プロジェクトエクスポート/インポート ===
	mux.HandleFunc("GET /api/project/export", s.handleExportProject)
	mux.HandleFunc("POST /api/project/import", s.handleImportProject)
}

// --- ヘルパー ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// --- サーバー管理ハンドラー ---

func (s *Server) handleGetServers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.GetServerInstances())
}

func (s *Server) handleAddServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProtocolType string `json:"protocolType"`
		VariantID    string `json:"variantId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストボディが不正です")
		return
	}
	if err := s.svc.AddServer(body.ProtocolType, body.VariantID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleRemoveServer(w http.ResponseWriter, r *http.Request) {
	pt := r.PathValue("protocolType")
	if err := s.svc.RemoveServer(pt); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStartServer(w http.ResponseWriter, r *http.Request) {
	pt := r.PathValue("protocolType")
	if err := s.svc.StartServer(pt); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStopServer(w http.ResponseWriter, r *http.Request) {
	pt := r.PathValue("protocolType")
	if err := s.svc.StopServer(pt); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetServerStatus(w http.ResponseWriter, r *http.Request) {
	pt := r.PathValue("protocolType")
	status := s.svc.GetServerStatus(pt)
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) handleGetServerConfig(w http.ResponseWriter, r *http.Request) {
	pt := r.PathValue("protocolType")
	config := s.svc.GetServerConfig(pt)
	if config == nil {
		writeError(w, http.StatusNotFound, "サーバーが見つかりません")
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) handleUpdateServerConfig(w http.ResponseWriter, r *http.Request) {
	pt := r.PathValue("protocolType")
	var dto application.ServerConfigDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストボディが不正です")
		return
	}
	dto.ProtocolType = pt
	if err := s.svc.UpdateServerConfig(&dto); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- メモリ操作ハンドラー ---

func (s *Server) handleGetMemoryAreas(w http.ResponseWriter, r *http.Request) {
	pt := r.PathValue("protocolType")
	writeJSON(w, http.StatusOK, s.svc.GetMemoryAreas(pt))
}

func (s *Server) handleReadWords(w http.ResponseWriter, r *http.Request) {
	pt := r.PathValue("protocolType")
	area := r.PathValue("area")

	address, _ := strconv.Atoi(r.URL.Query().Get("address"))
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count <= 0 {
		count = 1
	}

	values, err := s.svc.ReadWords(pt, area, address, count)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"values": values})
}

func (s *Server) handleWriteWord(w http.ResponseWriter, r *http.Request) {
	pt := r.PathValue("protocolType")
	area := r.PathValue("area")
	address, err := strconv.Atoi(r.PathValue("address"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "アドレスが不正です")
		return
	}

	var body struct {
		Value int `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストボディが不正です")
		return
	}

	if err := s.svc.WriteWord(pt, area, address, body.Value); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReadBits(w http.ResponseWriter, r *http.Request) {
	pt := r.PathValue("protocolType")
	area := r.PathValue("area")

	address, _ := strconv.Atoi(r.URL.Query().Get("address"))
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count <= 0 {
		count = 1
	}

	values, err := s.svc.ReadBits(pt, area, address, count)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"values": values})
}

func (s *Server) handleWriteBit(w http.ResponseWriter, r *http.Request) {
	pt := r.PathValue("protocolType")
	area := r.PathValue("area")
	address, err := strconv.Atoi(r.PathValue("address"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "アドレスが不正です")
		return
	}

	var body struct {
		Value bool `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストボディが不正です")
		return
	}

	if err := s.svc.WriteBit(pt, area, address, body.Value); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- 変数管理ハンドラー ---

func (s *Server) handleGetVariables(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.GetVariables())
}

func (s *Server) handleCreateVariable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string      `json:"name"`
		DataType string      `json:"dataType"`
		Value    interface{} `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストボディが不正です")
		return
	}

	v, err := s.svc.CreateVariable(body.Name, body.DataType, body.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) handleUpdateVariableValue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body struct {
		Value interface{} `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストボディが不正です")
		return
	}

	if err := s.svc.UpdateVariableValue(id, body.Value); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteVariable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.svc.DeleteVariable(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- プロジェクトエクスポート/インポートハンドラー ---

func (s *Server) handleExportProject(w http.ResponseWriter, r *http.Request) {
	data := s.svc.ExportProject()
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) handleImportProject(w http.ResponseWriter, r *http.Request) {
	var data application.ProjectDataDTO
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストボディが不正です")
		return
	}
	if err := s.svc.ImportProject(&data); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
