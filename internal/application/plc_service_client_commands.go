package application

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// === クライアントの保存したコマンド ===
//
// 接続先ごとに、組み立てたコマンドを名前を付けて保存しておき、あとから送れるようにする。
// 保存したコマンドは、組み立てたもの（コマンドの ID と項目の値）か、16 進で入れたもの。
// どちらも保存するときにリクエストのバイト列を作っておき、送るときはそれを送る。
// 保存した順に並べ、プロジェクトに保存する。

const (
	savedModeBuild = "build"
	savedModeHex   = "hex"

	maxSavedCommands       = 200
	maxSavedCommandNameLen = 100
)

type savedCommand struct {
	id        string
	name      string
	mode      string
	commandID string
	params    map[string]interface{}
	request   []byte
	summary   string
}

func (c *savedCommand) toDTO() ClientSavedCommandDTO {
	var params map[string]interface{}
	if c.params != nil {
		params = copyParams(c.params)
	}
	return ClientSavedCommandDTO{
		ID:        c.id,
		Name:      c.name,
		Mode:      c.mode,
		CommandID: c.commandID,
		Params:    params,
		Request:   formatHex(c.request),
		Summary:   c.summary,
	}
}

func cleanCommandName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) > maxSavedCommandNameLen {
		return "", fmt.Errorf("コマンドの名前は %d 文字までです", maxSavedCommandNameLen)
	}
	return name, nil
}

// newSavedCommand は入力から保存するコマンドを作る（組み立てられない・16 進が読めないならエラー）。名前が空なら要約を名前にする
func (inst *clientInstance) newSavedCommand(in ClientSavedCommandInputDTO) (*savedCommand, error) {
	name, err := cleanCommandName(in.Name)
	if err != nil {
		return nil, err
	}
	c := &savedCommand{mode: in.Mode}
	switch in.Mode {
	case savedModeBuild:
		req, err := inst.factory.BuildRequest(in.CommandID, in.Params)
		if err != nil {
			return nil, err
		}
		c.commandID = in.CommandID
		c.params = copyParams(in.Params)
		c.request = req
	case savedModeHex:
		req, err := parseHex(in.Request)
		if err != nil {
			return nil, err
		}
		if len(req) == 0 {
			return nil, fmt.Errorf("リクエストを入力してください")
		}
		c.request = req
	default:
		return nil, fmt.Errorf("不明な入れ方です: %s", in.Mode)
	}
	c.summary = inst.factory.DescribeRequest(c.request)
	if name == "" {
		name = c.summary
	}
	c.name = name
	return c, nil
}

func (inst *clientInstance) savedCommandIndex(commandID string) (int, error) {
	for i, c := range inst.savedCommands {
		if c.id == commandID {
			return i, nil
		}
	}
	return -1, fmt.Errorf("保存したコマンドが見つかりません: %s", commandID)
}

// withClient は接続先をロックの中で処理する
func (s *PLCService) withClient(id string, fn func(inst *clientInstance) error) error {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		return err
	}
	return fn(inst)
}

// GetClientSavedCommands は接続先の保存したコマンドを、保存した順に返す
func (s *PLCService) GetClientSavedCommands(id string) ([]ClientSavedCommandDTO, error) {
	var result []ClientSavedCommandDTO
	err := s.withClient(id, func(inst *clientInstance) error {
		result = make([]ClientSavedCommandDTO, len(inst.savedCommands))
		for i, c := range inst.savedCommands {
			result[i] = c.toDTO()
		}
		return nil
	})
	return result, err
}

// AddClientSavedCommand はコマンドを末尾に保存する
func (s *PLCService) AddClientSavedCommand(id string, in ClientSavedCommandInputDTO) (*ClientSavedCommandDTO, error) {
	var dto ClientSavedCommandDTO
	err := s.withClient(id, func(inst *clientInstance) error {
		if len(inst.savedCommands) >= maxSavedCommands {
			return fmt.Errorf("保存できるコマンドは %d 個までです", maxSavedCommands)
		}
		c, err := inst.newSavedCommand(in)
		if err != nil {
			return err
		}
		c.id = uuid.NewString()
		inst.savedCommands = append(inst.savedCommands, c)
		dto = c.toDTO()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &dto, nil
}

// UpdateClientSavedCommand は保存したコマンドを、入力の内容に置き換える（並び順と ID は変えない）
func (s *PLCService) UpdateClientSavedCommand(id, commandID string, in ClientSavedCommandInputDTO) error {
	return s.withClient(id, func(inst *clientInstance) error {
		i, err := inst.savedCommandIndex(commandID)
		if err != nil {
			return err
		}
		c, err := inst.newSavedCommand(in)
		if err != nil {
			return err
		}
		c.id = commandID
		inst.savedCommands[i] = c
		return nil
	})
}

// RenameClientSavedCommand は保存したコマンドの名前だけを変える（空なら内容の要約にする）
func (s *PLCService) RenameClientSavedCommand(id, commandID, name string) error {
	return s.withClient(id, func(inst *clientInstance) error {
		i, err := inst.savedCommandIndex(commandID)
		if err != nil {
			return err
		}
		n, err := cleanCommandName(name)
		if err != nil {
			return err
		}
		if n == "" {
			n = inst.savedCommands[i].summary
		}
		inst.savedCommands[i].name = n
		return nil
	})
}

// RemoveClientSavedCommand は保存したコマンドを削除する
func (s *PLCService) RemoveClientSavedCommand(id, commandID string) error {
	return s.withClient(id, func(inst *clientInstance) error {
		i, err := inst.savedCommandIndex(commandID)
		if err != nil {
			return err
		}
		inst.savedCommands = append(inst.savedCommands[:i:i], inst.savedCommands[i+1:]...)
		return nil
	})
}

// savedRequest は保存したコマンドの、送るバイト列と名前を返す
func (s *PLCService) savedRequest(id, commandID string) (req []byte, name string, err error) {
	err = s.withClient(id, func(inst *clientInstance) error {
		i, err := inst.savedCommandIndex(commandID)
		if err != nil {
			return err
		}
		req = append([]byte(nil), inst.savedCommands[i].request...)
		name = inst.savedCommands[i].name
		return nil
	})
	return
}

// SendClientSavedCommand は保存したコマンドを送り、結果を返す（履歴にはコマンドの名前を付けて残す）
func (s *PLCService) SendClientSavedCommand(id, commandID string) (*ClientCommandLogDTO, error) {
	req, name, err := s.savedRequest(id, commandID)
	if err != nil {
		return nil, err
	}
	return s.sendAndLog(id, req, name)
}

// savedCommandsSnapshotLocked はプロジェクトに保存するコマンドを返す
func (inst *clientInstance) savedCommandsSnapshotLocked() []ClientSavedCommandDTO {
	if len(inst.savedCommands) == 0 {
		return nil
	}
	result := make([]ClientSavedCommandDTO, len(inst.savedCommands))
	for i, c := range inst.savedCommands {
		result[i] = c.toDTO()
	}
	return result
}

// restoreSavedCommandsLocked はプロジェクトの保存したコマンドを戻す（使えないコマンドは読み飛ばす）。ID は作り直す
func (inst *clientInstance) restoreSavedCommandsLocked(cmds []ClientSavedCommandDTO) {
	for _, cs := range cmds {
		if len(inst.savedCommands) >= maxSavedCommands {
			return
		}
		c, err := inst.newSavedCommand(ClientSavedCommandInputDTO{
			Name: cs.Name, Mode: cs.Mode, CommandID: cs.CommandID, Params: cs.Params, Request: cs.Request,
		})
		if err != nil {
			continue
		}
		c.id = uuid.NewString()
		inst.savedCommands = append(inst.savedCommands, c)
	}
}
