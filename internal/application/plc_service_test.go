package application

import (
	"testing"
	"time"

	"karakuri/internal/domain/protocol/protocoltest"
)

// newTestService はテスト用のクリーンな PLCService を作成する。
// フェイクファクトリーをインプロセスで登録し、デフォルトで modbus-tcp を追加する。
func newTestService(t *testing.T) *PLCService {
	t.Helper()
	svc := NewPLCService()

	// Modbus 互換フェイクファクトリーを登録（プロトコル固有実装に依存しない）
	svc.RegisterFactory(protocoltest.NewFactory("modbus-tcp", "tcp", "Modbus TCP"))
	svc.RegisterFactory(protocoltest.NewFactory("modbus-rtu", "rtu", "Modbus RTU"))
	svc.RegisterFactory(protocoltest.NewFactory("modbus-ascii", "ascii", "Modbus ASCII"))

	// デフォルトで modbus-tcp を追加（旧 NewPLCService() の初期動作を再現）
	_ = svc.AddServer("modbus-tcp", "tcp")

	// ファイルから読み込まれた既存モニタリング項目をクリア（テスト間干渉を防止）
	svc.ClearMonitoringItems()
	t.Cleanup(func() {
		// 全サーバーを停止（リソースリーク防止）
		for _, inst := range svc.GetServerInstances() {
			_ = svc.StopServer(inst.ProtocolType)
		}
	})
	return svc
}

// ===== サーバーインスタンス管理テスト =====

func TestPLCService_GetServerInstances_InitialState(t *testing.T) {
	svc := newTestService(t)

	instances := svc.GetServerInstances()
	if len(instances) != 1 {
		t.Fatalf("expected 1 server instance, got %d", len(instances))
	}
	if instances[0].ProtocolType != "modbus-tcp" {
		t.Errorf("expected protocolType 'modbus', got '%s'", instances[0].ProtocolType)
	}
	if instances[0].Variant != "tcp" {
		t.Errorf("expected variant 'tcp', got '%s'", instances[0].Variant)
	}
	if instances[0].Status != "Stopped" {
		t.Errorf("expected status 'Stopped', got '%s'", instances[0].Status)
	}
	if instances[0].DisplayName == "" {
		t.Error("expected non-empty DisplayName")
	}
}

func TestPLCService_AddServer_Success(t *testing.T) {
	svc := newTestService(t)

	if err := svc.AddServer("modbus-rtu", "rtu"); err != nil {
		t.Fatalf("AddServer modbus-rtu failed: %v", err)
	}

	instances := svc.GetServerInstances()
	if len(instances) != 2 {
		t.Fatalf("expected 2 server instances, got %d", len(instances))
	}
}

func TestPLCService_AddServer_DuplicateError(t *testing.T) {
	svc := newTestService(t)

	// modbus はデフォルトで追加済み
	err := svc.AddServer("modbus-tcp", "tcp")
	if err == nil {
		t.Fatal("expected error for duplicate server, got nil")
	}
}

func TestPLCService_AddServer_UnknownProtocol(t *testing.T) {
	svc := newTestService(t)

	err := svc.AddServer("unknown_protocol", "variant1")
	if err == nil {
		t.Fatal("expected error for unknown protocol, got nil")
	}
}

func TestPLCService_RemoveServer_Success(t *testing.T) {
	svc := newTestService(t)

	if err := svc.RemoveServer("modbus-tcp"); err != nil {
		t.Fatalf("RemoveServer failed: %v", err)
	}

	instances := svc.GetServerInstances()
	if len(instances) != 0 {
		t.Fatalf("expected 0 server instances after removal, got %d", len(instances))
	}
}

func TestPLCService_RemoveServer_NotFound(t *testing.T) {
	svc := newTestService(t)

	err := svc.RemoveServer("unknown-protocol")
	if err == nil {
		t.Fatal("expected error for non-existent server, got nil")
	}
}

func TestPLCService_GetServerInstances_SortedAlphabetically(t *testing.T) {
	svc := newTestService(t)

	if err := svc.AddServer("modbus-rtu", "rtu"); err != nil {
		t.Fatalf("AddServer modbus-rtu failed: %v", err)
	}

	instances := svc.GetServerInstances()
	// プロトコルタイプ順（アルファベット昇順）にソートされていることを確認
	for i := 1; i < len(instances); i++ {
		if instances[i-1].ProtocolType > instances[i].ProtocolType {
			t.Errorf("instances not sorted: %s > %s",
				instances[i-1].ProtocolType, instances[i].ProtocolType)
		}
	}
	// modbus-rtu < modbus-tcp なので modbus-rtu が先
	if instances[0].ProtocolType != "modbus-rtu" {
		t.Errorf("expected 'modbus-rtu' first, got '%s'", instances[0].ProtocolType)
	}
}

func TestPLCService_GetServerStatus_Stopped(t *testing.T) {
	svc := newTestService(t)

	status := svc.GetServerStatus("modbus-tcp")
	if status != "Stopped" {
		t.Errorf("expected 'Stopped', got '%s'", status)
	}
}

func TestPLCService_GetServerStatus_NotFound_ReturnsStopped(t *testing.T) {
	svc := newTestService(t)

	// 存在しないプロトコルはエラーにならず "Stopped" を返す
	status := svc.GetServerStatus("unknown-protocol")
	if status != "Stopped" {
		t.Errorf("expected 'Stopped' for non-existent server, got '%s'", status)
	}
}

// ===== プロトコルスキーマテスト =====

func TestPLCService_GetAvailableProtocols(t *testing.T) {
	svc := newTestService(t)

	protocols := svc.GetAvailableProtocols()
	if len(protocols) == 0 {
		t.Fatal("expected at least one available protocol")
	}

	typeMap := make(map[string]bool)
	for _, p := range protocols {
		typeMap[p.Type] = true
		if p.DisplayName == "" {
			t.Errorf("protocol %s has empty DisplayName", p.Type)
		}
	}
	if !typeMap["modbus-tcp"] {
		t.Error("expected 'modbus-tcp' in available protocols")
	}
	if !typeMap["modbus-rtu"] {
		t.Error("expected 'modbus-rtu' in available protocols")
	}
}

func TestPLCService_GetProtocolSchema_Modbus(t *testing.T) {
	svc := newTestService(t)

	schema, err := svc.GetProtocolSchema("modbus-tcp")
	if err != nil {
		t.Fatalf("GetProtocolSchema failed: %v", err)
	}
	if schema.ProtocolType != "modbus-tcp" {
		t.Errorf("expected protocolType 'modbus', got '%s'", schema.ProtocolType)
	}
	if len(schema.Variants) == 0 {
		t.Error("expected at least one variant")
	}
	// Modbus は UnitID をサポートする
	if !schema.Capabilities.SupportsUnitID {
		t.Error("expected modbus to support UnitID")
	}
	if schema.Capabilities.UnitIDMin <= 0 {
		t.Errorf("expected positive UnitIDMin, got %d", schema.Capabilities.UnitIDMin)
	}
}

func TestPLCService_GetProtocolSchema_Unknown(t *testing.T) {
	svc := newTestService(t)

	_, err := svc.GetProtocolSchema("unknown_protocol")
	if err == nil {
		t.Fatal("expected error for unknown protocol")
	}
}

// ===== メモリ操作テスト =====

func TestPLCService_GetMemoryAreas_Modbus(t *testing.T) {
	svc := newTestService(t)

	areas := svc.GetMemoryAreas("modbus-tcp")
	// Modbus は coils / discreteInputs / holdingRegisters / inputRegisters の4エリア
	if len(areas) < 4 {
		t.Errorf("expected at least 4 areas for modbus, got %d", len(areas))
	}

	areaIDs := make(map[string]bool)
	for _, a := range areas {
		areaIDs[a.ID] = true
	}
	for _, expected := range []string{"coils", "holdingRegisters"} {
		if !areaIDs[expected] {
			t.Errorf("expected area '%s' in modbus areas", expected)
		}
	}
}

func TestPLCService_GetMemoryAreas_NotFound(t *testing.T) {
	svc := newTestService(t)

	areas := svc.GetMemoryAreas("unknown-protocol") // 未追加のプロトコル
	if areas != nil {
		t.Errorf("expected nil for non-existent server, got %v", areas)
	}
}

func TestPLCService_ReadWriteWord_Modbus(t *testing.T) {
	svc := newTestService(t)

	const (
		area  = "holdingRegisters"
		addr  = 10
		value = 12345
	)

	if err := svc.WriteWord("modbus-tcp", area, addr, value); err != nil {
		t.Fatalf("WriteWord failed: %v", err)
	}

	words, err := svc.ReadWords("modbus-tcp", area, addr, 1)
	if err != nil {
		t.Fatalf("ReadWords failed: %v", err)
	}
	if len(words) != 1 || words[0] != value {
		t.Errorf("expected %d, got %v", value, words)
	}
}

func TestPLCService_ReadWords_Multiple(t *testing.T) {
	svc := newTestService(t)

	const area = "holdingRegisters"
	values := []int{100, 200, 300}
	for i, v := range values {
		if err := svc.WriteWord("modbus-tcp", area, i, v); err != nil {
			t.Fatalf("WriteWord[%d] failed: %v", i, err)
		}
	}

	words, err := svc.ReadWords("modbus-tcp", area, 0, len(values))
	if err != nil {
		t.Fatalf("ReadWords failed: %v", err)
	}
	if len(words) != len(values) {
		t.Fatalf("expected %d words, got %d", len(values), len(words))
	}
	for i, expected := range values {
		if words[i] != expected {
			t.Errorf("words[%d]: expected %d, got %d", i, expected, words[i])
		}
	}
}

func TestPLCService_ReadWriteWord_NotFound(t *testing.T) {
	svc := newTestService(t)

	err := svc.WriteWord("unknown-protocol", "holdingRegisters", 0, 100)
	if err == nil {
		t.Fatal("expected error for non-existent server (WriteWord)")
	}

	_, err = svc.ReadWords("unknown-protocol", "holdingRegisters", 0, 1)
	if err == nil {
		t.Fatal("expected error for non-existent server (ReadWords)")
	}
}

func TestPLCService_ReadWriteBit_Modbus(t *testing.T) {
	svc := newTestService(t)

	const (
		area = "coils"
		addr = 5
	)

	if err := svc.WriteBit("modbus-tcp", area, addr, true); err != nil {
		t.Fatalf("WriteBit failed: %v", err)
	}

	bits, err := svc.ReadBits("modbus-tcp", area, addr, 1)
	if err != nil {
		t.Fatalf("ReadBits failed: %v", err)
	}
	if len(bits) != 1 || bits[0] != true {
		t.Errorf("expected [true], got %v", bits)
	}
}

func TestPLCService_ReadWriteBit_NotFound(t *testing.T) {
	svc := newTestService(t)

	err := svc.WriteBit("unknown-protocol", "coils", 0, true)
	if err == nil {
		t.Fatal("expected error for non-existent server (WriteBit)")
	}

	_, err = svc.ReadBits("unknown-protocol", "coils", 0, 1)
	if err == nil {
		t.Fatal("expected error for non-existent server (ReadBits)")
	}
}

func TestPLCService_MultiServer_IndependentMemory(t *testing.T) {
	svc := newTestService(t)

	if err := svc.AddServer("modbus-rtu", "rtu"); err != nil {
		t.Fatalf("AddServer modbus-rtu failed: %v", err)
	}

	// Modbus TCP に書き込み
	if err := svc.WriteWord("modbus-tcp", "holdingRegisters", 0, 1111); err != nil {
		t.Fatalf("WriteWord modbus-tcp failed: %v", err)
	}

	// Modbus RTU に書き込み
	if err := svc.WriteWord("modbus-rtu", "holdingRegisters", 0, 2222); err != nil {
		t.Fatalf("WriteWord modbus-rtu failed: %v", err)
	}

	// Modbus TCP の値が Modbus RTU に影響していないことを確認
	modbusWords, err := svc.ReadWords("modbus-tcp", "holdingRegisters", 0, 1)
	if err != nil {
		t.Fatalf("ReadWords modbus-tcp failed: %v", err)
	}
	if modbusWords[0] != 1111 {
		t.Errorf("modbus-tcp expected 1111, got %d", modbusWords[0])
	}

	// Modbus RTU の値が Modbus TCP に影響していないことを確認
	rtuWords, err := svc.ReadWords("modbus-rtu", "holdingRegisters", 0, 1)
	if err != nil {
		t.Fatalf("ReadWords modbus-rtu failed: %v", err)
	}
	if rtuWords[0] != 2222 {
		t.Errorf("modbus-rtu expected 2222, got %d", rtuWords[0])
	}
}

// ===== サーバー設定テスト =====

func TestPLCService_GetServerConfig(t *testing.T) {
	svc := newTestService(t)

	cfg := svc.GetServerConfig("modbus-tcp")
	if cfg == nil {
		t.Fatal("expected non-nil server config")
	}
	if cfg.ProtocolType != "modbus-tcp" {
		t.Errorf("expected protocolType 'modbus', got '%s'", cfg.ProtocolType)
	}
	if cfg.Variant != "tcp" {
		t.Errorf("expected variant 'tcp', got '%s'", cfg.Variant)
	}
	if cfg.Settings == nil {
		t.Error("expected non-nil settings map")
	}
}

func TestPLCService_GetServerConfig_NotFound(t *testing.T) {
	svc := newTestService(t)

	cfg := svc.GetServerConfig("unknown-protocol")
	if cfg != nil {
		t.Errorf("expected nil for non-existent server, got %+v", cfg)
	}
}

// ===== モニタリング管理テスト =====

func TestPLCService_AddMonitoringItem_GeneratesIDAndOrder(t *testing.T) {
	svc := newTestService(t)

	item := &MonitoringItemDTO{
		ProtocolType:  "modbus-tcp",
		MemoryArea:    "holdingRegisters",
		Address:       0,
		BitWidth:      16,
		Endianness:    "big",
		DisplayFormat: "decimal",
	}

	added, err := svc.AddMonitoringItem(item)
	if err != nil {
		t.Fatalf("AddMonitoringItem failed: %v", err)
	}
	if added.ID == "" {
		t.Error("expected non-empty ID after adding")
	}
	if added.Order != 1 {
		t.Errorf("expected Order 1 for first item, got %d", added.Order)
	}
	if added.ProtocolType != "modbus-tcp" {
		t.Errorf("expected protocolType 'modbus', got '%s'", added.ProtocolType)
	}
}

func TestPLCService_AddMonitoringItem_OrderIncrementsSequentially(t *testing.T) {
	svc := newTestService(t)

	for i := 0; i < 3; i++ {
		item := &MonitoringItemDTO{
			ProtocolType: "modbus-tcp",
			MemoryArea:   "holdingRegisters",
			Address:      i,
			BitWidth:     16,
		}
		if _, err := svc.AddMonitoringItem(item); err != nil {
			t.Fatalf("AddMonitoringItem %d failed: %v", i, err)
		}
	}

	items := svc.GetMonitoringItems()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// 各項目の Order が昇順になっていることを確認
	for i := 1; i < len(items); i++ {
		if items[i].Order <= items[i-1].Order {
			t.Errorf("items[%d].Order=%d is not greater than items[%d].Order=%d",
				i, items[i].Order, i-1, items[i-1].Order)
		}
	}
}

func TestPLCService_GetMonitoringItems_SortedByOrder(t *testing.T) {
	svc := newTestService(t)

	// 3項目追加
	var firstID string
	for i := 0; i < 3; i++ {
		item := &MonitoringItemDTO{
			ProtocolType: "modbus-tcp",
			MemoryArea:   "holdingRegisters",
			Address:      i,
			BitWidth:     16,
		}
		added, _ := svc.AddMonitoringItem(item)
		if i == 0 {
			firstID = added.ID
		}
	}

	// 最初の項目を末尾に移動
	_ = svc.ReorderMonitoringItem(firstID, 2)

	items := svc.GetMonitoringItems()
	// Order順に並んでいることを確認
	for i := 1; i < len(items); i++ {
		if items[i].Order < items[i-1].Order {
			t.Errorf("items not sorted at index %d: Order[%d]=%d > Order[%d]=%d",
				i, i-1, items[i-1].Order, i, items[i].Order)
		}
	}
}

func TestPLCService_DeleteMonitoringItem(t *testing.T) {
	svc := newTestService(t)

	added, _ := svc.AddMonitoringItem(&MonitoringItemDTO{
		ProtocolType: "modbus-tcp",
		MemoryArea:   "holdingRegisters",
		Address:      0,
		BitWidth:     16,
	})

	if err := svc.DeleteMonitoringItem(added.ID); err != nil {
		t.Fatalf("DeleteMonitoringItem failed: %v", err)
	}

	if len(svc.GetMonitoringItems()) != 0 {
		t.Error("expected 0 items after deletion")
	}
}

func TestPLCService_DeleteMonitoringItem_NotFound(t *testing.T) {
	svc := newTestService(t)

	if err := svc.DeleteMonitoringItem("nonexistent-id"); err == nil {
		t.Fatal("expected error for non-existent item")
	}
}

func TestPLCService_UpdateMonitoringItem(t *testing.T) {
	svc := newTestService(t)

	added, _ := svc.AddMonitoringItem(&MonitoringItemDTO{
		ProtocolType: "modbus-tcp",
		MemoryArea:   "holdingRegisters",
		Address:      0,
		BitWidth:     16,
	})

	added.Address = 99
	added.BitWidth = 32
	if err := svc.UpdateMonitoringItem(added); err != nil {
		t.Fatalf("UpdateMonitoringItem failed: %v", err)
	}

	items := svc.GetMonitoringItems()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Address != 99 {
		t.Errorf("expected Address 99, got %d", items[0].Address)
	}
	if items[0].BitWidth != 32 {
		t.Errorf("expected BitWidth 32, got %d", items[0].BitWidth)
	}
}

func TestPLCService_UpdateMonitoringItem_NotFound(t *testing.T) {
	svc := newTestService(t)

	err := svc.UpdateMonitoringItem(&MonitoringItemDTO{ID: "nonexistent-id"})
	if err == nil {
		t.Fatal("expected error for non-existent item")
	}
}

func TestPLCService_ReorderMonitoringItem(t *testing.T) {
	svc := newTestService(t)

	var ids []string
	for i := 0; i < 3; i++ {
		item := &MonitoringItemDTO{
			ProtocolType: "modbus-tcp",
			MemoryArea:   "holdingRegisters",
			Address:      i,
			BitWidth:     16,
		}
		added, _ := svc.AddMonitoringItem(item)
		ids = append(ids, added.ID)
	}

	// 最初の項目を末尾に移動
	if err := svc.ReorderMonitoringItem(ids[0], 2); err != nil {
		t.Fatalf("ReorderMonitoringItem failed: %v", err)
	}

	items := svc.GetMonitoringItems()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// ids[0] が末尾に来ているはず
	if items[2].ID != ids[0] {
		t.Errorf("expected items[2].ID = '%s', got '%s'", ids[0], items[2].ID)
	}
	// ids[1] が先頭になっているはず
	if items[0].ID != ids[1] {
		t.Errorf("expected items[0].ID = '%s', got '%s'", ids[1], items[0].ID)
	}
}

func TestPLCService_ReorderMonitoringItem_NotFound(t *testing.T) {
	svc := newTestService(t)

	if err := svc.ReorderMonitoringItem("nonexistent-id", 0); err == nil {
		t.Fatal("expected error for non-existent item")
	}
}

func TestPLCService_ClearMonitoringItems(t *testing.T) {
	svc := newTestService(t)

	for i := 0; i < 3; i++ {
		svc.AddMonitoringItem(&MonitoringItemDTO{
			ProtocolType: "modbus-tcp",
			MemoryArea:   "holdingRegisters",
			Address:      i,
			BitWidth:     16,
		})
	}

	svc.ClearMonitoringItems()

	if len(svc.GetMonitoringItems()) != 0 {
		t.Error("expected 0 items after ClearMonitoringItems")
	}
}

func TestPLCService_MonitoringItem_ProtocolType_MultiServer(t *testing.T) {
	svc := newTestService(t)

	if err := svc.AddServer("modbus-rtu", "rtu"); err != nil {
		t.Fatalf("AddServer modbus-rtu failed: %v", err)
	}

	svc.AddMonitoringItem(&MonitoringItemDTO{
		ProtocolType: "modbus-tcp",
		MemoryArea:   "holdingRegisters",
		Address:      10,
		BitWidth:     16,
	})
	svc.AddMonitoringItem(&MonitoringItemDTO{
		ProtocolType: "modbus-rtu",
		MemoryArea:   "holdingRegisters",
		Address:      20,
		BitWidth:     16,
	})

	items := svc.GetMonitoringItems()
	protocolTypes := make(map[string]bool)
	for _, item := range items {
		protocolTypes[item.ProtocolType] = true
	}

	if !protocolTypes["modbus-tcp"] {
		t.Error("expected modbus-tcp monitoring item")
	}
	if !protocolTypes["modbus-rtu"] {
		t.Error("expected modbus-rtu monitoring item")
	}
}

// ===== スクリプト管理テスト =====

func TestPLCService_CreateScript(t *testing.T) {
	svc := newTestService(t)

	dto, err := svc.CreateScript("test_script", "1 + 2", 1000)
	if err != nil {
		t.Fatalf("CreateScript failed: %v", err)
	}
	if dto.ID == "" {
		t.Error("expected non-empty ID")
	}
	if dto.Name != "test_script" {
		t.Errorf("expected name 'test_script', got '%s'", dto.Name)
	}
	if dto.Code != "1 + 2" {
		t.Errorf("expected code '1 + 2', got '%s'", dto.Code)
	}
	if dto.IntervalMs != 1000 {
		t.Errorf("expected intervalMs 1000, got %d", dto.IntervalMs)
	}
	if dto.IsRunning {
		t.Error("expected script not running initially")
	}
}

func TestPLCService_GetScript(t *testing.T) {
	svc := newTestService(t)

	created, _ := svc.CreateScript("my_script", "2 + 3", 500)

	got, err := svc.GetScript(created.ID)
	if err != nil {
		t.Fatalf("GetScript failed: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, got.ID)
	}
	if got.Name != "my_script" {
		t.Errorf("expected name 'my_script', got '%s'", got.Name)
	}
}

func TestPLCService_GetScript_NotFound(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.GetScript("nonexistent-id"); err == nil {
		t.Fatal("expected error for non-existent script")
	}
}

func TestPLCService_GetScripts(t *testing.T) {
	svc := newTestService(t)

	// 初期状態は空
	if scripts := svc.GetScripts(); len(scripts) != 0 {
		t.Errorf("expected 0 scripts initially, got %d", len(scripts))
	}

	svc.CreateScript("s1", "1+1", 100)
	svc.CreateScript("s2", "2+2", 200)

	if scripts := svc.GetScripts(); len(scripts) != 2 {
		t.Errorf("expected 2 scripts, got %d", len(scripts))
	}
}

func TestPLCService_UpdateScript(t *testing.T) {
	svc := newTestService(t)

	created, _ := svc.CreateScript("original", "1+1", 100)

	if err := svc.UpdateScript(created.ID, "updated", "2+2", 200); err != nil {
		t.Fatalf("UpdateScript failed: %v", err)
	}

	got, _ := svc.GetScript(created.ID)
	if got.Name != "updated" {
		t.Errorf("expected name 'updated', got '%s'", got.Name)
	}
	if got.Code != "2+2" {
		t.Errorf("expected code '2+2', got '%s'", got.Code)
	}
	if got.IntervalMs != 200 {
		t.Errorf("expected intervalMs 200, got %d", got.IntervalMs)
	}
}

func TestPLCService_UpdateScript_NotFound(t *testing.T) {
	svc := newTestService(t)

	if err := svc.UpdateScript("nonexistent-id", "name", "code", 100); err == nil {
		t.Fatal("expected error for non-existent script")
	}
}

func TestPLCService_DeleteScript(t *testing.T) {
	svc := newTestService(t)

	created, _ := svc.CreateScript("to_delete", "1+1", 100)

	if err := svc.DeleteScript(created.ID); err != nil {
		t.Fatalf("DeleteScript failed: %v", err)
	}
	if scripts := svc.GetScripts(); len(scripts) != 0 {
		t.Errorf("expected 0 scripts after deletion, got %d", len(scripts))
	}
}

func TestPLCService_DeleteScript_NotFound(t *testing.T) {
	svc := newTestService(t)

	if err := svc.DeleteScript("nonexistent-id"); err == nil {
		t.Fatal("expected error for non-existent script")
	}
}

func TestPLCService_StartStopScript(t *testing.T) {
	svc := newTestService(t)

	created, _ := svc.CreateScript("runner", `1+1`, 50)

	if err := svc.StartScript(created.ID); err != nil {
		t.Fatalf("StartScript failed: %v", err)
	}

	// IsRunning を確認
	got, _ := svc.GetScript(created.ID)
	if !got.IsRunning {
		t.Error("expected script to be running after StartScript")
	}

	if err := svc.StopScript(created.ID); err != nil {
		t.Fatalf("StopScript failed: %v", err)
	}

	// 停止後は false になるはず
	time.Sleep(20 * time.Millisecond)
	got, _ = svc.GetScript(created.ID)
	if got.IsRunning {
		t.Error("expected script to be stopped after StopScript")
	}
}

func TestPLCService_StartScript_NotFound(t *testing.T) {
	svc := newTestService(t)

	if err := svc.StartScript("nonexistent-id"); err == nil {
		t.Fatal("expected error for non-existent script")
	}
}

// ===== Export/Import テスト =====

func TestPLCService_ExportProject_ContainsServers(t *testing.T) {
	svc := newTestService(t)

	exported := svc.ExportProject()

	if len(exported.Servers) == 0 {
		t.Error("expected at least one server in exported data")
	}

	// modbus サーバーが含まれていることを確認
	found := false
	for _, s := range exported.Servers {
		if s.ProtocolType == "modbus-tcp" {
			found = true
			if s.Variant == "" {
				t.Error("expected non-empty variant for modbus server snapshot")
			}
		}
	}
	if !found {
		t.Error("expected modbus server in exported servers")
	}
}


func TestPLCService_ExportProject_MultipleServers(t *testing.T) {
	svc := newTestService(t)

	if err := svc.AddServer("modbus-rtu", "rtu"); err != nil {
		t.Fatalf("AddServer modbus-rtu failed: %v", err)
	}

	exported := svc.ExportProject()
	if len(exported.Servers) != 2 {
		t.Errorf("expected 2 servers in export, got %d", len(exported.Servers))
	}
}

func TestPLCService_ExportProject_ContainsMonitoringItems(t *testing.T) {
	svc := newTestService(t)

	svc.AddMonitoringItem(&MonitoringItemDTO{
		ProtocolType: "modbus-tcp",
		MemoryArea:   "holdingRegisters",
		Address:      0,
		BitWidth:     16,
	})

	exported := svc.ExportProject()

	if len(exported.MonitoringItems) != 1 {
		t.Errorf("expected 1 monitoring item in export, got %d", len(exported.MonitoringItems))
	}
	if exported.MonitoringItems[0].ProtocolType != "modbus-tcp" {
		t.Errorf("expected protocolType 'modbus', got '%s'", exported.MonitoringItems[0].ProtocolType)
	}
}

func TestPLCService_ImportProject_RestoresServers(t *testing.T) {
	svc := newTestService(t)

	data := &ProjectDataDTO{
		Servers: []ServerSnapshotDTO{
			{ProtocolType: "modbus-tcp", Variant: "tcp"},
			{ProtocolType: "modbus-rtu", Variant: "rtu"},
		},
		Scripts: []*ScriptDTO{},
	}

	if err := svc.ImportProject(data); err != nil {
		t.Fatalf("ImportProject failed: %v", err)
	}

	instances := svc.GetServerInstances()
	if len(instances) != 2 {
		t.Fatalf("expected 2 server instances after import, got %d", len(instances))
	}

	typeMap := make(map[string]bool)
	for _, inst := range instances {
		typeMap[inst.ProtocolType] = true
	}
	if !typeMap["modbus-tcp"] {
		t.Error("expected modbus-tcp server after import")
	}
	if !typeMap["modbus-rtu"] {
		t.Error("expected modbus-rtu server after import")
	}
}


func TestPLCService_ImportProject_RestoresScripts(t *testing.T) {
	svc := newTestService(t)

	data := &ProjectDataDTO{
		Servers: []ServerSnapshotDTO{
			{ProtocolType: "modbus-tcp", Variant: "tcp"},
		},
		Scripts: []*ScriptDTO{
			{ID: "script-1", Name: "counter", Code: "1+1", IntervalMs: 1000},
			{ID: "script-2", Name: "timer", Code: "2+2", IntervalMs: 500},
		},
	}

	if err := svc.ImportProject(data); err != nil {
		t.Fatalf("ImportProject failed: %v", err)
	}

	scripts := svc.GetScripts()
	if len(scripts) != 2 {
		t.Fatalf("expected 2 scripts after import, got %d", len(scripts))
	}
}

func TestPLCService_ImportProject_RestoresMonitoringItems(t *testing.T) {
	svc := newTestService(t)

	data := &ProjectDataDTO{
		Servers: []ServerSnapshotDTO{
			{ProtocolType: "modbus-tcp", Variant: "tcp"},
		},
		Scripts: []*ScriptDTO{},
		MonitoringItems: []*MonitoringItemDTO{
			{ID: "item-1", ProtocolType: "modbus-tcp", MemoryArea: "holdingRegisters", Address: 0, BitWidth: 16, Order: 1},
			{ID: "item-2", ProtocolType: "modbus-tcp", MemoryArea: "coils", Address: 5, BitWidth: 1, Order: 2},
		},
	}

	if err := svc.ImportProject(data); err != nil {
		t.Fatalf("ImportProject failed: %v", err)
	}

	items := svc.GetMonitoringItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 monitoring items after import, got %d", len(items))
	}
}

func TestPLCService_ImportProject_ClearsExistingServers(t *testing.T) {
	svc := newTestService(t)

	// modbus-rtu を追加してからインポートでリセット
	if err := svc.AddServer("modbus-rtu", "rtu"); err != nil {
		t.Fatalf("AddServer modbus-rtu failed: %v", err)
	}

	data := &ProjectDataDTO{
		Servers: []ServerSnapshotDTO{
			{ProtocolType: "modbus-tcp", Variant: "tcp"},
		},
		Scripts: []*ScriptDTO{},
	}

	if err := svc.ImportProject(data); err != nil {
		t.Fatalf("ImportProject failed: %v", err)
	}

	// インポート後は modbus のみになるはず
	instances := svc.GetServerInstances()
	if len(instances) != 1 {
		t.Fatalf("expected 1 server after import, got %d", len(instances))
	}
	if instances[0].ProtocolType != "modbus-tcp" {
		t.Errorf("expected 'modbus', got '%s'", instances[0].ProtocolType)
	}
}

func TestPLCService_ExportImport_RoundTrip(t *testing.T) {
	svc := newTestService(t)

	// マルチサーバー構成
	if err := svc.AddServer("modbus-rtu", "rtu"); err != nil {
		t.Fatalf("AddServer modbus-rtu failed: %v", err)
	}

	// スクリプトとモニタリング項目を追加
	svc.CreateScript("rtt_script", "1+1", 100)
	svc.AddMonitoringItem(&MonitoringItemDTO{
		ProtocolType: "modbus-tcp",
		MemoryArea:   "holdingRegisters",
		Address:      3,
		BitWidth:     16,
	})

	exported := svc.ExportProject()

	// 別のサービスにインポートして検証
	svc2 := newTestService(t)
	if err := svc2.ImportProject(exported); err != nil {
		t.Fatalf("ImportProject round-trip failed: %v", err)
	}

	// サーバー構成を確認
	instances := svc2.GetServerInstances()
	if len(instances) != 2 {
		t.Fatalf("expected 2 servers after round-trip, got %d", len(instances))
	}

	// スクリプト数を確認
	if scripts := svc2.GetScripts(); len(scripts) != 1 {
		t.Errorf("expected 1 script, got %d", len(scripts))
	}

	// モニタリング項目を確認
	if items := svc2.GetMonitoringItems(); len(items) != 1 {
		t.Errorf("expected 1 monitoring item, got %d", len(items))
	}
}

// 実行中のスクリプトを、コンパイルできないコードで保存すると、エラーが返り、スクリプトは停止する（変更は保存される）
func TestPLCService_UpdateScript_CompileErrorWhileRunning(t *testing.T) {
	svc := newTestService(t)

	created, _ := svc.CreateScript("runner", `1+1`, 50)
	if err := svc.StartScript(created.ID); err != nil {
		t.Fatalf("StartScript failed: %v", err)
	}

	if err := svc.UpdateScript(created.ID, "runner", `invalid syntax {{{`, 50); err == nil {
		t.Fatal("コンパイルできないコードの保存はエラーになるはず")
	}

	got, _ := svc.GetScript(created.ID)
	if got.IsRunning {
		t.Error("スクリプトは停止しているはず")
	}
	if got.Code != `invalid syntax {{{` {
		t.Errorf("変更は保存されているはず: %q", got.Code)
	}

	// エラーはコンソールログにも出る
	logs := svc.GetConsoleLogs()
	if len(logs) != 1 || logs[0].Level != "error" || logs[0].ScriptName != "runner" {
		t.Errorf("コンソールログ = %+v", logs)
	}

	// 止まっているスクリプトの保存は、コンパイルできなくてもエラーにしない（開始するときにエラーになる）
	if err := svc.UpdateScript(created.ID, "runner", `still invalid {{{`, 50); err != nil {
		t.Errorf("停止中の保存はエラーにしない: %v", err)
	}
}
