package script

import (
	"testing"
	"time"
)

func TestNewScript(t *testing.T) {
	s := NewScript("id-1", "test script", "console.log('hello')", 1*time.Second, 0)

	if s == nil {
		t.Fatal("NewScript returned nil")
	}
	if s.ID != "id-1" {
		t.Errorf("expected ID 'id-1', got '%s'", s.ID)
	}
	if s.Name != "test script" {
		t.Errorf("expected Name 'test script', got '%s'", s.Name)
	}
	if s.Code != "console.log('hello')" {
		t.Errorf("expected Code 'console.log('hello')', got '%s'", s.Code)
	}
	if s.Interval != 1*time.Second {
		t.Errorf("expected Interval 1s, got %v", s.Interval)
	}
	if s.Enabled {
		t.Error("expected Enabled to be false by default")
	}
}

func TestIntervalPresets(t *testing.T) {
	if len(IntervalPresets) == 0 {
		t.Fatal("IntervalPresets is empty")
	}

	// 各プリセットが有効な値を持つことを確認
	for _, preset := range IntervalPresets {
		if preset.Label == "" {
			t.Error("preset Label is empty")
		}
		if preset.Duration <= 0 {
			t.Errorf("preset Duration is not positive: %v", preset.Duration)
		}
	}

	// 100msプリセットの存在確認
	found := false
	for _, preset := range IntervalPresets {
		if preset.Duration == 100*time.Millisecond {
			found = true
			break
		}
	}
	if !found {
		t.Error("100ms preset not found")
	}

	// 1秒プリセットの存在確認
	found = false
	for _, preset := range IntervalPresets {
		if preset.Duration == 1*time.Second {
			found = true
			break
		}
	}
	if !found {
		t.Error("1 second preset not found")
	}
}

func TestScript_Fields(t *testing.T) {
	s := &Script{
		ID:       "test-id",
		Name:     "Test Script",
		Code:     "var x = 1;",
		Interval: 500 * time.Millisecond,
		Enabled:  true,
	}

	if s.ID != "test-id" {
		t.Errorf("expected ID 'test-id', got '%s'", s.ID)
	}
	if s.Name != "Test Script" {
		t.Errorf("expected Name 'Test Script', got '%s'", s.Name)
	}
	if s.Code != "var x = 1;" {
		t.Errorf("expected Code 'var x = 1;', got '%s'", s.Code)
	}
	if s.Interval != 500*time.Millisecond {
		t.Errorf("expected Interval 500ms, got %v", s.Interval)
	}
	if !s.Enabled {
		t.Error("expected Enabled to be true")
	}
}
