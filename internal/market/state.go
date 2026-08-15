package market

import (
	"math/big"
	"regexp"
	"sync"
	"time"
)

var decimalPattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)

type Reason string

const (
	ReasonUnavailable      Reason = "UNAVAILABLE"
	ReasonDelisted         Reason = "ASSET_DELISTED"
	ReasonOpenInterestZero Reason = "OPEN_INTEREST_ZERO"
	ReasonMissingPrice     Reason = "MARK_OR_ORACLE_MISSING"
	ReasonInvalidBook      Reason = "BOOK_EMPTY_OR_ONE_SIDED"
	ReasonStale            Reason = "STALE"
	ReasonRecoveryPending  Reason = "RECOVERY_PENDING"
	ReasonOpenInterestCap  Reason = "OPEN_INTEREST_CAP_REACHED"
)

type Snapshot struct {
	Symbol            string
	MarkPrice         string
	OraclePrice       string
	OpenInterest      string
	Funding           string
	ChangePercent     string
	BestBid           string
	BestAsk           string
	ExchangeTime      time.Time
	ReceivedAt        time.Time
	Source            string
	MarketStatus      string
	Generation        uint64
	Delisted          bool
	AtOpenInterestCap bool
}

type Eligibility struct {
	AnalysisEligible bool     `json:"analysis_eligible"`
	EntryEligible    bool     `json:"entry_eligible"`
	AnalysisReasons  []Reason `json:"analysis_reasons"`
	EntryReasons     []Reason `json:"entry_reasons"`
}

type State struct {
	mu                 sync.RWMutex
	watched            map[string]struct{}
	snapshots          map[string]Snapshot
	staleAfter         time.Duration
	recoveryPending    bool
	recoveryGeneration uint64
}

func NewState(symbols []string, staleAfter time.Duration) *State {
	watched := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		watched[symbol] = struct{}{}
	}
	return &State{watched: watched, snapshots: make(map[string]Snapshot), staleAfter: staleAfter}
}

func (s *State) Replace(next Snapshot) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.watched[next.Symbol]; !ok {
		return false
	}
	current, ok := s.snapshots[next.Symbol]
	if ok && (next.Generation < current.Generation || next.ExchangeTime.Before(current.ExchangeTime)) {
		return false
	}
	s.snapshots[next.Symbol] = next
	return true
}

func (s *State) ReplaceBatch(next []Snapshot) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(next) != len(s.watched) {
		return false
	}
	seen := make(map[string]struct{}, len(next))
	for _, snapshot := range next {
		if _, ok := s.watched[snapshot.Symbol]; !ok {
			return false
		}
		if _, duplicate := seen[snapshot.Symbol]; duplicate {
			return false
		}
		seen[snapshot.Symbol] = struct{}{}
		if current, ok := s.snapshots[snapshot.Symbol]; ok && (snapshot.Generation < current.Generation || snapshot.ExchangeTime.Before(current.ExchangeTime)) {
			return false
		}
	}
	for _, snapshot := range next {
		s.snapshots[snapshot.Symbol] = snapshot
	}
	return true
}

func (s *State) Snapshot(symbol string) (Snapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.snapshots[symbol]
	return snapshot, ok
}

func (s *State) Update(symbol string, generation uint64, update func(*Snapshot)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.snapshots[symbol]
	if !ok || snapshot.Generation != generation {
		return false
	}
	update(&snapshot)
	s.snapshots[symbol] = snapshot
	return true
}

func (s *State) BeginRecovery() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoveryPending = true
	s.recoveryGeneration = 0
}

func (s *State) CompleteRecovery(generation uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoveryGeneration = generation
	for _, snapshot := range s.snapshots {
		if snapshot.Generation < generation {
			return
		}
	}
	s.recoveryPending = false
}

func (s *State) Eligibility(symbol string, now time.Time) Eligibility {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.snapshots[symbol]
	reasons := make([]Reason, 0, 4)
	if _, watched := s.watched[symbol]; !watched || !ok {
		reasons = append(reasons, ReasonUnavailable)
	} else {
		if snapshot.Delisted {
			reasons = append(reasons, ReasonDelisted)
		}
		if !positive(snapshot.OpenInterest) {
			reasons = append(reasons, ReasonOpenInterestZero)
		}
		if !positive(snapshot.MarkPrice) || !positive(snapshot.OraclePrice) {
			reasons = append(reasons, ReasonMissingPrice)
		}
		if !positive(snapshot.BestBid) || !positive(snapshot.BestAsk) {
			reasons = append(reasons, ReasonInvalidBook)
		}
		exchangeAge := now.Sub(snapshot.ExchangeTime)
		receiveAge := now.Sub(snapshot.ReceivedAt)
		if snapshot.ExchangeTime.IsZero() || snapshot.ReceivedAt.IsZero() || exchangeAge > s.staleAfter || exchangeAge < -s.staleAfter || receiveAge > s.staleAfter || receiveAge < -s.staleAfter {
			reasons = append(reasons, ReasonStale)
		}
	}
	if s.recoveryPending {
		reasons = append(reasons, ReasonRecoveryPending)
	}
	entryReasons := make([]Reason, 0, 1)
	if ok && snapshot.AtOpenInterestCap {
		entryReasons = append(entryReasons, ReasonOpenInterestCap)
	}
	analysisEligible := len(reasons) == 0
	return Eligibility{
		AnalysisEligible: analysisEligible,
		EntryEligible:    analysisEligible && len(entryReasons) == 0,
		AnalysisReasons:  reasons,
		EntryReasons:     entryReasons,
	}
}

func positive(value string) bool {
	if !decimalPattern.MatchString(value) {
		return false
	}
	n, ok := new(big.Rat).SetString(value)
	return ok && n.Sign() > 0
}
