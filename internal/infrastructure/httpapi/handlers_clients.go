package httpapi

import (
	"fmt"
	"net/http"

	"karakuri/internal/application"
)

// --- クライアント（外部の機器へつなぐ側）ハンドラー ---
//
// パスの {ref} は接続先の ID か名前（ID を先に探す。名前が重なれば、追加した順で最初のもの）。
// {cmd} は保存したコマンドの ID か名前

// findClient は ID か名前で接続先を探す
func (s *Server) findClient(ref string) *application.ClientDTO {
	clients := s.svc.GetClients()
	for i := range clients {
		if clients[i].ID == ref {
			return &clients[i]
		}
	}
	for i := range clients {
		if clients[i].Name == ref {
			return &clients[i]
		}
	}
	return nil
}

// clientOr404 はパスの {ref} の接続先を返す。なければ 404 を返して nil
func (s *Server) clientOr404(w http.ResponseWriter, r *http.Request) *application.ClientDTO {
	ref := r.PathValue("ref")
	c := s.findClient(ref)
	if c == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("接続先がありません: %s", ref))
	}
	return c
}

// savedCommandOr404 はパスの {cmd} の保存したコマンドの ID を返す。なければ 404 を返して空文字列
func (s *Server) savedCommandOr404(w http.ResponseWriter, r *http.Request, clientID string) string {
	ref := r.PathValue("cmd")
	cmds, err := s.svc.GetClientSavedCommands(clientID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return ""
	}
	for _, c := range cmds {
		if c.ID == ref {
			return c.ID
		}
	}
	for _, c := range cmds {
		if c.Name == ref {
			return c.ID
		}
	}
	writeError(w, http.StatusNotFound, fmt.Sprintf("保存したコマンドがありません: %s", ref))
	return ""
}

func (s *Server) handleGetClientProtocols(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.GetClientProtocols())
}

func (s *Server) handleGetClients(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.GetClients())
}

// handleAddClient は接続先を追加する。settings を省いた項目は既定値
func (s *Server) handleAddClient(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProtocolType string                 `json:"protocolType"`
		Name         string                 `json:"name"`
		Settings     map[string]interface{} `json:"settings"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	c, err := s.svc.AddClient(body.ProtocolType, body.Name, body.Settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleGetClient(w http.ResponseWriter, r *http.Request) {
	if c := s.clientOr404(w, r); c != nil {
		writeJSON(w, http.StatusOK, c)
	}
}

// handleUpdateClient は名前と設定を変える。省いた名前・設定の項目は、いまの値のまま
func (s *Server) handleUpdateClient(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	var body struct {
		Name     *string                `json:"name"`
		Settings map[string]interface{} `json:"settings"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	name := c.Name
	if body.Name != nil {
		name = *body.Name
	}
	settings := make(map[string]interface{}, len(c.Settings)+len(body.Settings))
	for k, v := range c.Settings {
		settings[k] = v
	}
	for k, v := range body.Settings {
		settings[k] = v
	}
	if err := s.svc.UpdateClient(c.ID, name, settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated := s.findClient(c.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleRemoveClient(w http.ResponseWriter, r *http.Request) {
	if c := s.clientOr404(w, r); c != nil {
		noContent(w, s.svc.RemoveClient(c.ID))
	}
}

func (s *Server) handleDisconnectClient(w http.ResponseWriter, r *http.Request) {
	if c := s.clientOr404(w, r); c != nil {
		noContent(w, s.svc.DisconnectClient(c.ID))
	}
}

// buildBody はコマンドを組み立てる入力
type buildBody struct {
	CommandID string                 `json:"commandId"`
	Params    map[string]interface{} `json:"params"`
}

// handleBuildClientRequest はコマンドと項目の値からリクエストを組み立てる（送らない）
func (s *Server) handleBuildClientRequest(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	var body buildBody
	if !decodeBody(w, r, &body) {
		return
	}
	hex, err := s.svc.BuildClientRequest(c.ProtocolType, body.CommandID, body.Params)
	jsonOr400(w, map[string]string{"request": hex}, err)
}

// handleSendClientRequest はリクエストを送る。ボディは {"request": "16 進"} か {"commandId", "params"}。
// 通信の失敗（タイムアウト・例外応答など）も 200 で、結果の status に入る
func (s *Server) handleSendClientRequest(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	var body struct {
		Request string `json:"request"`
		buildBody
	}
	if !decodeBody(w, r, &body) {
		return
	}
	hex := body.Request
	if hex == "" {
		if body.CommandID == "" {
			writeError(w, http.StatusBadRequest, "request か commandId を指定してください")
			return
		}
		var err error
		if hex, err = s.svc.BuildClientRequest(c.ProtocolType, body.CommandID, body.Params); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	result, err := s.svc.SendClientRequest(c.ID, hex)
	jsonOr400(w, result, err)
}

// handleBrowseClient は機器のアドレス（OPC UA ならノード）のうち、?parent= の子を返す（なければ、いちばん上）
func (s *Server) handleBrowseClient(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	entries, err := s.svc.BrowseClient(c.ID, r.URL.Query().Get("parent"))
	if entries == nil {
		entries = []application.ClientBrowseEntryDTO{}
	}
	jsonOr400(w, entries, err)
}

// --- 保存したコマンド ---

func (s *Server) handleGetSavedCommands(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	cmds, err := s.svc.GetClientSavedCommands(c.ID)
	if cmds == nil {
		cmds = []application.ClientSavedCommandDTO{}
	}
	jsonOr400(w, cmds, err)
}

func (s *Server) handleAddSavedCommand(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	var in application.ClientSavedCommandInputDTO
	if !decodeBody(w, r, &in) {
		return
	}
	cmd, err := s.svc.AddClientSavedCommand(c.ID, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, cmd)
}

// handleUpdateSavedCommand は保存したコマンドを置き換える。mode を省けば、名前だけを変える
func (s *Server) handleUpdateSavedCommand(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	cmdID := s.savedCommandOr404(w, r, c.ID)
	if cmdID == "" {
		return
	}
	var in application.ClientSavedCommandInputDTO
	if !decodeBody(w, r, &in) {
		return
	}
	if in.Mode == "" {
		noContent(w, s.svc.RenameClientSavedCommand(c.ID, cmdID, in.Name))
		return
	}
	noContent(w, s.svc.UpdateClientSavedCommand(c.ID, cmdID, in))
}

func (s *Server) handleRemoveSavedCommand(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	if cmdID := s.savedCommandOr404(w, r, c.ID); cmdID != "" {
		noContent(w, s.svc.RemoveClientSavedCommand(c.ID, cmdID))
	}
}

func (s *Server) handleSendSavedCommand(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	if cmdID := s.savedCommandOr404(w, r, c.ID); cmdID != "" {
		result, err := s.svc.SendClientSavedCommand(c.ID, cmdID)
		jsonOr400(w, result, err)
	}
}

// --- ポーリング ---

func (s *Server) handleGetPolling(w http.ResponseWriter, r *http.Request) {
	if c := s.clientOr404(w, r); c != nil {
		p, err := s.svc.GetClientPolling(c.ID)
		jsonOr400(w, p, err)
	}
}

// handleUpdatePolling は周期（intervalMs）と、通信エラーにするまでの失敗の回数（failThreshold）を変える（省いたものはそのまま）
func (s *Server) handleUpdatePolling(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	var body struct {
		IntervalMs    *int `json:"intervalMs"`
		FailThreshold *int `json:"failThreshold"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.IntervalMs != nil {
		if err := s.svc.SetClientPollInterval(c.ID, *body.IntervalMs); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if body.FailThreshold != nil {
		if err := s.svc.SetClientFailThreshold(c.ID, *body.FailThreshold); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	p, err := s.svc.GetClientPolling(c.ID)
	jsonOr400(w, p, err)
}

func (s *Server) handleStartPolling(w http.ResponseWriter, r *http.Request) {
	if c := s.clientOr404(w, r); c != nil {
		noContent(w, s.svc.StartClientPolling(c.ID))
	}
}

func (s *Server) handleStopPolling(w http.ResponseWriter, r *http.Request) {
	if c := s.clientOr404(w, r); c != nil {
		noContent(w, s.svc.StopClientPolling(c.ID))
	}
}

type pollItemBody struct {
	CommandID string                 `json:"commandId"`
	Params    map[string]interface{} `json:"params"`
	Format    string                 `json:"format"`
}

func (s *Server) handleAddPollItem(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	var body pollItemBody
	if !decodeBody(w, r, &body) {
		return
	}
	item, err := s.svc.AddClientPollItem(c.ID, body.CommandID, body.Params, body.Format)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleUpdatePollItem(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	var body pollItemBody
	if !decodeBody(w, r, &body) {
		return
	}
	noContent(w, s.svc.UpdateClientPollItem(c.ID, r.PathValue("itemId"), body.CommandID, body.Params, body.Format))
}

func (s *Server) handleRemovePollItem(w http.ResponseWriter, r *http.Request) {
	if c := s.clientOr404(w, r); c != nil {
		noContent(w, s.svc.RemoveClientPollItem(c.ID, r.PathValue("itemId")))
	}
}

// --- サブスクリプション ---

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	if c := s.clientOr404(w, r); c != nil {
		sub, err := s.svc.GetClientSubscription(c.ID)
		jsonOr400(w, sub, err)
	}
}

// handleUpdateSubscription は通知の間隔（intervalMs）を変える
func (s *Server) handleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	var body struct {
		IntervalMs *int `json:"intervalMs"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.IntervalMs != nil {
		if err := s.svc.SetClientSubscriptionInterval(c.ID, *body.IntervalMs); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	sub, err := s.svc.GetClientSubscription(c.ID)
	jsonOr400(w, sub, err)
}

func (s *Server) handleStartSubscription(w http.ResponseWriter, r *http.Request) {
	if c := s.clientOr404(w, r); c != nil {
		noContent(w, s.svc.StartClientSubscription(c.ID))
	}
}

func (s *Server) handleStopSubscription(w http.ResponseWriter, r *http.Request) {
	if c := s.clientOr404(w, r); c != nil {
		noContent(w, s.svc.StopClientSubscription(c.ID))
	}
}

// handleAddSubscriptionItems は監視項目を足す。ボディは {"params": {...}}（1 回の入力から、いくつも足されることがある）
func (s *Server) handleAddSubscriptionItems(w http.ResponseWriter, r *http.Request) {
	c := s.clientOr404(w, r)
	if c == nil {
		return
	}
	var body struct {
		Params map[string]interface{} `json:"params"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	items, err := s.svc.AddClientSubscriptionItems(c.ID, body.Params)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, items)
}

func (s *Server) handleRemoveSubscriptionItem(w http.ResponseWriter, r *http.Request) {
	if c := s.clientOr404(w, r); c != nil {
		noContent(w, s.svc.RemoveClientSubscriptionItem(c.ID, r.PathValue("itemId")))
	}
}

// --- コマンドの履歴 ---

func (s *Server) handleGetClientLogs(w http.ResponseWriter, r *http.Request) {
	logs := s.svc.GetClientCommandLogs()
	if logs == nil {
		logs = []application.ClientCommandLogDTO{}
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) handleClearClientLogs(w http.ResponseWriter, r *http.Request) {
	s.svc.ClearClientCommandLogs()
	w.WriteHeader(http.StatusNoContent)
}
