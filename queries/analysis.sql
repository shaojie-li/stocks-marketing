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
