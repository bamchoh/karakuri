package application

import (
	"fmt"
	"strings"
)

// === スクリプトから外部の機器へ送る（plc.sendCommand / plc.sendRequest） ===
//
// 接続先・グループ・コマンドは名前で指定する（同じ名前が複数あれば、追加した順で最初のもの）。
// 見つからない・16 進が読めないときも、例外にせず status が "error" の結果を返す（スクリプトを止めないため）。
// 送った内容は履歴にも残し、label に「スクリプト「名前」: …」を付ける。

// scriptClientSender は scripting.ClientSender の実装
type scriptClientSender struct {
	svc *PLCService
}

func scriptError(msg string) map[string]any {
	return map[string]any{"status": "error", "error": msg, "values": []any{}}
}

// clientIDByName は名前で接続先を探す（追加した順で最初のもの）
func (m *clientManager) clientIDByName(name string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.sortedDTOsLocked() {
		if c.Name == name {
			return c.ID, true
		}
	}
	return "", false
}

// savedCommandByName は、名前でグループとコマンドを探す
func (s *PLCService) savedCommandByName(clientID, group, command string) (groupID, commandID string, err error) {
	err = s.withClient(clientID, func(inst *clientInstance) error {
		for _, g := range inst.commandGroups {
			if g.name != group {
				continue
			}
			for _, c := range g.commands {
				if c.name == command {
					groupID, commandID = g.id, c.id
					return nil
				}
			}
			return fmt.Errorf("グループ「%s」にコマンド「%s」がありません", group, command)
		}
		return fmt.Errorf("コマンドグループ「%s」がありません", group)
	})
	return
}

func scriptLabel(scriptName, what string) string {
	if strings.TrimSpace(scriptName) == "" {
		return "スクリプト: " + what
	}
	return "スクリプト「" + scriptName + "」: " + what
}

// logToScript は履歴の 1 件を、スクリプトに返すオブジェクトにする
func logToScript(l *ClientCommandLogDTO) map[string]any {
	values := make([]any, len(l.Values))
	for i, v := range l.Values {
		o := map[string]any{"label": v.Label, "value": v.Value}
		if v.Word != nil {
			o["word"] = *v.Word
		}
		if v.Bit != nil {
			o["bit"] = *v.Bit
		}
		values[i] = o
	}
	return map[string]any{
		"status":    l.Status,
		"error":     l.Error,
		"summary":   l.Summary,
		"label":     l.Label,
		"sent":      l.Sent,
		"received":  l.Received,
		"elapsedMs": l.ElapsedMs,
		"values":    values,
	}
}

func (c scriptClientSender) SendCommand(scriptName, client, group, command string) map[string]any {
	clientID, ok := c.svc.clients.clientIDByName(client)
	if !ok {
		return scriptError(fmt.Sprintf("接続先「%s」がありません", client))
	}
	groupID, commandID, err := c.svc.savedCommandByName(clientID, group, command)
	if err != nil {
		return scriptError(err.Error())
	}
	var req []byte
	err = c.svc.withClient(clientID, func(inst *clientInstance) error {
		g, err := inst.groupLocked(groupID)
		if err != nil {
			return err
		}
		i, err := g.commandIndex(commandID)
		if err != nil {
			return err
		}
		req = append([]byte(nil), g.commands[i].request...)
		return nil
	})
	if err != nil {
		return scriptError(err.Error())
	}
	l, err := c.svc.sendAndLog(clientID, req, scriptLabel(scriptName, group+" / "+command))
	if err != nil {
		return scriptError(err.Error())
	}
	return logToScript(l)
}

func (c scriptClientSender) SendRequest(scriptName, client, requestHex string) map[string]any {
	clientID, ok := c.svc.clients.clientIDByName(client)
	if !ok {
		return scriptError(fmt.Sprintf("接続先「%s」がありません", client))
	}
	req, err := parseHex(requestHex)
	if err != nil {
		return scriptError(err.Error())
	}
	if len(req) == 0 {
		return scriptError("リクエストを入力してください")
	}
	l, err := c.svc.sendAndLog(clientID, req, scriptLabel(scriptName, "16 進で送信"))
	if err != nil {
		return scriptError(err.Error())
	}
	return logToScript(l)
}
