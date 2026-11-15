package scripting

import (
	"fmt"
	"strconv"
	"strings"

	"karakuri/internal/domain/variable"

	"github.com/dop251/goja"
)

// スクリプトから使う API。グローバルに 3 つの名前を置く（ほかに console）:
//
//	vars     変数。値はプロパティとして読み書きする（vars.counter += 1n、vars.Motor1.speed = 100、vars.Values[1] = 5、
//	         vars["Sensor" + i]）。あるかは "x" in vars、名前の一覧は Object.keys(vars)。
//	         値以外の操作は呼ぶ形（vars("x").status()・vars("x").read() など。名前に ".field" / "[i]" も書ける）
//	clients  外部の機器（クライアントの接続先）。clients("PLC-A").send("保存したコマンドの名前") / .sendHex("03 00 …")
//	iec      時間・日付の文字列と数値の変換（iec.parseTime("T#1s") など）
//
// 値の決まり: LINT / ULINT は BigInt、ほかの整数・実数は Number、BOOL は boolean、文字列と時間・日付の型は文字列。
// 構造体・配列は、中に入っていける Proxy（読むたびに変数ストアから読み、書くと変数ストアに書く）。
// まるごと普通のオブジェクト・配列で欲しいときは vars("Motor1").read()。
//
// 失敗（ない変数・ないフィールド・範囲外の添字・型に合わない値・64 ビットに収まらない BigInt・ない接続先）は、
// どれもスクリプトのエラー（例外）にする。黙って undefined を返したり、何もしなかったりはしない。

// varsMarker は、vars の Proxy（構造体・配列の中）が自分の場所を返すための、スクリプトからは見えない Symbol
var varsMarker = goja.NewSymbol("karakuri.vars")

// varNode は、vars の中の 1 か所（変数そのもの、または構造体のフィールド・配列の要素）
type varNode struct {
	base string            // 変数の名前
	path string            // 変数の中の場所（".speed"、"[1]"、"[1].speed"。変数そのものなら空）
	dt   variable.DataType // その場所の型
}

func (n varNode) label() string { return n.base + n.path }

// scriptAPI は、1 つの VM に置く API（vars / clients / iec）を作る
type scriptAPI struct {
	e     *ScriptEngine
	vm    *goja.Runtime
	throw func(format string, args ...any)
}

func (e *ScriptEngine) installAPI(vm *goja.Runtime, scriptName string, throw func(format string, args ...any)) {
	a := &scriptAPI{e: e, vm: vm, throw: throw}
	if e.variableStore != nil {
		vm.Set("vars", a.varsRoot())
	}
	vm.Set("clients", a.clientsFunc(scriptName))
	vm.Set("iec", a.iecObject())
}

// --- vars ---

// lookup は変数を返す。なければ、スクリプトのエラーにする
func (a *scriptAPI) lookup(name string) *variable.Variable {
	v, err := a.e.variableStore.GetVariableByName(name)
	if err != nil {
		a.throw("vars: 変数 %q がありません", name)
	}
	return v
}

// read は、場所の値をスクリプトに渡す形で返す（構造体・配列も、普通のオブジェクト・配列で）
func (a *scriptAPI) read(n varNode) any {
	if n.path == "" {
		return toJSCompatibleValue(a.lookup(n.base).Value)
	}
	val, err := a.e.variableStore.ReadFieldValue(n.base, n.path)
	if err != nil {
		a.throw("vars: %s を読めません: %v", n.label(), err)
	}
	return toJSCompatibleValue(val)
}

// write は、場所に値を書く
func (a *scriptAPI) write(n varNode, value goja.Value) {
	v, err := jsToStore(value.Export())
	if err != nil {
		a.throw("vars: %s に書けません: %v", n.label(), err)
	}
	if n.path == "" {
		err = a.e.variableStore.UpdateValueByName(n.base, v)
	} else {
		err = a.e.variableStore.WriteFieldValueByName(n.base, n.path, v)
	}
	if err != nil {
		a.throw("vars: %s に書けません: %v", n.label(), err)
	}
}

// value は、場所の値をプロパティとして返す。構造体・配列なら、中に入っていける Proxy
func (a *scriptAPI) value(n varNode) goja.Value {
	switch {
	case n.dt.IsArrayType():
		return a.arrayProxy(n)
	case n.dt.IsStructType():
		return a.structProxy(n)
	}
	return a.vm.ToValue(a.read(n))
}

// structFields は、構造体の型のフィールドを返す
func (a *scriptAPI) structFields(n varNode) []variable.StructField {
	def, err := a.e.variableStore.GetStructType(string(n.dt))
	if err != nil {
		a.throw("vars: %s の型 %s がありません", n.label(), n.dt)
	}
	return def.Fields
}

func (a *scriptAPI) field(n varNode, name string) (varNode, bool) {
	for _, f := range a.structFields(n) {
		if f.Name == name {
			return varNode{base: n.base, path: n.path + "." + name, dt: f.DataType}, true
		}
	}
	return varNode{}, false
}

// arrayBounds は、配列の型の最初の添字・要素の数・要素の型を返す
func (a *scriptAPI) arrayBounds(n varNode) (lower, size int, elem variable.DataType) {
	lower, _ = variable.ParseArrayLower(n.dt)
	elem, size, err := variable.ParseArrayType(n.dt)
	if err != nil {
		a.throw("vars: %s の型 %s を読めません: %v", n.label(), n.dt, err)
	}
	return lower, size, elem
}

func (a *scriptAPI) element(n varNode, index int) varNode {
	lower, size, elem := a.arrayBounds(n)
	if index < lower || index >= lower+size {
		a.throw("vars: %s の添字 %d は範囲（%d〜%d）の外です", n.label(), index, lower, lower+size-1)
	}
	return varNode{base: n.base, path: fmt.Sprintf("%s[%d]", n.path, index), dt: elem}
}

// dataDescriptor は、Object.keys などに返す、プロパティの説明
func dataDescriptor(v goja.Value) goja.PropertyDescriptor {
	return goja.PropertyDescriptor{Value: v, Writable: goja.FLAG_TRUE, Enumerable: goja.FLAG_TRUE, Configurable: goja.FLAG_TRUE}
}

// ignoredProp は、ない名前でもエラーにしないプロパティ（console.log や Promise などが、黙って調べに来るもの）
func ignoredProp(name string) bool {
	switch name {
	case "toJSON", "then", "toString", "valueOf", "constructor", "inspect":
		return true
	}
	return false
}

// markerValue は、Proxy の場所を返す（console.log が、中身を普通の値で出すために使う）
func (a *scriptAPI) markerValue(n varNode) goja.Value {
	return a.vm.ToValue(func() any { return a.read(n) })
}

// varsRoot は、グローバルの vars（呼べる Proxy）を作る
func (a *scriptAPI) varsRoot() goja.Value {
	vm := a.vm
	target := vm.ToValue(func(goja.FunctionCall) goja.Value { return goja.Undefined() }).ToObject(vm)
	exists := func(name string) (*variable.Variable, bool) {
		v, err := a.e.variableStore.GetVariableByName(name)
		return v, err == nil
	}
	node := func(name string) varNode {
		v := a.lookup(name)
		return varNode{base: name, dt: v.DataType}
	}
	return vm.ToValue(vm.NewProxy(target, &goja.ProxyTrapConfig{
		Get: func(_ *goja.Object, prop string, _ goja.Value) goja.Value {
			if _, ok := exists(prop); !ok && ignoredProp(prop) {
				return goja.Undefined()
			}
			return a.value(node(prop))
		},
		GetSym: func(*goja.Object, *goja.Symbol, goja.Value) goja.Value { return goja.Undefined() },
		Set: func(_ *goja.Object, prop string, value goja.Value, _ goja.Value) bool {
			a.write(node(prop), value)
			return true
		},
		Has: func(_ *goja.Object, prop string) bool {
			_, ok := exists(prop)
			return ok
		},
		OwnKeys: func(*goja.Object) *goja.Object {
			vars := a.e.variableStore.GetAllVariables()
			names := make([]any, len(vars))
			for i, v := range vars {
				names[i] = v.Name
			}
			return vm.NewArray(names...)
		},
		GetOwnPropertyDescriptor: func(_ *goja.Object, prop string) goja.PropertyDescriptor {
			if _, ok := exists(prop); !ok {
				return goja.PropertyDescriptor{}
			}
			return dataDescriptor(a.value(node(prop)))
		},
		DeleteProperty: func(_ *goja.Object, prop string) bool {
			a.throw("vars: 変数 %q は消せません", prop)
			return false
		},
		Apply: func(_ *goja.Object, _ goja.Value, args []goja.Value) goja.Value {
			if len(args) == 0 {
				a.throw("vars(): 変数の名前を渡してください")
			}
			return a.handle(args[0].String())
		},
	}))
}

// structProxy は、構造体の場所の Proxy（フィールドをプロパティとして読み書きする）
func (a *scriptAPI) structProxy(n varNode) goja.Value {
	vm := a.vm
	return vm.ToValue(vm.NewProxy(vm.NewObject(), &goja.ProxyTrapConfig{
		Get: func(_ *goja.Object, prop string, _ goja.Value) goja.Value {
			child, ok := a.field(n, prop)
			if !ok {
				if ignoredProp(prop) {
					return goja.Undefined()
				}
				a.throw("vars: %s にフィールド %q がありません", n.label(), prop)
			}
			return a.value(child)
		},
		GetSym: func(_ *goja.Object, sym *goja.Symbol, _ goja.Value) goja.Value {
			if sym == varsMarker {
				return a.markerValue(n)
			}
			return goja.Undefined()
		},
		Set: func(_ *goja.Object, prop string, value goja.Value, _ goja.Value) bool {
			child, ok := a.field(n, prop)
			if !ok {
				a.throw("vars: %s にフィールド %q がありません", n.label(), prop)
			}
			a.write(child, value)
			return true
		},
		Has: func(_ *goja.Object, prop string) bool {
			_, ok := a.field(n, prop)
			return ok
		},
		OwnKeys: func(*goja.Object) *goja.Object {
			fields := a.structFields(n)
			names := make([]any, len(fields))
			for i, f := range fields {
				names[i] = f.Name
			}
			return vm.NewArray(names...)
		},
		GetOwnPropertyDescriptor: func(_ *goja.Object, prop string) goja.PropertyDescriptor {
			child, ok := a.field(n, prop)
			if !ok {
				return goja.PropertyDescriptor{}
			}
			return dataDescriptor(a.value(child))
		},
	}))
}

// arrayProxy は、配列の場所の Proxy（要素を、型の添字（ARRAY[1..3] なら 1〜3）で読み書きする）。
// length と for...of（[...vars.Values] で普通の配列にもできる）に対応する
func (a *scriptAPI) arrayProxy(n varNode) goja.Value {
	vm := a.vm
	index := func(prop string) (int, bool) {
		i, err := strconv.Atoi(prop)
		return i, err == nil
	}
	inRange := func(i int) bool {
		lower, size, _ := a.arrayBounds(n)
		return i >= lower && i < lower+size
	}
	values := func() []any {
		lower, size, _ := a.arrayBounds(n)
		out := make([]any, size)
		for k := range size {
			out[k] = a.value(a.element(n, lower+k))
		}
		return out
	}
	getIdx := func(i int) goja.Value { return a.value(a.element(n, i)) }
	setIdx := func(i int, value goja.Value) bool {
		a.write(a.element(n, i), value)
		return true
	}
	return vm.ToValue(vm.NewProxy(vm.NewObject(), &goja.ProxyTrapConfig{
		Get: func(_ *goja.Object, prop string, _ goja.Value) goja.Value {
			if prop == "length" {
				_, size, _ := a.arrayBounds(n)
				return vm.ToValue(size)
			}
			if i, ok := index(prop); ok {
				return getIdx(i)
			}
			if ignoredProp(prop) {
				return goja.Undefined()
			}
			a.throw("vars: %s は配列です（%q ではなく、添字で読みます）", n.label(), prop)
			return nil
		},
		GetIdx: func(_ *goja.Object, i int, _ goja.Value) goja.Value { return getIdx(i) },
		GetSym: func(_ *goja.Object, sym *goja.Symbol, _ goja.Value) goja.Value {
			switch sym {
			case varsMarker:
				return a.markerValue(n)
			case goja.SymIterator:
				return vm.ToValue(func(goja.FunctionCall) goja.Value {
					arr := vm.NewArray(values()...)
					iter, _ := goja.AssertFunction(arr.GetSymbol(goja.SymIterator))
					v, err := iter(arr)
					if err != nil {
						panic(err)
					}
					return v
				})
			}
			return goja.Undefined()
		},
		Set: func(_ *goja.Object, prop string, value goja.Value, _ goja.Value) bool {
			i, ok := index(prop)
			if !ok {
				a.throw("vars: %s は配列です（%q ではなく、添字で書きます）", n.label(), prop)
			}
			return setIdx(i, value)
		},
		SetIdx: func(_ *goja.Object, i int, value goja.Value, _ goja.Value) bool { return setIdx(i, value) },
		Has: func(_ *goja.Object, prop string) bool {
			i, ok := index(prop)
			return (ok && inRange(i)) || prop == "length"
		},
		HasIdx: func(_ *goja.Object, i int) bool { return inRange(i) },
		OwnKeys: func(*goja.Object) *goja.Object {
			lower, size, _ := a.arrayBounds(n)
			keys := make([]any, size)
			for k := range size {
				keys[k] = strconv.Itoa(lower + k)
			}
			return vm.NewArray(keys...)
		},
		GetOwnPropertyDescriptor: func(_ *goja.Object, prop string) goja.PropertyDescriptor {
			i, ok := index(prop)
			if !ok || !inRange(i) {
				return goja.PropertyDescriptor{}
			}
			return dataDescriptor(getIdx(i))
		},
		GetOwnPropertyDescriptorIdx: func(_ *goja.Object, i int) goja.PropertyDescriptor {
			if !inRange(i) {
				return goja.PropertyDescriptor{}
			}
			return dataDescriptor(getIdx(i))
		},
	}))
}

// handle は、vars("名前") が返すもの（値以外の操作。名前には ".field" / "[i]" も書ける）
func (a *scriptAPI) handle(name string) goja.Value {
	base, path := splitNameAndPath(name)
	v := a.lookup(base)
	n := varNode{base: base, path: path, dt: v.DataType}
	if path != "" {
		// 場所があるかを、ここで確かめる（打ち間違いにすぐ気づけるように）
		if _, err := a.e.variableStore.ReadFieldValue(base, path); err != nil {
			a.throw("vars(%q): %v", name, err)
		}
	}
	h := a.vm.NewObject()
	_ = h.Set("name", name)
	// read() - 値を普通の値で返す（構造体・配列は、普通のオブジェクト・配列）
	_ = h.Set("read", func() any { return a.read(n) })
	// write(value) - 値を書く
	_ = h.Set("write", func(value goja.Value) { a.write(n, value) })
	// status() - 外部の機器（クライアントの接続先）にマッピングした変数の同期の状態。
	// { state, writePending, error, client }。接続先にマッピングしていなければ null
	_ = h.Set("status", func() any {
		a.e.mu.Lock()
		statusOf := a.e.statusOf
		a.e.mu.Unlock()
		if statusOf == nil {
			return nil
		}
		if st := statusOf(v.ID); st != nil {
			return st
		}
		return nil
	})
	return h
}

// plainValue は、console.log に渡された値が vars の Proxy なら、中身を普通の値にして返す
func plainValue(v goja.Value) any {
	if obj, ok := v.(*goja.Object); ok {
		if read, ok := goja.AssertFunction(obj.GetSymbol(varsMarker)); ok {
			if out, err := read(goja.Undefined()); err == nil {
				return out.Export()
			}
		}
	}
	return v.Export()
}

// --- clients ---

// clientsFunc は、グローバルの clients（clients("接続先の名前") で、接続先の操作を返す）を作る
func (a *scriptAPI) clientsFunc(scriptName string) func(string) goja.Value {
	return func(name string) goja.Value {
		s := a.e.getClientSender()
		if s == nil || !s.HasClient(name) {
			a.throw("clients: 接続先 %q がありません", name)
		}
		c := a.vm.NewObject()
		_ = c.Set("name", name)
		// send(command) - 保存したコマンドを、名前で指定して送る
		_ = c.Set("send", func(command string) any { return s.SendCommand(scriptName, name, command) })
		// sendHex(hex) - 16 進で指定したリクエストを送る
		_ = c.Set("sendHex", func(requestHex string) any { return s.SendRequest(scriptName, name, requestHex) })
		return c
	}
}

// --- iec ---

// iecObject は、時間・日付の文字列と数値の変換（読めない文字列はエラー）
func (a *scriptAPI) iecObject() *goja.Object {
	o := a.vm.NewObject()
	parse := func(fn string, s string, f func(string) (int64, error)) int64 {
		v, err := f(s)
		if err != nil {
			a.throw("iec.%s(%q): %v", fn, s, err)
		}
		return v
	}
	_ = o.Set("parseTime", func(s string) int64 {
		return parse("parseTime", s, func(s string) (int64, error) { v, err := variable.ParseTIME(s); return int64(v), err })
	})
	_ = o.Set("formatTime", func(ms int64) string { return variable.FormatTIME(int32(ms)) })
	_ = o.Set("parseDate", func(s string) int64 {
		return parse("parseDate", s, func(s string) (int64, error) { v, err := variable.ParseDATE(s); return int64(v), err })
	})
	_ = o.Set("formatDate", func(sec int64) string { return variable.FormatDATE(uint64(sec)) })
	_ = o.Set("parseTimeOfDay", func(s string) int64 {
		return parse("parseTimeOfDay", s, func(s string) (int64, error) { v, err := variable.ParseTIME_OF_DAY(s); return int64(v), err })
	})
	_ = o.Set("formatTimeOfDay", func(ms int64) string { return variable.FormatTIME_OF_DAY(uint32(ms)) })
	_ = o.Set("parseDateAndTime", func(s string) int64 {
		return parse("parseDateAndTime", s, func(s string) (int64, error) { v, err := variable.ParseDATE_AND_TIME(s); return int64(v), err })
	})
	_ = o.Set("formatDateAndTime", func(sec int64) string { return variable.FormatDATE_AND_TIME(uint64(sec)) })
	return o
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
