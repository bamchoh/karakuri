package protocols_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"karakuri/internal/application"
	"karakuri/internal/infrastructure/httpapi"
)

// HTTP API のメモリの読み書きを、組み込みの全部のレジスタ型のプロトコルで、PLC の書き方のアドレスで確かめる。
//   - ワードの中のビットを書くと、そのワードにマッピングした変数に反映する（ワードのエリアにビットで書く）
//   - ビットのデバイスを 16 点まとめてワードで書くと、その中のビットにマッピングした BOOL の変数に反映する
//
// 以前は、単位をまたいで書くと、変数との同期が書かれた範囲を取り違えて、変数に反映しなかった
func TestMemoryAddressText_AllProtocols(t *testing.T) {
	cases := []struct {
		protocolType string
		// ワード: INT の変数をマッピングするエリアとアドレス、そのワードのアドレスとビット 1 の書き方
		wordArea           string
		wordAddr           int
		wordText, bit1Text string
		// ビット: BOOL の変数をマッピングするエリアとアドレス、16 点をまとめて書くときの先頭の書き方（変数は 5 点目）
		bitArea  string
		bitAddr  int
		bitsText string
	}{
		{"slmp", "D", 20, "D20", "D20.1", "X", 0x105, "X100"},
		{"mc-serial", "D", 20, "D20", "D20.1", "M", 37, "M32"},
		{"fins", "DM", 20, "D20", "D20.1", "T_FLAG", 5, "TF0"},
		{"fins-hostlink", "WR", 20, "W20", "W20.01", "CIO_BIT", 30*16 + 5, "CIO 30"},
		{"modbus-tcp", "holdingRegisters", 20, "40021", "40021.1", "coils", 37, "00033"},
		{"modbus-rtu", "holdingRegisters", 20, "40021", "40021.1", "coils", 37, "00033"},
		{"modbus-ascii", "holdingRegisters", 20, "40021", "40021.1", "coils", 37, "00033"},
	}

	svc := newService(t)
	port := freePort(t)
	api := httpapi.NewServer(svc, port)
	must(t, api.Start())
	t.Cleanup(func() { _ = api.Shutdown(context.Background()) })
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	call := func(method, path, body string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, bytes.NewReader([]byte(body)))
		must(t, err)
		res, err := http.DefaultClient.Do(req)
		must(t, err)
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, b
	}
	put := func(path, body string) {
		t.Helper()
		if code, b := call("PUT", path, body); code != http.StatusNoContent {
			t.Fatalf("PUT %s %s: %d %s", path, body, code, b)
		}
	}
	value := func(id string) string {
		t.Helper()
		return variableValue(t, svc, id)
	}

	for _, c := range cases {
		t.Run(c.protocolType, func(t *testing.T) {
			must(t, svc.AddServer(c.protocolType, "")) // 起動しなくても、メモリは読み書きできる
			word, err := svc.CreateVariable("Word_"+c.protocolType, "INT", nil)
			must(t, err)
			flag, err := svc.CreateVariable("Flag_"+c.protocolType, "BOOL", nil)
			must(t, err)
			must(t, svc.UpdateVariableMappings(word.ID, []application.ProtocolMappingDTO{{ProtocolType: c.protocolType, MemoryArea: c.wordArea, Address: c.wordAddr, Endianness: "big"}}))
			must(t, svc.UpdateVariableMappings(flag.ID, []application.ProtocolMappingDTO{{ProtocolType: c.protocolType, MemoryArea: c.bitArea, Address: c.bitAddr, Endianness: "big"}}))
			mem := "/api/memory/" + c.protocolType

			// ワードの中のビット
			put(mem+"/bits", fmt.Sprintf(`{"address":%q,"value":true}`, c.bit1Text))
			if got := value(word.ID); got != "2" {
				t.Errorf("%s を ON にしたあとの変数 = %s, want 2", c.bit1Text, got)
			}
			code, b := call("GET", mem+"/words?address="+url.QueryEscape(c.wordText), "")
			var words struct{ Values []int }
			_ = json.Unmarshal(b, &words)
			if code != http.StatusOK || !reflect.DeepEqual(words.Values, []int{2}) {
				t.Errorf("GET words %s: %d %s", c.wordText, code, b)
			}
			// ワードの中のビットは、ワードとしては読めない
			if code, _ := call("GET", mem+"/words?address="+url.QueryEscape(c.bit1Text), ""); code != http.StatusBadRequest {
				t.Errorf("GET words %s: %d, want 400", c.bit1Text, code)
			}

			// ビットのデバイスを 16 点まとめてワードで
			put(mem+"/words", fmt.Sprintf(`{"address":%q,"value":32}`, c.bitsText))
			if got := value(flag.ID); got != "true" {
				t.Errorf("%s からワード 0x20 を書いたあとの BOOL の変数 = %s, want true", c.bitsText, got)
			}
			code, b = call("GET", mem+"/bits?address="+url.QueryEscape(c.bitsText)+"&count=6", "")
			var bits struct{ Values []bool }
			_ = json.Unmarshal(b, &bits)
			if code != http.StatusOK || !reflect.DeepEqual(bits.Values, []bool{false, false, false, false, false, true}) {
				t.Errorf("GET bits %s: %d %s", c.bitsText, code, b)
			}
		})
	}

	// 最後のワードの最後のビットまで、ビットで書ける（以前は、ワードの数の 1/16 までしか書けなかった）
	t.Run("最後のビット", func(t *testing.T) {
		for _, c := range []struct{ pt, text, word string }{
			{"slmp", "D16777215.F", "D16777215"}, {"fins", "D65535.15", "D65535"}, {"modbus-tcp", "465536.15", "465536"},
		} {
			put("/api/memory/"+c.pt+"/bits", fmt.Sprintf(`{"address":%q,"value":true}`, c.text))
			code, b := call("GET", "/api/memory/"+c.pt+"/words?address="+c.word, "")
			if code != http.StatusOK || !bytes.Contains(b, []byte(`"values":[32768]`)) {
				t.Errorf("%s: GET words %s = %d %s, want [32768]", c.pt, c.word, code, b)
			}
		}
	})
}
