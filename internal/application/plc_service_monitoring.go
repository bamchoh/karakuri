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

// GetMonitoringItems はモニタリング項目一覧をOrder順で返す
func (s *PLCService) GetMonitoringItems() []*MonitoringItemDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*MonitoringItemDTO, 0, len(s.monitoringItems))
	for _, item := range s.monitoringItems {
		result = append(result, item)
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
	s.monitoringItems[item.ID] = item

	// 自動保存
	go s.saveMonitoringConfigInternal()

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

	// 自動保存
	go s.saveMonitoringConfigInternal()

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

	// 自動保存
	go s.saveMonitoringConfigInternal()

	return nil
}

// UpdateMonitoringItem はモニタリング項目を更新する
func (s *PLCService) UpdateMonitoringItem(item *MonitoringItemDTO) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.monitoringItems[item.ID]; !ok {
		return fmt.Errorf("monitoring item not found: %s", item.ID)
	}

	s.monitoringItems[item.ID] = item

	// 自動保存
	go s.saveMonitoringConfigInternal()

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

	// 自動保存
	go s.saveMonitoringConfigInternal()

	return nil
}

// ClearMonitoringItems は全モニタリング項目を削除する
func (s *PLCService) ClearMonitoringItems() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.monitoringItems = make(map[string]*MonitoringItemDTO)

	// 自動保存
	go s.saveMonitoringConfigInternal()
}

// getMonitoringConfigPath はモニタリング設定ファイルのパスを返す
func getMonitoringConfigPath() (string, error) {
	return appconfig.Path("monitoring_config.json")
}

// SaveMonitoringConfig はモニタリング設定をファイルに保存する
func (s *PLCService) SaveMonitoringConfig() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveMonitoringConfigInternal()
}

// saveMonitoringConfigInternal は内部保存処理（ロック不要）
func (s *PLCService) saveMonitoringConfigInternal() error {
	configPath, err := getMonitoringConfigPath()
	if err != nil {
		return err
	}

	items := make([]*MonitoringItemDTO, 0, len(s.monitoringItems))
	for _, item := range s.monitoringItems {
		items = append(items, item)
	}

	config := &MonitoringConfigDTO{
		Version: 1,
		Items:   items,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
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
