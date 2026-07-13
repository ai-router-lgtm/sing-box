package trafficontrol

import (
	"runtime"
	"sort"
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

const (
	closedConnectionsLimit = 1000
	disconnectCloseWorkers = 512
)

const (
	principalCounterTTL           = 30 * 24 * time.Hour
	principalCounterMaxEntries    = 2048
	principalCounterPruneInterval = time.Minute
)

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

type principalCounter struct {
	uploadTotal   atomic.Int64
	downloadTotal atomic.Int64
	lastSeenUnix  atomic.Int64
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

	limitersAccess sync.Mutex
	limiters       map[string]*principalRateLimiter

	principalCountersAccess sync.RWMutex
	principalCounters       map[string]*principalCounter
	lastPrincipalPruneUnix  atomic.Int64

	eventSubscriber    *observable.Subscriber[ConnectionEvent]
	disconnectScanHook func()
}

func NewManager() *Manager {
	return &Manager{
		policies:          make(map[string]PrincipalPolicy),
		limiters:          make(map[string]*principalRateLimiter),
		principalCounters: make(map[string]*principalCounter),
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

func (m *Manager) PushPrincipalUploaded(principal string, size int64) {
	if size <= 0 {
		return
	}
	principal = strings.TrimSpace(principal)
	if principal == "" {
		return
	}
	now := time.Now()
	counter := m.loadOrStorePrincipalCounter(principal)
	counter.uploadTotal.Add(size)
	counter.lastSeenUnix.Store(now.Unix())
	m.maybePrunePrincipalCounters(now)
}

func (m *Manager) PushPrincipalDownloaded(principal string, size int64) {
	if size <= 0 {
		return
	}
	principal = strings.TrimSpace(principal)
	if principal == "" {
		return
	}
	now := time.Now()
	counter := m.loadOrStorePrincipalCounter(principal)
	counter.downloadTotal.Add(size)
	counter.lastSeenUnix.Store(now.Unix())
	m.maybePrunePrincipalCounters(now)
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
	m.principalCountersAccess.Lock()
	m.principalCounters = make(map[string]*principalCounter)
	m.principalCountersAccess.Unlock()
}

func (m *Manager) CurrentPolicyRevision() int64 {
	return m.policyRevision.Load()
}

func (m *Manager) ApplyPolicyRevision(revision int64, replace bool, policies []PrincipalPolicy) bool {
	m.policiesAccess.Lock()
	defer m.policiesAccess.Unlock()
	if revision > 0 && revision < m.policyRevision.Load() {
		return false
	}
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
	if revision > 0 {
		m.policyRevision.Store(revision)
	}
	return true
}

func (m *Manager) DisconnectPrincipal(principal string) int {
	return m.DisconnectSelectors([]string{principal}, nil)
}

func (m *Manager) DisconnectUser(userID string) int {
	return m.DisconnectSelectors(nil, []string{userID})
}

// DisconnectSelectors closes every connection matched by an exact principal
// or a user ID. All selectors are evaluated during one connection-map scan.
func (m *Manager) DisconnectSelectors(principals, userIDs []string) int {
	principalSet := make(map[string]struct{}, len(principals))
	for _, principal := range principals {
		principal = strings.TrimSpace(principal)
		if principal != "" {
			principalSet[principal] = struct{}{}
		}
	}
	userSet := make(map[string]struct{}, len(userIDs))
	for _, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID != "" {
			userSet[userID] = struct{}{}
		}
	}
	if len(principalSet) == 0 && len(userSet) == 0 {
		return 0
	}
	if m.disconnectScanHook != nil {
		m.disconnectScanHook()
	}
	var selected []Tracker
	m.connections.Range(func(_ uuid.UUID, value Tracker) bool {
		principal := metadataPrincipal(value.Metadata())
		if _, matched := principalSet[principal]; matched {
			selected = append(selected, value)
			return true
		}
		userID := principalUserID(principal)
		if _, matched := userSet[userID]; matched {
			selected = append(selected, value)
		}
		return true
	})
	closeTrackers(selected)
	return len(selected)
}

func closeTrackers(trackers []Tracker) {
	workerCount := min(len(trackers), disconnectCloseWorkers)
	if workerCount == 0 {
		return
	}

	jobs := make(chan Tracker)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for tracker := range jobs {
				_ = tracker.Close()
			}
		}()
	}
	for _, tracker := range trackers {
		jobs <- tracker
	}
	close(jobs)
	workers.Wait()
}

// PolicySnapshot returns a stable, detached copy of all configured policies.
func (m *Manager) PolicySnapshot() []PrincipalPolicy {
	_, policies := m.PolicyStateSnapshot()
	return policies
}

// PolicyStateSnapshot returns a revision and policy copy from the same lock
// boundary so Runtime status cannot mix states from concurrent updates.
func (m *Manager) PolicyStateSnapshot() (int64, []PrincipalPolicy) {
	m.policiesAccess.RLock()
	revision := m.policyRevision.Load()
	result := make([]PrincipalPolicy, 0, len(m.policies))
	for _, policy := range m.policies {
		result = append(result, policy)
	}
	m.policiesAccess.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		return result[i].Principal < result[j].Principal
	})
	return revision, result
}

func (m *Manager) SnapshotByPrincipal() []PrincipalSnapshot {
	m.maybePrunePrincipalCounters(time.Now())
	snapshotMap := make(map[string]PrincipalSnapshot)
	m.principalCountersAccess.RLock()
	for principal, counter := range m.principalCounters {
		snapshotMap[principal] = PrincipalSnapshot{
			Principal: principal,
			Upload:    counter.uploadTotal.Load(),
			Download:  counter.downloadTotal.Load(),
		}
	}
	m.principalCountersAccess.RUnlock()

	m.connections.Range(func(_ uuid.UUID, value Tracker) bool {
		metadata := value.Metadata()
		principal := metadataPrincipal(metadata)
		if principal == "" {
			return true
		}
		current := snapshotMap[principal]
		current.Principal = principal
		current.Active++
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
	policy, _, ok := m.policyForPrincipalLocked(principal)
	m.policiesAccess.RUnlock()
	return policy, ok
}

func (m *Manager) PolicyForPrincipalResolved(principal string) (PrincipalPolicy, string, bool) {
	principal = strings.TrimSpace(principal)
	if principal == "" {
		return PrincipalPolicy{}, "", false
	}
	m.policiesAccess.RLock()
	policy, policyKey, ok := m.policyForPrincipalLocked(principal)
	m.policiesAccess.RUnlock()
	return policy, policyKey, ok
}

func (m *Manager) allowPrincipalJoin(principal string) bool {
	m.policiesAccess.RLock()
	policy, _, ok := m.policyForPrincipalLocked(principal)
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

func (m *Manager) loadOrStorePrincipalCounter(principal string) *principalCounter {
	m.principalCountersAccess.RLock()
	counter, loaded := m.principalCounters[principal]
	m.principalCountersAccess.RUnlock()
	if loaded {
		return counter
	}

	m.principalCountersAccess.Lock()
	defer m.principalCountersAccess.Unlock()
	counter, loaded = m.principalCounters[principal]
	if loaded {
		return counter
	}
	counter = &principalCounter{}
	counter.lastSeenUnix.Store(time.Now().Unix())
	m.principalCounters[principal] = counter
	return counter
}

func (m *Manager) maybePrunePrincipalCounters(now time.Time) {
	lastPruneUnix := m.lastPrincipalPruneUnix.Load()
	if lastPruneUnix > 0 && now.Sub(time.Unix(lastPruneUnix, 0)) < principalCounterPruneInterval {
		return
	}
	if !m.lastPrincipalPruneUnix.CompareAndSwap(lastPruneUnix, now.Unix()) {
		return
	}

	activePrincipals := make(map[string]struct{})
	m.connections.Range(func(_ uuid.UUID, value Tracker) bool {
		principal := metadataPrincipal(value.Metadata())
		if principal != "" {
			activePrincipals[principal] = struct{}{}
		}
		return true
	})

	type pruneCandidate struct {
		principal string
		lastSeen  int64
	}
	var evictionCandidates []pruneCandidate

	m.principalCountersAccess.Lock()
	for principal, counter := range m.principalCounters {
		if _, active := activePrincipals[principal]; active {
			continue
		}
		lastSeen := counter.lastSeenUnix.Load()
		if lastSeen <= 0 {
			lastSeen = now.Unix()
		}
		lastSeenTime := time.Unix(lastSeen, 0)
		if now.Sub(lastSeenTime) >= principalCounterTTL {
			delete(m.principalCounters, principal)
			continue
		}
		evictionCandidates = append(evictionCandidates, pruneCandidate{principal: principal, lastSeen: lastSeen})
	}
	if len(m.principalCounters) > principalCounterMaxEntries && len(evictionCandidates) > 0 {
		sort.Slice(evictionCandidates, func(i, j int) bool {
			return evictionCandidates[i].lastSeen < evictionCandidates[j].lastSeen
		})
		for _, candidate := range evictionCandidates {
			if len(m.principalCounters) <= principalCounterMaxEntries {
				break
			}
			delete(m.principalCounters, candidate.principal)
		}
	}
	m.principalCountersAccess.Unlock()
}

func (m *Manager) policyForPrincipalLocked(principal string) (PrincipalPolicy, string, bool) {
	if policy, found := m.policies[principal]; found {
		return policy, principal, true
	}
	if userID := principalUserID(principal); userID != "" {
		wildcardKey := userID + ":*"
		if policy, found := m.policies[wildcardKey]; found {
			return policy, wildcardKey, true
		}
	}
	return PrincipalPolicy{}, "", false
}

type principalRateLimiter struct {
	nextUpload   time.Time
	nextDownload time.Time
}

func (m *Manager) WaitPrincipalRateLimit(policyKey string, direction trafficDirection, bytes int64, bytesPerSecond int64) {
	if strings.TrimSpace(policyKey) == "" || bytes <= 0 || bytesPerSecond <= 0 {
		return
	}
	duration := time.Duration(float64(bytes) / float64(bytesPerSecond) * float64(time.Second))
	if duration <= 0 {
		return
	}

	now := time.Now()
	var sleepUntil time.Time

	m.limitersAccess.Lock()
	limiter, found := m.limiters[policyKey]
	if !found {
		limiter = &principalRateLimiter{}
		m.limiters[policyKey] = limiter
	}

	switch direction {
	case directionUpload:
		start := now
		if limiter.nextUpload.After(start) {
			start = limiter.nextUpload
		}
		sleepUntil = start.Add(duration)
		limiter.nextUpload = sleepUntil
	case directionDownload:
		start := now
		if limiter.nextDownload.After(start) {
			start = limiter.nextDownload
		}
		sleepUntil = start.Add(duration)
		limiter.nextDownload = sleepUntil
	default:
		m.limitersAccess.Unlock()
		return
	}
	m.limitersAccess.Unlock()

	if sleep := time.Until(sleepUntil); sleep > 0 {
		time.Sleep(sleep)
	}
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
