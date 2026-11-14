package applog

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"testing"
)

func TestLog_AddAndLimit(t *testing.T) {
	l := New(3)
	var notified []int64
	l.SetOnAdded(func(e Entry) { notified = append(notified, e.ID) })

	for i := 1; i <= 5; i++ {
		l.Infof("テスト", "%d 件目", i)
	}
	entries := l.Entries()
	if len(entries) != 3 || entries[0].Message != "3 件目" || entries[2].ID != 5 {
		t.Fatalf("直近 3 件だけを持つはず: %+v", entries)
	}
	if fmt.Sprint(notified) != "[1 2 3 4 5]" {
		t.Errorf("追加のたびに通知する: %v", notified)
	}
	if entries[0].Level != LevelInfo || entries[0].Source != "テスト" || entries[0].At.IsZero() {
		t.Errorf("ログの内容: %+v", entries[0])
	}

	l.Clear()
	if len(l.Entries()) != 0 {
		t.Error("消したあとも残っている")
	}
	// 消しても、番号は続きから付く（画面が、どこまで受け取ったかを番号で見分けられるように）
	l.Warnf("テスト", "消したあと")
	if e := l.Entries(); len(e) != 1 || e[0].ID != 6 || e[0].Level != LevelWarn {
		t.Errorf("消したあとの 1 件: %+v", e)
	}
}

func TestLog_Result(t *testing.T) {
	l := New(0)
	l.Result("Modbus TCP", "起動しました", "起動に失敗しました", nil)
	l.Result("Modbus TCP", "起動しました", "起動に失敗しました", errors.New("ポートが使用中です"))
	e := l.Entries()
	if len(e) != 2 || e[0].Level != LevelInfo || e[0].Message != "起動しました" {
		t.Fatalf("成功のログ: %+v", e)
	}
	if e[1].Level != LevelError || e[1].Message != "起動に失敗しました: ポートが使用中です" {
		t.Errorf("失敗のログ: %+v", e[1])
	}
}

// 通知の関数の中からログを追加しても、止まらない（ロックの外で呼ぶ）
func TestLog_AddFromCallback(t *testing.T) {
	l := New(0)
	l.SetOnAdded(func(e Entry) {
		if e.Message == "1" {
			l.Infof("テスト", "2")
		}
	})
	l.Infof("テスト", "1")
	if len(l.Entries()) != 2 {
		t.Errorf("通知の中から追加したログがない: %+v", l.Entries())
	}
}

func TestLog_Concurrent(t *testing.T) {
	l := New(50)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				l.Infof("並行", "%d", i)
				_ = l.Entries()
			}
		}()
	}
	wg.Wait()
	if e := l.Entries(); len(e) != 50 || e[49].ID != 800 {
		t.Errorf("件数 = %d、最後の番号 = %d", len(e), e[len(e)-1].ID)
	}
}

// 標準のログの出力を、1 行ずつ取り込む
func TestWriter(t *testing.T) {
	l := New(0)
	w := l.Writer("システム")

	// log パッケージの書式: 日時が付く。log.New の接頭辞は日時の前に付く
	log.New(w, "", log.LstdFlags).Printf("ふつうの行")
	log.New(w, "[opcua] ", log.LstdFlags).Printf("session %q created", "Karakuri")
	log.New(w, "", log.LstdFlags|log.Lmicroseconds).Printf("RTU: failed to parse request: %v", errors.New("短すぎます"))
	// 日時のあとに出どころが来る書き方
	fmt.Fprintf(w, "2026/11/14 10:00:00 [fins] 待ち受けを始めました\n")
	// 何行かをまとめて書く・途中で切れた行をあとから続ける・空行は捨てる
	fmt.Fprintf(w, "1 行目\n\n2 行目\r\n書きかけ")
	fmt.Fprintf(w, "の続き\n")

	want := []struct {
		level           Level
		source, message string
	}{
		{LevelInfo, "システム", "ふつうの行"},
		{LevelInfo, "opcua", `session "Karakuri" created`},
		{LevelWarn, "システム", "RTU: failed to parse request: 短すぎます"},
		{LevelInfo, "fins", "待ち受けを始めました"},
		{LevelInfo, "システム", "1 行目"},
		{LevelInfo, "システム", "2 行目"},
		{LevelInfo, "システム", "書きかけの続き"},
	}
	got := l.Entries()
	if len(got) != len(want) {
		t.Fatalf("件数 = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Level != w.level || got[i].Source != w.source || got[i].Message != w.message {
			t.Errorf("%d 件目 = {%s %q %q}, want {%s %q %q}", i+1, got[i].Level, got[i].Source, got[i].Message, w.level, w.source, w.message)
		}
	}
}
