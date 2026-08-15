-- name: InsertAnalysisRun :one
INSERT INTO analysis_runs (input_hash, rule_version, report, indicators)
VALUES ($1, $2, $3, $4)
ON CONFLICT (input_hash) DO NOTHING
RETURNING *;

-- name: GetAnalysisRunByInputHash :one
SELECT *
FROM analysis_runs
WHERE input_hash = $1;

-- name: GetAnalysisRun :one
SELECT *
FROM analysis_runs
WHERE id = $1;

-- name: CountAnalysisRuns :one
SELECT count(*)
FROM analysis_runs;

-- name: InsertBundleAnalysisRun :one
INSERT INTO analysis_runs (
    input_hash, rule_version, phase, primary_asset, window_type,
    window_start, window_end, as_of_bucket, bundle, indicators
) VALUES (
    sqlc.arg(input_hash), sqlc.arg(rule_version), sqlc.arg(phase), sqlc.arg(primary_asset), sqlc.arg(window_type),
    sqlc.arg(window_start)::timestamptz, sqlc.arg(window_end)::timestamptz,
    sqlc.arg(as_of_bucket)::timestamptz, sqlc.arg(bundle), sqlc.arg(indicators)
)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetBundleAnalysisRunByIdentity :one
SELECT *
FROM analysis_runs
WHERE rule_version = sqlc.arg(rule_version)
  AND phase = sqlc.arg(phase)
  AND primary_asset = sqlc.arg(primary_asset)
  AND window_type = sqlc.arg(window_type)
  AND window_start = sqlc.arg(window_start)::timestamptz
  AND window_end = sqlc.arg(window_end)::timestamptz
  AND as_of_bucket = sqlc.arg(as_of_bucket)::timestamptz
  AND bundle IS NOT NULL;

-- name: InsertAnalysisScore :one
INSERT INTO analysis_scores (
    analysis_run_id, score_type, rule_version, primary_asset, phase, window_type,
    component_set, value, direction, coverage_pct, confidence_max, payload
) VALUES (
    sqlc.arg(analysis_run_id), sqlc.arg(score_type), sqlc.arg(rule_version), sqlc.arg(primary_asset),
    sqlc.arg(phase), sqlc.arg(window_type), sqlc.arg(component_set), sqlc.arg(value)::numeric,
    NULLIF(sqlc.arg(direction), ''), sqlc.arg(coverage_pct), sqlc.arg(confidence_max), sqlc.arg(payload)
)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetAnalysisScoreByRun :one
SELECT * FROM analysis_scores
WHERE analysis_run_id = sqlc.arg(analysis_run_id) AND score_type = sqlc.arg(score_type);

-- name: FindComparableAnalysisScore :one
SELECT s.*
FROM analysis_scores s
JOIN analysis_runs r ON r.id = s.analysis_run_id
WHERE s.score_type = sqlc.arg(score_type)
  AND s.rule_version = sqlc.arg(rule_version)
  AND s.primary_asset = sqlc.arg(primary_asset)
  AND s.phase = sqlc.arg(phase)
  AND s.window_type = sqlc.arg(window_type)
  AND s.component_set = sqlc.arg(component_set)
  AND r.window_end < sqlc.arg(before_window_end)::timestamptz
ORDER BY r.window_end DESC, s.id DESC
LIMIT 1;

-- name: InsertMemoryTrendHistory :one
INSERT INTO memory_trend_history (
    analysis_run_id, rule_version, primary_asset, session_date, previous_state, state,
    day_classification, transitioned, supportive_streak, adverse_streak, reason,
    confidence_max, evidence_refs, payload
) VALUES (
    sqlc.arg(analysis_run_id), sqlc.arg(rule_version), sqlc.arg(primary_asset), sqlc.arg(session_date)::date,
    sqlc.arg(previous_state), sqlc.arg(state), sqlc.arg(day_classification), sqlc.arg(transitioned),
    sqlc.arg(supportive_streak), sqlc.arg(adverse_streak), sqlc.arg(reason),
    sqlc.arg(confidence_max), sqlc.arg(evidence_refs), sqlc.arg(payload)
)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetMemoryTrendHistoryBySession :one
SELECT * FROM memory_trend_history
WHERE rule_version = sqlc.arg(rule_version)
  AND primary_asset = sqlc.arg(primary_asset)
  AND session_date = sqlc.arg(session_date)::date;

-- name: FindLatestMemoryTrendHistory :one
SELECT * FROM memory_trend_history
WHERE rule_version = sqlc.arg(rule_version)
  AND primary_asset = sqlc.arg(primary_asset)
  AND session_date < sqlc.arg(before_session_date)::date
ORDER BY session_date DESC, id DESC
LIMIT 1;

-- name: GetLatestMemoryTrendHistory :one
SELECT * FROM memory_trend_history
WHERE rule_version = sqlc.arg(rule_version)
  AND primary_asset = sqlc.arg(primary_asset)
ORDER BY session_date DESC, id DESC
LIMIT 1;

-- name: InsertDeliveryAttempt :one
INSERT INTO delivery_attempts (analysis_run_id, idempotency_key, status)
VALUES ($1, $2, 'PENDING')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING *;

-- name: GetDeliveryAttemptByKey :one
SELECT *
FROM delivery_attempts
WHERE idempotency_key = $1;

-- name: MarkDeliverySucceeded :one
UPDATE delivery_attempts
SET status = 'SUCCEEDED',
    provider_message_id = $2,
    attempt_count = attempt_count + 1,
    last_error = NULL,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkDeliveryFailed :one
UPDATE delivery_attempts
SET status = 'FAILED',
    attempt_count = attempt_count + 1,
    last_error = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CountDeliveryAttempts :one
SELECT count(*)
FROM delivery_attempts;
