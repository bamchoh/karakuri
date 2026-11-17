package application

import (
	"fmt"
	"strings"
)

// モニタリングに出せるのは、レジスタのほかに、変数・接続先のサブスクリプションの監視項目・ポーリングの読み取り項目。
// レジスタの値は、画面がメモリを読んで作る（ビット幅・表示形式を画面が持つため）。ほかの項目の値は、ここで作る

// removeMonitoringItems は、match が true を返す項目を消す（変数・接続先を消したときに、それを指す項目を消す）
func (s *PLCService) removeMonitoringItems(match func(*MonitoringItemDTO) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := false
	for id, item := range s.monitoringItems {
		if match(item) {
			delete(s.monitoringItems, id)
			removed = true
		}
	}
	if removed {
		s.scheduleMonitoringSave()
	}
}

// GetMonitoringValues は、レジスタ以外のモニタリング項目のいまの値を、項目の並びの順で返す
func (s *PLCService) GetMonitoringValues() []MonitoringValueDTO {
	out := []MonitoringValueDTO{}
	clientNames := map[string]string{}
	for _, c := range s.GetClients() {
		clientNames[c.ID] = c.Name
	}
	// 接続先ごとに 1 回だけ読む
	subs := map[string]*ClientSubscriptionDTO{}
	polls := map[string]*ClientPollingDTO{}

	for _, item := range s.GetMonitoringItems() {
		switch item.Kind {
		case MonitoringKindVariable:
			out = append(out, s.monitoringVariableValue(item))
		case MonitoringKindSubscription, MonitoringKindPoll:
			v := MonitoringValueDTO{ID: item.ID, Label: item.Summary}
			name, ok := clientNames[item.ClientID]
			if !ok {
				v.Source, v.Status, v.Error = "接続先", "error", "接続先がありません"
				out = append(out, v)
				continue
			}
			v.Source = "接続先: " + name
			if item.Kind == MonitoringKindSubscription {
				sub, ok := subs[item.ClientID]
				if !ok {
					sub, _ = s.GetClientSubscription(item.ClientID)
					subs[item.ClientID] = sub
				}
				fillSubscriptionValue(&v, sub, item.Summary)
			} else {
				poll, ok := polls[item.ClientID]
				if !ok {
					poll, _ = s.GetClientPolling(item.ClientID)
					polls[item.ClientID] = poll
				}
				fillPollValue(&v, poll, item.Summary)
			}
			out = append(out, v)
		}
	}
	return out
}

func (s *PLCService) monitoringVariableValue(item *MonitoringItemDTO) MonitoringValueDTO {
	v := MonitoringValueDTO{ID: item.ID, Source: "変数"}
	variable, err := s.variableStore.GetVariable(item.VariableID)
	if err != nil {
		v.Label, v.Status, v.Error = item.FieldPath, "error", "変数がありません"
		return v
	}
	v.Label = variable.Name + fieldPathSuffix(item.FieldPath)
	value, err := s.ReadVariableValue(item.VariableID, item.FieldPath)
	if err != nil {
		v.Status, v.Error = "error", err.Error()
		return v
	}
	v.Status = "ok"
	if isComposite(value) {
		// 構造体・配列は、中身を 1 つずつの行にする
		v.Rows = s.variableRows(s.typeAtPath(variable.DataType, item.FieldPath), value, item.FieldPath)
		return v
	}
	v.Value = value
	v.Text = monitoringText(value)
	return v
}

// fieldPathSuffix は、変数名のあとに付けるパス（"speed" → ".speed"、"[1].x" → "[1].x"）
func fieldPathSuffix(path string) string {
	if path == "" || strings.HasPrefix(path, "[") || strings.HasPrefix(path, ".") {
		return path
	}
	return "." + path
}

// monitoringText は、変数の値（数値・真偽・文字列）を表示の文字にする
func monitoringText(value interface{}) string {
	switch x := value.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "TRUE"
		}
		return "FALSE"
	}
	return fmt.Sprint(value)
}

// fillResultValues は、接続先から読んだ値を入れる（1 つなら項目の行に、いくつもあれば中身の行に）
func fillResultValues(v *MonitoringValueDTO, values []ClientResultValueDTO) {
	if len(values) == 1 {
		v.Text = values[0].Value
		return
	}
	v.Rows = resultValueRows(values)
}

func fillSubscriptionValue(v *MonitoringValueDTO, sub *ClientSubscriptionDTO, summary string) {
	if sub != nil {
		for _, it := range sub.Items {
			if it.Summary != summary {
				continue
			}
			fillResultValues(v, it.Values)
			switch {
			case !sub.Running:
				v.Status, v.Error = "stale", "監視していません"
			case it.Status == "ok" && sub.State == "ok":
				v.Status = "ok"
			default:
				v.Status = "stale"
				if v.Error = it.Error; v.Error == "" {
					v.Error = sub.Error
				}
				if v.Error == "" {
					v.Error = "まだ値が届いていません"
				}
			}
			return
		}
	}
	v.Status, v.Error = "error", "監視項目がありません"
}

func fillPollValue(v *MonitoringValueDTO, poll *ClientPollingDTO, summary string) {
	if poll != nil {
		for _, it := range poll.Items {
			if it.Summary != summary {
				continue
			}
			fillResultValues(v, it.Values)
			switch {
			case !poll.Running:
				v.Status, v.Error = "stale", "ポーリングしていません"
			case it.Status == "ok":
				v.Status = "ok"
			default:
				v.Status = "stale"
				if v.Error = it.Error; v.Error == "" {
					v.Error = "まだ読めていません"
				}
			}
			return
		}
	}
	v.Status, v.Error = "error", "読み取り項目がありません"
}
