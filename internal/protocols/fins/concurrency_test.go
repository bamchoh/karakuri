package fins

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
)

// 複数のクライアントから同時に読み書きしても、応答が取り違えられず、どの書き込みも反映される。
// 競合は CI の -race で見つける
func TestConcurrentClients(t *testing.T) {
	const clients, rounds = 8, 30
	for _, transport := range []string{transportUDP, transportTCP} {
		t.Run(transport, func(t *testing.T) {
			_, port := startServer(t, transportBoth)
			f := &ClientFactory{}
			var wg sync.WaitGroup
			errs := make(chan error, clients)
			for i := range clients {
				c := newClient(t, port, transport)
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs <- hammer(f, c, 1000+i*10, i, rounds)
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Error(err)
				}
			}
		})
	}
}

// hammer は、自分の受け持ちのアドレス（D[base]〜D[base+2]）に書いては読み、書いた値が返るかを確かめる
func hammer(f *ClientFactory, c *Client, base, id, rounds int) error {
	for k := range rounds {
		v := id<<8 | k
		req, err := f.BuildRequest(commandWrite, map[string]interface{}{"area": AreaDM, "unit": unitWord, "address": base, "values": fmt.Sprintf("%d, %d, %d", v, v+1, v+2)})
		if err != nil {
			return err
		}
		if r := c.Send(context.Background(), req); r.Status != protocol.CommandOK {
			return fmt.Errorf("クライアント %d の %d 回目の書き込み: %s %s", id, k, r.Status, r.Error)
		}
		req, err = f.BuildRequest(commandRead, map[string]interface{}{"area": AreaDM, "unit": unitWord, "address": base, "count": 3})
		if err != nil {
			return err
		}
		r := c.Send(context.Background(), req)
		if r.Status != protocol.CommandOK || len(r.Values) != 3 {
			return fmt.Errorf("クライアント %d の %d 回目の読み出し: %s %s（%d 個）", id, k, r.Status, r.Error, len(r.Values))
		}
		for j, val := range r.Values {
			if val.Word == nil || int(*val.Word) != v+j {
				return fmt.Errorf("クライアント %d の %d 回目: D%d = %v, want %d（ほかのクライアントの応答と取り違えた？）", id, k, base+j, val.Value, v+j)
			}
		}
	}
	return nil
}

// 読み書きの最中に止めても、Stop がすぐ戻り、クライアントは失敗を返す（固まらない）。止めたあと、また起動できる
func TestStopDuringTraffic(t *testing.T) {
	for _, transport := range []string{transportUDP, transportTCP} {
		t.Run(transport, func(t *testing.T) {
			srv, port := startServer(t, transportBoth)
			f := &ClientFactory{}
			ctx, cancel := context.WithCancel(context.Background())
			var wg sync.WaitGroup
			for i := range 4 {
				c := newClient(t, port, transport)
				wg.Add(1)
				go func() {
					defer wg.Done()
					req, _ := f.BuildRequest(commandRead, map[string]interface{}{"area": AreaDM, "unit": unitWord, "address": i, "count": 10})
					for ctx.Err() == nil {
						c.Send(ctx, req)
					}
				}()
			}
			time.Sleep(100 * time.Millisecond)

			stopped := make(chan error, 1)
			go func() { stopped <- srv.Stop() }()
			select {
			case err := <-stopped:
				if err != nil {
					t.Fatalf("Stop: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("読み書きの最中の Stop が 3 秒で戻らない")
			}
			cancel()
			wg.Wait()

			if err := srv.Start(context.Background()); err != nil {
				t.Fatalf("止めたあとの Start: %v", err)
			}
			c := newClient(t, port, transport)
			if err := hammer(f, c, 2000, 1, 3); err != nil {
				t.Fatalf("起動し直したあと: %v", err)
			}
		})
	}
}
