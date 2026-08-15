package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/shaojie-li/stocks-marketing/internal/domain"
	"github.com/shaojie-li/stocks-marketing/internal/storage/postgres/db"
)

type BundleSubmission struct {
	AnalysisRunID   int64
	ScoreID         int64
	MemoryHistoryID int64
	Bundle          domain.AnalysisBundle
}

func (a *App) SubmitBundle(ctx context.Context, input domain.AnalysisBundleInput) (BundleSubmission, error) {
	input.PreviousTrend = nil
	input.PreviousMemory = nil
	base, err := domain.BuildAnalysisBundle(input)
	if err != nil {
		return BundleSubmission{}, err
	}
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return BundleSubmission{}, fmt.Errorf("begin Analysis Bundle: %w", err)
	}
	defer tx.Rollback(ctx)

	identityKey := fmt.Sprintf("analysis:%s:%s:%s:%s:%s:%s:%s",
		base.Identity.RuleVersion, base.Identity.Phase, base.Identity.PrimaryAsset, base.Identity.WindowType,
		base.Identity.WindowStart, base.Identity.WindowEnd, base.Identity.AsOfBucket)
	memoryKey := "memory:" + base.Identity.RuleVersion + ":" + base.Identity.PrimaryAsset
	lockKeys := []string{identityKey, memoryKey}
	slices.Sort(lockKeys)
	for _, key := range lockKeys {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", key); err != nil {
			return BundleSubmission{}, fmt.Errorf("lock Analysis Bundle: %w", err)
		}
	}

	queries := a.queries.WithTx(tx)
	componentSet := availableComponentSet(base.Trend)
	previousScore, err := queries.FindComparableAnalysisScore(ctx, db.FindComparableAnalysisScoreParams{
		ScoreType: "TREND", RuleVersion: base.Identity.RuleVersion, PrimaryAsset: base.Identity.PrimaryAsset,
		Phase: base.Identity.Phase, WindowType: base.Identity.WindowType, ComponentSet: componentSet,
		BeforeWindowEnd: timestamp(base.Identity.WindowEnd),
	})
	if err == nil {
		var trend domain.TrendScore
		if err := json.Unmarshal(previousScore.Payload, &trend); err != nil {
			return BundleSubmission{}, errors.New("decode comparable Trend Score")
		}
		input.PreviousTrend = &trend
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return BundleSubmission{}, fmt.Errorf("load comparable Trend Score: %w", err)
	}

	sessionDate := base.Memory.SessionDate
	latestMemory, err := queries.GetLatestMemoryTrendHistory(ctx, db.GetLatestMemoryTrendHistoryParams{
		RuleVersion: base.Identity.RuleVersion, PrimaryAsset: base.Identity.PrimaryAsset,
	})
	if err == nil && latestMemory.SessionDate.Time.After(date(sessionDate).Time) {
		return BundleSubmission{}, errors.New("Memory session is older than stored history")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return BundleSubmission{}, fmt.Errorf("load latest Memory history: %w", err)
	}
	previousMemory, err := queries.FindLatestMemoryTrendHistory(ctx, db.FindLatestMemoryTrendHistoryParams{
		RuleVersion: base.Identity.RuleVersion, PrimaryAsset: base.Identity.PrimaryAsset,
		BeforeSessionDate: date(sessionDate),
	})
	if err == nil {
		var memory domain.MemoryTrendTransition
		if err := json.Unmarshal(previousMemory.Payload, &memory); err != nil {
			return BundleSubmission{}, errors.New("decode previous Memory history")
		}
		input.PreviousMemory = &memory
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return BundleSubmission{}, fmt.Errorf("load previous Memory history: %w", err)
	}

	bundle, err := domain.BuildAnalysisBundle(input)
	if err != nil {
		return BundleSubmission{}, err
	}
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		return BundleSubmission{}, errors.New("encode Analysis Bundle")
	}
	inputHash, err := hex.DecodeString(bundle.InputHash)
	if err != nil {
		return BundleSubmission{}, errors.New("decode Analysis Bundle input hash")
	}

	indicatorsJSON, err := json.Marshal(bundle.Indicators)
	if err != nil {
		return BundleSubmission{}, errors.New("encode core indicators")
	}
	run, err := queries.InsertBundleAnalysisRun(ctx, db.InsertBundleAnalysisRunParams{
		InputHash: inputHash, RuleVersion: bundle.Identity.RuleVersion,
		Phase: textValue(bundle.Identity.Phase), PrimaryAsset: textValue(bundle.Identity.PrimaryAsset),
		WindowType: textValue(bundle.Identity.WindowType), WindowStart: timestamp(bundle.Identity.WindowStart),
		WindowEnd: timestamp(bundle.Identity.WindowEnd), AsOfBucket: timestamp(bundle.Identity.AsOfBucket),
		Bundle: bundleJSON, Indicators: indicatorsJSON,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		run, err = queries.GetBundleAnalysisRunByIdentity(ctx, db.GetBundleAnalysisRunByIdentityParams{
			RuleVersion: bundle.Identity.RuleVersion, Phase: textValue(bundle.Identity.Phase),
			PrimaryAsset: textValue(bundle.Identity.PrimaryAsset), WindowType: textValue(bundle.Identity.WindowType),
			WindowStart: timestamp(bundle.Identity.WindowStart), WindowEnd: timestamp(bundle.Identity.WindowEnd),
			AsOfBucket: timestamp(bundle.Identity.AsOfBucket),
		})
		if err == nil && (!bytes.Equal(run.InputHash, inputHash) || !sameJSON(run.Bundle, bundleJSON) || !sameJSON(run.Indicators, indicatorsJSON)) {
			return BundleSubmission{}, errors.New("analysis identity conflicts with stored bundle")
		}
		if errors.Is(err, pgx.ErrNoRows) {
			if _, hashErr := queries.GetAnalysisRunByInputHash(ctx, inputHash); hashErr == nil {
				return BundleSubmission{}, errors.New("analysis input hash conflicts with stored identity")
			} else if !errors.Is(hashErr, pgx.ErrNoRows) {
				return BundleSubmission{}, fmt.Errorf("load Analysis Bundle by input hash: %w", hashErr)
			}
		}
	}
	if err != nil {
		return BundleSubmission{}, fmt.Errorf("store Analysis Bundle: %w", err)
	}

	trendJSON, err := json.Marshal(bundle.Trend)
	if err != nil {
		return BundleSubmission{}, errors.New("encode Trend Score")
	}
	var numericValue pgtype.Numeric
	if err := numericValue.Scan(bundle.Trend.Value); err != nil {
		return BundleSubmission{}, errors.New("encode Trend Score value")
	}
	score, err := queries.InsertAnalysisScore(ctx, db.InsertAnalysisScoreParams{
		AnalysisRunID: run.ID, ScoreType: "TREND", RuleVersion: bundle.Identity.RuleVersion,
		PrimaryAsset: bundle.Identity.PrimaryAsset, Phase: bundle.Identity.Phase, WindowType: bundle.Identity.WindowType,
		ComponentSet: componentSet, Value: numericValue, Direction: string(bundle.Trend.Direction),
		CoveragePct: int16(bundle.Trend.CoveragePct), ConfidenceMax: string(bundle.Trend.ConfidenceMax), Payload: trendJSON,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		score, err = queries.GetAnalysisScoreByRun(ctx, db.GetAnalysisScoreByRunParams{AnalysisRunID: run.ID, ScoreType: "TREND"})
		if err == nil && !sameStoredScore(score, run.ID, bundle, componentSet, trendJSON) {
			return BundleSubmission{}, errors.New("analysis score conflicts with stored result")
		}
	}
	if err != nil {
		return BundleSubmission{}, fmt.Errorf("store Trend Score: %w", err)
	}

	memoryJSON, err := json.Marshal(bundle.Memory)
	if err != nil {
		return BundleSubmission{}, errors.New("encode Memory history")
	}
	evidenceJSON, err := json.Marshal(bundle.Memory.EvidenceRefs)
	if err != nil {
		return BundleSubmission{}, errors.New("encode Memory evidence")
	}
	memory, err := queries.InsertMemoryTrendHistory(ctx, db.InsertMemoryTrendHistoryParams{
		AnalysisRunID: run.ID, RuleVersion: bundle.Identity.RuleVersion, PrimaryAsset: bundle.Identity.PrimaryAsset,
		SessionDate: date(bundle.Memory.SessionDate), PreviousState: string(bundle.Memory.PreviousState),
		State: string(bundle.Memory.State), DayClassification: string(bundle.Memory.Day), Transitioned: bundle.Memory.Transitioned,
		SupportiveStreak: int32(bundle.Memory.SupportiveStreak), AdverseStreak: int32(bundle.Memory.AdverseStreak),
		Reason: bundle.Memory.Reason, ConfidenceMax: string(bundle.Memory.ConfidenceMax), EvidenceRefs: evidenceJSON, Payload: memoryJSON,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		memory, err = queries.GetMemoryTrendHistoryBySession(ctx, db.GetMemoryTrendHistoryBySessionParams{
			RuleVersion: bundle.Identity.RuleVersion, PrimaryAsset: bundle.Identity.PrimaryAsset, SessionDate: date(bundle.Memory.SessionDate),
		})
		if err == nil && !sameStoredMemory(memory, bundle, evidenceJSON, memoryJSON) {
			return BundleSubmission{}, errors.New("Memory session conflicts with stored result")
		}
	}
	if err != nil {
		return BundleSubmission{}, fmt.Errorf("store Memory history: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return BundleSubmission{}, fmt.Errorf("commit Analysis Bundle: %w", err)
	}
	return BundleSubmission{AnalysisRunID: run.ID, ScoreID: score.ID, MemoryHistoryID: memory.ID, Bundle: bundle}, nil
}

func availableComponentSet(score domain.TrendScore) []string {
	components := make([]string, 0, len(score.Components))
	for _, component := range score.Components {
		if component.Availability == domain.AvailabilityAvailable {
			components = append(components, component.Name)
		}
	}
	slices.Sort(components)
	return components
}

func textValue(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

func timestamp(value string) pgtype.Timestamptz {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return pgtype.Timestamptz{Time: parsed, Valid: true}
}

func date(value string) pgtype.Date {
	parsed, _ := time.Parse(time.DateOnly, value)
	return pgtype.Date{Time: parsed, Valid: true}
}

func sameJSON(left, right []byte) bool {
	canonicalLeft, leftErr := canonicalJSON(left)
	canonicalRight, rightErr := canonicalJSON(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(canonicalLeft, canonicalRight)
}

func sameStoredScore(stored db.AnalysisScore, runID int64, bundle domain.AnalysisBundle, componentSet []string, payload []byte) bool {
	directionMatches := (bundle.Trend.Direction == "" && !stored.Direction.Valid) ||
		(stored.Direction.Valid && stored.Direction.String == string(bundle.Trend.Direction))
	return stored.AnalysisRunID == runID && stored.ScoreType == "TREND" &&
		stored.RuleVersion == bundle.Identity.RuleVersion && stored.PrimaryAsset == bundle.Identity.PrimaryAsset &&
		stored.Phase == bundle.Identity.Phase && stored.WindowType == bundle.Identity.WindowType &&
		slices.Equal(stored.ComponentSet, componentSet) && numericText(stored.Value) == bundle.Trend.Value &&
		directionMatches && stored.CoveragePct == int16(bundle.Trend.CoveragePct) &&
		stored.ConfidenceMax == string(bundle.Trend.ConfidenceMax) && sameJSON(stored.Payload, payload)
}

func sameStoredMemory(stored db.MemoryTrendHistory, bundle domain.AnalysisBundle, evidence, payload []byte) bool {
	return stored.RuleVersion == bundle.Identity.RuleVersion && stored.PrimaryAsset == bundle.Identity.PrimaryAsset &&
		stored.SessionDate.Time.Format(time.DateOnly) == bundle.Memory.SessionDate &&
		stored.PreviousState == string(bundle.Memory.PreviousState) && stored.State == string(bundle.Memory.State) &&
		stored.DayClassification == string(bundle.Memory.Day) && stored.Transitioned == bundle.Memory.Transitioned &&
		stored.SupportiveStreak == int32(bundle.Memory.SupportiveStreak) && stored.AdverseStreak == int32(bundle.Memory.AdverseStreak) &&
		stored.Reason == bundle.Memory.Reason && stored.ConfidenceMax == string(bundle.Memory.ConfidenceMax) &&
		sameJSON(stored.EvidenceRefs, evidence) && sameJSON(stored.Payload, payload)
}

func numericText(value pgtype.Numeric) string {
	if !value.Valid || value.NaN || value.InfinityModifier != pgtype.Finite || value.Int == nil {
		return ""
	}
	rational := new(big.Rat).SetInt(value.Int)
	if value.Exp < 0 {
		rational.Quo(rational, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-value.Exp)), nil)))
	}
	return rational.FloatString(1)
}
