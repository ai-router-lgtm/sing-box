package trafficontrol

import (
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/common/compatible"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/observable"
	"github.com/sagernet/sing/common/x/list"

	"github.com/gofrs/uuid/v5"
)

type ConnectionEventType int

const (
	ConnectionEventNew ConnectionEventType = iota
	ConnectionEventUpdate
	ConnectionEventClosed
)

type ConnectionEvent struct {
	Type          ConnectionEventType
	ID            uuid.UUID
	Metadata      *TrackerMetadata
	UplinkDelta   int64
	DownlinkDelta int64
	ClosedAt      time.Time
}

const closedConnectionsLimit = 1000

type PrincipalPolicy struct {
	Principal      string `json:"principal"`
	MaxConnections int    `json:"max_connections"`
	UpBPS          int64  `json:"up_bps"`
	DownBPS        int64  `json:"down_bps"`
}

type PrincipalSnapshot struct {
	Principal      string `json:"principal"`
	Active         int    `json:"active"`
	Upload         int64  `json:"upload"`
	Download       int64  `json:"download"`
	MaxConnections int    `json:"max_connections,omitempty"`
	UpBPS          int64  `json:"up_bps,omitempty"`
	DownBPS        int64  `json:"down_bps,omitempty"`
}

type Manager struct {
	uploadTotal   atomic.Int64
	downloadTotal atomic.Int64

	connections             compatible.Map[uuid.UUID, Tracker]
	closedConnectionsAccess sync.Mutex
	closedConnections       list.List[TrackerMetadata]
	memory                  uint64

	policiesAccess sync.RWMutex
	policies       map[string]PrincipalPolicy
	policyRevision atomic.Int64

	eventSubscriber *observable.Subscriber[ConnectionEvent]
}

func NewManager() *Manager {
	return &Manager{
		policies: make(map[string]PrincipalPolicy),
	}
}

func (m *Manager) SetEventHook(subscriber *observable.Subscriber[ConnectionEvent]) {
	m.eventSubscriber = subscriber
}

func (m *Manager) Join(c Tracker) bool {
	metadata := c.Metadata()
	principal := metadataPrincipal(metadata)
	if principal != "" && !m.allowPrincipalJoin(principal) {
		_ = c.Close()
		return false
	}
	m.connections.Store(metadata.ID, c)
	if m.eventSubscriber != nil {
		m.eventSubscriber.Emit(ConnectionEvent{
			Type:     ConnectionEventNew,
			ID:       metadata.ID,
			Metadata: metadata,
		})
	}
	return true
}

func (m *Manager) Leave(c Tracker) {
	metadata := c.Metadata()
	_, loaded := m.connections.LoadAndDelete(metadata.ID)
	if loaded {
		closedAt := time.Now()
		metadata.ClosedAt = closedAt
		metadataCopy := *metadata
		m.closedConnectionsAccess.Lock()
		if m.closedConnections.Len() >= closedConnectionsLimit {
			m.closedConnections.PopFront()
		}
		m.closedConnections.PushBack(metadataCopy)
		m.closedConnectionsAccess.Unlock()
		if m.eventSubscriber != nil {
			m.eventSubscriber.Emit(ConnectionEvent{
				Type:     ConnectionEventClosed,
				ID:       metadata.ID,
				Metadata: &metadataCopy,
				ClosedAt: closedAt,
			})
		}
	}
}

func (m *Manager) PushUploaded(size int64) {
	m.uploadTotal.Add(size)
}

func (m *Manager) PushDownloaded(size int64) {
	m.downloadTotal.Add(size)
}

func (m *Manager) Total() (up int64, down int64) {
	return m.uploadTotal.Load(), m.downloadTotal.Load()
}

func (m *Manager) ConnectionsLen() int {
	return m.connections.Len()
}

func (m *Manager) Connections() []*TrackerMetadata {
	var connections []*TrackerMetadata
	m.connections.Range(func(_ uuid.UUID, value Tracker) bool {
		connections = append(connections, value.Metadata())
		return true
	})
	return connections
}

func (m *Manager) ClosedConnections() []*TrackerMetadata {
	m.closedConnectionsAccess.Lock()
	values := m.closedConnections.Array()
	m.closedConnectionsAccess.Unlock()
	if len(values) == 0 {
		return nil
	}
	connections := make([]*TrackerMetadata, len(values))
	for i := range values {
		connections[i] = &values[i]
	}
	return connections
}

func (m *Manager) Connection(id uuid.UUID) Tracker {
	connection, loaded := m.connections.Load(id)
	if !loaded {
		return nil
	}
	return connection
}

func (m *Manager) Snapshot() *Snapshot {
	var connections []Tracker
	m.connections.Range(func(_ uuid.UUID, value Tracker) bool {
		if value.Metadata().OutboundType != C.TypeDNS {
			connections = append(connections, value)
		}
		return true
	})

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	m.memory = memStats.StackInuse + memStats.HeapInuse + memStats.HeapIdle - memStats.HeapReleased

	return &Snapshot{
		Upload:      m.uploadTotal.Load(),
		Download:    m.downloadTotal.Load(),
		Connections: connections,
		Memory:      m.memory,
	}
}

func (m *Manager) ResetStatistic() {
	m.uploadTotal.Store(0)
	m.downloadTotal.Store(0)
}

func (m *Manager) CurrentPolicyRevision() int64 {
	return m.policyRevision.Load()
}

func (m *Manager) ApplyPolicyRevision(revision int64, replace bool, policies []PrincipalPolicy) bool {
	if revision > 0 && revision < m.policyRevision.Load() {
		return false
	}

	m.policiesAccess.Lock()
	if replace {
		m.policies = make(map[string]PrincipalPolicy, len(policies))
	}
	for _, policy := range policies {
		principal := strings.TrimSpace(policy.Principal)
		if principal == "" {
			continue
		}
		policy.Principal = principal
		if policy.MaxConnections < 0 {
			policy.MaxConnections = 0
		}
		if policy.UpBPS < 0 {
			policy.UpBPS = 0
		}
		if policy.DownBPS < 0 {
			policy.DownBPS = 0
		}
		m.policies[principal] = policy
	}
	m.policiesAccess.Unlock()

	if revision > 0 {
		m.policyRevision.Store(revision)
	}
	return true
}

func (m *Manager) DisconnectPrincipal(principal string) int {
	principal = strings.TrimSpace(principal)
	if principal == "" {
		return 0
	}
	var selected []Tracker
	m.connections.Range(func(_ uuid.UUID, value Tracker) bool {
		metadata := value.Metadata()
		if metadataPrincipal(metadata) == principal {
			selected = append(selected, value)
		}
		return true
	})
	for _, tracker := range selected {
		_ = tracker.Close()
	}
	return len(selected)
}

func (m *Manager) DisconnectUser(userID string) int {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return 0
	}
	prefix := userID + ":"
	var selected []Tracker
	m.connections.Range(func(_ uuid.UUID, value Tracker) bool {
		principal := metadataPrincipal(value.Metadata())
		if principal == userID || strings.HasPrefix(principal, prefix) {
			selected = append(selected, value)
		}
		return true
	})
	for _, tracker := range selected {
		_ = tracker.Close()
	}
	return len(selected)
}

func (m *Manager) SnapshotByPrincipal() []PrincipalSnapshot {
	snapshotMap := make(map[string]PrincipalSnapshot)
	m.connections.Range(func(_ uuid.UUID, value Tracker) bool {
		metadata := value.Metadata()
		principal := metadataPrincipal(metadata)
		if principal == "" {
			return true
		}
		current := snapshotMap[principal]
		current.Principal = principal
		current.Active++
		current.Upload += metadata.Upload.Load()
		current.Download += metadata.Download.Load()
		snapshotMap[principal] = current
		return true
	})

	for principal, current := range snapshotMap {
		if policy, loaded := m.PolicyForPrincipal(principal); loaded {
			current.MaxConnections = policy.MaxConnections
			current.UpBPS = policy.UpBPS
			current.DownBPS = policy.DownBPS
			snapshotMap[principal] = current
		}
	}

	m.policiesAccess.RLock()
	for principal, policy := range m.policies {
		current := snapshotMap[principal]
		current.Principal = principal
		current.MaxConnections = policy.MaxConnections
		current.UpBPS = policy.UpBPS
		current.DownBPS = policy.DownBPS
		snapshotMap[principal] = current
	}
	m.policiesAccess.RUnlock()

	result := make([]PrincipalSnapshot, 0, len(snapshotMap))
	for _, item := range snapshotMap {
		result = append(result, item)
	}
	return result
}

func (m *Manager) PolicyForPrincipal(principal string) (PrincipalPolicy, bool) {
	principal = strings.TrimSpace(principal)
	if principal == "" {
		return PrincipalPolicy{}, false
	}
	m.policiesAccess.RLock()
	policy, ok := m.policyForPrincipalLocked(principal)
	m.policiesAccess.RUnlock()
	return policy, ok
}

func (m *Manager) allowPrincipalJoin(principal string) bool {
	m.policiesAccess.RLock()
	policy, ok := m.policyForPrincipalLocked(principal)
	m.policiesAccess.RUnlock()
	if !ok || policy.MaxConnections <= 0 {
		return true
	}

	active := 0
	m.connections.Range(func(_ uuid.UUID, value Tracker) bool {
		if metadataPrincipal(value.Metadata()) == principal {
			active++
		}
		return true
	})
	return active < policy.MaxConnections
}

func metadataPrincipal(metadata *TrackerMetadata) string {
	if metadata == nil {
		return ""
	}
	return strings.TrimSpace(metadata.Metadata.User)
}

func (m *Manager) policyForPrincipalLocked(principal string) (PrincipalPolicy, bool) {
	if policy, found := m.policies[principal]; found {
		return policy, true
	}
	if userID := principalUserID(principal); userID != "" {
		if policy, found := m.policies[userID+":*"]; found {
			return policy, true
		}
	}
	return PrincipalPolicy{}, false
}

func principalUserID(principal string) string {
	principal = strings.TrimSpace(principal)
	if principal == "" {
		return ""
	}
	parts := strings.SplitN(principal, ":", 2)
	return strings.TrimSpace(parts[0])
}

type Snapshot struct {
	Download    int64
	Upload      int64
	Connections []Tracker
	Memory      uint64
}

func (s *Snapshot) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"downloadTotal": s.Download,
		"uploadTotal":   s.Upload,
		"connections":   common.Map(s.Connections, func(t Tracker) *TrackerMetadata { return t.Metadata() }),
		"memory":        s.Memory,
	})
}
