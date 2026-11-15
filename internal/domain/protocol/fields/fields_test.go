package fields

import "testing"

func TestRows(t *testing.T) {
	// 画面から（JSON を通った値）
	got := Rows(map[string]interface{}{"items": []interface{}{
		map[string]interface{}{"device": "D", "address": "100"},
		"行ではない値は読み飛ばす",
		map[string]interface{}{"device": "M"},
	}}, "items")
	if len(got) != 2 || got[0]["device"] != "D" || got[1]["device"] != "M" {
		t.Errorf("Rows = %v", got)
	}

	// Go のコードから
	got = Rows(map[string]interface{}{"items": []map[string]interface{}{{"device": "W"}}}, "items")
	if len(got) != 1 || got[0]["device"] != "W" {
		t.Errorf("Rows = %v", got)
	}

	// 行の並びでなければ、行はない
	for _, v := range []interface{}{nil, "D100", 3} {
		if got := Rows(map[string]interface{}{"items": v}, "items"); len(got) != 0 {
			t.Errorf("%v: Rows = %v", v, got)
		}
	}
}
