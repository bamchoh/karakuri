package scripting

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"sync"
	"time"

	"karakuri/internal/domain/script"
	"karakuri/internal/domain/variable"

	"github.com/dop251/goja"
)

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
	// onRunStateChanged は、周期実行のスクリプトでエラーが起き始めたとき・解消したときに呼ぶ（SetOnRunStateChanged）
	onRunStateChanged func(scriptID, scriptName, errMsg string)
	// statusOf は、外部の機器にマッピングした変数の同期の状態を返す（vars("x").status() 用）。
	// 変数 ID を受け取り、マッピングしていなければ nil を返す
	statusOf func(variableID string) map[string]any
	// clientSender は外部の機器へコマンドを送る（clients("名前").send() / .sendHex() 用）
	clientSender ClientSender
}

// ClientSender は、スクリプトから外部の機器（クライアントの接続先）へコマンドを送るためのもの（clients("名前") 用）。
// 戻り値は JavaScript のオブジェクトにする内容（status・error・values など）。見つからない・送れないときも、
// 例外ではなく status が "error" の内容を返す。scriptName は履歴に出すスクリプトの名前
type ClientSender interface {
	SendCommand(scriptName, client, command string) map[string]any
	SendRequest(scriptName, client, requestHex string) map[string]any
	// HasClient は、その名前の接続先があるか（clients("名前") で、打ち間違いにすぐ気づけるように）
	HasClient(name string) bool
	// SetPolling は、接続先のポーリングを開始・停止する（停止は、送信中のリクエストが終わるまで待つ）
	SetPolling(client string, running bool) error
	// IsPolling は、接続先がポーリング中か
	IsPolling(client string) (bool, error)
}

// SetClientSender は clients("名前").send() / .sendHex() が使う送り先を設定する
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

// SetVariableStatusProvider は vars("x").status() が使う、変数の同期の状態を返す関数を設定する
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
			parts[i] = fmt.Sprintf("%v", plainValue(arg)) // vars の構造体・配列は、中身を普通の値で出す
		}
		e.addLog(scriptID, scriptName, LogLevelLog, strings.Join(parts, " "))
		return goja.Undefined()
	})
	vm.Set("console", console)

	// throw は、スクリプトのエラー（例外）を投げる。スクリプトの try / catch で受け取れる
	throw := func(format string, args ...any) {
		errObj, err := vm.New(vm.Get("Error"), vm.ToValue(fmt.Sprintf(format, args...)))
		if err != nil {
			panic(vm.NewGoError(fmt.Errorf(format, args...)))
		}
		panic(errObj)
	}

	// vars（変数）・clients（外部の機器）・iec（時間・日付の変換）。中身は api.go
	e.installAPI(vm, scriptName, throw)

	return vm
}

// toJSCompatibleValue は、変数ストアの値を、スクリプトに渡す値にする。
//   - LINT（int64）・ULINT（uint64）は BigInt（*big.Int）。Number では ±2^53 を超えると桁が落ちるため
//   - ほかの整数（int8〜int32・uint8〜uint32）は Number（int64 で渡す）、REAL（float32）は Number（float64）
//   - 構造体（map）・配列（slice）は、中の値も同じように変換した新しいものにする
func toJSCompatibleValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, f := range v {
			out[k] = toJSCompatibleValue(f)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, el := range v {
			out[i] = toJSCompatibleValue(el)
		}
		return out
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
		return new(big.Int).SetInt64(v) // LINT
	case uint64:
		return new(big.Int).SetUint64(v) // ULINT
	case float32:
		return float64(v)
	default:
		return value
	}
}

// jsToStore は、スクリプトから書く値を、変数ストアが受け付ける形にする。
// BigInt（*big.Int）は int64 か uint64 にする（どちらにも収まらなければエラー）。構造体・配列の中も同じ
func jsToStore(value any) (any, error) {
	switch v := value.(type) {
	case *big.Int:
		switch {
		case v.IsInt64():
			return v.Int64(), nil
		case v.IsUint64():
			return v.Uint64(), nil
		}
		return nil, fmt.Errorf("BigInt %s は 64 ビットの整数の範囲（-9223372036854775808〜18446744073709551615）を超えています", v.String())
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, f := range v {
			c, err := jsToStore(f)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = c
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, el := range v {
			c, err := jsToStore(el)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out[i] = c
		}
		return out, nil
	}
	return value, nil
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
				// エラーが起き始めたときと、解消したときに知らせる（別のエラーに変わっただけなら知らせない）
				if (errMsg != "") != (lastErr != "") {
					e.notifyRunState(id, name, errMsg)
				}
				lastErr = errMsg
			}
		}
	}()

	return nil
}

// SetOnRunStateChanged は、周期実行のスクリプトで、エラーが起き始めたとき（errMsg がエラーの内容）と
// 解消したとき（errMsg が空）に呼ぶ関数を設定する。エラーが続いている間は呼ばない
func (e *ScriptEngine) SetOnRunStateChanged(cb func(scriptID, scriptName, errMsg string)) {
	e.mu.Lock()
	e.onRunStateChanged = cb
	e.mu.Unlock()
}

func (e *ScriptEngine) notifyRunState(scriptID, scriptName, errMsg string) {
	e.mu.Lock()
	cb := e.onRunStateChanged
	e.mu.Unlock()
	if cb != nil {
		cb(scriptID, scriptName, errMsg)
	}
}

// runProgram はプログラムを 1 回実行し、エラーの内容を返す（成功なら空）。panic もエラーとして返す
func runProgram(vm *goja.Runtime, program *goja.Program) (errMsg string) {
	defer func() {
		if r := recover(); r != nil {
			errMsg = fmt.Sprintf("panic: %v", r)
		}
	}()
	if _, err := vm.RunProgram(program); err != nil {
		return errorText(err)
	}
	return ""
}

// nativeFrame は、エラーの文面に付く、Go の関数の呼び出し位置（スクリプトを書く人には意味がない）
var nativeFrame = regexp.MustCompile(` at karakuri/\S+ \(native\)`)

// errorText は、スクリプトのエラーをコンソールに出す文面にする
func errorText(err error) string {
	return nativeFrame.ReplaceAllString(err.Error(), "")
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
		e.addLog("", OnceScriptName, LogLevelError, errorText(err))
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
