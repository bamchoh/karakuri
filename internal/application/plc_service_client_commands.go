package application

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// === クライアントのコマンドグループ（保存したコマンドの整理棚） ===
//
// 接続先ごとに、名前を付けたグループを持ち、グループにコマンドを保存する。
// 保存したコマンドは、組み立てたもの（コマンドの ID と項目の値）か、16 進で入れたもの。
// どちらも保存するときにリクエストのバイト列を作っておき、送るときはそれを送る。
// グループとコマンドは追加した順に並べ、プロジェクトに保存する。

const (
	savedModeBuild = "build"
	savedModeHex   = "hex"

	maxCommandGroups      = 50
	maxCommandsPerGroup   = 200
	maxCommandGroupNameLn = 100
)

type commandGroup struct {
	id       string
	name     string
	commands []*savedCommand
}

type savedCommand struct {
	id        string
	name      string
	mode      string
	commandID string
	params    map[string]interface{}
	request   []byte
	summary   string
}

func (g *commandGroup) toDTO() ClientCommandGroupDTO {
	cmds := make([]ClientSavedCommandDTO, len(g.commands))
	for i, c := range g.commands {
		cmds[i] = c.toDTO()
	}
	return ClientCommandGroupDTO{ID: g.id, Name: g.name, Commands: cmds}
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

func cleanName(name, what string) (string, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) > maxCommandGroupNameLn {
		return "", fmt.Errorf("%sは %d 文字までです", what, maxCommandGroupNameLn)
	}
	return name, nil
}

// newSavedCommand は入力から保存するコマンドを作る（組み立てられない・16 進が読めないならエラー）。名前が空なら要約を名前にする
func (inst *clientInstance) newSavedCommand(in ClientSavedCommandInputDTO) (*savedCommand, error) {
	name, err := cleanName(in.Name, "コマンドの名前")
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

func (inst *clientInstance) groupLocked(groupID string) (*commandGroup, error) {
	for _, g := range inst.commandGroups {
		if g.id == groupID {
			return g, nil
		}
	}
	return nil, fmt.Errorf("コマンドグループが見つかりません: %s", groupID)
}

func (g *commandGroup) commandIndex(commandID string) (int, error) {
	for i, c := range g.commands {
		if c.id == commandID {
			return i, nil
		}
	}
	return -1, fmt.Errorf("保存したコマンドが見つかりません: %s", commandID)
}

// unusedGroupNameLocked は、まだ使われていない「グループ N」のうち N が最も小さい名前を返す
func (inst *clientInstance) unusedGroupNameLocked() string {
	used := make(map[string]bool, len(inst.commandGroups))
	for _, g := range inst.commandGroups {
		used[g.name] = true
	}
	for n := 1; ; n++ {
		if name := fmt.Sprintf("グループ %d", n); !used[name] {
			return name
		}
	}
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

// GetClientCommandGroups は接続先のコマンドグループを、追加した順に返す
func (s *PLCService) GetClientCommandGroups(id string) ([]ClientCommandGroupDTO, error) {
	var result []ClientCommandGroupDTO
	err := s.withClient(id, func(inst *clientInstance) error {
		result = make([]ClientCommandGroupDTO, len(inst.commandGroups))
		for i, g := range inst.commandGroups {
			result[i] = g.toDTO()
		}
		return nil
	})
	return result, err
}

// AddClientCommandGroup はコマンドグループを追加する。名前が空なら「グループ N」にする
func (s *PLCService) AddClientCommandGroup(id, name string) (*ClientCommandGroupDTO, error) {
	var dto ClientCommandGroupDTO
	err := s.withClient(id, func(inst *clientInstance) error {
		if len(inst.commandGroups) >= maxCommandGroups {
			return fmt.Errorf("コマンドグループは %d 個までです", maxCommandGroups)
		}
		n, err := cleanName(name, "グループの名前")
		if err != nil {
			return err
		}
		if n == "" {
			n = inst.unusedGroupNameLocked()
		}
		g := &commandGroup{id: uuid.NewString(), name: n}
		inst.commandGroups = append(inst.commandGroups, g)
		dto = g.toDTO()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &dto, nil
}

// RenameClientCommandGroup はコマンドグループの名前を変える
func (s *PLCService) RenameClientCommandGroup(id, groupID, name string) error {
	return s.withClient(id, func(inst *clientInstance) error {
		g, err := inst.groupLocked(groupID)
		if err != nil {
			return err
		}
		n, err := cleanName(name, "グループの名前")
		if err != nil {
			return err
		}
		if n == "" {
			return fmt.Errorf("グループの名前を入力してください")
		}
		g.name = n
		return nil
	})
}

// RemoveClientCommandGroup はコマンドグループを、保存したコマンドごと削除する
func (s *PLCService) RemoveClientCommandGroup(id, groupID string) error {
	return s.withClient(id, func(inst *clientInstance) error {
		for i, g := range inst.commandGroups {
			if g.id == groupID {
				inst.commandGroups = append(inst.commandGroups[:i:i], inst.commandGroups[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("コマンドグループが見つかりません: %s", groupID)
	})
}

// AddClientSavedCommand はコマンドをグループの末尾に保存する
func (s *PLCService) AddClientSavedCommand(id, groupID string, in ClientSavedCommandInputDTO) (*ClientSavedCommandDTO, error) {
	var dto ClientSavedCommandDTO
	err := s.withClient(id, func(inst *clientInstance) error {
		g, err := inst.groupLocked(groupID)
		if err != nil {
			return err
		}
		if len(g.commands) >= maxCommandsPerGroup {
			return fmt.Errorf("1 つのグループに保存できるコマンドは %d 個までです", maxCommandsPerGroup)
		}
		c, err := inst.newSavedCommand(in)
		if err != nil {
			return err
		}
		c.id = uuid.NewString()
		g.commands = append(g.commands, c)
		dto = c.toDTO()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &dto, nil
}

// UpdateClientSavedCommand は保存したコマンドを、入力の内容に置き換える（並び順と ID は変えない）
func (s *PLCService) UpdateClientSavedCommand(id, groupID, commandID string, in ClientSavedCommandInputDTO) error {
	return s.withClient(id, func(inst *clientInstance) error {
		g, err := inst.groupLocked(groupID)
		if err != nil {
			return err
		}
		i, err := g.commandIndex(commandID)
		if err != nil {
			return err
		}
		c, err := inst.newSavedCommand(in)
		if err != nil {
			return err
		}
		c.id = commandID
		g.commands[i] = c
		return nil
	})
}

// RenameClientSavedCommand は保存したコマンドの名前だけを変える
func (s *PLCService) RenameClientSavedCommand(id, groupID, commandID, name string) error {
	return s.withClient(id, func(inst *clientInstance) error {
		g, err := inst.groupLocked(groupID)
		if err != nil {
			return err
		}
		i, err := g.commandIndex(commandID)
		if err != nil {
			return err
		}
		n, err := cleanName(name, "コマンドの名前")
		if err != nil {
			return err
		}
		if n == "" {
			n = g.commands[i].summary
		}
		g.commands[i].name = n
		return nil
	})
}

// RemoveClientSavedCommand は保存したコマンドを削除する
func (s *PLCService) RemoveClientSavedCommand(id, groupID, commandID string) error {
	return s.withClient(id, func(inst *clientInstance) error {
		g, err := inst.groupLocked(groupID)
		if err != nil {
			return err
		}
		i, err := g.commandIndex(commandID)
		if err != nil {
			return err
		}
		g.commands = append(g.commands[:i:i], g.commands[i+1:]...)
		return nil
	})
}

// SendClientSavedCommand は保存したコマンドを送り、結果を返す（履歴には「グループ名 / コマンド名」を付けて残す）
func (s *PLCService) SendClientSavedCommand(id, groupID, commandID string) (*ClientCommandLogDTO, error) {
	var req []byte
	var label string
	err := s.withClient(id, func(inst *clientInstance) error {
		g, err := inst.groupLocked(groupID)
		if err != nil {
			return err
		}
		i, err := g.commandIndex(commandID)
		if err != nil {
			return err
		}
		req = append([]byte(nil), g.commands[i].request...)
		label = g.name + " / " + g.commands[i].name
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.sendAndLog(id, req, label)
}

// commandGroupsSnapshotLocked はプロジェクトに保存するコマンドグループを返す
func (inst *clientInstance) commandGroupsSnapshotLocked() []ClientCommandGroupDTO {
	if len(inst.commandGroups) == 0 {
		return nil
	}
	result := make([]ClientCommandGroupDTO, len(inst.commandGroups))
	for i, g := range inst.commandGroups {
		result[i] = g.toDTO()
	}
	return result
}

// restoreCommandGroupsLocked はプロジェクトのコマンドグループを戻す（使えないコマンドは読み飛ばす）。ID は作り直す
func (inst *clientInstance) restoreCommandGroupsLocked(groups []ClientCommandGroupDTO) {
	for _, gs := range groups {
		if len(inst.commandGroups) >= maxCommandGroups {
			return
		}
		name, err := cleanName(gs.Name, "グループの名前")
		if err != nil || name == "" {
			name = inst.unusedGroupNameLocked()
		}
		g := &commandGroup{id: uuid.NewString(), name: name}
		for _, cs := range gs.Commands {
			if len(g.commands) >= maxCommandsPerGroup {
				break
			}
			c, err := inst.newSavedCommand(ClientSavedCommandInputDTO{
				Name: cs.Name, Mode: cs.Mode, CommandID: cs.CommandID, Params: cs.Params, Request: cs.Request,
			})
			if err != nil {
				continue
			}
			c.id = uuid.NewString()
			g.commands = append(g.commands, c)
		}
		inst.commandGroups = append(inst.commandGroups, g)
	}
}
