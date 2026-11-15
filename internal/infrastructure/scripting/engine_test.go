package scripting

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"karakuri/internal/domain/script"
	"karakuri/internal/domain/variable"
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

// 変数の関数は、失敗したらスクリプトのエラーになる（黙って null を返したり、何もしなかったりしない）
func TestScriptEngine_VariableFunctionsFailLoudly(t *testing.T) {
	engine, vs := newTestEngine()
	mustCreate := func(name string, dt variable.DataType, value any) {
		t.Helper()
		if _, err := vs.CreateVariable(name, dt, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := vs.RegisterStructType(&variable.StructTypeDef{Name: "Motor", Fields: []variable.StructField{
		{Name: "speed", DataType: variable.TypeINT},
	}}); err != nil {
		t.Fatal(err)
	}
	mustCreate("Count", variable.TypeINT, int16(10))
	mustCreate("Big", variable.TypeLINT, int64(1))
	mustCreate("Wait", variable.TypeTIME, "T#1s")
	mustCreate("Values", variable.DataType("ARRAY[1..3] OF INT"), nil)
	mustCreate("M1", variable.DataType("Motor"), nil)

	cases := []struct {
		name, code, wantInError string
	}{
		{"ない変数を読む", `plc.readVariable("Nope")`, `plc.readVariable: 変数 "Nope" がありません`},
		{"ない変数に書く", `plc.writeVariable("Nope", 1)`, `plc.writeVariable: 変数 "Nope" がありません`},
		{"ない変数のフィールドを読む", `plc.readVariable("Nope.speed")`, `変数 "Nope" がありません`},
		{"ない変数の要素に書く", `plc.writeVariable("Nope[1]", 1)`, `変数 "Nope" がありません`},
		{"ないフィールドを読む", `plc.readVariable("M1.nope")`, `plc.readVariable("M1.nope")`},
		{"ないフィールドに書く", `plc.writeVariable("M1.nope", 1)`, `plc.writeVariable("M1.nope")`},
		{"配列の範囲外を読む", `plc.readVariable("Values[9]")`, `plc.readVariable("Values[9]")`},
		{"型に合わない値を書く", `plc.writeVariable("Count", "abc")`, `plc.writeVariable("Count")`},
		{"readArrayElement: ない変数", `plc.readArrayElement("Nope", 1)`, `変数 "Nope" がありません`},
		{"readArrayElement: 配列でない変数", `plc.readArrayElement("Count", 1)`, `plc.readArrayElement("Count")`},
		{"readArrayElement: 範囲外", `plc.readArrayElement("Values", 0)`, `plc.readArrayElement("Values")`},
		{"writeArrayElement: 範囲外", `plc.writeArrayElement("Values", 4, 1)`, `plc.writeArrayElement("Values")`},
		{"writeArrayElement: ない変数", `plc.writeArrayElement("Nope", 1, 1)`, `変数 "Nope" がありません`},
		{"readStructField: ない変数", `plc.readStructField("Nope", "speed")`, `変数 "Nope" がありません`},
		{"readStructField: 構造体でない変数", `plc.readStructField("Count", "speed")`, `は構造体ではありません`},
		{"readStructField: ないフィールド", `plc.readStructField("M1", "nope")`, `フィールド "nope" がありません`},
		{"writeStructField: ないフィールド", `plc.writeStructField("M1", "nope", 1)`, `フィールド "nope" がありません`},
		{"64 ビットを超える BigInt を書く", `plc.writeVariable("Big", 2n ** 64n)`, `64 ビットの整数の範囲`},
		{"構造体の中の 64 ビットを超える BigInt", `plc.writeVariable("M1", {speed: -(2n ** 63n) - 1n})`, `speed: BigInt`},
		{"readTimeMs: ない変数", `plc.readTimeMs("Nope")`, `変数 "Nope" がありません`},
		{"readTimeMs: TIME でない変数", `plc.readTimeMs("Count")`, `は TIME ではありません`},
		{"readDateSec: DATE でない値", `plc.readDateSec("Wait")`, `DATE として読めません`},
		{"writeTimeMs: ない変数", `plc.writeTimeMs("Nope", 1000)`, `変数 "Nope" がありません`},
		{"getVariableStatus: ない変数", `plc.getVariableStatus("Nope")`, `plc.getVariableStatus: 変数 "Nope" がありません`},
	}
	for _, c := range cases {
		_, err := engine.RunOnce(c.code)
		if err == nil {
			t.Errorf("%s: エラーにならなかった（%s）", c.name, c.code)
			continue
		}
		if !strings.Contains(err.Error(), c.wantInError) {
			t.Errorf("%s: エラー = %q, want …%s…", c.name, err.Error(), c.wantInError)
		}
	}

	// コンソールに出る文面には、Go の関数の呼び出し位置を含めない
	logs := engine.GetConsoleLogs()
	last := logs[len(logs)-1]
	if last.Level != LogLevelError || strings.Contains(last.Message, "(native)") || !strings.Contains(last.Message, `変数 "Nope" がありません`) {
		t.Errorf("コンソールの文面: %+v", last)
	}

	// 失敗した書き込みで、値は変わっていない
	if v, _ := vs.GetVariableByName("Count"); v.Value != int16(10) {
		t.Errorf("Count = %v, want 10", v.Value)
	}

	// 成功する呼び出しは、これまでどおり
	ok := []struct {
		code string
		want any
	}{
		{`plc.hasVariable("Count")`, true},
		{`plc.hasVariable("Nope")`, false},
		{`plc.readVariable("Count")`, int64(10)},
		{`plc.writeVariable("M1.speed", 7); plc.readStructField("M1", "speed")`, int64(7)},
		{`plc.writeStructField("M1", "speed", 8); plc.readVariable("M1.speed")`, int64(8)},
		{`plc.writeArrayElement("Values", 3, 5); plc.readArrayElement("Values", 3)`, int64(5)},
		{`plc.writeTimeMs("Wait", 2500); plc.readTimeMs("Wait")`, int64(2500)},
		// エラーは、スクリプトの try / catch で受け取れる
		{`let caught = ""; try { plc.readVariable("Nope") } catch (e) { caught = e.message } caught`, `plc.readVariable: 変数 "Nope" がありません`},
		// 変数はあるが、接続先にマッピングしていなければ null
		{`plc.getVariableStatus("Count")`, nil},
	}
	for _, c := range ok {
		got, err := engine.RunOnce(c.code)
		if err != nil {
			t.Errorf("%s: %v", c.code, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s = %v (%T), want %v (%T)", c.code, got, got, c.want, c.want)
		}
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

// ===== コンソールログ（エラー・警告・1 回実行の結果） =====

// logRecorder は、コンソールログの通知（SetOnLogAdded）を記録する
type logRecorder struct {
	mu      sync.Mutex
	entries []ConsoleLogEntry
}

func recordLogs(engine *ScriptEngine) *logRecorder {
	r := &logRecorder{}
	engine.SetOnLogAdded(func(e ConsoleLogEntry) {
		r.mu.Lock()
		r.entries = append(r.entries, e)
		r.mu.Unlock()
	})
	return r
}

// messages は、指定した種類のログのメッセージを返す
func (r *logRecorder) messages(level string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []string
	for _, e := range r.entries {
		if e.Level == level {
			result = append(result, e.Message)
		}
	}
	return result
}

// 1 回実行の結果とエラーは、コンソールに「1 回実行」の名前で出る
func TestScriptEngine_RunOnce_LogsResultAndError(t *testing.T) {
	engine, _ := newTestEngine()
	rec := recordLogs(engine)

	if _, err := engine.RunOnce(`console.log("hello"); 1 + 2`); err != nil {
		t.Fatal(err)
	}
	if got, want := rec.messages(LogLevelLog), []string{"hello", "=> 3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ログ = %q, want %q", got, want)
	}

	// 値を返さないコードは「完了」
	if _, err := engine.RunOnce(`var x = 1;`); err != nil {
		t.Fatal(err)
	}
	if got := rec.messages(LogLevelLog); got[len(got)-1] != "完了" {
		t.Errorf("最後のログ = %q, want 完了", got[len(got)-1])
	}

	// エラーは [ERROR] として出る（実行時のエラーと構文エラー）
	if _, err := engine.RunOnce(`throw new Error("boom")`); err == nil {
		t.Fatal("エラーになるはず")
	}
	if _, err := engine.RunOnce(`invalid syntax {{{`); err == nil {
		t.Fatal("エラーになるはず")
	}
	errs := rec.messages(LogLevelError)
	if len(errs) != 2 || !strings.HasPrefix(errs[0], "[ERROR] ") || !strings.Contains(errs[0], "boom") || !strings.HasPrefix(errs[1], "[ERROR] ") {
		t.Errorf("エラーのログ = %q", errs)
	}

	// バッファにも同じ内容が入り、スクリプト名は「1 回実行」
	for _, e := range engine.GetConsoleLogs() {
		if e.ScriptName != OnceScriptName {
			t.Errorf("ScriptName = %q, want %q", e.ScriptName, OnceScriptName)
		}
	}
	if got, want := len(engine.GetConsoleLogs()), 5; got != want {
		t.Errorf("バッファの件数 = %d, want %d", got, want)
	}
}

// 周期実行のスクリプトと同じく、トップレベルに return を書いたコードも 1 回実行できる
func TestScriptEngine_RunOnce_TopLevelReturn(t *testing.T) {
	engine, vs := newTestEngine()
	if _, err := vs.CreateVariable("Counter", variable.TypeINT, int16(0)); err != nil {
		t.Fatal(err)
	}

	result, err := engine.RunOnce(`
		plc.writeVariable("Counter", 5);
		if (plc.readVariable("Counter") === 5) { return 42; }
		plc.writeVariable("Counter", 9);
	`)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result != int64(42) {
		t.Errorf("result = %v (%T), want 42", result, result)
	}
	if v, _ := vs.GetVariableByName("Counter"); v.Value != int16(5) {
		t.Errorf("Counter = %v, want 5", v.Value)
	}
}

// 周期実行のエラーはコンソールに出る。同じエラーが続く間は最初の 1 回だけで、直ったあとにまた起きたら出し直す
func TestScriptEngine_RuntimeError_LoggedOncePerStreak(t *testing.T) {
	engine, vs := newTestEngine()
	rec := recordLogs(engine)
	if _, err := vs.CreateVariable("Fail", variable.TypeBOOL, true); err != nil {
		t.Fatal(err)
	}

	s := script.NewScript("err-1", "failing", `if (plc.readVariable("Fail")) { throw new Error("boom"); }`, 10*time.Millisecond, 0)
	if err := engine.StartScript(s); err != nil {
		t.Fatal(err)
	}
	defer engine.StopAll()

	// 何周期も失敗しても、ログは 1 件
	time.Sleep(150 * time.Millisecond)
	errs := rec.messages(LogLevelError)
	if len(errs) != 1 || !strings.Contains(errs[0], "boom") {
		t.Fatalf("エラーのログ = %q, want 1 件", errs)
	}
	if !engine.IsRunning("err-1") {
		t.Error("エラーが出ても実行は続くはず")
	}

	// 直ってから、また失敗すると出し直す
	vs.UpdateValueByName("Fail", false)
	time.Sleep(80 * time.Millisecond)
	vs.UpdateValueByName("Fail", true)
	time.Sleep(150 * time.Millisecond)
	if got := len(rec.messages(LogLevelError)); got != 2 {
		t.Errorf("エラーのログ = %d 件, want 2", got)
	}
}

// コンパイルできないスクリプトは開始できず、エラーがコンソールに出る
func TestScriptEngine_StartScript_CompileError_Logged(t *testing.T) {
	engine, _ := newTestEngine()
	rec := recordLogs(engine)

	s := script.NewScript("bad-1", "bad", `invalid syntax {{{`, 100*time.Millisecond, 0)
	if err := engine.StartScript(s); err == nil {
		t.Fatal("エラーになるはず")
	}
	if engine.IsRunning("bad-1") {
		t.Error("開始されていないはず")
	}
	errs := rec.messages(LogLevelError)
	if len(errs) != 1 || !strings.HasPrefix(errs[0], "[ERROR] ") {
		t.Errorf("エラーのログ = %q", errs)
	}
	if logs := engine.GetConsoleLogs(); len(logs) != 1 || logs[0].ScriptName != "bad" || logs[0].ScriptID != "bad-1" {
		t.Errorf("バッファ = %+v", logs)
	}
}

// 警告も、[WARN] を付けてコンソールに足され、通知される
func TestScriptEngine_Warn_Notified(t *testing.T) {
	engine, _ := newTestEngine()
	rec := recordLogs(engine)
	engine.addLog("s-1", "s", LogLevelWarn, "注意")
	warns := rec.messages(LogLevelWarn)
	if len(warns) != 1 || warns[0] != "[WARN] 注意" {
		t.Errorf("警告のログ = %q", warns)
	}
}

// LINT / ULINT は、いつも BigInt で読み書きする（64 ビットの全範囲を、桁を落とさずに）。ほかの整数は Number
func TestScriptEngine_LintUlintAreBigInt(t *testing.T) {
	engine, vs := newTestEngine()
	mustCreate := func(name string, dt variable.DataType, value any) {
		t.Helper()
		if _, err := vs.CreateVariable(name, dt, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := vs.RegisterStructType(&variable.StructTypeDef{Name: "Counter", Fields: []variable.StructField{
		{Name: "total", DataType: variable.TypeLINT},
		{Name: "count", DataType: variable.TypeDINT},
	}}); err != nil {
		t.Fatal(err)
	}
	mustCreate("L", variable.TypeLINT, int64(1)<<60+1)
	mustCreate("U", variable.TypeULINT, uint64(18446744073709551615))
	mustCreate("D", variable.TypeDINT, int32(7))
	mustCreate("C", variable.DataType("Counter"), nil)
	mustCreate("Arr", variable.DataType("ARRAY[1..2] OF ULINT"), nil)

	run := func(code string) any {
		t.Helper()
		v, err := engine.RunOnce(code)
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		return v
	}
	read := func(name string) any {
		t.Helper()
		v, err := vs.GetVariableByName(name)
		if err != nil {
			t.Fatal(err)
		}
		return v.Value
	}

	// 読むと BigInt。ほかの整数は Number のまま
	if got := run(`typeof plc.readVariable("L") + "," + typeof plc.readVariable("D")`); got != "bigint,number" {
		t.Errorf("型 = %v", got)
	}
	// 2^53 を超える値も、桁を落とさずに計算して書ける
	run(`plc.writeVariable("L", plc.readVariable("L") + 1n)`)
	if got := read("L"); got != int64(1)<<60+2 {
		t.Errorf("L = %v", got)
	}
	// ULINT の最大値（2^64 - 1）も、そのまま読める
	if got := run(`String(plc.readVariable("U"))`); got != "18446744073709551615" {
		t.Errorf("U = %v", got)
	}
	run(`plc.writeVariable("U", plc.readVariable("U") - 5n)`)
	if got := read("U"); got != uint64(18446744073709551610) {
		t.Errorf("U = %v", got)
	}
	// Number でも書ける
	run(`plc.writeVariable("L", 5)`)
	if got := read("L"); got != int64(5) {
		t.Errorf("Number を書いた L = %v", got)
	}

	// 構造体のフィールド: 読むと BigInt、書くのは BigInt でも Number でも
	if got := run(`typeof plc.readVariable("C").total + "," + typeof plc.readStructField("C", "total") + "," + typeof plc.readVariable("C.count")`); got != "bigint,bigint,number" {
		t.Errorf("構造体の型 = %v", got)
	}
	run(`plc.writeVariable("C.total", 2n ** 62n)`)
	run(`plc.writeStructField("C", "count", 3)`)
	run(`plc.writeVariable("C", {total: plc.readVariable("C").total + 1n, count: 4})`)
	if got := read("C").(map[string]any); got["total"] != int64(1)<<62+1 || got["count"] != int32(4) {
		t.Errorf("C = %v", got)
	}

	// 配列の要素
	if got := run(`typeof plc.readVariable("Arr")[0] + "," + typeof plc.readArrayElement("Arr", 2)`); got != "bigint,bigint" {
		t.Errorf("配列の型 = %v", got)
	}
	run(`plc.writeArrayElement("Arr", 2, 2n ** 63n)`)
	run(`plc.writeVariable("Arr[1]", 9n)`)
	if got := read("Arr").([]any); got[0] != uint64(9) || got[1] != uint64(1)<<63 {
		t.Errorf("Arr = %v", got)
	}

	// BigInt と Number は混ぜて計算できない（JS の決まり）。比較は混ぜてよい
	if _, err := engine.RunOnce(`plc.readVariable("L") + 1`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("混ぜた計算のエラー = %v", err)
	}
	if got := run(`plc.readVariable("L") > 4`); got != true {
		t.Errorf("比較 = %v", got)
	}

	// console.log には、そのままの桁で出る
	rec := recordLogs(engine)
	run(`console.log(plc.readVariable("U"))`)
	if logs := rec.messages(LogLevelLog); len(logs) == 0 || !strings.Contains(logs[0], "18446744073709551610") {
		t.Errorf("console.log = %q", logs)
	}
}
