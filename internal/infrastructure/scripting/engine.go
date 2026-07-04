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

// ConsoleLogEntry はconsole.logの1エントリ
type ConsoleLogEntry struct {
	ScriptID   string
	ScriptName string
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
}

type runningScript struct {
	script    *script.Script
	cancel    context.CancelFunc
	vm        *goja.Runtime
	lastError string
	errorAt   time.Time
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
		message := strings.Join(parts, " ")
		fmt.Printf("[%s] %s\n", scriptName, message)
		entry := ConsoleLogEntry{
			ScriptID:   scriptID,
			ScriptName: scriptName,
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
			go cb(entry)
		}
		return goja.Undefined()
	})
	vm.Set("console", console)

	// PLCオブジェクト - 変数アクセス用
	plc := vm.NewObject()

	// addConsoleWarn はコンソールログに警告を追加するヘルパー
	addConsoleWarn := func(msg string) {
		fmt.Printf("[WARN][%s] %s\n", scriptName, msg)
		entry := ConsoleLogEntry{
			ScriptID:   scriptID,
			ScriptName: scriptName,
			Message:    "[WARN] " + msg,
			At:         time.Now(),
		}
		e.mu.Lock()
		e.consoleLogs = append(e.consoleLogs, entry)
		if len(e.consoleLogs) > maxConsoleLogs {
			e.consoleLogs = e.consoleLogs[len(e.consoleLogs)-maxConsoleLogs:]
		}
		e.mu.Unlock()
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

// StartScript はスクリプトを開始する
func (e *ScriptEngine) StartScript(s *script.Script) error {
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
		return fmt.Errorf("failed to compile script: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	rs := &runningScript{
		script: s,
		cancel: cancel,
		vm:     vm,
	}
	e.scripts[s.ID] = rs

	// 周期実行ゴルーチン
	go func() {
		ticker := time.NewTicker(s.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				func() {
					defer func() {
						if r := recover(); r != nil {
							errMsg := fmt.Sprintf("panic: %v", r)
							fmt.Printf("Script %s panicked: %v\n", s.Name, r)
							e.mu.Lock()
							if cur, ok := e.scripts[s.ID]; ok {
								cur.lastError = errMsg
								cur.errorAt = time.Now()
							}
							e.mu.Unlock()
						}
					}()
					_, runErr := vm.RunProgram(program)
					if runErr != nil {
						fmt.Printf("Script %s error: %v\n", s.Name, runErr)
						e.mu.Lock()
						if cur, ok := e.scripts[s.ID]; ok {
							cur.lastError = runErr.Error()
							cur.errorAt = time.Now()
						}
						e.mu.Unlock()
					}
				}()
			}
		}
	}()

	return nil
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

// GetLastError はスクリプトの最新エラー情報を返す
func (e *ScriptEngine) GetLastError(scriptID string) (string, time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	rs, ok := e.scripts[scriptID]
	if !ok {
		return "", time.Time{}
	}
	return rs.lastError, rs.errorAt
}

// ClearError はスクリプトのエラー情報をクリアする
func (e *ScriptEngine) ClearError(scriptID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if rs, ok := e.scripts[scriptID]; ok {
		rs.lastError = ""
		rs.errorAt = time.Time{}
	}
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

// RunOnce はスクリプトを1回だけ実行する（テスト用）
func (e *ScriptEngine) RunOnce(code string) (any, error) {
	vm := e.createVM("", "テスト実行")
	result, err := vm.RunString(code)
	if err != nil {
		return nil, err
	}
	return result.Export(), nil
}
