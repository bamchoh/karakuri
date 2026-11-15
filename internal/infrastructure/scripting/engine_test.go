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
	_, err = engine.RunOnce(`vars.Counter = 1234`)
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}

	// VariableStoreで確認
	v, _ := vs.GetVariableByName("Counter")
	if v.Value != int16(1234) {
		t.Errorf("expected int16(1234), got %v (%T)", v.Value, v.Value)
	}

	// スクリプトで読み取り
	result, err := engine.RunOnce(`vars.Counter`)
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

	_, err = engine.RunOnce(`vars.Flag = true`)
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}

	v, _ := vs.GetVariableByName("Flag")
	if v.Value != true {
		t.Errorf("expected true, got %v", v.Value)
	}

	result, err := engine.RunOnce(`vars.Flag`)
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

	_, err = engine.RunOnce(`vars.Temperature = 25.5`)
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

	result, err := engine.RunOnce(`Object.keys(vars)`)
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
		{"ない変数を読む", `vars.Nope`, `vars: 変数 "Nope" がありません`},
		{"ない変数に書く", `vars.Nope = 1`, `vars: 変数 "Nope" がありません`},
		{"ない変数のフィールドを読む", `vars.Nope.speed`, `変数 "Nope" がありません`},
		{"ない変数の要素に書く", `vars.Nope[1] = 1`, `変数 "Nope" がありません`},
		{"ないフィールドを読む", `vars.M1.nope`, `vars: M1 にフィールド "nope" がありません`},
		{"ないフィールドに書く", `vars.M1.nope = 1`, `vars: M1 にフィールド "nope" がありません`},
		{"配列の範囲外を読む", `vars.Values[9]`, `vars: Values の添字 9 は範囲（1〜3）の外です`},
		{"配列の範囲外に書く", `vars.Values[0] = 1`, `vars: Values の添字 0 は範囲（1〜3）の外です`},
		{"配列を名前で読む", `vars.Values.first`, `vars: Values は配列です`},
		{"構造体でない変数のフィールド", `vars.Count.speed.x`, `TypeError`},
		{"型に合わない値を書く", `vars.Count = "abc"`, `vars: Count に書けません`},
		{"64 ビットを超える BigInt を書く", `vars.Big = 2n ** 64n`, `64 ビットの整数の範囲`},
		{"構造体の中の 64 ビットを超える BigInt", `vars.M1 = {speed: -(2n ** 63n) - 1n}`, `speed: BigInt`},
		{"変数は消せない", `delete vars.Count`, `vars: 変数 "Count" は消せません`},
		{"vars(): ない変数", `vars("Nope")`, `vars: 変数 "Nope" がありません`},
		{"vars(): ないフィールド", `vars("M1.nope")`, `vars("M1.nope")`},
		{"vars(): 名前がない", `vars()`, `変数の名前を渡してください`},
		{"clients(): ない接続先", `clients("Nope")`, `clients: 接続先 "Nope" がありません`},
		{"iec: 読めない時間", `iec.parseTime("abc")`, `iec.parseTime("abc")`},
		{"iec: 読めない日付", `iec.parseDate(vars.Wait)`, `iec.parseDate("T#1s")`},
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
	if _, err := engine.RunOnce(`vars.Nope`); err == nil {
		t.Fatal("エラーにならない")
	}
	logs := engine.GetConsoleLogs()
	last := logs[len(logs)-1]
	if last.Level != LogLevelError || strings.Contains(last.Message, "(native)") || !strings.Contains(last.Message, `変数 "Nope" がありません`) {
		t.Errorf("コンソールの文面: %+v", last)
	}

	// 失敗した書き込みで、値は変わっていない
	if v, _ := vs.GetVariableByName("Count"); v.Value != int16(10) {
		t.Errorf("Count = %v, want 10", v.Value)
	}

	// 成功する呼び出し
	ok := []struct {
		code string
		want any
	}{
		{`"Count" in vars`, true},
		{`"Nope" in vars`, false},
		{`"speed" in vars.M1`, true},
		{`3 in vars.Values`, true},
		{`0 in vars.Values`, false},
		{`vars.Count`, int64(10)},
		{`vars.M1.speed = 7; vars.M1.speed`, int64(7)},
		{`vars["M" + 1].speed = 8; vars("M1.speed").read()`, int64(8)},
		{`vars.Values[3] = 5; vars.Values[3]`, int64(5)},
		{`vars("Values[2]").write(4); vars.Values[2]`, int64(4)},
		{`vars.Values.length`, int64(3)},
		{`[...vars.Values].join(",")`, "0,4,5"},
		{`Object.keys(vars.Values).join(",")`, "1,2,3"},
		{`Object.keys(vars.M1).join(",")`, "speed"},
		{`JSON.stringify(vars("M1").read())`, `{"speed":8}`},
		{`vars.Wait`, "T#1s"},
		{`vars.Wait = iec.formatTime(2500); iec.parseTime(vars.Wait)`, int64(2500)},
		{`vars("Count").name`, "Count"},
		// エラーは、スクリプトの try / catch で受け取れる
		{`let caught = ""; try { vars.Nope } catch (e) { caught = e.message } caught`, `vars: 変数 "Nope" がありません`},
		// 変数はあるが、接続先にマッピングしていなければ null
		{`vars("Count").status()`, nil},
		// console.log などが黙って調べに来る名前は、エラーにしない
		{`typeof vars.toJSON`, "undefined"},
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
		var count = vars.Count;
		vars.Count = count + 1;
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
		var val = vars.Counter;
		vars.Counter = val + 1;
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

	s1 := script.NewScript("test-1", "script1", `vars.Val = 111`, 50*time.Millisecond, 0)
	s2 := script.NewScript("test-1", "script1-updated", `vars.Val = 222`, 50*time.Millisecond, 0)

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
		vars.Counter = 5;
		if (vars.Counter === 5) { return 42; }
		vars.Counter = 9;
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

	s := script.NewScript("err-1", "failing", `if (vars.Fail) { throw new Error("boom"); }`, 10*time.Millisecond, 0)
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
	if got := run(`typeof vars.L + "," + typeof vars.D`); got != "bigint,number" {
		t.Errorf("型 = %v", got)
	}
	// 2^53 を超える値も、桁を落とさずに計算して書ける
	run(`vars.L = vars.L + 1n`)
	if got := read("L"); got != int64(1)<<60+2 {
		t.Errorf("L = %v", got)
	}
	// ULINT の最大値（2^64 - 1）も、そのまま読める
	if got := run(`String(vars.U)`); got != "18446744073709551615" {
		t.Errorf("U = %v", got)
	}
	run(`vars.U = vars.U - 5n`)
	if got := read("U"); got != uint64(18446744073709551610) {
		t.Errorf("U = %v", got)
	}
	// Number でも書ける
	run(`vars.L = 5`)
	if got := read("L"); got != int64(5) {
		t.Errorf("Number を書いた L = %v", got)
	}

	// 構造体のフィールド: 読むと BigInt、書くのは BigInt でも Number でも
	if got := run(`typeof vars.C.total + "," + typeof vars("C.total").read() + "," + typeof vars.C.count`); got != "bigint,bigint,number" {
		t.Errorf("構造体の型 = %v", got)
	}
	run(`vars("C.total").write(2n ** 62n)`)
	run(`vars.C.count = 3`)
	run(`vars.C = {total: vars.C.total + 1n, count: 4}`)
	if got := read("C").(map[string]any); got["total"] != int64(1)<<62+1 || got["count"] != int32(4) {
		t.Errorf("C = %v", got)
	}

	// 配列の要素
	if got := run(`typeof vars.Arr[1] + "," + typeof vars("Arr[2]").read()`); got != "bigint,bigint" {
		t.Errorf("配列の型 = %v", got)
	}
	run(`vars.Arr[2] = 2n ** 63n`)
	run(`vars("Arr[1]").write(9n)`)
	if got := read("Arr").([]any); got[0] != uint64(9) || got[1] != uint64(1)<<63 {
		t.Errorf("Arr = %v", got)
	}

	// BigInt と Number は混ぜて計算できない（JS の決まり）。比較は混ぜてよい
	if _, err := engine.RunOnce(`vars.L + 1`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("混ぜた計算のエラー = %v", err)
	}
	if got := run(`vars.L > 4`); got != true {
		t.Errorf("比較 = %v", got)
	}

	// console.log には、そのままの桁で出る
	rec := recordLogs(engine)
	run(`console.log(vars.U)`)
	if logs := rec.messages(LogLevelLog); len(logs) == 0 || !strings.Contains(logs[0], "18446744073709551610") {
		t.Errorf("console.log = %q", logs)
	}
}

// fakeClientSender は、送った内容を覚えて決まった結果を返す
type fakeClientSender struct {
	sent []string
}

func (f *fakeClientSender) HasClient(name string) bool { return name == "PLC-A" }
func (f *fakeClientSender) SendCommand(scriptName, client, command string) map[string]any {
	f.sent = append(f.sent, scriptName+"|"+client+"|cmd:"+command)
	return map[string]any{"status": "ok", "values": []any{map[string]any{"label": "D0", "value": "1"}}}
}
func (f *fakeClientSender) SendRequest(scriptName, client, requestHex string) map[string]any {
	f.sent = append(f.sent, scriptName+"|"+client+"|hex:"+requestHex)
	return map[string]any{"status": "ok", "values": []any{}}
}

func TestScriptEngine_Clients(t *testing.T) {
	engine, _ := newTestEngine()
	sender := &fakeClientSender{}
	engine.SetClientSender(sender)

	got, err := engine.RunOnce(`
		const a = clients("PLC-A");
		const r = a.send("速度");
		a.sendHex("0300000001");
		a.name + "," + r.status + "," + r.values[0].value
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "PLC-A,ok,1" {
		t.Errorf("結果 = %v", got)
	}
	want := []string{"1 回実行|PLC-A|cmd:速度", "1 回実行|PLC-A|hex:0300000001"}
	if !reflect.DeepEqual(sender.sent, want) {
		t.Errorf("送った内容 = %q, want %q", sender.sent, want)
	}

	// ない接続先はエラー（送らない）
	if _, err := engine.RunOnce(`clients("Nope").send("速度")`); err == nil || !strings.Contains(err.Error(), `clients: 接続先 "Nope" がありません`) {
		t.Errorf("ない接続先のエラー = %v", err)
	}
	if len(sender.sent) != 2 {
		t.Errorf("ない接続先にも送った: %q", sender.sent)
	}
}

// console.log に構造体・配列の変数を渡すと、中身を出す
func TestScriptEngine_ConsoleLogVars(t *testing.T) {
	engine, vs := newTestEngine()
	if err := vs.RegisterStructType(&variable.StructTypeDef{Name: "Motor", Fields: []variable.StructField{
		{Name: "speed", DataType: variable.TypeINT},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := vs.CreateVariable("M1", variable.DataType("Motor"), map[string]any{"speed": int16(12)}); err != nil {
		t.Fatal(err)
	}
	if _, err := vs.CreateVariable("Values", variable.DataType("ARRAY[1..3] OF INT"), nil); err != nil {
		t.Fatal(err)
	}
	rec := recordLogs(engine)
	if _, err := engine.RunOnce(`console.log(vars.M1, vars.Values)`); err != nil {
		t.Fatal(err)
	}
	logs := rec.messages(LogLevelLog)
	if len(logs) == 0 || !strings.Contains(logs[0], "speed:12") || !strings.Contains(logs[0], "[0 0 0]") {
		t.Errorf("console.log = %q", logs)
	}
}
