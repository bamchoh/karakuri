package script

import (
	"time"
)

// Script はJavaScriptスクリプトを表す
type Script struct {
	ID       string
	Name     string
	Code     string
	Interval time.Duration
	Enabled  bool
	Order    int
}

// NewScript は新しいスクリプトを作成する
func NewScript(id, name, code string, interval time.Duration, order int) *Script {
	return &Script{
		ID:       id,
		Name:     name,
		Code:     code,
		Interval: interval,
		Enabled:  false,
		Order:    order,
	}
}

// IntervalPreset は周期のプリセット値
type IntervalPreset struct {
	Label    string
	Duration time.Duration
}

// 一般的な周期プリセット
var IntervalPresets = []IntervalPreset{
	{Label: "100ms", Duration: 100 * time.Millisecond},
	{Label: "500ms", Duration: 500 * time.Millisecond},
	{Label: "1秒", Duration: 1 * time.Second},
	{Label: "5秒", Duration: 5 * time.Second},
	{Label: "10秒", Duration: 10 * time.Second},
	{Label: "1分", Duration: 1 * time.Minute},
	{Label: "5分", Duration: 5 * time.Minute},
	{Label: "1時間", Duration: 1 * time.Hour},
}

// ScriptRepository はスクリプトの永続化インターフェース
type ScriptRepository interface {
	Save(script *Script) error
	FindByID(id string) (*Script, error)
	FindAll() ([]*Script, error)
	Delete(id string) error
}
