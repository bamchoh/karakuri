package httpapi

import (
	_ "embed"
	"net/http"
)

// openAPISpec は REST API の仕様（OpenAPI 3.0）。エンドポイントを足したら、ここにも書く
// （routes() と一致しているかを server_test.go が確かめる）
//
//go:embed openapi.json
var openAPISpec []byte

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(openAPISpec) //nolint:errcheck
}
