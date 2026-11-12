package scripting

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"karakuri/internal/domain/script"
	"karakuri/internal/domain/variable"

	"github.com/dop251/goja"
)

// jsMaxSafeInt はJavaScriptのNumber型が正確に表現できる整数の最大値（2^53）
const jsMaxSafeInt = int64(1) << 53 // 9007199254740992

const maxConsoleLogs = 500

// コンソールログの種類
const (
	LogLevelLog   = "log"   // console.log() の出力、1 回実行の結果
	LogLevelWarn  = "warn"  // 警告（メッセージの先頭に [WARN] が付く）
	LogLevelError = "error" // スクリプトのエラー（メッセージの先頭に [ERROR] が付く）
)

// OnceScriptName は、1 回実行の出力に付けるスクリプト名
const OnceScriptName = "1 回実行"

// ConsoleLogEntry はコンソールログの1エントリ
type ConsoleLogEntry struct {
	ScriptID   string
	ScriptName string
	Level      string
	Message    string
	At         time.Time
}

// ScriptEngine はJavaScriptスクリプトを実行するエンジン
type ScriptEngine struct {
	mu            sync.Mutex
	variableStore *variable.VariableStore
	scripts       map[string]*runningScript
	consoleLogs   []ConsoleLogEntry
	onLogAdded    func(ConsoleLogEntry)
	// statusOf は、外部の機器にマッピングした変数の同期の状態を返す（plc.getVariableStatus 用）。
	// 変数 ID を受け取り、マッピングしていなければ nil を返す
	statusOf func(variableID string) map[string]any
	// clientSender は外部の機器へコマンドを送る（plc.sendCommand / plc.sendRequest 用）
	clientSender ClientSender
}

// ClientSender は、スクリプトから外部の機器（クライアントの接続先）へコマンドを送るためのもの（plc.sendCommand / plc.sendRequest 用）。
// 戻り値は JavaScript のオブジェクトにする内容（status・error・values など）。見つからない・送れないときも、
// 例外ではなく status が "error" の内容を返す。scriptName は履歴に出すスクリプトの名前
type ClientSender interface {
	SendCommand(scriptName, client, command string) map[string]any
	SendRequest(scriptName, client, requestHex string) map[string]any
}

// SetClientSender は plc.sendCommand() / plc.sendRequest() が使う送り先を設定する
func (e *ScriptEngine) SetClientSender(s ClientSender) {
	e.mu.Lock()
	e.clientSender = s
	e.mu.Unlock()
}

func (e *ScriptEngine) getClientSender() ClientSender {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.clientSender
}

// SetVariableStatusProvider は plc.getVariableStatus() が使う、変数の同期の状態を返す関数を設定する
func (e *ScriptEngine) SetVariableStatusProvider(fn func(variableID string) map[string]any) {
	e.mu.Lock()
	e.statusOf = fn
	e.mu.Unlock()
}

type runningScript struct {
	script *script.Script
	cancel context.CancelFunc
	vm     *goja.Runtime
}

// NewScriptEngine は新しいスクリプトエンジンを作成する
func NewScriptEngine(varStore *variable.VariableStore) *ScriptEngine {
	return &ScriptEngine{
		variableStore: varStore,
		scripts:       make(map[string]*runningScript),
	}
}

// SetOnLogAdded はコンソールログ追加時のコールバックを設定する
func (e *ScriptEngine) SetOnLogAdded(cb func(ConsoleLogEntry)) {
	e.mu.Lock()
	e.onLogAdded = cb
	e.mu.Unlock()
}

// addLog はコンソールログに 1 件追加し、コールバックで通知する（mu をロックしていない状態で呼ぶ）
func (e *ScriptEngine) addLog(scriptID, scriptName, level, message string) {
	switch level {
	case LogLevelWarn:
		message = "[WARN] " + message
	case LogLevelError:
		message = "[ERROR] " + message
	}
	fmt.Printf("[%s] %s\n", scriptName, message)
	entry := ConsoleLogEntry{
		ScriptID:   scriptID,
		ScriptName: scriptName,
		Level:      level,
		Message:    message,
		At:         time.Now(),
	}
	e.mu.Lock()
	e.consoleLogs = append(e.consoleLogs, entry)
	if len(e.consoleLogs) > maxConsoleLogs {
		e.consoleLogs = e.consoleLogs[len(e.consoleLogs)-maxConsoleLogs:]
	}
	cb := e.onLogAdded
	e.mu.Unlock()
	if cb != nil {
		cb(entry)
	}
}

// createVM は新しいJavaScript VMを作成し、変数アクセス関数を登録する
func (e *ScriptEngine) createVM(scriptID, scriptName string) *goja.Runtime {
	vm := goja.New()

	// コンソールオブジェクト
	console := vm.NewObject()
	console.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, arg := range call.Arguments {
			parts[i] = fmt.Sprintf("%v", arg.Export())
		}
		e.addLog(scriptID, scriptName, LogLevelLog, strings.Join(parts, " "))
		return goja.Undefined()
	})
	vm.Set("console", console)

	// PLCオブジェクト - 変数アクセス用
	plc := vm.NewObject()

	// sendCommand(client, command) - 保存したコマンドを、接続先とコマンドの名前で指定して送る。
	// sendRequest(client, hex) - 16 進で指定したリクエストを送る。
	// どちらも応答（またはタイムアウト）まで待ち、{ status, error, values, sent, received, elapsedMs, ... } を返す
	noSender := map[string]any{"status": "error", "error": "外部の機器へ送る機能が使えません"}
	plc.Set("sendCommand", func(client, command string) any {
		s := e.getClientSender()
		if s == nil {
			return noSender
		}
		return s.SendCommand(scriptName, client, command)
	})
	plc.Set("sendRequest", func(client, requestHex string) any {
		s := e.getClientSender()
		if s == nil {
			return noSender
		}
		return s.SendRequest(scriptName, client, requestHex)
	})

	// addConsoleWarn はコンソールログに警告を追加するヘルパー
	addConsoleWarn := func(msg string) {
		e.addLog(scriptID, scriptName, LogLevelWarn, msg)
	}

	if e.variableStore != nil {
		// readVariable(name) - 変数名で値を読む
		// name は "VarName", "Array[2]", "Struct.field", "Array[1].field" など
		plc.Set("readVariable", func(name string) any {
			baseName, fieldPath := splitNameAndPath(name)
			if fieldPath != "" {
				val, err := e.variableStore.ReadFieldValue(baseName, fieldPath)
				if err != nil {
					return nil
				}
				return toJSCompatibleValue(val)
			}
			v, err := e.variableStore.GetVariableByName(name)
			if err != nil {
				return nil
			}
			// LINT/ULINT が safe integer 範囲を超えた場合は精度損失の警告を出す
			switch val := v.Value.(type) {
			case int64:
				if val > jsMaxSafeInt || val < -jsMaxSafeInt {
					addConsoleWarn(fmt.Sprintf(
						"readVariable('%s'): LINT値 %d はJSのsafe integer範囲(±2^53)を超えています。精度損失が発生します。plc.readLintBig() を使用してください。",
						name, val))
				}
			case uint64:
				if val > uint64(jsMaxSafeInt) {
					addConsoleWarn(fmt.Sprintf(
						"readVariable('%s'): ULINT値 %d はJSのsafe integer範囲(2^53)を超えています。精度損失が発生します。plc.readUlintBig() を使用してください。",
						name, val))
				}
			}
			// gojaがJavaScript numberとして扱えるようGoの標準型に変換
			return toJSCompatibleValue(v.Value)
		})

		// getVariableStatus(name) - 外部の機器（クライアントの接続先）にマッピングした変数の同期の状態。
		// { state: "ok" | "comm_error" | "invalid" | "stopped" | "", writePending: bool, error: string, client: 接続先の名前 }
		// を返す。マッピングしていない変数・ない変数は null
		plc.Set("getVariableStatus", func(name string) any {
			v, err := e.variableStore.GetVariableByName(name)
			if err != nil {
				return nil
			}
			e.mu.Lock()
			statusOf := e.statusOf
			e.mu.Unlock()
			if statusOf == nil {
				return nil
			}
			st := statusOf(v.ID)
			if st == nil {
				return nil
			}
			return st
		})

		// writeVariable(name, value) - 変数名で値を書く
		// name は "VarName", "Array[2]", "Struct.field", "Array[1].field" など
		plc.Set("writeVariable", func(name string, value any) {
			baseName, fieldPath := splitNameAndPath(name)
			if fieldPath != "" {
				e.variableStore.WriteFieldValueByName(baseName, fieldPath, value)
				return
			}
			e.variableStore.UpdateValueByName(name, value)
		})

		// readArrayElement(name, index) - 配列要素読み込み（外部インデックス：表示ベース）
		// 例: ARRAY[1..10] の場合、index=1 が最初の要素
		plc.Set("readArrayElement", func(name string, index int) any {
			val, err := e.variableStore.ReadArrayElement(name, index)
			if err != nil {
				return nil
			}
			return toJSCompatibleValue(val)
		})

		// writeArrayElement(name, index, value) - 配列要素書き込み（外部インデックス：表示ベース）
		// 例: ARRAY[1..10] の場合、index=1 が最初の要素
		plc.Set("writeArrayElement", func(name string, index int, value any) {
			e.variableStore.WriteArrayElement(name, index, value)
		})

		// readStructField(name, fieldName) - 構造体フィールド読み込み
		plc.Set("readStructField", func(name string, fieldName string) any {
			v, err := e.variableStore.GetVariableByName(name)
			if err != nil {
				return nil
			}
			m, ok := v.Value.(map[string]any)
			if !ok {
				return nil
			}
			val, exists := m[fieldName]
			if !exists {
				return nil
			}
			return toJSCompatibleValue(val)
		})

		// writeStructField(name, fieldName, value) - 構造体フィールド書き込み
		plc.Set("writeStructField", func(name string, fieldName string, value any) {
			v, err := e.variableStore.GetVariableByName(name)
			if err != nil {
				return
			}
			m, ok := v.Value.(map[string]any)
			if !ok {
				return
			}
			newMap := make(map[string]any)
			for k, val := range m {
				newMap[k] = val
			}
			newMap[fieldName] = value
			e.variableStore.UpdateValueByName(name, newMap)
		})

		// getVariables() - 全変数名一覧を取得
		plc.Set("getVariables", func() []string {
			vars := e.variableStore.GetAllVariables()
			names := make([]string, len(vars))
			for i, v := range vars {
				names[i] = v.Name
			}
			return names
		})
	}

	// LINT/ULINT BigInt API（精度損失なく64ビット整数を読み書きするための専用関数）
	// JavaScriptのBigIntリテラル（例: 9007199254740993n）を使った演算が可能

	if e.variableStore != nil {
		// readLintBig(name) -> BigInt - LINT変数をBigIntとして読む（精度損失なし）
		plc.Set("readLintBig", func(name string) goja.Value {
			v, err := e.variableStore.GetVariableByName(name)
			if err != nil {
				return vm.ToValue(nil)
			}
			val, ok := v.Value.(int64)
			if !ok {
				return vm.ToValue(nil)
			}
			return vm.ToValue(new(big.Int).SetInt64(val))
		})

		// writeLintBig(name, value) - BigIntまたはNumberをLINT変数に書く（精度損失なし）
		// 例: plc.writeLintBig("myVar", plc.readLintBig("myVar") + 1n)
		plc.Set("writeLintBig", func(name string, val goja.Value) {
			var int64Val int64
			if goja.IsBigInt(val) {
				if bigInt, ok := val.Export().(*big.Int); ok {
					int64Val = bigInt.Int64()
				}
			} else {
				int64Val = val.ToInteger()
			}
			e.variableStore.UpdateValueByName(name, int64Val)
		})

		// readUlintBig(name) -> BigInt - ULINT変数をBigIntとして読む（精度損失なし）
		plc.Set("readUlintBig", func(name string) goja.Value {
			v, err := e.variableStore.GetVariableByName(name)
			if err != nil {
				return vm.ToValue(nil)
			}
			val, ok := v.Value.(uint64)
			if !ok {
				return vm.ToValue(nil)
			}
			return vm.ToValue(new(big.Int).SetUint64(val))
		})

		// writeUlintBig(name, value) - BigIntまたはNumberをULINT変数に書く（精度損失なし）
		// 例: plc.writeUlintBig("myVar", plc.readUlintBig("myVar") + 1n)
		plc.Set("writeUlintBig", func(name string, val goja.Value) {
			var uint64Val uint64
			if goja.IsBigInt(val) {
				if bigInt, ok := val.Export().(*big.Int); ok {
					if bigInt.Sign() >= 0 {
						uint64Val = bigInt.Uint64()
					}
				}
			} else {
				f := val.ToFloat()
				if f >= 0 {
					uint64Val = uint64(f)
				}
			}
			e.variableStore.UpdateValueByName(name, uint64Val)
		})
	}

	// TIME/DATE型シンタックスシュガー（変数の読み書きをワンステップで）

	if e.variableStore != nil {
		// readTimeMs(name) -> ミリ秒(number)
		plc.Set("readTimeMs", func(name string) any {
			v, err := e.variableStore.GetVariableByName(name)
			if err != nil {
				return nil
			}
			s, ok := v.Value.(string)
			if !ok {
				return nil
			}
			ms, err := variable.ParseTIME(s)
			if err != nil {
				return nil
			}
			return int64(ms)
		})
		// writeTimeMs(name, ms)
		plc.Set("writeTimeMs", func(name string, ms int64) {
			e.variableStore.UpdateValueByName(name, variable.FormatTIME(int32(ms)))
		})

		// readDateSec(name) -> Unix秒(number)
		plc.Set("readDateSec", func(name string) any {
			v, err := e.variableStore.GetVariableByName(name)
			if err != nil {
				return nil
			}
			s, ok := v.Value.(string)
			if !ok {
				return nil
			}
			sec, err := variable.ParseDATE(s)
			if err != nil {
				return nil
			}
			return int64(sec)
		})
		// writeDateSec(name, sec)
		plc.Set("writeDateSec", func(name string, sec int64) {
			e.variableStore.UpdateValueByName(name, variable.FormatDATE(uint64(sec)))
		})

		// readTimeOfDayMs(name) -> ミリ秒(number)
		plc.Set("readTimeOfDayMs", func(name string) any {
			v, err := e.variableStore.GetVariableByName(name)
			if err != nil {
				return nil
			}
			s, ok := v.Value.(string)
			if !ok {
				return nil
			}
			ms, err := variable.ParseTIME_OF_DAY(s)
			if err != nil {
				return nil
			}
			return int64(ms)
		})
		// writeTimeOfDayMs(name, ms)
		plc.Set("writeTimeOfDayMs", func(name string, ms int64) {
			e.variableStore.UpdateValueByName(name, variable.FormatTIME_OF_DAY(uint32(ms)))
		})

		// readDateAndTimeSec(name) -> Unix秒(number)
		plc.Set("readDateAndTimeSec", func(name string) any {
			v, err := e.variableStore.GetVariableByName(name)
			if err != nil {
				return nil
			}
			s, ok := v.Value.(string)
			if !ok {
				return nil
			}
			sec, err := variable.ParseDATE_AND_TIME(s)
			if err != nil {
				return nil
			}
			return int64(sec)
		})
		// writeDateAndTimeSec(name, sec)
		plc.Set("writeDateAndTimeSec", func(name string, sec int64) {
			e.variableStore.UpdateValueByName(name, variable.FormatDATE_AND_TIME(uint64(sec)))
		})
	}

	// TIME/DATE型ユーティリティ（文字列⇔数値変換のみ）

	// parseTime("T#1h30m45s") -> ミリ秒(number)
	plc.Set("parseTime", func(s string) any {
		ms, err := variable.ParseTIME(s)
		if err != nil {
			return nil
		}
		return int64(ms)
	})
	// formatTime(ms) -> "T#1h30m45s"
	plc.Set("formatTime", func(ms int64) string {
		return variable.FormatTIME(int32(ms))
	})

	// parseDate("D#2024-01-01") -> Unix秒(number)
	plc.Set("parseDate", func(s string) any {
		sec, err := variable.ParseDATE(s)
		if err != nil {
			return nil
		}
		return int64(sec)
	})
	// formatDate(sec) -> "D#2024-01-01"
	plc.Set("formatDate", func(sec int64) string {
		return variable.FormatDATE(uint64(sec))
	})

	// parseTimeOfDay("TOD#12:30:15") -> ミリ秒(number)
	plc.Set("parseTimeOfDay", func(s string) any {
		ms, err := variable.ParseTIME_OF_DAY(s)
		if err != nil {
			return nil
		}
		return int64(ms)
	})
	// formatTimeOfDay(ms) -> "TOD#12:30:15"
	plc.Set("formatTimeOfDay", func(ms int64) string {
		return variable.FormatTIME_OF_DAY(uint32(ms))
	})

	// parseDateAndTime("DT#2024-01-01-12:30:15") -> Unix秒(number)
	plc.Set("parseDateAndTime", func(s string) any {
		sec, err := variable.ParseDATE_AND_TIME(s)
		if err != nil {
			return nil
		}
		return int64(sec)
	})
	// formatDateAndTime(sec) -> "DT#2024-01-01-12:30:15"
	plc.Set("formatDateAndTime", func(sec int64) string {
		return variable.FormatDATE_AND_TIME(uint64(sec))
	})

	vm.Set("plc", plc)

	return vm
}

// splitNameAndPath は "Test[2]" → ("Test", "[2]") や "Test.field" → ("Test", ".field") に分割する。
// パスがない場合は (name, "") を返す。
func splitNameAndPath(name string) (baseName, fieldPath string) {
	idx := strings.IndexAny(name, "[.")
	if idx < 0 {
		return name, ""
	}
	return name[:idx], name[idx:]
}

// toJSCompatibleValue はGoの型をgojaが扱えるJavaScript互換の型に変換する
// int8/int16/int32 → int64, uint8/uint16/uint32 → int64, float32 → float64
// int64/uint64はそのまま返す（JavaScriptのNumber精度: ±2^53以内なら正確）
func toJSCompatibleValue(value any) any {
	switch v := value.(type) {
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case int64:
		return v // LINT型（2^53超の値はJSで精度損失あり）
	case uint64:
		return v // ULINT型（2^53超の値はJSで精度損失あり）
	case float32:
		return float64(v)
	default:
		return value
	}
}

// StartScript はスクリプトを開始する。
// コンパイルできなければ、エラーを返し、コンソールにも出す。
// 実行中のエラーはコンソールに出す（同じエラーが続く間は、最初の 1 回だけ）。
func (e *ScriptEngine) StartScript(s *script.Script) error {
	err := e.startScript(s)
	if err != nil {
		e.addLog(s.ID, s.Name, LogLevelError, err.Error())
	}
	return err
}

func (e *ScriptEngine) startScript(s *script.Script) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 既に実行中の場合は停止
	if existing, ok := e.scripts[s.ID]; ok {
		existing.cancel()
		delete(e.scripts, s.ID)
	}

	vm := e.createVM(s.ID, s.Name)

	// スクリプトをIIFEでラップしてコンパイル（const/letの再宣言エラーを防止）
	wrappedCode := "(function(){\n" + s.Code + "\n})();"
	program, err := goja.Compile(s.Name, wrappedCode, false)
	if err != nil {
		return fmt.Errorf("スクリプトをコンパイルできません: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	rs := &runningScript{
		script: s,
		cancel: cancel,
		vm:     vm,
	}
	e.scripts[s.ID] = rs

	// 周期実行ゴルーチン
	// s は呼び出し側が書き換えることがある（スクリプトの保存）。ゴルーチンには、開始した時点の値を渡す
	id, name, interval := s.ID, s.Name, s.Interval
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// 直前の実行のエラー（成功なら空）。同じエラーが続く間は、コンソールに出し直さない
		lastErr := ""
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				errMsg := runProgram(vm, program)
				if ctx.Err() != nil {
					return // 実行中に停止された
				}
				if errMsg != "" && errMsg != lastErr {
					e.addLog(id, name, LogLevelError, errMsg)
				}
				lastErr = errMsg
			}
		}
	}()

	return nil
}

// runProgram はプログラムを 1 回実行し、エラーの内容を返す（成功なら空）。panic もエラーとして返す
func runProgram(vm *goja.Runtime, program *goja.Program) (errMsg string) {
	defer func() {
		if r := recover(); r != nil {
			errMsg = fmt.Sprintf("panic: %v", r)
		}
	}()
	if _, err := vm.RunProgram(program); err != nil {
		return err.Error()
	}
	return ""
}

// StopScript はスクリプトを停止する
func (e *ScriptEngine) StopScript(scriptID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	rs, ok := e.scripts[scriptID]
	if !ok {
		return fmt.Errorf("script not found: %s", scriptID)
	}

	rs.cancel()
	delete(e.scripts, scriptID)
	return nil
}

// StopAll は全てのスクリプトを停止する
func (e *ScriptEngine) StopAll() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for id, rs := range e.scripts {
		rs.cancel()
		delete(e.scripts, id)
	}
}

// IsRunning はスクリプトが実行中かどうかを返す
func (e *ScriptEngine) IsRunning(scriptID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.scripts[scriptID]
	return ok
}

// GetRunningScripts は実行中のスクリプトIDのリストを返す
func (e *ScriptEngine) GetRunningScripts() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	ids := make([]string, 0, len(e.scripts))
	for id := range e.scripts {
		ids = append(ids, id)
	}
	return ids
}

// GetConsoleLogs はコンソールログの一覧を返す
func (e *ScriptEngine) GetConsoleLogs() []ConsoleLogEntry {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]ConsoleLogEntry, len(e.consoleLogs))
	copy(result, e.consoleLogs)
	return result
}

// ClearConsoleLogs はコンソールログをクリアする
func (e *ScriptEngine) ClearConsoleLogs() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consoleLogs = nil
}

// RunOnce はコードを 1 回だけ実行し、最後の式の値を返す（保存も周期実行もしない）。
// 結果とエラーは、コンソールにも OnceScriptName の名前で出す。
//
// 周期実行のスクリプトは関数で包んで実行するので、トップレベルに return を書ける。
// 同じコードを 1 回実行でも動かせるよう、そのままコンパイルできなければ関数で包んで実行する。
func (e *ScriptEngine) RunOnce(code string) (any, error) {
	result, err := e.runOnce(code)
	switch {
	case err != nil:
		e.addLog("", OnceScriptName, LogLevelError, err.Error())
	case result != nil:
		e.addLog("", OnceScriptName, LogLevelLog, fmt.Sprintf("=> %v", result))
	default:
		e.addLog("", OnceScriptName, LogLevelLog, "完了")
	}
	return result, err
}

func (e *ScriptEngine) runOnce(code string) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("panic: %v", r)
		}
	}()

	program, err := goja.Compile(OnceScriptName, code, false)
	if err != nil {
		wrapped, wrapErr := goja.Compile(OnceScriptName, "(function(){\n"+code+"\n})();", false)
		if wrapErr != nil {
			return nil, err // 包んでもコンパイルできないなら、元のエラーを返す
		}
		program = wrapped
	}

	value, err := e.createVM("", OnceScriptName).RunProgram(program)
	if err != nil {
		return nil, err
	}
	return value.Export(), nil
}
