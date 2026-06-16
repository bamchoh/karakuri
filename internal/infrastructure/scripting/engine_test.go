package scripting

import (
	"testing"
	"time"

	"modbus_simulator/internal/domain/script"
	"modbus_simulator/internal/domain/variable"
)

func newTestEngine() (*ScriptEngine, *variable.VariableStore) {
	vs := variable.NewVariableStore()
	engine := NewScriptEngine(vs)
	return engine, vs
}

func TestNewScriptEngine(t *testing.T) {
	engine, _ := newTestEngine()
	if engine == nil {
		t.Fatal("NewScriptEngine returned nil")
	}
	if engine.variableStore == nil {
		t.Fatal("variableStore is nil")
	}
}

func TestScriptEngine_RunOnce(t *testing.T) {
	engine, _ := newTestEngine()

	// 単純な計算
	result, err := engine.RunOnce("1 + 2")
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if result != int64(3) {
		t.Errorf("expected 3, got %v (%T)", result, result)
	}
}

func TestScriptEngine_RunOnce_WithConsole(t *testing.T) {
	engine, _ := newTestEngine()

	// console.logは正常に動作するべき
	_, err := engine.RunOnce(`console.log("test message")`)
	if err != nil {
		t.Fatalf("RunOnce with console.log failed: %v", err)
	}
}

func TestScriptEngine_RunOnce_SyntaxError(t *testing.T) {
	engine, _ := newTestEngine()

	// 構文エラー
	_, err := engine.RunOnce("invalid syntax {{{")
	if err == nil {
		t.Fatal("expected syntax error")
	}
}

func TestScriptEngine_RunOnce_ReadWriteVariable(t *testing.T) {
	engine, vs := newTestEngine()

	// 変数を作成
	_, err := vs.CreateVariable("Counter", variable.TypeINT, int16(0))
	if err != nil {
		t.Fatalf("CreateVariable failed: %v", err)
	}

	// スクリプトで書き込む
	_, err = engine.RunOnce(`plc.writeVariable("Counter", 1234)`)
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}

	// VariableStoreで確認
	v, _ := vs.GetVariableByName("Counter")
	if v.Value != int16(1234) {
		t.Errorf("expected int16(1234), got %v (%T)", v.Value, v.Value)
	}

	// スクリプトで読み取り
	result, err := engine.RunOnce(`plc.readVariable("Counter")`)
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if result != int64(1234) {
		t.Errorf("expected 1234, got %v (%T)", result, result)
	}
}

func TestScriptEngine_RunOnce_ReadWriteVariable_BOOL(t *testing.T) {
	engine, vs := newTestEngine()

	_, err := vs.CreateVariable("Flag", variable.TypeBOOL, false)
	if err != nil {
		t.Fatalf("CreateVariable failed: %v", err)
	}

	_, err = engine.RunOnce(`plc.writeVariable("Flag", true)`)
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}

	v, _ := vs.GetVariableByName("Flag")
	if v.Value != true {
		t.Errorf("expected true, got %v", v.Value)
	}

	result, err := engine.RunOnce(`plc.readVariable("Flag")`)
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if result != true {
		t.Errorf("expected true, got %v", result)
	}
}

func TestScriptEngine_RunOnce_ReadWriteVariable_REAL(t *testing.T) {
	engine, vs := newTestEngine()

	_, err := vs.CreateVariable("Temperature", variable.TypeREAL, float32(0))
	if err != nil {
		t.Fatalf("CreateVariable failed: %v", err)
	}

	_, err = engine.RunOnce(`plc.writeVariable("Temperature", 25.5)`)
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}

	v, _ := vs.GetVariableByName("Temperature")
	if v.Value != float32(25.5) {
		t.Errorf("expected float32(25.5), got %v (%T)", v.Value, v.Value)
	}
}

func TestScriptEngine_RunOnce_GetVariables(t *testing.T) {
	engine, vs := newTestEngine()

	vs.CreateVariable("Var1", variable.TypeINT, int16(0))
	vs.CreateVariable("Var2", variable.TypeBOOL, false)

	result, err := engine.RunOnce(`plc.getVariables()`)
	if err != nil {
		t.Fatalf("getVariables failed: %v", err)
	}

	switch v := result.(type) {
	case []interface{}:
		if len(v) != 2 {
			t.Errorf("expected 2 variables, got %d", len(v))
		}
	case []string:
		if len(v) != 2 {
			t.Errorf("expected 2 variables, got %d", len(v))
		}
	default:
		t.Fatalf("unexpected type: %T", result)
	}
}

func TestScriptEngine_RunOnce_ReadNonexistentVariable(t *testing.T) {
	engine, _ := newTestEngine()

	result, err := engine.RunOnce(`plc.readVariable("Nonexistent")`)
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestScriptEngine_RunOnce_IncrementVariable(t *testing.T) {
	engine, vs := newTestEngine()

	_, err := vs.CreateVariable("Count", variable.TypeINT, int16(10))
	if err != nil {
		t.Fatalf("CreateVariable failed: %v", err)
	}

	// readして+1してwrite（ユーザーの実際のユースケース）
	_, err = engine.RunOnce(`
		var count = plc.readVariable("Count");
		plc.writeVariable("Count", count + 1);
	`)
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}

	v, _ := vs.GetVariableByName("Count")
	if v.Value != int16(11) {
		t.Errorf("expected int16(11), got %v (%T)", v.Value, v.Value)
	}
}

func TestScriptEngine_StartStopScript(t *testing.T) {
	engine, vs := newTestEngine()

	_, err := vs.CreateVariable("Counter", variable.TypeINT, int16(0))
	if err != nil {
		t.Fatalf("CreateVariable failed: %v", err)
	}

	s := script.NewScript("test-1", "counter", `
		var val = plc.readVariable("Counter");
		plc.writeVariable("Counter", val + 1);
	`, 50*time.Millisecond, 0)

	// スクリプト開始
	err = engine.StartScript(s)
	if err != nil {
		t.Fatalf("StartScript failed: %v", err)
	}

	// 実行中であることを確認
	if !engine.IsRunning("test-1") {
		t.Error("expected script to be running")
	}

	// 少し待ってカウンターが増加していることを確認
	time.Sleep(200 * time.Millisecond)

	v, _ := vs.GetVariableByName("Counter")
	val := v.Value.(int16)
	if val < 2 {
		t.Errorf("expected counter >= 2, got %d", val)
	}

	// スクリプト停止
	err = engine.StopScript("test-1")
	if err != nil {
		t.Fatalf("StopScript failed: %v", err)
	}

	// 停止していることを確認
	if engine.IsRunning("test-1") {
		t.Error("expected script to be stopped")
	}
}

func TestScriptEngine_StopScript_NotFound(t *testing.T) {
	engine, _ := newTestEngine()

	err := engine.StopScript("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent script")
	}
}

func TestScriptEngine_StopAll(t *testing.T) {
	engine, _ := newTestEngine()

	// 複数のスクリプトを開始
	s1 := script.NewScript("test-1", "script1", `1+1`, 100*time.Millisecond, 0)
	s2 := script.NewScript("test-2", "script2", `2+2`, 100*time.Millisecond, 0)

	_ = engine.StartScript(s1)
	_ = engine.StartScript(s2)

	// 実行中であることを確認
	if len(engine.GetRunningScripts()) != 2 {
		t.Errorf("expected 2 running scripts, got %d", len(engine.GetRunningScripts()))
	}

	// 全て停止
	engine.StopAll()

	// 停止していることを確認
	if len(engine.GetRunningScripts()) != 0 {
		t.Errorf("expected 0 running scripts, got %d", len(engine.GetRunningScripts()))
	}
}

func TestScriptEngine_GetRunningScripts(t *testing.T) {
	engine, _ := newTestEngine()

	// 初期状態
	if len(engine.GetRunningScripts()) != 0 {
		t.Error("expected no running scripts initially")
	}

	// スクリプトを開始
	s := script.NewScript("test-1", "script1", `1+1`, 100*time.Millisecond, 0)
	_ = engine.StartScript(s)

	running := engine.GetRunningScripts()
	if len(running) != 1 {
		t.Errorf("expected 1 running script, got %d", len(running))
	}
	if running[0] != "test-1" {
		t.Errorf("expected 'test-1', got '%s'", running[0])
	}

	// 停止
	_ = engine.StopScript("test-1")

	if len(engine.GetRunningScripts()) != 0 {
		t.Error("expected no running scripts after stop")
	}
}

func TestScriptEngine_StartScript_ReplaceRunning(t *testing.T) {
	engine, vs := newTestEngine()

	_, _ = vs.CreateVariable("Val", variable.TypeINT, int16(0))

	s1 := script.NewScript("test-1", "script1", `plc.writeVariable("Val", 111)`, 50*time.Millisecond, 0)
	s2 := script.NewScript("test-1", "script1-updated", `plc.writeVariable("Val", 222)`, 50*time.Millisecond, 0)

	// 最初のスクリプト開始
	_ = engine.StartScript(s1)
	time.Sleep(100 * time.Millisecond)

	v, _ := vs.GetVariableByName("Val")
	if v.Value != int16(111) {
		t.Errorf("expected int16(111), got %v", v.Value)
	}

	// 同じIDで別のスクリプトを開始（置き換え）
	_ = engine.StartScript(s2)
	time.Sleep(100 * time.Millisecond)

	v, _ = vs.GetVariableByName("Val")
	if v.Value != int16(222) {
		t.Errorf("expected int16(222), got %v", v.Value)
	}

	// クリーンアップ
	engine.StopAll()
}

func TestScriptEngine_StartScript_CompileError(t *testing.T) {
	engine, _ := newTestEngine()

	s := script.NewScript("test-1", "invalid", `invalid syntax {{{`, 100*time.Millisecond, 0)

	err := engine.StartScript(s)
	if err == nil {
		t.Fatal("expected compile error")
	}
}

func TestScriptEngine_IsRunning(t *testing.T) {
	engine, _ := newTestEngine()

	// 存在しないスクリプト
	if engine.IsRunning("nonexistent") {
		t.Error("expected false for nonexistent script")
	}

	// スクリプトを開始
	s := script.NewScript("test-1", "script1", `1+1`, 100*time.Millisecond, 0)
	_ = engine.StartScript(s)

	if !engine.IsRunning("test-1") {
		t.Error("expected true for running script")
	}

	// 停止
	_ = engine.StopScript("test-1")

	if engine.IsRunning("test-1") {
		t.Error("expected false after stop")
	}
}
