package rtu

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// 実際の COM ポートを使う試験。2 つのポートをクロスでつなぎ、環境変数で指定したときだけ動く（CI では飛ばす）。
//
//	$env:KARAKURI_SERIAL_PAIR = "COM4,COM6"   # サーバー側, マスター側
//	go test -run HW -v ./internal/protocols/modbus/rtu/

func hwPair(t *testing.T) (server, master string) {
	t.Helper()
	v := os.Getenv("KARAKURI_SERIAL_PAIR")
	parts := strings.Split(v, ",")
	if len(parts) != 2 {
		t.Skip("KARAKURI_SERIAL_PAIR（例: COM4,COM6）が設定されていない")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

var hwBauds = []int{9600, 115200}

func hwConfig(port string, baud int) SerialConfig {
	return SerialConfig{Port: port, BaudRate: baud, DataBits: 8, StopBits: 1, Parity: "N"}
}

// hwMaster は、マスター側のポート。応答を want バイト読むまで（または timeout まで）待つ
type hwMaster struct {
	t    *testing.T
	port Port
}

func openHWMaster(t *testing.T, name string, baud int) *hwMaster {
	t.Helper()
	p, err := OpenSerialPort(hwConfig(name, baud))
	if err != nil {
		t.Fatalf("マスター側 %s を開けない: %v", name, err)
	}
	t.Cleanup(func() { p.Close() })
	p.SetReadTimeout(20 * time.Millisecond)
	return &hwMaster{t: t, port: p}
}

// drain は、受信済みのバイトを捨てる
func (m *hwMaster) drain() {
	buf := make([]byte, 512)
	for {
		n, _ := m.port.Read(buf)
		if n == 0 {
			return
		}
	}
}

func (m *hwMaster) send(b []byte) {
	m.t.Helper()
	if _, err := m.port.Write(b); err != nil {
		m.t.Fatalf("送信: %v", err)
	}
}

// recv は、want バイト（ASCII なら LF まで）届くのを待つ
func (m *hwMaster) recv(want int, untilLF bool, timeout time.Duration) []byte {
	got := []byte{}
	buf := make([]byte, 512)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n, err := m.port.Read(buf)
		if err != nil {
			m.t.Fatalf("受信: %v", err)
		}
		got = append(got, buf[:n]...)
		if untilLF && len(got) > 0 && got[len(got)-1] == '\n' {
			return got
		}
		if !untilLF && len(got) >= want {
			return got
		}
	}
	return got
}

// TestHW_Wiring は、2 つのポートがつながっていることを確かめる（両方向）
func TestHW_Wiring(t *testing.T) {
	a, b := hwPair(t)
	for _, dir := range [][2]string{{b, a}, {a, b}} {
		func() {
			tx := openHWMaster(t, dir[0], 9600)
			rx := openHWMaster(t, dir[1], 9600)
			defer tx.port.Close()
			defer rx.port.Close()
			rx.drain()
			msg := []byte("karakuri-wiring-check")
			tx.send(msg)
			got := rx.recv(len(msg), false, 2*time.Second)
			if !bytes.Equal(got, msg) {
				t.Fatalf("%s → %s: 届いたもの %q, want %q", dir[0], dir[1], got, msg)
			}
			t.Logf("%s → %s: OK", dir[0], dir[1])
		}()
	}
}

func readRegsRequest(addr, count uint16) []byte {
	b := []byte{0x01, 0x03, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(b[2:], addr)
	binary.BigEndian.PutUint16(b[4:], count)
	return b
}

func writeRegsRequest(addr uint16, vals []uint16) []byte {
	b := []byte{0x01, 0x10, 0, 0, 0, 0, byte(len(vals) * 2)}
	binary.BigEndian.PutUint16(b[2:], addr)
	binary.BigEndian.PutUint16(b[4:], uint16(len(vals)))
	for _, v := range vals {
		b = binary.BigEndian.AppendUint16(b, v)
	}
	return b
}

func withCRC(body []byte) []byte {
	crc := CRC16(body)
	return append(append([]byte(nil), body...), byte(crc), byte(crc>>8))
}

// TestHW_RTU は、実際のポートで RTU サーバーに読み書きし、応答までの時間を測る
func TestHW_RTU(t *testing.T) {
	srvPort, masterPort := hwPair(t)
	for _, baud := range hwBauds {
		t.Run(fmt.Sprintf("%dbps", baud), func(t *testing.T) {
			srv := NewRTUServer(hwConfig(srvPort, baud), newMemHandler())
			if err := srv.Start(); err != nil {
				t.Fatalf("サーバー %s を開けない: %v", srvPort, err)
			}
			defer srv.Stop()
			m := openHWMaster(t, masterPort, baud)
			m.drain()

			exchange := func(body []byte, want []byte) time.Duration {
				t.Helper()
				start := time.Now()
				m.send(withCRC(body))
				got := m.recv(len(want)+2, false, 2*time.Second)
				elapsed := time.Since(start)
				if len(got) < 2 || !CheckCRC(got) || !bytes.Equal(got[:len(got)-2], want) {
					t.Fatalf("% X → 応答 % X, want % X", body, got, want)
				}
				return elapsed
			}

			// 最大の書き込み（123 ワード。255 バイトのフレーム）。USB 変換器が途中で分けて渡しても 1 つのフレームになるか
			vals := make([]uint16, 123)
			for i := range vals {
				vals[i] = uint16(i*257 + 1)
			}
			exchange(writeRegsRequest(0, vals), []byte{0x01, 0x10, 0, 0, 0, 123})
			want := []byte{0x01, 0x03, 246}
			for _, v := range vals {
				want = binary.BigEndian.AppendUint16(want, v)
			}
			exchange(readRegsRequest(0, 123), want)

			// 小さいリクエストを続けて送り、応答までの時間を測る
			const n = 50
			var total, worst time.Duration
			for i := 0; i < n; i++ {
				d := exchange(readRegsRequest(uint16(i), 1), append([]byte{0x01, 0x03, 2}, byte(vals[i]>>8), byte(vals[i])))
				total += d
				worst = max(worst, d)
			}
			t.Logf("%dbps: FC03（1 ワード）%d 回 平均 %v / 最大 %v", baud, n, total/n, worst)

			// 2 つのリクエストを続けて送る（応答を待たない）。両方に応答するか
			m.send(append(withCRC(readRegsRequest(0, 1)), withCRC(readRegsRequest(1, 1))...))
			got := m.recv(14, false, 2*time.Second)
			if len(got) != 14 || !CheckCRC(got[:7]) || !CheckCRC(got[7:]) {
				t.Fatalf("続けて送った 2 つのリクエストへの応答: % X", got)
			}
		})
	}
}

// TestHW_ASCII は、実際のポートで ASCII サーバーに読み書きする
func TestHW_ASCII(t *testing.T) {
	srvPort, masterPort := hwPair(t)
	for _, baud := range hwBauds {
		t.Run(fmt.Sprintf("%dbps", baud), func(t *testing.T) {
			srv := NewASCIIServer(hwConfig(srvPort, baud), newMemHandler())
			if err := srv.Start(); err != nil {
				t.Fatalf("サーバー %s を開けない: %v", srvPort, err)
			}
			defer srv.Stop()
			m := openHWMaster(t, masterPort, baud)
			m.drain()

			exchange := func(body []byte, want []byte) time.Duration {
				t.Helper()
				start := time.Now()
				m.send(BuildASCIIFrame(body))
				got := m.recv(0, true, 3*time.Second)
				elapsed := time.Since(start)
				resp, err := ParseASCIIFrame(got)
				if err != nil || !bytes.Equal(resp, want) {
					t.Fatalf("% X → 応答 %q（%v）, want % X", body, got, err, want)
				}
				return elapsed
			}

			vals := make([]uint16, 123)
			for i := range vals {
				vals[i] = uint16(i*257 + 1)
			}
			exchange(writeRegsRequest(0, vals), []byte{0x01, 0x10, 0, 0, 0, 123})

			const n = 30
			var total, worst time.Duration
			for i := 0; i < n; i++ {
				d := exchange(readRegsRequest(uint16(i), 1), append([]byte{0x01, 0x03, 2}, byte(vals[i]>>8), byte(vals[i])))
				total += d
				worst = max(worst, d)
			}
			t.Logf("%dbps: FC03（1 ワード）%d 回 平均 %v / 最大 %v", baud, n, total/n, worst)
		})
	}
}
