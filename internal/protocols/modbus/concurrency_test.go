package modbus

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/simonvetter/modbus"
)

// 複数のクライアント（他社製の simonvetter/modbus）から同時に読み書きしても、
// 応答が取り違えられず、どの書き込みも反映される。
// あわせて、読み書きの最中に UnitID の応答設定を切り替える（サーバーが読む設定を同時に触る）。競合は CI の -race で見つける
func TestTCP_ConcurrentClients(t *testing.T) {
	const clients, rounds = 8, 30
	srv, port := startTCPServer(t)

	stop := make(chan struct{})
	var toggler sync.WaitGroup
	toggler.Add(1)
	go func() { // どのクライアントも使わない UnitID 9 の設定を、切り替え続ける
		defer toggler.Done()
		for on := false; ; on = !on {
			select {
			case <-stop:
				return
			default:
				srv.handler.SetUnitIdEnabled(9, on)
			}
		}
	}()

	var wg sync.WaitGroup
	errs := make(chan error, clients)
	for i := range clients {
		client, err := modbus.NewClient(&modbus.ClientConfiguration{URL: fmt.Sprintf("tcp://127.0.0.1:%d", port), Timeout: 2 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Open(); err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { _ = client.Close() })
		client.SetUnitId(1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			base := uint16(i * 10) // 受け持ちの保持レジスタ（サーバーの大きさは 100）
			for k := range rounds {
				v := uint16(i<<8 | k)
				if err := client.WriteRegisters(base, []uint16{v, v + 1, v + 2}); err != nil {
					errs <- fmt.Errorf("クライアント %d の %d 回目の書き込み: %v", i, k, err)
					return
				}
				got, err := client.ReadRegisters(base, 3, modbus.HOLDING_REGISTER)
				if err != nil {
					errs <- fmt.Errorf("クライアント %d の %d 回目の読み出し: %v", i, k, err)
					return
				}
				if got[0] != v || got[1] != v+1 || got[2] != v+2 {
					errs <- fmt.Errorf("クライアント %d の %d 回目: %v, want [%d %d %d]（ほかのクライアントの応答と取り違えた？）", i, k, got, v, v+1, v+2)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	toggler.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
