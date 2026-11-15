package rtu

import (
	"os"
	"testing"
	"time"
)

// TestHW_Unplug は、RTU サーバーの動いている間に、サーバー側の USB 変換器を抜いて挿し直す試験。
// 手で抜き差しするので、KARAKURI_SERIAL_UNPLUG=1 のときだけ動く。始まったら変換器を抜き、しばらくして挿し直す。
// 1 秒ごとに応答の有無を出し、抜いたあとに応答が戻らなければ失敗にする。
//
//	$env:KARAKURI_SERIAL_PAIR = "COM4,COM6"; $env:KARAKURI_SERIAL_UNPLUG = "1"
//	go test -run HW_Unplug -v -timeout 10m ./internal/protocols/modbus/rtu/
//
// CPU の使い方は、OS の側（タスクマネージャー、PowerShell の Get-Process rtu.test の TotalProcessorTime）で見る
// （Go の runtime/metrics の CPU 時間は、システムコールの中の時間を数えないので、読み直し続けていても 0% に見える）。
func TestHW_Unplug(t *testing.T) {
	srvPort, masterPort := hwPair(t)
	if os.Getenv("KARAKURI_SERIAL_UNPLUG") == "" {
		t.Skip("KARAKURI_SERIAL_UNPLUG が設定されていない")
	}
	const baud = 9600
	srv := NewRTUServer(hwConfig(srvPort, baud), newMemHandler())
	if err := srv.Start(); err != nil {
		t.Fatalf("サーバー %s を開けない: %v", srvPort, err)
	}
	m := openHWMaster(t, masterPort, baud)
	m.drain()

	req := withCRC(readRegsRequest(0, 1))
	start := time.Now()
	lost, recovered := false, false
	for time.Since(start) < 150*time.Second && !recovered {
		tick := time.Now()
		m.send(req)
		got := m.recv(7, false, 500*time.Millisecond)
		ok := len(got) == 7 && CheckCRC(got)
		switch {
		case !ok:
			lost = true
		case lost:
			recovered = true
		}
		status := "応答あり"
		if !ok {
			status = "応答なし"
		}
		t.Logf("%3ds %s", int(time.Since(start).Seconds()), status)
		time.Sleep(time.Until(tick.Add(time.Second)))
	}
	if !lost {
		t.Error("150 秒の間、応答が途切れなかった（変換器を抜いていない？）")
	} else if !recovered {
		t.Error("抜いたあと、応答が戻らなかった")
	}

	stopped := make(chan error, 1)
	go func() { stopped <- srv.Stop() }()
	select {
	case err := <-stopped:
		t.Logf("Stop が戻った（%v）", err)
	case <-time.After(5 * time.Second):
		t.Errorf("Stop が 5 秒たっても戻らない")
	}
}
