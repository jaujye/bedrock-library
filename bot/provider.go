package bot

import _ "unsafe"

import (
	"context"
	"errors"
	"fmt"
	"github.com/df-mc/atomic"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/google/uuid"
	"github.com/goxiaoy/go-eventbus"
	"patyhank_gomc/config"
	"patyhank_gomc/extra"
	"patyhank_gomc/internal/logger"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/auth"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"golang.org/x/oauth2"
	"reflect"
	"sync"
	"time"
)

var ErrConnected = errors.New("already connected")

type ClientConfig struct {
	Address string
	Token   *oauth2.Token
}

type PlayerStatus struct {
	connected    bool
	flyLock      sync.Mutex
	breakLock    sync.Mutex
	teleportChan chan any
	PlayerName   string
	CurrentForm  *Form
}

type Client struct {
	config   ClientConfig
	Config   *config.Config
	Conn     *minecraft.Conn
	Events   *Events
	world    *World
	Logger   *logger.Logger
	Screen   *ScreenManager
	Entity   *EntityManager
	Self     *Player
	EventBus *eventbus.EventBus

	*PlayerStatus
}

func init() {
	_ = extra.Init
	world_finaliseBlockRegistry()
}

// noinspection ALL
//
//go:linkname world_finaliseBlockRegistry github.com/df-mc/dragonfly/server/world.finaliseBlockRegistry
func world_finaliseBlockRegistry()

func NewClient(cfg *config.Config) *Client {
	if cfg == nil {
		cfg = config.LoadDefault()
	}

	clientLogger := logger.NewLogger(cfg)

	client := &Client{
		Config: cfg,
		Events: &Events{
			hLock:    sync.Mutex{},
			generic:  []GenericHandler{},
			handlers: map[uint32][]any{},
			tickers:  []TickHandler{},
		},
		Logger: clientLogger,
		PlayerStatus: &PlayerStatus{
			flyLock:      sync.Mutex{},
			breakLock:    sync.Mutex{},
			teleportChan: make(chan any, 255),
		},
	}

	clientLogger.LogSystemEvent("CLIENT_INIT", fmt.Sprintf("Bot client initialized with logging level: %s", cfg.Logging.Level))
	if cfg.ShouldLogToFile() {
		clientLogger.LogSystemEvent("LOGGING", fmt.Sprintf("File logging enabled: %s", cfg.Logging.LogFile))
	}

	return client
}

func (c *Client) ConnectTo(config ClientConfig) error {
	//if c.connected {
	//	return ErrConnected
	//}
	//c.connected = true
	c.config = config
	tkn := auth.RefreshTokenSource(config.Token)
	serverConn, err := minecraft.Dialer{
		TokenSource: tkn,
		ClientData: login.ClientData{
			DeviceModel:   "WTF OS 1.0",
			DeviceOS:      protocol.DeviceAndroid,
			GameVersion:   protocol.CurrentVersion,
			LanguageCode:  "zh_TW",
			ServerAddress: config.Address,
		},
	}.Dial("raknet", config.Address)
	if err != nil {
		return err
	}
	c.Conn = serverConn
	return nil
}

func (c *Client) HandleGame() error {
	c.Logger.LogConnection("GAME_START", "Starting game packet handling")
	lastTime := time.Now()
	exited := atomic.NewBool(false)

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		for {
			<-ticker.C
			if time.Now().After(lastTime.Add(time.Second * 15)) {
				err := c.Conn.Close()
				c.Conn.Flush()
				c.Logger.LogConnection("TIMEOUT", fmt.Sprintf("Connection timeout detected: %v", err))
				exited.Store(true)
			}
		}
	}()

	for {
		if exited.Load() {
			return context.DeadlineExceeded
		}
		pk, err := c.Conn.ReadPacket()
		if err != nil {
			c.connected = false
			c.Logger.LogConnection("DISCONNECT", fmt.Sprintf("Connection lost: %v", err))
			return err
		}
		lastTime = time.Now()
		id := pk.ID()

		// Log packet information based on configuration
		packetName := fmt.Sprintf("%T", pk)
		if len(packetName) > 0 && packetName[0] == '*' {
			packetName = packetName[1:] // Remove * prefix
		}
		c.Logger.LogPacket(packetName, id, pk)

		c.Events.hLock.Lock()
		handlers := c.Events.handlers[id]
		c.Events.hLock.Unlock()
		if len(handlers) > 0 {
			for _, handler := range handlers {
				res := reflect.ValueOf(handler).FieldByName("F").Call([]reflect.Value{reflect.ValueOf(c), reflect.ValueOf(pk)})
				err := res[0].Interface()
				if err != nil {
					c.Logger.LogError("PACKET_HANDLER", fmt.Sprintf("Handler error for packet %s", packetName), err.(error))
					break
				}
			}
		}
		for _, handler := range c.Events.generic {
			err := handler.F(c, pk)
			if err != nil {
				c.Logger.LogError("GENERIC_HANDLER", fmt.Sprintf("Generic handler error for packet %s", packetName), err)
				break
			}
		}
	}
}
func (c *Client) Reconnect() error {
	c.Logger.LogConnection("RECONNECT_ATTEMPT", "Attempting to reconnect to server")
	err := c.ConnectTo(c.config)
	if err != nil {
		c.Logger.LogError("RECONNECT", "Failed to reconnect", err)
		return err
	}

	c.Logger.LogConnection("RECONNECT_SUCCESS", fmt.Sprintf("Connected as %s", c.Conn.IdentityData().Identity))
	return c.HandleGame()
}

func (c *Client) OpenContainer(pos protocol.BlockPos) error {
	wID := c.Screen.OpenedWindowID.Load()
	if wID != -1 {
		c.Screen.CloseCurrentWindow()
		wID = -1
		time.Sleep(time.Second)
	}
	stack, _ := c.Screen.Inv.Item(0)

	c.Self.Yaw = 0
	c.Self.Pitch = 0
	c.SendCurrentPosition()

	c.Conn.WritePacket(&packet.InventoryTransaction{
		TransactionData: &protocol.UseItemTransactionData{
			ActionType:      protocol.UseItemActionClickBlock,
			BlockPosition:   protocol.BlockPos{int32(pos.X()), int32(pos.Y()), int32(pos.Z())},
			BlockFace:       int32(1),
			ClickedPosition: mgl32.Vec3{0, 0, 0},
			HeldItem:        InstanceFromItem(stack),
			HotBarSlot:      0,
		},
	})
	ticker := time.NewTicker(time.Millisecond * 1000)
	for i := 0; i < 20; i++ {
		<-ticker.C
		if c.Screen.OpenedWindowID.Load() != wID {
			return nil
		}
		c.Self.Yaw = 0
		c.Self.Pitch = 0
		c.SendCurrentPosition()
		c.Conn.WritePacket(&packet.InventoryTransaction{
			TransactionData: &protocol.UseItemTransactionData{
				ActionType:      protocol.UseItemActionClickBlock,
				BlockPosition:   protocol.BlockPos{int32(pos.X()), int32(pos.Y()), int32(pos.Z())},
				BlockFace:       int32(1),
				ClickedPosition: mgl32.Vec3{0, 0, 0},
				HeldItem:        InstanceFromItem(stack),
				HotBarSlot:      0,
			},
		})
	}
	return fmt.Errorf("container opened timeout %v", pos)
}
func (c *Client) OpenSystemContainer(command string) error {
	wID := c.Screen.OpenedWindowID.Load()
	err := c.SendCommand(command)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(time.Millisecond * 500)
	for i := 0; i < 20; i++ {
		<-ticker.C
		if c.Screen.OpenedWindowID.Load() != wID {
			return nil
		}
	}
	return fmt.Errorf("container opened timeout, command: %v", command)
}
func (c *Client) SendText(command string) error {
	return c.Conn.WritePacket(&packet.Text{
		TextType: packet.TextTypeChat,
		Message:  command,
	})
}
func (c *Client) SendCommand(command string) error {
	return c.Conn.WritePacket(&packet.CommandRequest{
		CommandLine: command,
		CommandOrigin: protocol.CommandOrigin{
			Origin: protocol.CommandOriginPlayer,
			UUID:   uuid.Nil,
		},
		Internal: false,
	})
}
func (c *Client) OpenInventory() {
	c.Conn.WritePacket(&packet.Interact{
		ActionType:            packet.InteractActionOpenInventory,
		TargetEntityRuntimeID: c.Conn.GameData().EntityRuntimeID,
		Position:              protocol.Option(mgl32.Vec3{0, 0, 0}),
	})
}

func (c *Client) PlaceBlock(pos cube.Pos) {
	hotbarSlot := int(c.Screen.HeldSlot.Load())
	stack, _ := c.Screen.Inv.Item(hotbarSlot)

	c.Conn.WritePacket(&packet.InventoryTransaction{
		TransactionData: &protocol.UseItemTransactionData{
			ActionType:      protocol.UseItemActionClickBlock,
			BlockPosition:   protocol.BlockPos{int32(pos.X()), int32(pos.Y()), int32(pos.Z())},
			BlockFace:       int32(0),
			ClickedPosition: mgl32.Vec3{0.5, 0.5, 0.5},
			HeldItem:        InstanceFromItem(stack),
			HotBarSlot:      int32(hotbarSlot),
		},
	})
}

func (c *Client) SendFormResponse(data string) {
	c.Conn.WritePacket(&packet.ModalFormResponse{
		FormID:       uint32(c.CurrentForm.ID),
		ResponseData: protocol.Option([]byte(data)),
	})
}

func (c *Client) SendFormClose() {
	c.Conn.WritePacket(&packet.ModalFormResponse{
		FormID:       uint32(c.CurrentForm.ID),
		CancelReason: protocol.Option(uint8(packet.ModalFormCancelReasonUserClosed)),
	})
}

func (c *Client) BreakBlock(pos cube.Pos) {
	c.breakLock.Lock()
	defer c.breakLock.Unlock()
	bPos := protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}

	//c.Conn.WritePacket(&packet.PlayerAuthInput{
	//	InputData:    protocol.NewBitset(),
	//	BlockActions: nil,
	//})

	c.Conn.WritePacket(&packet.PlayerAction{
		EntityRuntimeID: c.Self.EntityRuntimeID,
		ActionType:      protocol.PlayerActionStartBreak,
		BlockPosition:   bPos,
		BlockFace:       0,
	})

	broke := make(chan any, 255)
	disposable, _ := eventbus.Subscribe[*BrokeBlockEvent](c.EventBus)(func(ctx context.Context, event *BrokeBlockEvent) error {
		if event.Position == bPos {
			if c.World().Block(pos) != airB {
				c.Logger.LogDebug("BREAK_BLOCK", fmt.Sprintf("Block not broken yet: %v", c.World().Block(pos)))
				return nil
			}
			go func() {
				close(broke)
			}()
		}
		return nil
	})

	j, _ := c.Screen.Inv.Item(int(c.Screen.HeldSlot.Load()))
	c.Conn.WritePacket(&packet.InventoryTransaction{
		TransactionData: &protocol.UseItemTransactionData{
			ActionType:      protocol.UseItemActionBreakBlock,
			BlockPosition:   bPos,
			BlockFace:       int32(0),
			ClickedPosition: mgl32.Vec3{0.5, 0.5, 0.5},
			HeldItem:        InstanceFromItem(j),
			HotBarSlot:      int32(c.Screen.HeldSlot.Load()),
		},
	})

	breakTime := block.BreakDuration(c.World().Block(pos), j)
	if !c.Self.OnGround {
		breakTime *= 5
	}
	duration := breakTime / 20

	t := time.NewTicker(duration)
F:
	for i := 0; i < 25; i++ {
		c.Conn.WritePacket(&packet.PlayerAction{
			EntityRuntimeID: c.Self.EntityRuntimeID,
			ActionType:      protocol.PlayerActionContinueDestroyBlock,
			BlockPosition:   bPos,
			ResultPosition:  bPos,
			BlockFace:       0,
		})
		select {
		case <-broke:
			break F
		case <-t.C:
			continue
		}
	}
	t.Stop()
	disposable.Dispose()
}
