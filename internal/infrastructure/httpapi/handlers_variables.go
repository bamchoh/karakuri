package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"karakuri/internal/application"
)

// --- 変数管理ハンドラー ---
//
// パスの {ref} は変数の ID か名前（ID を先に探す）

// findVariable は ID か名前で変数を探す
func (s *Server) findVariable(ref string) *application.VariableDTO {
	vars := s.svc.GetVariables()
	for _, v := range vars {
		if v.ID == ref {
			return v
		}
	}
	for _, v := range vars {
		if v.Name == ref {
			return v
		}
	}
	return nil
}

// variableOr404 はパスの {ref} の変数を返す。なければ 404 を返して nil
func (s *Server) variableOr404(w http.ResponseWriter, r *http.Request) *application.VariableDTO {
	ref := r.PathValue("ref")
	v := s.findVariable(ref)
	if v == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("変数がありません: %s", ref))
	}
	return v
}

func (s *Server) handleGetVariables(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.GetVariables())
}

func (s *Server) handleCreateVariable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string      `json:"name"`
		DataType string      `json:"dataType"`
		Value    interface{} `json:"value"`
	}
	if !decodeValueBody(w, r, &body) {
		return
	}
	v, err := s.svc.CreateVariable(body.Name, body.DataType, normalizeNumbers(body.Value))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) handleGetVariable(w http.ResponseWriter, r *http.Request) {
	if v := s.variableOr404(w, r); v != nil {
		writeJSON(w, http.StatusOK, v)
	}
}

// handleUpdateVariable は名前・データ型を変える（省略した方はそのまま）。データ型を変えると値は既定値に戻る
func (s *Server) handleUpdateVariable(w http.ResponseWriter, r *http.Request) {
	v := s.variableOr404(w, r)
	if v == nil {
		return
	}
	var body struct {
		Name     *string `json:"name"`
		DataType *string `json:"dataType"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	name, dataType := v.Name, v.DataType
	if body.Name != nil {
		name = *body.Name
	}
	if body.DataType != nil {
		dataType = *body.DataType
	}
	updated, err := s.svc.UpdateVariable(v.ID, name, dataType)
	jsonOr400(w, updated, err)
}

func (s *Server) handleDeleteVariable(w http.ResponseWriter, r *http.Request) {
	if v := s.variableOr404(w, r); v != nil {
		noContent(w, s.svc.DeleteVariable(v.ID))
	}
}

// handleReadVariableValue は値を読む。?path= でフィールド・要素（"speed"、"[1]"、"[1].name"）
func (s *Server) handleReadVariableValue(w http.ResponseWriter, r *http.Request) {
	v := s.variableOr404(w, r)
	if v == nil {
		return
	}
	value, err := s.svc.ReadVariableValue(v.ID, r.URL.Query().Get("path"))
	jsonOr400(w, map[string]interface{}{"value": value}, err)
}

// handleUpdateVariableValue は値を書く。?path= でフィールド・要素だけを書く
func (s *Server) handleUpdateVariableValue(w http.ResponseWriter, r *http.Request) {
	v := s.variableOr404(w, r)
	if v == nil {
		return
	}
	var body struct {
		Value interface{} `json:"value"`
	}
	if !decodeValueBody(w, r, &body) {
		return
	}
	noContent(w, s.writeValue(v.ID, r.URL.Query().Get("path"), normalizeNumbers(body.Value)))
}

func (s *Server) writeValue(id, path string, value interface{}) error {
	if path == "" {
		return s.svc.UpdateVariableValue(id, value)
	}
	return s.svc.UpdateVariableFieldValue(id, path, value)
}

func (s *Server) handleGetVariableMappings(w http.ResponseWriter, r *http.Request) {
	v := s.variableOr404(w, r)
	if v == nil {
		return
	}
	mappings, err := s.svc.GetVariableMappings(v.ID)
	if mappings == nil {
		mappings = []application.ProtocolMappingDTO{}
	}
	jsonOr400(w, mappings, err)
}

// handleUpdateVariableMappings はマッピングをまるごと置き換える（空の配列なら、すべて外す）
func (s *Server) handleUpdateVariableMappings(w http.ResponseWriter, r *http.Request) {
	v := s.variableOr404(w, r)
	if v == nil {
		return
	}
	var mappings []application.ProtocolMappingDTO
	if !decodeBody(w, r, &mappings) {
		return
	}
	noContent(w, s.svc.UpdateVariableMappings(v.ID, mappings))
}

func (s *Server) handleUpdateNodePublishing(w http.ResponseWriter, r *http.Request) {
	v := s.variableOr404(w, r)
	if v == nil {
		return
	}
	var dto application.NodePublishingDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	pt := r.PathValue("protocolType")
	dto.ProtocolType = pt
	if dto.AccessMode == "" {
		dto.AccessMode = "readwrite"
	}
	noContent(w, s.svc.UpdateVariableNodePublishing(v.ID, pt, &dto))
}

// --- 名前で、まとめて読み書き ---
//
// キーは変数の名前。フィールド・要素まで書ける（"Motor1.speed"、"Values[1]"、"Line[1].motor.speed"）

// resolveValueKey はキーを変数とフィールドパスに分ける。名前がそのままあれば変数全体。
// なければ、"." か "[" の前までが変数の名前になる区切りを探す（いちばん長い名前を選ぶ）
func resolveValueKey(vars []*application.VariableDTO, key string) (*application.VariableDTO, string, bool) {
	byName := make(map[string]*application.VariableDTO, len(vars))
	for _, v := range vars {
		byName[v.Name] = v
	}
	if v, ok := byName[key]; ok {
		return v, "", true
	}
	for i := len(key) - 1; i > 0; i-- {
		if key[i] != '.' && key[i] != '[' {
			continue
		}
		if v, ok := byName[key[:i]]; ok {
			return v, strings.TrimPrefix(key[i:], "."), true
		}
	}
	return nil, "", false
}

// handleReadValues は値をまとめて読む。?names=A,B.x（なければ、すべての変数）。結果は名前 → 値
func (s *Server) handleReadValues(w http.ResponseWriter, r *http.Request) {
	vars := s.svc.GetVariables()
	var keys []string
	if q := r.URL.Query().Get("names"); q != "" {
		for _, k := range strings.Split(q, ",") {
			if k = strings.TrimSpace(k); k != "" {
				keys = append(keys, k)
			}
		}
	} else {
		for _, v := range vars {
			keys = append(keys, v.Name)
		}
	}
	result := make(map[string]interface{}, len(keys))
	for _, key := range keys {
		v, path, ok := resolveValueKey(vars, key)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Sprintf("変数がありません: %s", key))
			return
		}
		value, err := s.svc.ReadVariableValue(v.ID, path)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s: %v", key, err))
			return
		}
		result[key] = value
	}
	writeJSON(w, http.StatusOK, result)
}

// handleWriteValues は値をまとめて書く。ボディは名前 → 値。
// 先にすべてのキーの変数があるかを確かめる（なければ 1 つも書かない）。書くのはキーの名前順。
// 途中で書けないものがあれば、そこで止めて 400（それより前のものは書かれている）
func (s *Server) handleWriteValues(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	if !decodeValueBody(w, r, &body) {
		return
	}
	vars := s.svc.GetVariables()
	type target struct {
		key, id, path string
		value         interface{}
	}
	targets := make([]target, 0, len(body))
	for key, value := range body {
		v, path, ok := resolveValueKey(vars, key)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Sprintf("変数がありません: %s", key))
			return
		}
		targets = append(targets, target{key, v.ID, path, normalizeNumbers(value)})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].key < targets[j].key })
	for _, t := range targets {
		if err := s.writeValue(t.id, t.path, t.value); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s: %v", t.key, err))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- データ型・構造体型・マッピング先・同期の状態 ---

func (s *Server) handleGetDataTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.GetDataTypes())
}

func (s *Server) handleGetStructTypes(w http.ResponseWriter, r *http.Request) {
	types := s.svc.GetStructTypes()
	if types == nil {
		types = []application.StructTypeDTO{}
	}
	writeJSON(w, http.StatusOK, types)
}

func (s *Server) handleRegisterStructType(w http.ResponseWriter, r *http.Request) {
	var dto application.StructTypeDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	created, err := s.svc.RegisterStructType(dto)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteStructType(w http.ResponseWriter, r *http.Request) {
	noContent(w, s.svc.DeleteStructType(r.PathValue("name")))
}

func (s *Server) handleGetMappingTargets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.GetMappingTargets())
}

func (s *Server) handleGetVariableStatuses(w http.ResponseWriter, r *http.Request) {
	statuses := s.svc.GetVariableDeviceStatuses()
	if statuses == nil {
		statuses = []application.VariableDeviceStatusDTO{}
	}
	writeJSON(w, http.StatusOK, statuses)
}
