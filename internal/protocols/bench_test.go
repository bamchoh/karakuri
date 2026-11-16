package protocols_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	uaclient "github.com/bamchoh/uastack/client"
	"github.com/bamchoh/uastack/ua"
	"github.com/simonvetter/modbus"

	"karakuri/internal/application"
)

// 主な経路のベンチマーク。アプリと同じ組み立て（実際のプロトコル・変数との同期）で、遅くなったことに気づくための目安にする。
// ふだんの go test では流れない。流すときは:
//
//	go test -run '^$' -bench . -benchmem ./internal/protocols
//
// 変数は 100 個（INT）を、Modbus TCP の保持レジスタ 0〜99 と、FINS の DM 0〜99 の両方にマッピングしておく
// （レジスタへの書き込み → 変数 → もう 1 つのサーバーのレジスタ、まで反映する）

const benchVars = 100

// benchService は、変数 100 個を Modbus TCP と FINS にマッピングし、Modbus TCP と OPC UA を起動した PLCService を作る
func benchService(b *testing.B) (svc *application.PLCService, modbusPort, opcuaPort int, ids []string) {
	b.Helper()
	svc = newService(b)
	modbusPort = addModbus(b, svc)
	must(b, svc.AddServer(finsType, ""))
	must(b, svc.AddServer(opcUA, ""))
	opcuaPort = freePort(b)
	cfg := svc.GetServerConfig(opcUA)
	cfg.Settings["host"] = "127.0.0.1"
	cfg.Settings["port"] = opcuaPort
	must(b, svc.UpdateServerConfig(cfg))

	for i := range benchVars {
		v, err := svc.CreateVariable(fmt.Sprintf("V%03d", i), "INT", 0)
		must(b, err)
		must(b, svc.UpdateVariableMappings(v.ID, []application.ProtocolMappingDTO{
			{ProtocolType: modbusTCP, MemoryArea: hr, Address: i, Endianness: "big"},
			{ProtocolType: finsType, MemoryArea: "DM", Address: i, Endianness: "big"},
		}))
		must(b, svc.UpdateVariableNodePublishing(v.ID, opcUA, &application.NodePublishingDTO{ProtocolType: opcUA, Enabled: true, AccessMode: "readwrite"}))
		ids = append(ids, v.ID)
	}
	must(b, svc.StartServer(modbusTCP))
	must(b, svc.StartServer(opcUA))
	return svc, modbusPort, opcuaPort, ids
}

// Modbus TCP のクライアントから、変数をマッピングした保持レジスタを 125 個（1 回の上限）読む
func BenchmarkModbusTCP_ReadHoldingRegisters125(b *testing.B) {
	_, port, _, _ := benchService(b)
	c := modbusClient(b, port)
	b.ResetTimer()
	for range b.N {
		if _, err := c.ReadRegisters(0, 125, modbus.HOLDING_REGISTER); err != nil {
			b.Fatal(err)
		}
	}
}

// Modbus TCP のクライアントから、変数をマッピングした保持レジスタ 100 個に書く（変数 100 個と、FINS の DM に反映する）
func BenchmarkModbusTCP_WriteHoldingRegisters100(b *testing.B) {
	svc, port, _, ids := benchService(b)
	c := modbusClient(b, port)
	values := make([]uint16, benchVars)
	b.ResetTimer()
	for i := range b.N {
		for j := range values {
			values[j] = uint16(i + j)
		}
		if err := c.WriteRegisters(0, values); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if got := variableValue(b, svc, ids[benchVars-1]); got != fmt.Sprint(int16(values[benchVars-1])) {
		b.Fatalf("最後の変数 = %s, want %d（書き込みが変数に届いていない）", got, int16(values[benchVars-1]))
	}
}

// 画面・スクリプトから変数を 1 つ書く（2 つのサーバーのレジスタと、OPC UA のノードに反映する）
func BenchmarkVariable_UpdateMappedValue(b *testing.B) {
	svc, _, _, ids := benchService(b)
	b.ResetTimer()
	for i := range b.N {
		if err := svc.UpdateVariableValue(ids[i%benchVars], int16(i)); err != nil {
			b.Fatal(err)
		}
	}
}

// OPC UA のクライアントから、公開した変数を 1 つ読む
func BenchmarkOPCUA_ReadPublishedVariable(b *testing.B) {
	svc, _, port, ids := benchService(b)
	addrs := svc.GetNodeAddresses(opcUA, ids[:1])
	var ns uint16
	if _, err := fmt.Sscanf(addrs[0].Address, "ns=%d;", &ns); err != nil {
		b.Fatalf("NodeID %q から名前空間を読めない: %v", addrs[0].Address, err)
	}
	ctx := context.Background()
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, err := uaclient.Dial(dialCtx, fmt.Sprintf("opc.tcp://127.0.0.1:%d", port), uaclient.Config{})
	must(b, err)
	defer c.Close(ctx) //nolint:errcheck
	node := ua.NewStringNodeId(ns, "V000")
	b.ResetTimer()
	for range b.N {
		if _, err := c.ReadValue(ctx, node); err != nil {
			b.Fatal(err)
		}
	}
}
