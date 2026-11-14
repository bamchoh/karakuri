// Package applog は、アプリ全体のログ（サーバーの起動・停止、クライアントの通信エラーなど）を持つ。
//
// 画面のどのページからも見られるログの欄に出すためのもので、直近の決まった件数だけをメモリに持つ。
// スクリプトのコンソール（console.log の出力）とは別。
package applog

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Level はログの種類
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// DefaultMaxEntries は、持っておくログの件数の既定値
const DefaultMaxEntries = 1000

// Entry はログ 1 件
type Entry struct {
	ID      int64 // 追加した順の番号（1 から。消しても戻らない）
	At      time.Time
	Level   Level
	Source  string // 出どころ（"Modbus TCP"、"接続先: PLC-A"、"プロジェクト" など）
	Message string
}

// Log はログの入れもの。複数のゴルーチンから呼んでよい
type Log struct {
	mu      sync.Mutex
	entries []Entry
	nextID  int64
	max     int
	onAdded func(Entry)
}

// New は、直近 max 件を持つログを作る（max が 0 以下なら DefaultMaxEntries）
func New(max int) *Log {
	if max <= 0 {
		max = DefaultMaxEntries
	}
	return &Log{max: max}
}

// SetOnAdded は、ログを追加するたびに呼ぶ関数を設定する（画面への通知に使う）。
// 関数は、ログのロックの外で呼ぶ（中からログを追加してもよい）
func (l *Log) SetOnAdded(fn func(Entry)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onAdded = fn
}

// Add はログを 1 件追加する
func (l *Log) Add(level Level, source, message string) {
	l.mu.Lock()
	l.nextID++
	entry := Entry{ID: l.nextID, At: time.Now(), Level: level, Source: source, Message: message}
	l.entries = append(l.entries, entry)
	if over := len(l.entries) - l.max; over > 0 {
		l.entries = append([]Entry(nil), l.entries[over:]...)
	}
	onAdded := l.onAdded
	l.mu.Unlock()
	if onAdded != nil {
		onAdded(entry)
	}
}

func (l *Log) Infof(source, format string, args ...interface{}) {
	l.Add(LevelInfo, source, fmt.Sprintf(format, args...))
}

func (l *Log) Warnf(source, format string, args ...interface{}) {
	l.Add(LevelWarn, source, fmt.Sprintf(format, args...))
}

func (l *Log) Errorf(source, format string, args ...interface{}) {
	l.Add(LevelError, source, fmt.Sprintf(format, args...))
}

// Result は、操作の結果を 1 件のログにする。err が nil なら okMessage を情報として、
// nil でなければ「failMessage: 理由」をエラーとして追加する
func (l *Log) Result(source, okMessage, failMessage string, err error) {
	if err != nil {
		l.Errorf(source, "%s: %v", failMessage, err)
		return
	}
	l.Add(LevelInfo, source, okMessage)
}

// Entries は、持っているログを古い順に返す（コピー）
func (l *Log) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Entry(nil), l.entries...)
}

// Clear は、持っているログをすべて消す
func (l *Log) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = nil
}

// --- Go の標準のログ（log パッケージ）を取り込む ---

// Writer は、書かれた文字列を 1 行ずつログに追加する io.Writer を返す。
// 標準のログ（log.SetOutput）や、ライブラリに渡すロガーの出力先にする。
//
//   - 行の先頭の "[名前] " は出どころにする（なければ defaultSource）
//   - log パッケージが付ける日時（"2006/01/02 15:04:05 "）は取り除く
//   - "error" / "fail" を含む行は警告、ほかは情報にする
func (l *Log) Writer(defaultSource string) io.Writer {
	return &lineWriter{log: l, defaultSource: defaultSource}
}

type lineWriter struct {
	log           *Log
	defaultSource string

	mu  sync.Mutex
	buf []byte // 改行がまだ来ていない書きかけの行
}

var (
	sourcePrefix    = regexp.MustCompile(`^\[([^\]]{1,40})\]\s*`)
	timestampPrefix = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(\.\d+)?\s*`)
)

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	var lines []string
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		lines = append(lines, string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
	// 改行のない出力がたまり続けないようにする
	if len(w.buf) > 64*1024 {
		lines = append(lines, string(w.buf))
		w.buf = nil
	}
	w.mu.Unlock()

	for _, line := range lines {
		w.add(line)
	}
	return len(p), nil
}

func (w *lineWriter) add(line string) {
	line = strings.TrimRight(line, "\r")
	source := w.defaultSource
	// 出どころと日時は、どちらが先でもよい（log.New の接頭辞は日時の前に付く）
	for i := 0; i < 2; i++ {
		if m := sourcePrefix.FindStringSubmatch(line); m != nil {
			source = m[1]
			line = line[len(m[0]):]
		}
		line = timestampPrefix.ReplaceAllString(line, "")
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	level := LevelInfo
	if lower := strings.ToLower(line); strings.Contains(lower, "error") || strings.Contains(lower, "fail") {
		level = LevelWarn
	}
	w.log.Add(level, source, line)
}
