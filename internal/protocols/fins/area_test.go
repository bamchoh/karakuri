package fins

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"karakuri/internal/domain/protocol"
)

// タイマ・カウンタ・EM のバンクの、エリア種別とアドレスの対応を、リクエストのバイト列で確かめる
func TestHandler_TimerCounterAndEMBanks(t *testing.T) {
	store := NewDataStore()
	h := newHandler(store)
	_ = store.WriteWord(AreaT, 5, 0x0123)
	_ = store.WriteWord(AreaC, 5, 0x0456)
	_ = store.WriteBit(AreaT+flagSuffix, 5, true)
	_ = store.WriteBit(AreaC+flagSuffix, 6, true)
	_ = store.WriteWord("EM1", 100, 0x1111)
	_ = store.WriteWord("EMC", 100, 0xCCCC)
	_ = store.WriteWord("EMF", 100, 0xFFFF)
	_ = store.WriteWord("EM10", 100, 0x1010)
	_ = store.WriteWord("EM18", 100, 0x1818)

	const head = "80 00 02 00 01 00 00 05 00 7A"
	const replyHead = "C0 00 02 00 05 00 00 01 00 7A"
	cases := []struct {
		name, request, want string
	}{
		{"タイマの現在値（T5）", "01 01 89 00 05 00 00 01", "01 01 00 00 01 23"},
		{"カウンタの現在値（C5。アドレスは 0x8000 から）", "01 01 89 80 05 00 00 01", "01 01 00 00 04 56"},
		{"タイマのアップフラグ（T5 から 2 個）", "01 01 09 00 05 00 00 02", "01 01 00 00 01 00"},
		{"カウンタのアップフラグ（C5 から 2 個）", "01 01 09 80 05 00 00 02", "01 01 00 00 00 01"},
		{"カウンタの現在値を書く（C4095）", "01 02 89 8F FF 00 00 01 00 99", "01 02 00 00"},
		{"タイマのアップフラグを書く（T4095）", "01 02 09 0F FF 00 00 01 01", "01 02 00 00"},
		{"タイマの番号が範囲外（T4096）", "01 01 89 10 00 00 00 01", "01 01 11 03"},
		{"カウンタの終わりを超える", "01 01 89 8F FF 00 00 02", "01 01 11 04"},
		{"アップフラグにビットの番号は付けられない", "01 01 09 00 05 01 00 01", "01 01 11 03"},
		{"EM バンク 1", "01 01 A1 00 64 00 00 01", "01 01 00 00 11 11"},
		{"EM バンク C", "01 01 AC 00 64 00 00 01", "01 01 00 00 CC CC"},
		{"EM バンク F", "01 01 AF 00 64 00 00 01", "01 01 00 00 FF FF"},
		{"EM バンク 10（種別は 0x60 から）", "01 01 60 00 64 00 00 01", "01 01 00 00 10 10"},
		{"EM バンク 18", "01 01 68 00 64 00 00 01", "01 01 00 00 18 18"},
		{"EM バンク 18 のビット（100.03 と 100.04）", "01 01 E8 00 64 03 00 02", "01 01 00 00 01 01"},
		{"EM バンク 1 のビットを書く（200.15）", "01 02 21 00 C8 0F 00 01 01", "01 02 00 00"},
		{"ない EM バンク（19）", "01 01 69 00 00 00 00 01", "01 01 11 01"},
	}
	for _, c := range cases {
		got := h.handle(unhex(t, head+c.request))
		want := unhex(t, replyHead+c.want)
		if !bytes.Equal(got, want) {
			t.Errorf("%s:\n got  %s\n want %s", c.name, hexOf(got), hexOf(want))
		}
	}
	if w, _ := store.ReadWord(AreaC, 4095); w != 0x99 {
		t.Errorf("C4095 = %04X", w)
	}
	if b, _ := store.ReadBit(AreaT+flagSuffix, 4095); !b {
		t.Error("T4095 のアップフラグが ON になっていない")
	}
	if w, _ := store.ReadWord("EM1", 200); w != 0x8000 {
		t.Errorf("E1_200 = %04X", w)
	}
	// タイマとカウンタ、現在値とアップフラグは、別のメモリ
	if w, _ := store.ReadWord(AreaT, 4095); w != 0 {
		t.Errorf("T4095 の現在値 = %04X, want 0", w)
	}
	if b, _ := store.ReadBit(AreaC+flagSuffix, 5); b {
		t.Error("C5 のアップフラグが ON になっている")
	}
}

// サーバーが持つすべてのエリアを、クライアントのコマンドで、ワードとビットの両方で読み書きできる
func TestClient_AllAreas(t *testing.T) {
	if len(areaDefs) != 7+emBanks {
		t.Fatalf("エリアの数 = %d", len(areaDefs))
	}
	srv, port := startServer(t, transportBoth)
	c := newClient(t, port, transportTCP)
	f := &ClientFactory{}
	send := func(cmd string, params map[string]interface{}) protocol.CommandResult {
		t.Helper()
		req, err := f.BuildRequest(cmd, params)
		if err != nil {
			t.Fatalf("%s %v: %v", cmd, params, err)
		}
		return c.Send(context.Background(), req)
	}
	for i, a := range areaDefs {
		last := a.Words - 1
		value := uint16(0x1000 + i)

		// ワード: エリアの最後のアドレスに書いて読む
		if r := send(commandWrite, map[string]interface{}{"area": a.ID, "unit": unitWord, "address": last, "values": strconv.Itoa(int(value))}); r.Status != protocol.CommandOK {
			t.Errorf("%s ワードの書き込み: %+v", a.ID, r)
		}
		r := send(commandRead, map[string]interface{}{"area": a.ID, "unit": unitWord, "address": last, "count": 1})
		if r.Status != protocol.CommandOK || len(r.Values) != 1 || *r.Values[0].Word != value || r.Values[0].Label != a.Prefix+strconv.Itoa(last) {
			t.Errorf("%s ワードの読み出し: %+v", a.ID, r)
		}
		if w, _ := srv.store.ReadWord(a.ID, uint32(last)); w != value {
			t.Errorf("%s: サーバーの値 = %04X, want %04X", a.ID, w, value)
		}

		// ビット: 番号 3 のワードのビット 15（タイマ・カウンタは、番号 3 のアップフラグ）
		if r := send(commandWrite, map[string]interface{}{"area": a.ID, "unit": unitBit, "address": 3, "bit": 15, "values": "1"}); r.Status != protocol.CommandOK {
			t.Errorf("%s ビットの書き込み: %+v", a.ID, r)
		}
		r = send(commandRead, map[string]interface{}{"area": a.ID, "unit": unitBit, "address": 3, "bit": 15, "count": 1})
		wantLabel := a.Prefix + "3.15"
		if a.Flags {
			wantLabel = a.Prefix + "3"
		}
		if r.Status != protocol.CommandOK || len(r.Values) != 1 || !*r.Values[0].Bit || r.Values[0].Label != wantLabel {
			t.Errorf("%s ビットの読み出し: %+v", a.ID, r)
		}
		if a.Flags {
			if b, _ := srv.store.ReadBit(a.ID+flagSuffix, 3); !b {
				t.Errorf("%s: サーバーのアップフラグが ON になっていない", a.ID)
			}
			if w, _ := srv.store.ReadWord(a.ID, 3); w != 0 {
				t.Errorf("%s: アップフラグを書いたら現在値が変わった（%04X）", a.ID, w)
			}
		} else if w, _ := srv.store.ReadWord(a.ID, 3); w != 0x8000 {
			t.Errorf("%s: サーバーの値 = %04X, want 8000", a.ID, w)
		}
	}

	// 変数のマッピングに使えるエリア（ワード・ビット・フラグ）は、どれも読み書きのリクエストを作れて、サーバーが応答する
	for _, area := range f.MappingAreas() {
		last := area.Size - 1
		var words []uint16
		var bits []bool
		if area.IsBit {
			bits = []bool{true}
		} else {
			words = []uint16{0xBEEF}
		}
		req, err := f.BuildWriteRequest(area.ID, last, words, bits)
		if err != nil {
			t.Errorf("マッピング %s の書き込み: %v", area.ID, err)
			continue
		}
		if r := c.Send(context.Background(), req); r.Status != protocol.CommandOK {
			t.Errorf("マッピング %s の書き込み: %+v", area.ID, r)
		}
		req, err = f.BuildReadRequest(area.ID, last, 1)
		if err != nil {
			t.Errorf("マッピング %s の読み出し: %v", area.ID, err)
			continue
		}
		r := c.Send(context.Background(), req)
		ok := r.Status == protocol.CommandOK && len(r.Values) == 1
		if ok && area.IsBit {
			ok = r.Values[0].Bit != nil && *r.Values[0].Bit
		} else if ok {
			ok = r.Values[0].Word != nil && *r.Values[0].Word == 0xBEEF
		}
		if !ok {
			t.Errorf("マッピング %s の読み出し: %+v", area.ID, r)
		}
	}
}
