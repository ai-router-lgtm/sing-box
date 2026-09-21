package vmess

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/gofrs/uuid/v5"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/common/listener"
	"github.com/sagernet/sing-box/common/mux"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/common/uot"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2ray"
	"github.com/sagernet/sing-vmess"
	"github.com/sagernet/sing-vmess/packetaddr"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/ntp"
)

func RegisterInbound(registry *inbound.Registry) {
	inbound.Register[option.VMessInboundOptions](registry, C.TypeVMess, NewInbound)
}

var _ adapter.TCPInjectableInbound = (*Inbound)(nil)
var _ adapter.AtomicRuntimeUserInbound = (*Inbound)(nil)

type Inbound struct {
	inbound.Adapter
	ctx                     context.Context
	router                  adapter.ConnectionRouterEx
	logger                  logger.ContextLogger
	listener                *listener.Listener
	service                 *vmess.Service[int]
	users                   []option.VMessUser
	tlsConfig               tls.ServerConfig
	transport               adapter.V2RayServerTransport
	userAccess              sync.RWMutex
	disableHeaderProtection bool
}

func NewInbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.VMessInboundOptions) (adapter.Inbound, error) {
	inbound := &Inbound{
		Adapter:                 inbound.NewAdapter(C.TypeVMess, tag),
		ctx:                     ctx,
		router:                  uot.NewRouter(router, logger),
		logger:                  logger,
		users:                   options.Users,
		disableHeaderProtection: options.Transport != nil && options.Transport.Type != "",
	}
	var err error
	inbound.router, err = mux.NewRouterWithOptions(inbound.router, logger, common.PtrValueOrDefault(options.Multiplex))
	if err != nil {
		return nil, err
	}
	service := inbound.newService()
	inbound.service = service
	err = service.UpdateUsers(common.MapIndexed(options.Users, func(index int, it option.VMessUser) int {
		return index
	}), common.Map(options.Users, func(it option.VMessUser) string {
		return it.UUID
	}), common.Map(options.Users, func(it option.VMessUser) int {
		return it.AlterId
	}))
	if err != nil {
		return nil, err
	}
	if options.TLS != nil {
		inbound.tlsConfig, err = tls.NewServer(ctx, logger, common.PtrValueOrDefault(options.TLS))
		if err != nil {
			return nil, err
		}
	}
	if options.Transport != nil {
		inbound.transport, err = v2ray.NewServerTransport(ctx, logger, common.PtrValueOrDefault(options.Transport), inbound.tlsConfig, (*inboundTransportHandler)(inbound))
		if err != nil {
			return nil, E.Cause(err, "create server transport: ", options.Transport.Type)
		}
	}
	inbound.listener = listener.New(listener.Options{
		Context:           ctx,
		Logger:            logger,
		Network:           []string{N.NetworkTCP},
		Listen:            options.ListenOptions,
		ConnectionHandler: inbound,
	})
	return inbound, nil
}

func (h *Inbound) newService() *vmess.Service[int] {
	var serviceOptions []vmess.ServiceOption
	if timeFunc := ntp.TimeFuncFromContext(h.ctx); timeFunc != nil {
		serviceOptions = append(serviceOptions, vmess.ServiceWithTimeFunc(timeFunc))
	}
	if h.disableHeaderProtection {
		serviceOptions = append(serviceOptions, vmess.ServiceWithDisableHeaderProtection())
	}
	return vmess.NewService[int](adapter.NewUpstreamContextHandlerEx(h.newConnectionEx, h.newPacketConnectionEx), serviceOptions...)
}

func (h *Inbound) Start(stage adapter.StartStage) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	err := h.service.Start()
	if err != nil {
		return err
	}
	if h.tlsConfig != nil {
		err = h.tlsConfig.Start()
		if err != nil {
			return err
		}
	}
	if h.transport == nil {
		return h.listener.Start()
	}
	if common.Contains(h.transport.Network(), N.NetworkTCP) {
		tcpListener, err := h.listener.ListenTCP()
		if err != nil {
			return err
		}
		go func() {
			sErr := h.transport.Serve(tcpListener)
			if sErr != nil && !E.IsClosed(sErr) {
				h.logger.Error("transport serve error: ", sErr)
			}
		}()
	}
	if common.Contains(h.transport.Network(), N.NetworkUDP) {
		udpConn, err := h.listener.ListenUDP()
		if err != nil {
			return err
		}
		go func() {
			sErr := h.transport.ServePacket(udpConn)
			if sErr != nil && !E.IsClosed(sErr) {
				h.logger.Error("transport serve error: ", sErr)
			}
		}()
	}
	return nil
}

func (h *Inbound) Close() error {
	return common.Close(
		h.service,
		h.listener,
		h.tlsConfig,
		h.transport,
	)
}

func (h *Inbound) NewConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	if h.tlsConfig != nil && h.transport == nil {
		tlsConn, err := tls.ServerHandshake(ctx, conn, h.tlsConfig)
		if err != nil {
			N.CloseOnHandshakeFailure(conn, onClose, err)
			h.logger.ErrorContext(ctx, E.Cause(err, "process connection from ", metadata.Source, ": TLS handshake"))
			return
		}
		conn = tlsConn
	}
	h.userAccess.RLock()
	service := h.service
	h.userAccess.RUnlock()
	err := service.NewConnection(adapter.WithContext(ctx, &metadata), conn, metadata.Source, onClose)
	if err != nil {
		N.CloseOnHandshakeFailure(conn, onClose, err)
		h.logger.ErrorContext(ctx, E.Cause(err, "process connection from ", metadata.Source))
	}
}

func (h *Inbound) newConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	metadata.Inbound = h.Tag()
	metadata.InboundType = h.Type()
	userIndex, loaded := auth.UserFromContext[int](ctx)
	if !loaded {
		N.CloseOnHandshakeFailure(conn, onClose, os.ErrInvalid)
		return
	}
	user := h.userName(userIndex)
	if user == "" {
		user = F.ToString(userIndex)
	}
	metadata.User = user
	h.logger.InfoContext(ctx, "[", user, "] inbound connection to ", metadata.Destination)
	h.router.RouteConnectionEx(ctx, conn, metadata, onClose)
}

func (h *Inbound) newPacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	metadata.Inbound = h.Tag()
	metadata.InboundType = h.Type()
	userIndex, loaded := auth.UserFromContext[int](ctx)
	if !loaded {
		N.CloseOnHandshakeFailure(conn, onClose, os.ErrInvalid)
		return
	}
	user := h.userName(userIndex)
	if user == "" {
		user = F.ToString(userIndex)
	}
	metadata.User = user
	if metadata.Destination.Fqdn == packetaddr.SeqPacketMagicAddress {
		metadata.Destination = M.Socksaddr{}
		conn = packetaddr.NewConn(bufio.NewNetPacketConn(conn), metadata.Destination)
		h.logger.InfoContext(ctx, "[", user, "] inbound packet addr connection")
	} else {
		h.logger.InfoContext(ctx, "[", user, "] inbound packet connection to ", metadata.Destination)
	}
	h.router.RoutePacketConnectionEx(ctx, conn, metadata, onClose)
}

func (h *Inbound) userName(index int) string {
	h.userAccess.RLock()
	defer h.userAccess.RUnlock()
	if index < 0 || index >= len(h.users) {
		return ""
	}
	user := h.users[index].Name
	if user == "" {
		user = h.users[index].UUID
	}
	return user
}

func (h *Inbound) SnapshotRuntimeUsers() []adapter.RuntimeUser {
	h.userAccess.RLock()
	defer h.userAccess.RUnlock()
	users := make([]adapter.RuntimeUser, 0, len(h.users))
	for _, user := range h.users {
		users = append(users, adapter.RuntimeUser{
			Principal: user.Name,
			UUID:      user.UUID,
			AlterID:   user.AlterId,
		})
	}
	return users
}

func (h *Inbound) UpsertRuntimeUsers(users []adapter.RuntimeUser) (int, error) {
	h.userAccess.Lock()
	defer h.userAccess.Unlock()
	existing := append([]option.VMessUser(nil), h.users...)
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
		vmessUser := option.VMessUser{
			Name:    principal,
			UUID:    runtimeUser.UUID,
			AlterId: runtimeUser.AlterID,
		}
		if index, found := indexByPrincipal[principal]; found {
			existing[index] = vmessUser
		} else {
			indexByPrincipal[principal] = len(existing)
			existing = append(existing, vmessUser)
		}
		applied++
	}
	err := h.service.UpdateUsers(common.MapIndexed(existing, func(index int, _ option.VMessUser) int {
		return index
	}), common.Map(existing, func(it option.VMessUser) string {
		return it.UUID
	}), common.Map(existing, func(it option.VMessUser) int {
		return it.AlterId
	}))
	if err != nil {
		return 0, err
	}
	h.users = existing
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
	filtered := make([]option.VMessUser, 0, len(h.users))
	deleted := 0
	for _, user := range h.users {
		if _, found := deleteSet[strings.TrimSpace(user.Name)]; found {
			deleted++
			continue
		}
		filtered = append(filtered, user)
	}
	err := h.service.UpdateUsers(common.MapIndexed(filtered, func(index int, _ option.VMessUser) int {
		return index
	}), common.Map(filtered, func(it option.VMessUser) string {
		return it.UUID
	}), common.Map(filtered, func(it option.VMessUser) int {
		return it.AlterId
	}))
	if err != nil {
		return 0, err
	}
	h.users = filtered
	return deleted, nil
}

type runtimeUserReplaceTransaction struct {
	inbound         *Inbound
	users           []option.VMessUser
	preparedService *vmess.Service[int]
	previousService *vmess.Service[int]
	result          adapter.RuntimeUserReplaceResult
}

func (h *Inbound) PrepareRuntimeUserReplace(users []adapter.RuntimeUser) (adapter.RuntimeUserReplaceTransaction, error) {
	h.userAccess.Lock()
	nextUsers := make([]option.VMessUser, 0, len(users))
	principals := make(map[string]struct{}, len(users))
	credentials := make(map[string]string, len(users))
	for index, runtimeUser := range users {
		principal := strings.TrimSpace(runtimeUser.Principal)
		if principal == "" {
			h.userAccess.Unlock()
			return nil, E.New("missing principal for runtime user ", index)
		}
		if _, loaded := principals[principal]; loaded {
			h.userAccess.Unlock()
			return nil, E.New("duplicate runtime user principal: ", principal)
		}
		credential := strings.TrimSpace(runtimeUser.UUID)
		if credential == "" {
			h.userAccess.Unlock()
			return nil, E.New("missing uuid for runtime user ", principal)
		}
		if _, err := uuid.FromString(credential); err != nil {
			h.userAccess.Unlock()
			return nil, E.Cause(err, "invalid uuid for runtime user ", principal)
		}
		if oldPrincipal, loaded := credentials[credential]; loaded {
			h.userAccess.Unlock()
			return nil, E.New("runtime user uuid is shared by ", oldPrincipal, " and ", principal)
		}
		if runtimeUser.AlterID < 0 {
			h.userAccess.Unlock()
			return nil, E.New("negative alter id for runtime user ", principal)
		}
		principals[principal] = struct{}{}
		credentials[credential] = principal
		nextUsers = append(nextUsers, option.VMessUser{
			Name:    principal,
			UUID:    credential,
			AlterId: runtimeUser.AlterID,
		})
	}
	preparedService := h.newService()
	err := preparedService.UpdateUsers(common.MapIndexed(nextUsers, func(index int, _ option.VMessUser) int {
		return index
	}), common.Map(nextUsers, func(it option.VMessUser) string {
		return it.UUID
	}), common.Map(nextUsers, func(it option.VMessUser) int {
		return it.AlterId
	}))
	if err != nil {
		h.userAccess.Unlock()
		return nil, err
	}
	if err = preparedService.Start(); err != nil {
		h.userAccess.Unlock()
		return nil, err
	}
	return &runtimeUserReplaceTransaction{
		inbound:         h,
		users:           nextUsers,
		preparedService: preparedService,
		result: adapter.RuntimeUserReplaceResult{
			Upserted: len(nextUsers),
			Deleted:  runtimeDeletedUserCount(h.users, principals),
		},
	}, nil
}

func (t *runtimeUserReplaceTransaction) Apply() adapter.RuntimeUserReplaceResult {
	t.previousService = t.inbound.service
	t.inbound.service = t.preparedService
	t.inbound.users = t.users
	return t.result
}

func (t *runtimeUserReplaceTransaction) Release() {
	t.inbound.userAccess.Unlock()
	if t.previousService != nil {
		_ = t.previousService.Close()
	}
}

func (t *runtimeUserReplaceTransaction) Abort() {
	_ = t.preparedService.Close()
	t.inbound.userAccess.Unlock()
}

func runtimeDeletedUserCount(current []option.VMessUser, desired map[string]struct{}) int {
	deleted := 0
	for _, user := range current {
		if _, loaded := desired[strings.TrimSpace(user.Name)]; !loaded {
			deleted++
		}
	}
	return deleted
}

var _ adapter.V2RayServerTransportHandler = (*inboundTransportHandler)(nil)

type inboundTransportHandler Inbound

func (h *inboundTransportHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	var metadata adapter.InboundContext
	metadata.Source = source
	metadata.Destination = destination
	//nolint:staticcheck
	metadata.InboundDetour = h.listener.ListenOptions().Detour
	//nolint:staticcheck
	h.logger.InfoContext(ctx, "inbound connection from ", metadata.Source)
	(*Inbound)(h).NewConnectionEx(ctx, conn, metadata, onClose)
}
