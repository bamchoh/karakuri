package application

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"karakuri/internal/infrastructure/appconfig"

	"github.com/google/uuid"
)

// === モニタリング管理 ===

// GetMonitoringItems はモニタリング項目一覧をOrder順で返す。
// 呼び出し側がロックの外で読むため、項目はコピーを返す（並べ替えで Order が書き換わる）。
func (s *PLCService) GetMonitoringItems() []*MonitoringItemDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*MonitoringItemDTO, 0, len(s.monitoringItems))
	for _, item := range s.monitoringItems {
		cp := *item
		result = append(result, &cp)
	}

	// Order順にソート
	sort.Slice(result, func(i, j int) bool {
		return result[i].Order < result[j].Order
	})

	return result
}

// getNextOrder は次のOrder値を返す（ロック済み前提）
func (s *PLCService) getNextOrder() int {
	maxOrder := 0
	for _, item := range s.monitoringItems {
		if item.Order > maxOrder {
			maxOrder = item.Order
		}
	}
	return maxOrder + 1
}

// AddMonitoringItem はモニタリング項目を追加する
func (s *PLCService) AddMonitoringItem(item *MonitoringItemDTO) (*MonitoringItemDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// IDを生成
	item.ID = uuid.New().String()
	// Orderを設定
	item.Order = s.getNextOrder()
	stored := *item
	s.monitoringItems[item.ID] = &stored

	s.scheduleMonitoringSave()

	return item, nil
}

// MoveMonitoringItem はモニタリング項目を移動する（fromIndex → toIndex）
func (s *PLCService) MoveMonitoringItem(id string, direction string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 現在の項目を取得
	target, ok := s.monitoringItems[id]
	if !ok {
		return fmt.Errorf("monitoring item not found: %s", id)
	}

	// 全項目をOrder順にソート
	items := make([]*MonitoringItemDTO, 0, len(s.monitoringItems))
	for _, item := range s.monitoringItems {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Order < items[j].Order
	})

	// 現在のインデックスを探す
	currentIndex := -1
	for i, item := range items {
		if item.ID == id {
			currentIndex = i
			break
		}
	}

	if currentIndex == -1 {
		return fmt.Errorf("item not found in sorted list")
	}

	// 移動先インデックスを計算
	var swapIndex int
	if direction == "up" {
		if currentIndex == 0 {
			return nil // すでに先頭
		}
		swapIndex = currentIndex - 1
	} else if direction == "down" {
		if currentIndex == len(items)-1 {
			return nil // すでに末尾
		}
		swapIndex = currentIndex + 1
	} else {
		return fmt.Errorf("invalid direction: %s", direction)
	}

	// Orderを入れ替え
	target.Order, items[swapIndex].Order = items[swapIndex].Order, target.Order

	s.scheduleMonitoringSave()

	return nil
}

// ReorderMonitoringItem はモニタリング項目を指定したインデックスに移動する
func (s *PLCService) ReorderMonitoringItem(id string, newIndex int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 項目の存在確認
	if _, ok := s.monitoringItems[id]; !ok {
		return fmt.Errorf("monitoring item not found: %s", id)
	}

	// 全項目をOrder順にソート
	items := make([]*MonitoringItemDTO, 0, len(s.monitoringItems))
	for _, item := range s.monitoringItems {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Order < items[j].Order
	})

	// 現在のインデックスを探す
	currentIndex := -1
	for i, item := range items {
		if item.ID == id {
			currentIndex = i
			break
		}
	}

	if currentIndex == -1 {
		return fmt.Errorf("item not found in sorted list")
	}

	// 範囲チェック
	if newIndex < 0 {
		newIndex = 0
	}
	if newIndex >= len(items) {
		newIndex = len(items) - 1
	}

	// 同じ位置なら何もしない
	if currentIndex == newIndex {
		return nil
	}

	// 項目を新しい位置に移動（配列操作）
	item := items[currentIndex]
	// 元の位置から削除
	items = append(items[:currentIndex], items[currentIndex+1:]...)
	// 新しい位置に挿入
	items = append(items[:newIndex], append([]*MonitoringItemDTO{item}, items[newIndex:]...)...)

	// Orderを再割り当て
	for i, item := range items {
		item.Order = i
	}

	s.scheduleMonitoringSave()

	return nil
}

// UpdateMonitoringItem はモニタリング項目を更新する
func (s *PLCService) UpdateMonitoringItem(item *MonitoringItemDTO) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.monitoringItems[item.ID]; !ok {
		return fmt.Errorf("monitoring item not found: %s", item.ID)
	}

	stored := *item
	s.monitoringItems[item.ID] = &stored

	s.scheduleMonitoringSave()

	return nil
}

// DeleteMonitoringItem はモニタリング項目を削除する
func (s *PLCService) DeleteMonitoringItem(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.monitoringItems[id]; !ok {
		return fmt.Errorf("monitoring item not found: %s", id)
	}

	delete(s.monitoringItems, id)

	s.scheduleMonitoringSave()

	return nil
}

// ClearMonitoringItems は全モニタリング項目を削除する
func (s *PLCService) ClearMonitoringItems() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.monitoringItems = make(map[string]*MonitoringItemDTO)

	s.scheduleMonitoringSave()
}

// getMonitoringConfigPath はモニタリング設定ファイルのパスを返す
func getMonitoringConfigPath() (string, error) {
	return appconfig.Path("monitoring_config.json")
}

// SaveMonitoringConfig はモニタリング設定をファイルに保存する
func (s *PLCService) SaveMonitoringConfig() error {
	s.mu.Lock()
	seq, items := s.snapshotMonitoringItems()
	s.mu.Unlock()
	return s.writeMonitoringConfig(seq, items)
}

// scheduleMonitoringSave は現在のモニタリング項目をバックグラウンドで保存する（mu をロック済みで呼ぶ）。
// ロック中に内容のコピーを取り、書き込みはロックの外で行う。
func (s *PLCService) scheduleMonitoringSave() {
	seq, items := s.snapshotMonitoringItems()
	s.monitoringSaveGroup.Add(1)
	go func() {
		defer s.monitoringSaveGroup.Done()
		_ = s.writeMonitoringConfig(seq, items)
	}()
}

// snapshotMonitoringItems は保存用に項目のコピーを Order 順で作り、通し番号を付ける（mu をロック済みで呼ぶ）。
// 項目は保存後も書き換えられる（並べ替えで Order が変わる）ため、ポインタではなく値をコピーする。
func (s *PLCService) snapshotMonitoringItems() (uint64, []*MonitoringItemDTO) {
	items := make([]*MonitoringItemDTO, 0, len(s.monitoringItems))
	for _, item := range s.monitoringItems {
		cp := *item
		items = append(items, &cp)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Order < items[j].Order })
	s.monitoringSaveSeq++
	return s.monitoringSaveSeq, items
}

// writeMonitoringConfig はスナップショットをファイルに書く。
// 書き込みは 1 つずつ行い、すでにより新しい内容を書いていれば何もしない（古い内容で上書きしない）。
func (s *PLCService) writeMonitoringConfig(seq uint64, items []*MonitoringItemDTO) error {
	s.monitoringSaveMu.Lock()
	defer s.monitoringSaveMu.Unlock()
	if seq < s.monitoringSavedSeq {
		return nil
	}

	configPath, err := getMonitoringConfigPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(&MonitoringConfigDTO{Version: 1, Items: items}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return err
	}
	s.monitoringSavedSeq = seq
	return nil
}

// LoadMonitoringConfig はモニタリング設定をファイルから読み込む
func (s *PLCService) LoadMonitoringConfig() error {
	configPath, err := getMonitoringConfigPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // ファイルがなければ無視
		}
		return err
	}

	var config MonitoringConfigDTO
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// ProtocolType のない古い項目は、追加済みサーバーのプロトコルタイプを割り当てる（後方互換用）
	defaultProtocol := ""
	for pt := range s.servers {
		defaultProtocol = string(pt)
		break
	}

	s.monitoringItems = make(map[string]*MonitoringItemDTO)
	for _, item := range config.Items {
		// ProtocolType が空の場合はデフォルトを設定
		if item.ProtocolType == "" {
			item.ProtocolType = defaultProtocol
		}
		s.monitoringItems[item.ID] = item
	}

	return nil
}
