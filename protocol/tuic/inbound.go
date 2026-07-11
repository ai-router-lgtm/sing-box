package tuic

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/common/listener"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/common/uot"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-quic/tuic"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/auth"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/gofrs/uuid/v5"
)

func RegisterInbound(registry *inbound.Registry) {
	inbound.Register[option.TUICInboundOptions](registry, C.TypeTUIC, NewInbound)
}

type Inbound struct {
	inbound.Adapter
	router       adapter.ConnectionRouterEx
	logger       log.ContextLogger
	listener     *listener.Listener
	tlsConfig    tls.ServerConfig
	server       *tuic.Service[int]
	userNameList []string
	userIDList   []string
	users        []option.TUICUser
	userAccess   sync.RWMutex
}

func NewInbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.TUICInboundOptions) (adapter.Inbound, error) {
	options.UDPFragmentDefault = true
	if options.TLS == nil || !options.TLS.Enabled {
		return nil, C.ErrTLSRequired
	}
	tlsConfig, err := tls.NewServer(ctx, logger, common.PtrValueOrDefault(options.TLS))
	if err != nil {
		return nil, err
	}
	inbound := &Inbound{
		Adapter: inbound.NewAdapter(C.TypeTUIC, tag),
		router:  uot.NewRouter(router, logger),
		logger:  logger,
		listener: listener.New(listener.Options{
			Context: ctx,
			Logger:  logger,
			Listen:  options.ListenOptions,
		}),
		tlsConfig: tlsConfig,
	}
	var udpTimeout time.Duration
	if options.UDPTimeout != 0 {
		udpTimeout = time.Duration(options.UDPTimeout)
	} else {
		udpTimeout = C.UDPTimeout
	}
	service, err := tuic.NewService[int](tuic.ServiceOptions{
		Context:           ctx,
		Logger:            logger,
		TLSConfig:         tlsConfig,
		CongestionControl: options.CongestionControl,
		AuthTimeout:       time.Duration(options.AuthTimeout),
		ZeroRTTHandshake:  options.ZeroRTTHandshake,
		Heartbeat:         time.Duration(options.Heartbeat),
		UDPTimeout:        udpTimeout,
		Handler:           inbound,
	})
	if err != nil {
		return nil, err
	}
	var userList []int
	var userNameList []string
	var userIDList []string
	var userUUIDList [][16]byte
	var userPasswordList []string
	for index, user := range options.Users {
		if user.UUID == "" {
			return nil, E.New("missing uuid for user ", index)
		}
		userUUID, err := uuid.FromString(user.UUID)
		if err != nil {
			return nil, E.Cause(err, "invalid uuid for user ", index)
		}
		userList = append(userList, index)
		userNameList = append(userNameList, user.Name)
		userIDList = append(userIDList, user.UUID)
		userUUIDList = append(userUUIDList, userUUID)
		userPasswordList = append(userPasswordList, user.Password)
	}
	service.UpdateUsers(userList, userUUIDList, userPasswordList)
	inbound.server = service
	inbound.userNameList = userNameList
	inbound.userIDList = userIDList
	inbound.users = append([]option.TUICUser(nil), options.Users...)
	return inbound, nil
}

func (h *Inbound) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	ctx = log.ContextWithNewID(ctx)
	var metadata adapter.InboundContext
	metadata.Inbound = h.Tag()
	metadata.InboundType = h.Type()
	//nolint:staticcheck
	metadata.InboundDetour = h.listener.ListenOptions().Detour
	//nolint:staticcheck
	metadata.OriginDestination = h.listener.UDPAddr()
	metadata.Source = source
	metadata.Destination = destination
	h.logger.InfoContext(ctx, "inbound connection from ", metadata.Source)
	userID, _ := auth.UserFromContext[int](ctx)
	userName := h.userName(userID)
	if userName == "" {
		userName = F.ToString(userID)
	}
	metadata.User = userName
	h.logger.InfoContext(ctx, "[", userName, "] inbound connection to ", metadata.Destination)
	h.router.RouteConnectionEx(ctx, conn, metadata, onClose)
}

func (h *Inbound) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	ctx = log.ContextWithNewID(ctx)
	var metadata adapter.InboundContext
	metadata.Inbound = h.Tag()
	metadata.InboundType = h.Type()
	//nolint:staticcheck
	metadata.InboundDetour = h.listener.ListenOptions().Detour
	//nolint:staticcheck
	metadata.OriginDestination = h.listener.UDPAddr()
	metadata.Source = source
	metadata.Destination = destination
	h.logger.InfoContext(ctx, "inbound packet connection from ", metadata.Source)
	userID, _ := auth.UserFromContext[int](ctx)
	userName := h.userName(userID)
	if userName == "" {
		userName = F.ToString(userID)
	}
	metadata.User = userName
	h.logger.InfoContext(ctx, "[", userName, "] inbound packet connection to ", metadata.Destination)
	h.router.RoutePacketConnectionEx(ctx, conn, metadata, onClose)
}

func (h *Inbound) Start(stage adapter.StartStage) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	if h.tlsConfig != nil {
		err := h.tlsConfig.Start()
		if err != nil {
			return err
		}
	}
	packetConn, err := h.listener.ListenUDP()
	if err != nil {
		return err
	}
	return h.server.Start(packetConn)
}

func (h *Inbound) Close() error {
	return common.Close(
		h.listener,
		h.tlsConfig,
		common.PtrOrNil(h.server),
	)
}

func (h *Inbound) userName(index int) string {
	h.userAccess.RLock()
	defer h.userAccess.RUnlock()
	if index < 0 || index >= len(h.userNameList) {
		return ""
	}
	userName := h.userNameList[index]
	if userName == "" && index < len(h.userIDList) {
		userName = h.userIDList[index]
	}
	return userName
}

func (h *Inbound) SnapshotRuntimeUsers() []adapter.RuntimeUser {
	h.userAccess.RLock()
	defer h.userAccess.RUnlock()
	users := make([]adapter.RuntimeUser, 0, len(h.users))
	for _, user := range h.users {
		users = append(users, adapter.RuntimeUser{
			Principal: user.Name,
			UUID:      user.UUID,
			Password:  user.Password,
		})
	}
	return users
}

func (h *Inbound) UpsertRuntimeUsers(users []adapter.RuntimeUser) (int, error) {
	h.userAccess.Lock()
	defer h.userAccess.Unlock()
	existing := append([]option.TUICUser(nil), h.users...)
	indexByPrincipal := make(map[string]int, len(existing))
	for i, user := range existing {
		principal := strings.TrimSpace(user.Name)
		if principal != "" {
			indexByPrincipal[principal] = i
		}
	}
	applied := 0
	for _, runtimeUser := range users {
		principal := strings.TrimSpace(runtimeUser.Principal)
		if principal == "" {
			continue
		}
		if runtimeUser.Enabled != nil && !*runtimeUser.Enabled {
			continue
		}
		tuicUser := option.TUICUser{
			Name:     principal,
			UUID:     runtimeUser.UUID,
			Password: runtimeUser.Password,
		}
		if index, found := indexByPrincipal[principal]; found {
			existing[index] = tuicUser
		} else {
			indexByPrincipal[principal] = len(existing)
			existing = append(existing, tuicUser)
		}
		applied++
	}
	if err := h.replaceUsersLocked(existing); err != nil {
		return 0, err
	}
	return applied, nil
}

func (h *Inbound) DeleteRuntimeUsers(principals []string) (int, error) {
	h.userAccess.Lock()
	defer h.userAccess.Unlock()
	if len(principals) == 0 {
		return 0, nil
	}
	deleteSet := make(map[string]struct{}, len(principals))
	for _, principal := range principals {
		principal = strings.TrimSpace(principal)
		if principal != "" {
			deleteSet[principal] = struct{}{}
		}
	}
	if len(deleteSet) == 0 {
		return 0, nil
	}
	filtered := make([]option.TUICUser, 0, len(h.users))
	deleted := 0
	for _, user := range h.users {
		if _, found := deleteSet[strings.TrimSpace(user.Name)]; found {
			deleted++
			continue
		}
		filtered = append(filtered, user)
	}
	if err := h.replaceUsersLocked(filtered); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (h *Inbound) replaceUsersLocked(users []option.TUICUser) error {
	userList := make([]int, 0, len(users))
	userNameList := make([]string, 0, len(users))
	userIDList := make([]string, 0, len(users))
	userUUIDList := make([][16]byte, 0, len(users))
	userPasswordList := make([]string, 0, len(users))
	for index, user := range users {
		if user.UUID == "" {
			return E.New("missing uuid for user ", index)
		}
		userUUID, err := uuid.FromString(user.UUID)
		if err != nil {
			return E.Cause(err, "invalid uuid for user ", index)
		}
		userList = append(userList, index)
		userNameList = append(userNameList, user.Name)
		userIDList = append(userIDList, user.UUID)
		userUUIDList = append(userUUIDList, userUUID)
		userPasswordList = append(userPasswordList, user.Password)
	}
	h.server.UpdateUsers(userList, userUUIDList, userPasswordList)
	h.users = users
	h.userNameList = userNameList
	h.userIDList = userIDList
	return nil
}
