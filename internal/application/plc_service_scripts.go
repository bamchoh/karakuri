package application

import (
	"fmt"
	"sort"
	"time"

	"karakuri/internal/domain/script"
	"karakuri/internal/infrastructure/scripting"

	"github.com/google/uuid"
)

// === スクリプト管理 ===

// CreateScript は新しいスクリプトを作成する
func (s *PLCService) CreateScript(name, code string, intervalMs int) (*ScriptDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := uuid.New().String()
	sc := script.NewScript(id, name, code, time.Duration(intervalMs)*time.Millisecond, s.nextOrder)
	s.scripts[id] = sc
	s.nextOrder++

	go s.emitScriptsChanged()

	return scriptToDTO(sc, false), nil
}

// UpdateScript はスクリプトを更新する
func (s *PLCService) UpdateScript(id, name, code string, intervalMs int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sc, ok := s.scripts[id]
	if !ok {
		return fmt.Errorf("script not found: %s", id)
	}

	// 実行中なら一旦停止
	wasRunning := s.scriptEngine.IsRunning(id)
	if wasRunning {
		s.scriptEngine.StopScript(id)
	}

	sc.Name = name
	sc.Code = code
	sc.Interval = time.Duration(intervalMs) * time.Millisecond

	// 実行中だった場合は再開する。新しいコードがコンパイルできなければ、停止したままにしてエラーを返す
	// （変更は保存されている。エラーはコンソールにも出る）
	var startErr error
	if wasRunning {
		startErr = s.scriptEngine.StartScript(sc)
	}

	go s.emitScriptsChanged()

	return startErr
}

// DeleteScript はスクリプトを削除する
func (s *PLCService) DeleteScript(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.scripts[id]; !ok {
		return fmt.Errorf("script not found: %s", id)
	}

	s.scriptEngine.StopScript(id)
	delete(s.scripts, id)
	go s.emitScriptsChanged()
	return nil
}

// GetScripts は全てのスクリプトを取得する
func (s *PLCService) GetScripts() []*ScriptDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*ScriptDTO, 0, len(s.scripts))
	for _, sc := range s.scripts {
		result = append(result, scriptToDTO(sc, s.scriptEngine.IsRunning(sc.ID)))
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Order < result[j].Order
	})

	return result
}

// GetScript は特定のスクリプトを取得する
func (s *PLCService) GetScript(id string) (*ScriptDTO, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sc, ok := s.scripts[id]
	if !ok {
		return nil, fmt.Errorf("script not found: %s", id)
	}

	return scriptToDTO(sc, s.scriptEngine.IsRunning(id)), nil
}

// StartScript はスクリプトを開始する
func (s *PLCService) StartScript(id string) error {
	s.mu.RLock()
	sc, ok := s.scripts[id]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("script not found: %s", id)
	}

	if err := s.scriptEngine.StartScript(sc); err != nil {
		return err
	}
	go s.emitScriptsChanged()
	return nil
}

// StopScript はスクリプトを停止する
func (s *PLCService) StopScript(id string) error {
	if err := s.scriptEngine.StopScript(id); err != nil {
		return err
	}
	go s.emitScriptsChanged()
	return nil
}

// RunScriptOnce はコードを 1 回だけ実行する（保存も周期実行もしない）。結果とエラーはコンソールにも出る
func (s *PLCService) RunScriptOnce(code string) (interface{}, error) {
	return s.scriptEngine.RunOnce(code)
}

// GetConsoleLogs はコンソールログの一覧を返す
func (s *PLCService) GetConsoleLogs() []ConsoleLogDTO {
	entries := s.scriptEngine.GetConsoleLogs()
	result := make([]ConsoleLogDTO, len(entries))
	for i, e := range entries {
		result[i] = consoleLogToDTO(e)
	}
	return result
}

// ClearConsoleLogs はコンソールログをクリアする
func (s *PLCService) ClearConsoleLogs() {
	s.scriptEngine.ClearConsoleLogs()
}

// SetConsoleLogCallback はコンソールログ追加時のコールバックを設定する
func (s *PLCService) SetConsoleLogCallback(cb func(ConsoleLogDTO)) {
	s.scriptEngine.SetOnLogAdded(func(entry scripting.ConsoleLogEntry) {
		cb(consoleLogToDTO(entry))
	})
}

func consoleLogToDTO(e scripting.ConsoleLogEntry) ConsoleLogDTO {
	return ConsoleLogDTO{
		ScriptID:   e.ScriptID,
		ScriptName: e.ScriptName,
		Level:      e.Level,
		Message:    e.Message,
		At:         e.At.UnixMilli(),
	}
}

// GetIntervalPresets は周期プリセットを取得する
func (s *PLCService) GetIntervalPresets() []IntervalPresetDTO {
	presets := script.IntervalPresets
	result := make([]IntervalPresetDTO, len(presets))
	for i, p := range presets {
		result[i] = IntervalPresetDTO{
			Label: p.Label,
			Ms:    int(p.Duration.Milliseconds()),
		}
	}
	return result
}

func scriptToDTO(sc *script.Script, isRunning bool) *ScriptDTO {
	return &ScriptDTO{
		ID:         sc.ID,
		Name:       sc.Name,
		Code:       sc.Code,
		IntervalMs: int(sc.Interval.Milliseconds()),
		IsRunning:  isRunning,
		Order:      sc.Order,
	}
}
