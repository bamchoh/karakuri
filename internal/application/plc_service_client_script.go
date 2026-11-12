package application

import (
	"fmt"
	"strings"
)

// === スクリプトから外部の機器へ送る（plc.sendCommand / plc.sendRequest） ===
//
// 接続先と保存したコマンドは名前で指定する（同じ名前が複数あれば、追加した順で最初のもの）。
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

// savedCommandIDByName は、名前で保存したコマンドを探す（保存した順で最初のもの）
func (s *PLCService) savedCommandIDByName(clientID, command string) (commandID string, err error) {
	err = s.withClient(clientID, func(inst *clientInstance) error {
		for _, c := range inst.savedCommands {
			if c.name == command {
				commandID = c.id
				return nil
			}
		}
		return fmt.Errorf("保存したコマンド「%s」がありません", command)
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

func (c scriptClientSender) SendCommand(scriptName, client, command string) map[string]any {
	clientID, ok := c.svc.clients.clientIDByName(client)
	if !ok {
		return scriptError(fmt.Sprintf("接続先「%s」がありません", client))
	}
	commandID, err := c.svc.savedCommandIDByName(clientID, command)
	if err != nil {
		return scriptError(err.Error())
	}
	req, name, err := c.svc.savedRequest(clientID, commandID)
	if err != nil {
		return scriptError(err.Error())
	}
	l, err := c.svc.sendAndLog(clientID, req, scriptLabel(scriptName, name))
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
