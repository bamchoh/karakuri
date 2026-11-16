package modbus

import (
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"karakuri/internal/protocols/modbus/rtu"
)

// RTU / ASCII のサーバーが実際に使う組み立て（rtu.Processor → RTUDataStoreAdapter → ModbusDataStore）に、
// すべてのファンクションコードのリクエストを通す。rtu のテストはフェイクの受け手を使うので、ここで DataStore までを確かめる
func TestRTUAdapter_AllFunctionCodesThroughDataStore(t *testing.T) {
	store := NewModbusDataStore(16, 16, 16, 16)
	handler := NewDataStoreHandler(store)
	p := rtu.NewProcessor(NewRTUDataStoreAdapter(handler))

	process := func(body string) string {
		t.Helper()
		raw, err := hex.DecodeString(strings.ReplaceAll(body, " ", ""))
		if err != nil {
			t.Fatal(err)
		}
		req, err := rtu.ParseRequest(rtu.AppendCRC(raw))
		if err != nil {
			t.Fatalf("ParseRequest(%s): %v", body, err)
		}
		res := p.Process(req)
		if res == nil {
			return ""
		}
		return strings.ToUpper(hex.EncodeToString(res[:len(res)-2])) // CRC を除く
	}
	expect := func(name, body, want string) {
		t.Helper()
		if got := process(body); got != strings.ReplaceAll(want, " ", "") {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}

	// 機器の側で書けないエリアは、DataStore に直接入れておく
	_ = store.WriteBit(AreaDiscreteInputs, 2, true)
	_ = store.WriteWord(AreaInputRegs, 3, 0x1234)

	expect("FC05 コイル 1 を ON", "01 05 00 01 FF 00", "01 05 00 01 FF 00")
	expect("FC15 コイル 4〜6", "01 0F 00 04 00 03 01 05", "01 0F 00 04 00 03")
	expect("FC01 コイル 0〜7", "01 01 00 00 00 08", "01 01 01 52") // 0101 0010: 1・4・6
	expect("FC02 ディスクリート入力 0〜3", "01 02 00 00 00 04", "01 02 01 04")
	expect("FC06 保持レジスタ 2", "01 06 00 02 AB CD", "01 06 00 02 AB CD")
	expect("FC16 保持レジスタ 3〜4", "01 10 00 03 00 02 04 00 01 00 02", "01 10 00 03 00 02")
	expect("FC03 保持レジスタ 2〜4", "01 03 00 02 00 03", "01 03 06 AB CD 00 01 00 02")
	expect("FC04 入力レジスタ 3", "01 04 00 03 00 01", "01 04 02 12 34")

	// DataStore に書かれている
	if bits, _ := store.ReadBits(AreaCoils, 0, 8); !slices.Equal(bits, []bool{false, true, false, false, true, false, true, false}) {
		t.Errorf("コイル = %v", bits)
	}
	if words, _ := store.ReadWords(AreaHoldingRegs, 2, 3); !slices.Equal(words, []uint16{0xABCD, 1, 2}) {
		t.Errorf("保持レジスタ = %v", words)
	}

	// エリアの最後と、終わりを超えるもの（例外 02）
	expect("FC03 最後のアドレス", "01 03 00 0F 00 01", "01 03 02 00 00")
	expect("FC01 終わりを超える", "01 01 00 0F 00 02", "01 81 02")
	expect("FC02 終わりを超える", "01 02 00 10 00 01", "01 82 02")
	expect("FC03 終わりを超える", "01 03 00 0F 00 02", "01 83 02")
	expect("FC04 終わりを超える", "01 04 00 10 00 01", "01 84 02")
	expect("FC05 終わりを超える", "01 05 00 10 FF 00", "01 85 02")
	expect("FC06 終わりを超える", "01 06 00 10 00 01", "01 86 02")
	expect("FC15 終わりを超える", "01 0F 00 0F 00 02 01 03", "01 8F 02")
	expect("FC16 終わりを超える", "01 10 00 0F 00 02 04 00 01 00 02", "01 90 02")

	// 仕様に合わない個数（例外 03）
	expect("FC03 個数 126", "01 03 00 00 00 7E", "01 83 03")
	expect("FC16 バイト数が個数と合わない", "01 10 00 00 00 02 02 00 01", "01 90 03")

	// 範囲外の書き込みでは、範囲内の分も書かない
	if words, _ := store.ReadWords(AreaHoldingRegs, 15, 1); words[0] != 0 {
		t.Errorf("範囲外の FC16 で、範囲内のアドレス 15 に書かれた: %v", words)
	}

	// オフにした UnitID には、どのファンクションコードでも応答しない（仕様に合わないリクエストでも）
	handler.SetUnitIdEnabled(1, false)
	for _, body := range []string{"01 01 00 00 00 01", "01 02 00 00 00 01", "01 03 00 00 00 01", "01 04 00 00 00 01",
		"01 05 00 00 FF 00", "01 06 00 00 00 01", "01 0F 00 00 00 01 01 01", "01 10 00 00 00 01 02 00 01", "01 03 00 00 00 00"} {
		if got := process(body); got != "" {
			t.Errorf("オフにした UnitID に応答した: %s => %s", body, got)
		}
	}
	// ほかの UnitID には応答する
	expect("UnitID 2", "02 03 00 02 00 01", "02 03 02 AB CD")
}
