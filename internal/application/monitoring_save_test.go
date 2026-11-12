package application

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
)

func readSavedMonitoringItems(t *testing.T) []*MonitoringItemDTO {
	t.Helper()
	path, err := getMonitoringConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg MonitoringConfigDTO
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("saved config is not valid JSON: %v\n%s", err, data)
	}
	return cfg.Items
}

// issue #9: 項目を続けて変更すると、バックグラウンドの保存が項目のマップを
// ロックなしで読んで競合し、プロセスが落ちることがあった
func TestMonitoring_ConcurrentChangesAreSavedSafely(t *testing.T) {
	svc := newTestService(t)

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				item, err := svc.AddMonitoringItem(&MonitoringItemDTO{
					ProtocolType: "modbus-tcp", MemoryArea: "holdingRegisters", Address: w*1000 + i, BitWidth: 16,
				})
				if err != nil {
					t.Errorf("add: %v", err)
					return
				}
				_ = svc.ReorderMonitoringItem(item.ID, 0)
				_ = svc.MoveMonitoringItem(item.ID, "down")
				_ = svc.GetMonitoringItems()
				if i%2 == 0 {
					_ = svc.DeleteMonitoringItem(item.ID)
				}
			}
		}(w)
	}
	wg.Wait()
	svc.monitoringSaveGroup.Wait()

	// 最後に保存された内容は、最後の状態と一致する（古い内容で上書きされていない）
	want := svc.GetMonitoringItems()
	got := readSavedMonitoringItems(t)
	if len(got) != len(want) {
		t.Fatalf("saved %d items, want %d", len(got), len(want))
	}
	for i := range want {
		if *got[i] != *want[i] {
			t.Errorf("item %d: saved %+v, want %+v", i, *got[i], *want[i])
		}
	}
}

// 返した項目や渡した項目を呼び出し側が書き換えても、内部の状態は変わらない
func TestMonitoring_ItemsAreCopied(t *testing.T) {
	svc := newTestService(t)
	in := &MonitoringItemDTO{ProtocolType: "modbus-tcp", MemoryArea: "coils", Address: 1, BitWidth: 1}
	added, err := svc.AddMonitoringItem(in)
	if err != nil {
		t.Fatal(err)
	}
	in.Address = 99
	svc.GetMonitoringItems()[0].Address = 77
	if got := svc.GetMonitoringItems()[0].Address; got != 1 {
		t.Errorf("address = %d, want 1", got)
	}

	upd := *added
	upd.Address = 5
	if err := svc.UpdateMonitoringItem(&upd); err != nil {
		t.Fatal(err)
	}
	upd.Address = 55
	if got := svc.GetMonitoringItems()[0].Address; got != 5 {
		t.Errorf("address after update = %d, want 5", got)
	}

	// SaveMonitoringConfig はその時点の内容を同期で書く
	if err := svc.SaveMonitoringConfig(); err != nil {
		t.Fatal(err)
	}
	if saved := readSavedMonitoringItems(t); len(saved) != 1 || saved[0].Address != 5 {
		t.Errorf("saved = %s", fmt.Sprint(saved))
	}
}
