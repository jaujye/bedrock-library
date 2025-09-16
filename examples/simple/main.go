package main

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"patyhank_gomc/auth"
	"patyhank_gomc/bot"
	"patyhank_gomc/config"

	"github.com/chzyer/readline"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/sandertv/gophertunnel/minecraft/text"
)

var recvPMRegex = regexp.MustCompile("^\\[(\\w+) -> 您]\\s([\\s\\S]+)")
var recvTeleportRegex = regexp.MustCompile("^\\[系統] (\\w+) 想要傳送到 你 的位置。")
var recvTeleportHereRegex = regexp.MustCompile("^\\[系統] (\\w+) 想要你傳送到 該玩家 的位置。")

// InventoryHelper 背包操作助手（基於 ScreenManager）
// 所有資料都來自 client.Screen (ScreenManager)，不需要額外的監聽器
type InventoryHelper struct {
	client *bot.Client
}

func NewInventoryHelper(client *bot.Client) *InventoryHelper {
	return &InventoryHelper{client: client}
}

// GetInventoryInfo 獲取背包基本資訊（直接從 ScreenManager 讀取）
func (h *InventoryHelper) GetInventoryInfo() map[string]interface{} {
	screen := h.client.Screen  // ScreenManager 已由 EventsListener.Attach 初始化
	if screen == nil {
		return nil
	}

	info := map[string]interface{}{
		"total_slots":      screen.Inv.Size(),
		"occupied_slots":   0,
		"empty_slots":      0,
		"held_slot":        screen.HeldSlot.Load(),
		"container_open":   screen.ContainerOpened.Load(),
		"opened_window_id": screen.OpenedWindowID.Load(),
		"armour_equipped":  false,
		"offhand_item":     false,
	}

	// 檢查主背包
	for _, slot := range screen.Inv.Slots() {
		if slot.Empty() {
			info["empty_slots"] = info["empty_slots"].(int) + 1
		} else {
			info["occupied_slots"] = info["occupied_slots"].(int) + 1
		}
	}

	// 檢查盔甲
	if !screen.Armour.Helmet().Empty() || !screen.Armour.Chestplate().Empty() ||
		!screen.Armour.Leggings().Empty() || !screen.Armour.Boots().Empty() {
		info["armour_equipped"] = true
	}

	// 檢查副手
	offhandSlot, _ := screen.OffHand.Item(0)
	if !offhandSlot.Empty() {
		info["offhand_item"] = true
	}

	return info
}

// GetCurrentHeldItem 獲取當前手持物品
func (h *InventoryHelper) GetCurrentHeldItem() (item.Stack, int) {
	screen := h.client.Screen
	if screen == nil {
		return item.Stack{}, -1
	}

	heldSlot := int(screen.HeldSlot.Load())
	heldItem, _ := screen.Inv.Item(heldSlot)
	return heldItem, heldSlot
}

// GetArmourInfo 獲取盔甲資訊
func (h *InventoryHelper) GetArmourInfo() map[string]string {
	screen := h.client.Screen
	if screen == nil {
		return nil
	}

	armourInfo := map[string]string{
		"helmet":     "empty",
		"chestplate": "empty",
		"leggings":   "empty",
		"boots":      "empty",
	}

	if helmet := screen.Armour.Helmet(); !helmet.Empty() {
		if name, _ := helmet.Item().EncodeItem(); name != "" {
			armourInfo["helmet"] = fmt.Sprintf("%s x%d", name, helmet.Count())
		}
	}
	if chestplate := screen.Armour.Chestplate(); !chestplate.Empty() {
		if name, _ := chestplate.Item().EncodeItem(); name != "" {
			armourInfo["chestplate"] = fmt.Sprintf("%s x%d", name, chestplate.Count())
		}
	}
	if leggings := screen.Armour.Leggings(); !leggings.Empty() {
		if name, _ := leggings.Item().EncodeItem(); name != "" {
			armourInfo["leggings"] = fmt.Sprintf("%s x%d", name, leggings.Count())
		}
	}
	if boots := screen.Armour.Boots(); !boots.Empty() {
		if name, _ := boots.Item().EncodeItem(); name != "" {
			armourInfo["boots"] = fmt.Sprintf("%s x%d", name, boots.Count())
		}
	}

	return armourInfo
}

// FindItem 在背包中尋找指定物品（支援模糊搜尋）
func (h *InventoryHelper) FindItem(itemName string) []int {
	screen := h.client.Screen
	if screen == nil {
		return nil
	}

	var slots []int
	for i, slot := range screen.Inv.Slots() {
		if !slot.Empty() {
			if name, _ := slot.Item().EncodeItem(); strings.Contains(name, itemName) {
				slots = append(slots, i)
			}
		}
	}

	return slots
}

// CountItem 計算背包中指定物品的總數量
func (h *InventoryHelper) CountItem(itemName string) int {
	screen := h.client.Screen
	if screen == nil {
		return 0
	}

	total := 0
	for _, slot := range screen.Inv.Slots() {
		if !slot.Empty() {
			if name, _ := slot.Item().EncodeItem(); strings.Contains(name, itemName) {
				total += slot.Count()
			}
		}
	}

	return total
}

// GetItemInSlot 獲取指定格位的物品
func (h *InventoryHelper) GetItemInSlot(slot int) item.Stack {
	screen := h.client.Screen
	if screen == nil || slot < 0 || slot >= screen.Inv.Size() {
		return item.Stack{}
	}

	item, _ := screen.Inv.Item(slot)
	return item
}

// SwitchHotbarSlot 切換快捷欄格位
func (h *InventoryHelper) SwitchHotbarSlot(slot int) bool {
	screen := h.client.Screen
	if screen == nil || slot < 0 || slot > 8 {
		return false
	}

	screen.SetCarriedItem(slot)
	h.client.Logger.LogSystemEvent("INVENTORY", fmt.Sprintf("Switched to hotbar slot %d", slot))
	return true
}

// GetOpenedContainer 獲取當前開啟的容器資訊
func (h *InventoryHelper) GetOpenedContainer() map[string]interface{} {
	screen := h.client.Screen
	if screen == nil || !screen.ContainerOpened.Load() {
		return nil
	}

	containerInfo := map[string]interface{}{
		"window_id":     screen.OpenedWindowID.Load(),
		"container_id":  screen.OpenedContainerID.Load(),
		"position":      screen.OpenedPos.Load(),
		"container":     nil,
	}

	if window := screen.OpenedWindow.Load(); window != nil {
		containerInfo["container_size"] = window.Size()

		// 計算容器中的物品數量
		itemCount := 0
		for _, slot := range window.Slots() {
			if !slot.Empty() {
				itemCount++
			}
		}
		containerInfo["item_count"] = itemCount
	}

	return containerInfo
}

// LogInventoryStatus 記錄背包狀態
func (h *InventoryHelper) LogInventoryStatus() {
	info := h.GetInventoryInfo()
	if info != nil {
		h.client.Logger.LogSystemEvent("INVENTORY", fmt.Sprintf("Status: %d/%d slots occupied, held slot: %d, armour: %v, offhand: %v",
			info["occupied_slots"], info["total_slots"], info["held_slot"],
			info["armour_equipped"], info["offhand_item"]))
	}
}

// GetInventorySummary 獲取背包物品統計摘要
func (h *InventoryHelper) GetInventorySummary() map[string]int {
	screen := h.client.Screen
	if screen == nil {
		return nil
	}

	summary := make(map[string]int)

	// 統計主背包
	for _, slot := range screen.Inv.Slots() {
		if !slot.Empty() {
			name, _ := slot.Item().EncodeItem()
			summary[name] += slot.Count()
		}
	}

	// 統計副手
	if offhand, _ := screen.OffHand.Item(0); !offhand.Empty() {
		name, _ := offhand.Item().EncodeItem()
		summary[name+" (副手)"] += offhand.Count()
	}

	return summary
}

// PrintInventoryLayout 打印背包布局（用於調試）
func (h *InventoryHelper) PrintInventoryLayout() string {
	screen := h.client.Screen
	if screen == nil {
		return "背包未初始化"
	}

	var layout strings.Builder
	layout.WriteString("=== 背包布局 ===\n")
	layout.WriteString("快捷欄 (0-8):\n")

	// 快捷欄 (前9格)
	for i := 0; i < 9; i++ {
		item, _ := screen.Inv.Item(i)
		if !item.Empty() {
			name, _ := item.Item().EncodeItem()
			layout.WriteString(fmt.Sprintf("  [%d] %s x%d", i, name, item.Count()))
			if i == int(screen.HeldSlot.Load()) {
				layout.WriteString(" ← 手持")
			}
			layout.WriteString("\n")
		} else {
			layout.WriteString(fmt.Sprintf("  [%d] 空\n", i))
		}
	}

	layout.WriteString("\n主背包 (9-35):\n")
	// 主背包 (9-35)
	emptyCount := 0
	for i := 9; i < 36; i++ {
		item, _ := screen.Inv.Item(i)
		if !item.Empty() {
			name, _ := item.Item().EncodeItem()
			layout.WriteString(fmt.Sprintf("  [%d] %s x%d\n", i, name, item.Count()))
		} else {
			emptyCount++
		}
	}
	layout.WriteString(fmt.Sprintf("  (還有 %d 個空格位)\n", emptyCount))

	return layout.String()
}

// BlockInfo 方塊資訊輔助函數
func GetBlockInfo(client *bot.Client, pos cube.Pos) map[string]interface{} {
	botWorld := client.World()
	if botWorld == nil {
		return nil
	}

	block := botWorld.Block(pos)
	blockName, properties := block.EncodeBlock()
	runtimeID := world.BlockRuntimeID(block)
	blockEntity := botWorld.BlockEntity(pos)

	info := map[string]interface{}{
		"position":     pos,
		"name":         blockName,
		"runtime_id":   runtimeID,
		"properties":   properties,
		"block_entity": blockEntity,
		"has_nbt":      len(blockEntity) > 0,
	}

	return info
}

func main() {
	// Load configuration
	cfg, err := config.Load("config/config.toml")
	if err != nil {
		log.Printf("Failed to load config: %v, using defaults", err)
		cfg = config.LoadDefault()
	}

	// Initialize client with logging before authentication
	client := bot.NewClient(cfg)

	client.Logger.LogSystemEvent("STARTUP", "Bot application starting...")
	client.Logger.LogSystemEvent("CONFIG", fmt.Sprintf("Configuration loaded: level=%s, file_logging=%v", cfg.Logging.Level, cfg.ShouldLogToFile()))

	token, err := auth.GetToken(cfg)
	if err != nil {
		client.Logger.LogError("AUTH", "Failed to get authentication token", err)
		panic(err)
	}

	client.Logger.LogSystemEvent("AUTH", "Authentication token obtained successfully")
	err = client.ConnectTo(bot.ClientConfig{
		Address: cfg.GetServerAddress(),
		Token:   token,
	})
	if err != nil {
		panic(err)
	}

	// Attach 會自動初始化 ScreenManager 和所有背包監聽器
	(&bot.EventsListener{}).Attach(client)

	// 初始化背包助手 (使用 client.Screen 中的資料)
	invHelper := NewInventoryHelper(client)

	bot.AddListener(client, bot.PacketHandler[*packet.Text]{ // Listen any text packet
		Priority: 1024,
		F: func(client *bot.Client, p *packet.Text) error {
			cleanString := text.Clean(p.Message)
			// client.Logger.LogText("SERVER", cleanString) // Log text messages using new logger

			if result := recvPMRegex.FindStringSubmatch(cleanString); result != nil {
				if cfg.IsOwner(result[1]) {
					args := strings.Split(result[2], " ")
					switch args[0] {
					case "cmd":
						client.SendCommand(strings.Join(args[1:], " "))
					case "chat":
						client.SendText(strings.Join(args[1:], " "))
					case "getblock":
						// 查詢指定座標的方塊資訊
						if len(args) >= 4 {
							var x, y, z int
							if _, err := fmt.Sscanf(strings.Join(args[1:4], " "), "%d %d %d", &x, &y, &z); err != nil {
								client.SendText("無效的座標格式，請使用: getblock <x> <y> <z>")
								break
							}

							pos := cube.Pos{x, y, z}
							blockInfo := GetBlockInfo(client, pos)

							if blockInfo == nil {
								client.SendText("世界資料未載入")
								break
							}

							// 構建回應訊息
							response := fmt.Sprintf("座標 (%d, %d, %d) 的方塊:\n", x, y, z)
							response += fmt.Sprintf("名稱: %s\n", blockInfo["name"])
							response += fmt.Sprintf("Runtime ID: %d", blockInfo["runtime_id"])

							if properties := blockInfo["properties"]; properties != nil {
								if props, ok := properties.(map[string]any); ok && len(props) > 0 {
									response += fmt.Sprintf("\n屬性: %v", props)
								}
							}

							if blockInfo["has_nbt"].(bool) {
								entityData := blockInfo["block_entity"].(map[string]any)
								response += fmt.Sprintf("\n方塊實體: 是 (%d 個屬性)", len(entityData))
							}

							client.SendText(response)
						} else {
							client.SendText("用法: getblock <x> <y> <z>")
						}

					case "inv":
						// 背包相關指令
						if len(args) > 1 {
							switch args[1] {
							case "status":
								invHelper.LogInventoryStatus()
								info := invHelper.GetInventoryInfo()
								if info != nil {
									client.SendText(fmt.Sprintf("背包: %d/%d 格已使用, 手持格位: %d, 盔甲: %v, 副手: %v",
										info["occupied_slots"], info["total_slots"], info["held_slot"],
										info["armour_equipped"], info["offhand_item"]))
								}

							case "find":
								if len(args) > 2 {
									itemName := strings.Join(args[2:], " ")
									slots := invHelper.FindItem(itemName)
									if len(slots) > 0 {
										client.SendText(fmt.Sprintf("找到 '%s' 在格位: %v", itemName, slots))
									} else {
										client.SendText(fmt.Sprintf("未找到包含 '%s' 的物品", itemName))
									}
								} else {
									client.SendText("用法: inv find <物品名稱>")
								}

							case "count":
								if len(args) > 2 {
									itemName := strings.Join(args[2:], " ")
									count := invHelper.CountItem(itemName)
									client.SendText(fmt.Sprintf("包含 '%s' 的物品總數: %d", itemName, count))
								} else {
									client.SendText("用法: inv count <物品名稱>")
								}

							case "slot":
								if len(args) > 2 {
									slotNum := 0
									fmt.Sscanf(args[2], "%d", &slotNum)
									item := invHelper.GetItemInSlot(slotNum)
									if !item.Empty() {
										name, _ := item.Item().EncodeItem()
										client.SendText(fmt.Sprintf("格位 %d: %s x%d", slotNum, name, item.Count()))
									} else {
										client.SendText(fmt.Sprintf("格位 %d 是空的", slotNum))
									}
								} else {
									client.SendText("用法: inv slot <格位編號>")
								}

							case "held":
								heldItem, slot := invHelper.GetCurrentHeldItem()
								if !heldItem.Empty() {
									name, _ := heldItem.Item().EncodeItem()
									client.SendText(fmt.Sprintf("手持物品: %s x%d (格位 %d)", name, heldItem.Count(), slot))
								} else {
									client.SendText(fmt.Sprintf("手持格位 %d 是空的", slot))
								}

							case "armour", "armor":
								armourInfo := invHelper.GetArmourInfo()
								if armourInfo != nil {
									client.SendText(fmt.Sprintf("頭盔: %s | 胸甲: %s | 護腿: %s | 靴子: %s",
										armourInfo["helmet"], armourInfo["chestplate"],
										armourInfo["leggings"], armourInfo["boots"]))
								}

							case "switch":
								if len(args) > 2 {
									slot := 0
									fmt.Sscanf(args[2], "%d", &slot)
									if invHelper.SwitchHotbarSlot(slot) {
										client.SendText(fmt.Sprintf("已切換到快捷欄格位 %d", slot))
									} else {
										client.SendText("無效的快捷欄格位 (0-8)")
									}
								} else {
									client.SendText("用法: inv switch <格位 0-8>")
								}

							case "container":
								containerInfo := invHelper.GetOpenedContainer()
								if containerInfo != nil {
									client.SendText(fmt.Sprintf("開啟容器: WindowID=%d, 大小=%d, 物品數=%d",
										containerInfo["window_id"], containerInfo["container_size"],
										containerInfo["item_count"]))
								} else {
									client.SendText("當前沒有開啟任何容器")
								}

							case "list":
								// 使用 InventoryHelper 的統計功能
								summary := invHelper.GetInventorySummary()
								if len(summary) > 0 {
									msg := "背包物品清單:\n"
									for name, count := range summary {
										msg += fmt.Sprintf("- %s: %d\n", name, count)
									}
									client.SendText(msg)
								} else {
									client.SendText("背包是空的")
								}

							case "layout":
								// 顯示背包布局
								layout := invHelper.PrintInventoryLayout()
								client.SendText(layout)

							case "help":
								helpMsg := "背包指令 (基於 ScreenManager):\n" +
									"status - 顯示背包狀態\n" +
									"find <物品> - 尋找物品位置\n" +
									"count <物品> - 計算物品數量\n" +
									"slot <編號> - 查看指定格位\n" +
									"held - 顯示手持物品\n" +
									"armour - 顯示盔甲狀態\n" +
									"switch <0-8> - 切換快捷欄\n" +
									"container - 顯示開啟的容器\n" +
									"list - 列出所有物品統計\n" +
									"layout - 顯示詳細背包布局"
								client.SendText(helpMsg)

							default:
								client.SendText("未知指令. 使用 'inv help' 查看幫助")
							}
						} else {
							invHelper.LogInventoryStatus()
						}
					}
				}
			}

			if result := recvTeleportRegex.FindStringSubmatch(cleanString); result != nil {
				if cfg.IsOwner(result[1]) {
					client.SendCommand("/tpaccept " + result[1])
				}
			}

			if result := recvTeleportHereRegex.FindStringSubmatch(cleanString); result != nil {
				if cfg.IsOwner(result[1]) {
					client.SendCommand("/tpaccept")
				}
			}

			return nil
		},
	})

	// 注意: 所有背包監聽器都已在 screen.go 的 NewManager 中自動註冊
	// 不需要重複添加 InventoryContent, InventorySlot, ContainerOpen 等監聽器
	// client.Screen (ScreenManager) 已經自動處理並儲存所有背包資料

	ticker := time.NewTicker(10 * time.Minute)
	go func() {
		if len(cfg.Bot.Ads) == 0 {
			return
		}
		for {
			for _, ad := range cfg.Bot.Ads {
				<-ticker.C
				if strings.HasPrefix(ad, "/") { // If the ad starts with a slash, it's a command
					client.SendCommand(ad) // Send a command packet every 10 minutes
				} else {
					client.SendText(ad) // Send a text packet every 10 minutes
				}
			}
		}
	}()

	// Initialize readline
	rl, err := readline.New("> ")
	if err != nil {
		client.Logger.LogError("READLINE", "Failed to initialize readline", err)
	} else {
		// Start readline input handling in a separate goroutine
		go func() {
			defer rl.Close()
			client.Logger.LogSystemEvent("READLINE", "Console input enabled. Use '/' prefix for commands, anything else for chat.")

			for {
				line, err := rl.Readline()
				if err != nil {
					client.Logger.LogError("READLINE", "Error reading input", err)
					break
				}

				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}

				if strings.HasPrefix(line, "/") {
					// Send as command
					client.Logger.LogSystemEvent("READLINE", fmt.Sprintf("Sending command: %s", line))
					client.SendCommand(line)
				} else {
					// Send as chat text
					client.Logger.LogSystemEvent("READLINE", fmt.Sprintf("Sending chat: %s", line))
					client.SendText(line)
				}
			}
		}()
	}

	err = client.HandleGame()
	if err != nil {
		panic(err)
	}
}
