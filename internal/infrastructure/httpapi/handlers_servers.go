package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

	"karakuri/internal/application"
)

// --- サーバー管理ハンドラー ---

func (s *Server) handleGetServers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.GetServerInstances())
}

func (s *Server) handleAddServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProtocolType string `json:"protocolType"`
		VariantID    string `json:"variantId"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if err := s.svc.AddServer(body.ProtocolType, body.VariantID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleRemoveServer(w http.ResponseWriter, r *http.Request) {
	noContent(w, s.svc.RemoveServer(r.PathValue("protocolType")))
}

func (s *Server) handleStartServer(w http.ResponseWriter, r *http.Request) {
	noContent(w, s.svc.StartServer(r.PathValue("protocolType")))
}

func (s *Server) handleStopServer(w http.ResponseWriter, r *http.Request) {
	noContent(w, s.svc.StopServer(r.PathValue("protocolType")))
}

func (s *Server) handleGetServerStatus(w http.ResponseWriter, r *http.Request) {
	status := s.svc.GetServerStatus(r.PathValue("protocolType"))
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) handleGetServerConfig(w http.ResponseWriter, r *http.Request) {
	config := s.svc.GetServerConfig(r.PathValue("protocolType"))
	if config == nil {
		writeError(w, http.StatusNotFound, "サーバーが見つかりません")
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) handleUpdateServerConfig(w http.ResponseWriter, r *http.Request) {
	var dto application.ServerConfigDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	dto.ProtocolType = r.PathValue("protocolType")
	noContent(w, s.svc.UpdateServerConfig(&dto))
}

// --- メモリ操作ハンドラー ---

// maxReadCount は 1 回で読めるワード・ビットの数
const maxReadCount = 65535

func (s *Server) handleGetMemoryAreas(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.GetMemoryAreas(r.PathValue("protocolType")))
}

// readRange はクエリの address（既定 0）と count（既定 1）を読んで確かめる
func readRange(r *http.Request) (address, count int, err error) {
	if address, err = queryInt(r, "address", 0); err != nil {
		return 0, 0, err
	}
	if count, err = queryInt(r, "count", 1); err != nil {
		return 0, 0, err
	}
	if address < 0 {
		return 0, 0, fmt.Errorf("address は 0 以上にしてください: %d", address)
	}
	if count < 1 || count > maxReadCount {
		return 0, 0, fmt.Errorf("count は 1〜%d にしてください: %d", maxReadCount, count)
	}
	return address, count, nil
}

// pathAddress はパスの {address} を読む
func pathAddress(r *http.Request) (int, error) {
	address, err := strconv.Atoi(r.PathValue("address"))
	if err != nil || address < 0 {
		return 0, fmt.Errorf("アドレスが不正です: %q", r.PathValue("address"))
	}
	return address, nil
}

// toWord はワードの値を確かめて uint16 にする。-32768〜65535（負の数は 2 の補数）を受け付ける
func toWord(v int) (uint16, error) {
	if v < -32768 || v > 65535 {
		return 0, fmt.Errorf("ワードの値は -32768〜65535 にしてください: %d", v)
	}
	return uint16(v), nil
}

func (s *Server) handleReadWords(w http.ResponseWriter, r *http.Request) {
	address, count, err := readRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	values, err := s.svc.ReadWords(r.PathValue("protocolType"), r.PathValue("area"), address, count)
	jsonOr400(w, map[string]interface{}{"values": values}, err)
}

func (s *Server) handleWriteWord(w http.ResponseWriter, r *http.Request) {
	address, err := pathAddress(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var body struct {
		Value *int `json:"value"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Value == nil {
		writeError(w, http.StatusBadRequest, "value がありません")
		return
	}
	word, err := toWord(*body.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	noContent(w, s.svc.WriteWords(r.PathValue("protocolType"), r.PathValue("area"), address, []uint16{word}))
}

// handleWriteWords は address から続けて複数ワードを書く（範囲外なら 1 つも書かない）
func (s *Server) handleWriteWords(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address *int  `json:"address"`
		Values  []int `json:"values"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Address == nil || len(body.Values) == 0 {
		writeError(w, http.StatusBadRequest, "address と values（1 個以上）を指定してください")
		return
	}
	words := make([]uint16, len(body.Values))
	for i, v := range body.Values {
		word, err := toWord(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("values[%d]: %v", i, err))
			return
		}
		words[i] = word
	}
	noContent(w, s.svc.WriteWords(r.PathValue("protocolType"), r.PathValue("area"), *body.Address, words))
}

func (s *Server) handleReadBits(w http.ResponseWriter, r *http.Request) {
	address, count, err := readRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	values, err := s.svc.ReadBits(r.PathValue("protocolType"), r.PathValue("area"), address, count)
	jsonOr400(w, map[string]interface{}{"values": values}, err)
}

func (s *Server) handleWriteBit(w http.ResponseWriter, r *http.Request) {
	address, err := pathAddress(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var body struct {
		Value *bool `json:"value"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Value == nil {
		writeError(w, http.StatusBadRequest, "value がありません")
		return
	}
	noContent(w, s.svc.WriteBits(r.PathValue("protocolType"), r.PathValue("area"), address, []bool{*body.Value}))
}

// handleWriteBits は address から続けて複数ビットを書く（範囲外なら 1 つも書かない）
func (s *Server) handleWriteBits(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address *int   `json:"address"`
		Values  []bool `json:"values"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Address == nil || len(body.Values) == 0 {
		writeError(w, http.StatusBadRequest, "address と values（1 個以上）を指定してください")
		return
	}
	noContent(w, s.svc.WriteBits(r.PathValue("protocolType"), r.PathValue("area"), *body.Address, body.Values))
}

// --- プロジェクトエクスポート/インポートハンドラー ---

func (s *Server) handleExportProject(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.ExportProject())
}

func (s *Server) handleImportProject(w http.ResponseWriter, r *http.Request) {
	var data application.ProjectDataDTO
	if !decodeBody(w, r, &data) {
		return
	}
	noContent(w, s.svc.ImportProject(&data))
}
