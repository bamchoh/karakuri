package httpapi

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

// アドレスを文字で指定する読み書き（/api/memory/{protocolType}/words・bits）。
// フェイクの書き方は「エリアの ID:番号[.ビット]」（0 始まり）。実際のプロトコルの書き方は internal/protocols の試験で確かめる
func TestMemory_AddressText(t *testing.T) {
	api := newTestAPIWithServer(t)
	type atResp struct {
		Area    string
		Address int
		Values  []interface{}
	}
	get := func(path string) atResp {
		t.Helper()
		var r atResp
		api.do("GET", path, "").status(http.StatusOK).json(&r)
		return r
	}

	// ワードの中のビットを書いて、ワードとビットで読む
	api.do("PUT", "/api/memory/fake-a/bits", `{"address":"holdingRegisters:10.3","value":true}`).status(http.StatusNoContent)
	if r := get("/api/memory/fake-a/words?address=holdingRegisters:10"); r.Area != "holdingRegisters" || r.Address != 10 || !reflect.DeepEqual(r.Values, []interface{}{float64(8)}) {
		t.Errorf("ビット 3 を ON にしたワード = %+v, want 8", r)
	}
	if r := get("/api/memory/fake-a/bits?address=holdingRegisters:10.2&count=3"); r.Address != 162 || !reflect.DeepEqual(r.Values, []interface{}{false, true, false}) {
		t.Errorf("ビットで読む = %+v", r)
	}
	// ビットの位置を付けないワードをビットで読むと、そのワードのビット 0 から
	if r := get("/api/memory/fake-a/bits?address=holdingRegisters:10&count=4"); r.Address != 160 || !reflect.DeepEqual(r.Values, []interface{}{false, false, false, true}) {
		t.Errorf("ワードをビットで読む = %+v", r)
	}

	// ビットのエリアをワードで書くと、そこから 16 点
	api.do("PUT", "/api/memory/fake-a/words", `{"address":"coils:16","values":[33]}`).status(http.StatusNoContent)
	if r := get("/api/memory/fake-a/bits?address=coils:16&count=6"); !reflect.DeepEqual(r.Values, []interface{}{true, false, false, false, false, true}) {
		t.Errorf("ワード 0x21 を書いたコイル = %+v", r)
	}

	// 誤り
	api.do("GET", "/api/memory/fake-a/words?address=holdingRegisters:10.3", "").isError(http.StatusBadRequest) // ワードの中のビットはワードで読めない
	api.do("GET", "/api/memory/fake-a/words", "").isError(http.StatusBadRequest)
	api.do("GET", "/api/memory/fake-a/words?address=nope:1", "").isError(http.StatusBadRequest)
	api.do("GET", "/api/memory/fake-a/words?address=holdingRegisters:1&count=0", "").isError(http.StatusBadRequest)
	api.do("PUT", "/api/memory/fake-a/bits", `{"address":"holdingRegisters:1.16","value":true}`).isError(http.StatusBadRequest)
	api.do("PUT", "/api/memory/fake-a/bits", `{"address":"holdingRegisters:1"}`).isError(http.StatusBadRequest)
	api.do("PUT", "/api/memory/fake-a/words", `{"address":"holdingRegisters:1","value":70000}`).isError(http.StatusBadRequest)
	api.do("GET", "/api/memory/fake-b/words?address=holdingRegisters:1", "").isError(http.StatusBadRequest) // サーバーがない
}

// 単位をまたいだ書き込み（ワードのエリアにビット、ビットのエリアにワード）の範囲は、ビットの番号で数える。
// 以前は、ワードのエリアにビットで書くと、エリアの大きさを「ワードの数」のまま比べて、後ろの 15/16 に書けなかった
func TestMemory_CrossUnitRange(t *testing.T) {
	api := newTestAPIWithServer(t)
	const words = 9999 // フェイクの holdingRegisters の大きさ
	lastBit := words*16 - 1
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/bits", fmt.Sprintf(`{"address":%d,"values":[true]}`, lastBit)).status(http.StatusNoContent)
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/bits", fmt.Sprintf(`{"address":%d,"values":[true,true]}`, lastBit)).isError(http.StatusBadRequest)
	var r struct{ Values []int }
	api.do("GET", fmt.Sprintf("/api/memory/fake-a/holdingRegisters/words?address=%d", words-1), "").status(http.StatusOK).json(&r)
	if !reflect.DeepEqual(r.Values, []int{0x8000}) {
		t.Errorf("最後のワード = %v, want [32768]（最後のビットだけ ON）", r.Values)
	}

	// ビットのエリアにワードで書くときは、1 ワードが 16 点
	const coils = 9999
	api.do("PUT", "/api/memory/fake-a/coils/words", fmt.Sprintf(`{"address":%d,"values":[1]}`, coils-16)).status(http.StatusNoContent)
	api.do("PUT", "/api/memory/fake-a/coils/words", fmt.Sprintf(`{"address":%d,"values":[1]}`, coils-15)).isError(http.StatusBadRequest)
}

// 単位をまたいで書いても、マッピングした変数に反映する（以前は、書かれた範囲を取り違えて反映しなかった）
func TestMemory_CrossUnitWriteSyncsVariables(t *testing.T) {
	api := newTestAPIWithServer(t)
	word, err := api.svc.CreateVariable("Word", "INT", nil)
	if err != nil {
		t.Fatal(err)
	}
	flag, err := api.svc.CreateVariable("Flag", "BOOL", nil)
	if err != nil {
		t.Fatal(err)
	}
	api.do("PUT", "/api/variables/Word/mappings", `[{"protocolType":"fake-a","memoryArea":"holdingRegisters","address":20,"endianness":"big"}]`).status(http.StatusNoContent)
	api.do("PUT", "/api/variables/Flag/mappings", `[{"protocolType":"fake-a","memoryArea":"coils","address":37,"endianness":"big"}]`).status(http.StatusNoContent)
	value := func(id string) interface{} {
		t.Helper()
		for _, v := range api.svc.GetVariables() {
			if v.ID == id {
				return v.Value
			}
		}
		t.Fatalf("変数 %s がない", id)
		return nil
	}

	// ワードのエリアにビットで書く: holdingRegisters 20 のビット 1
	api.do("PUT", "/api/memory/fake-a/bits", `{"address":"holdingRegisters:20.1","value":true}`).status(http.StatusNoContent)
	if got := fmt.Sprint(value(word.ID)); got != "2" {
		t.Errorf("ビット 1 を ON にしたあとの Word = %s, want 2", got)
	}
	// 別のワードのビットでは変わらない（ビットの番号 5 を、ワードの番号 5 と取り違えない）
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/bits", `{"address":20,"values":[true]}`).status(http.StatusNoContent) // ワード 1 のビット 4
	if got := fmt.Sprint(value(word.ID)); got != "2" {
		t.Errorf("ワード 1 のビットを書いたら Word = %s に変わった", got)
	}

	// ビットのエリアにワードで書く: coils 32 から 16 点（37 はビット 5）
	api.do("PUT", "/api/memory/fake-a/words", `{"address":"coils:32","value":32}`).status(http.StatusNoContent)
	if got := value(flag.ID); got != true {
		t.Errorf("coils 32 からワード 0x20 を書いたあとの Flag = %v, want true", got)
	}
	api.do("PUT", "/api/memory/fake-a/coils/words", `{"address":32,"values":[0]}`).status(http.StatusNoContent)
	if got := value(flag.ID); got != false {
		t.Errorf("coils 32 からワード 0 を書いたあとの Flag = %v, want false", got)
	}
}
