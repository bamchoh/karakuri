package application

import (
	"strings"
	"testing"
)

// appLogOf は、行き先が target のログを「種類: メッセージ」の形の行にして返す
func appLogOf(svc *PLCService, target string) []string {
	var lines []string
	for _, e := range svc.GetAppLogs() {
		if e.Target == target {
			lines = append(lines, e.Level+": "+e.Message)
		}
	}
	return lines
}

func hasLine(lines []string, part string) bool {
	for _, l := range lines {
		if strings.Contains(l, part) {
			return true
		}
	}
	return false
}

// スクリプトの開始・停止（理由つき）と、エラーの起き始め・解消が、アプリ全体のログに出る。
// console.log の出力や、同じエラーの繰り返しは出ない
func TestAppLog_Scripts(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.CreateVariable("Flag", "INT", 0); err != nil {
		t.Fatal(err)
	}
	// Flag が 0 以外の間は、存在しない変数を読んでエラーになる。毎周 console.log も出す
	code := `console.log("tick"); if (plc.readVariable("Flag") !== 0) { plc.readVariable("Nope"); }`
	sc, err := svc.CreateScript("周期", code, 10)
	if err != nil {
		t.Fatal(err)
	}
	target := "script:" + sc.ID
	logs := func() []string { return appLogOf(svc, target) }
	svc.ClearAppLogs()

	if err := svc.StartScript(sc.ID); err != nil {
		t.Fatal(err)
	}
	if got := logs(); len(got) != 1 || got[0] != "info: スクリプトを開始しました（周期 10 ms）" {
		t.Fatalf("開始のログ: %v", got)
	}
	// 出どころは「スクリプト: 名前」
	if e := svc.GetAppLogs()[0]; e.Source != "スクリプト: 周期" {
		t.Errorf("出どころ = %q", e.Source)
	}

	// エラーが起き始めたら 1 回だけ出る（内容の 1 行目）。続いている間は増えない
	setFlag := func(v int) {
		t.Helper()
		for _, vr := range svc.GetVariables() {
			if vr.Name == "Flag" {
				if err := svc.UpdateVariableValue(vr.ID, v); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	setFlag(1)
	waitFor(t, "エラーのログ", func() bool {
		return hasLine(logs(), `error: スクリプトでエラーが起きています: Error: plc.readVariable: 変数 "Nope" がありません`)
	})
	ticks := func() int {
		n := 0
		for _, c := range svc.GetConsoleLogs() {
			if c.Message == "tick" {
				n++
			}
		}
		return n
	}
	before := ticks()
	waitFor(t, "エラーのまま何周かする", func() bool { return ticks() >= before+5 })
	setFlag(0)
	waitFor(t, "解消のログ", func() bool { return hasLine(logs(), "info: スクリプトのエラーが解消しました") })

	if err := svc.StopScript(sc.ID); err != nil {
		t.Fatal(err)
	}
	got := logs()
	want := []string{
		"info: スクリプトを開始しました（周期 10 ms）",
		`error: スクリプトでエラーが起きています: Error: plc.readVariable: 変数 "Nope" がありません`,
		"info: スクリプトのエラーが解消しました",
		"info: スクリプトを停止しました",
	}
	if len(got) != len(want) {
		t.Fatalf("ログ = %v\nwant %v", got, want)
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("%d 件目 = %q, want %q", i+1, got[i], want[i])
		}
	}
	// console.log の出力は、アプリ全体のログには出ない
	for _, e := range svc.GetAppLogs() {
		if strings.Contains(e.Message, "tick") {
			t.Fatalf("console.log の出力がログに出ている: %+v", e)
		}
	}

	// 止まっているスクリプトを止めるのはエラーで、ログは増えない
	if err := svc.StopScript(sc.ID); err == nil {
		t.Error("止まっているスクリプトを止められた")
	}
	if len(logs()) != len(want) {
		t.Errorf("ログが増えた: %v", logs())
	}
}

// 開始できない・保存した内容で続けられない・削除・プロジェクトの読み込みで止まったときも、理由つきで出る
func TestAppLog_ScriptStopReasons(t *testing.T) {
	svc := newTestService(t)
	bad, _ := svc.CreateScript("壊れた", "const = ;", 100)
	good, _ := svc.CreateScript("動く", "1 + 1", 100)
	svc.ClearAppLogs()

	// コンパイルできないスクリプトは開始できない
	if err := svc.StartScript(bad.ID); err == nil {
		t.Fatal("コンパイルできないスクリプトを開始できた")
	}
	if got := appLogOf(svc, "script:"+bad.ID); len(got) != 1 || !strings.HasPrefix(got[0], "error: スクリプトを開始できませんでした: スクリプトをコンパイルできません") {
		t.Errorf("開始の失敗のログ: %v", got)
	}

	// 実行中に、コンパイルできない内容で保存すると止まる
	if err := svc.StartScript(good.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateScript(good.ID, "動く", "const = ;", 100); err == nil {
		t.Fatal("コンパイルできない内容で保存してもエラーにならなかった")
	}
	if got := appLogOf(svc, "script:"+good.ID); !hasLine(got, "error: スクリプトを停止しました（保存した内容で開始できません") {
		t.Errorf("保存で止まったときのログ: %v", got)
	}
	// 実行中に、動く内容で保存しても（止めて始め直すが）ログは出ない
	if err := svc.UpdateScript(good.ID, "動く", "2 + 2", 100); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartScript(good.ID); err != nil {
		t.Fatal(err)
	}
	before := len(appLogOf(svc, "script:"+good.ID))
	if err := svc.UpdateScript(good.ID, "動く", "3 + 3", 100); err != nil {
		t.Fatal(err)
	}
	if after := len(appLogOf(svc, "script:"+good.ID)); after != before {
		t.Errorf("動く内容で保存したらログが増えた: %v", appLogOf(svc, "script:"+good.ID)[before:])
	}

	// プロジェクトを読み込むと、実行中のスクリプトは止まる
	data := svc.ExportProject()
	if err := svc.ImportProject(data); err != nil {
		t.Fatal(err)
	}
	if got := appLogOf(svc, "script:"+good.ID); !hasLine(got, "info: スクリプトを停止しました（プロジェクトを読み込むため）") {
		t.Errorf("読み込みで止まったときのログ: %v", got)
	}

	// 実行中のスクリプトを削除すると、止めたことが出る。止まっているスクリプトの削除では出ない
	var running *ScriptDTO
	for _, s := range svc.GetScripts() {
		if s.Name == "動く" {
			running = s
		}
	}
	if running == nil {
		t.Fatal("読み込んだプロジェクトにスクリプトがない")
	}
	if err := svc.StartScript(running.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteScript(running.ID); err != nil {
		t.Fatal(err)
	}
	if got := appLogOf(svc, "script:"+running.ID); !hasLine(got, "info: スクリプトを停止しました（削除したため）") {
		t.Errorf("削除で止まったときのログ: %v", got)
	}
}

// サーバーと接続先のログには、通信画面のそのサーバー・接続先を指す行き先が付く
func TestAppLog_Targets(t *testing.T) {
	svc, _ := newClientTestService(t)
	svc.ClearAppLogs()
	if err := svc.StartServer("modbus-tcp"); err != nil {
		t.Fatal(err)
	}
	c, err := svc.AddClient("fake-client", "PLC", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := appLogOf(svc, "server:modbus-tcp"); !hasLine(got, "サーバーを起動しました") {
		t.Errorf("サーバーのログ: %v（全体: %v）", got, appLogLines(svc))
	}
	if got := appLogOf(svc, "client:"+c.ID); !hasLine(got, "接続先を追加しました") {
		t.Errorf("接続先のログ: %v（全体: %v）", got, appLogLines(svc))
	}
	// プロジェクトのログには、行き先がない
	if err := svc.ImportProject(svc.ExportProject()); err != nil {
		t.Fatal(err)
	}
	for _, e := range svc.GetAppLogs() {
		if e.Source == "プロジェクト" && e.Target != "" {
			t.Errorf("プロジェクトのログに行き先がある: %+v", e)
		}
	}
}
