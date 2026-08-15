#!/usr/bin/env bash
set -euo pipefail

audit_host="${PRODUCTION_HOST:-aliyun-esc-singapore}"
audit_limit=10

usage() {
  echo "usage: $0 [--limit 1-100] [--host ssh-alias]" >&2
}

while (($# > 0)); do
  case "$1" in
    --limit)
      (($# >= 2)) || { usage; exit 2; }
      audit_limit="$2"
      shift 2
      ;;
    --host)
      (($# >= 2)) || { usage; exit 2; }
      audit_host="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

[[ "$audit_limit" =~ ^[0-9]+$ ]] && ((audit_limit >= 1 && audit_limit <= 100)) || {
  echo "--limit must be an integer between 1 and 100" >&2
  exit 2
}
[[ "$audit_host" =~ ^[A-Za-z0-9._@-]+$ ]] || {
  echo "--host contains unsupported characters" >&2
  exit 2
}
command -v ssh >/dev/null || { echo "ssh is required" >&2; exit 1; }

ssh -o BatchMode=yes -o ConnectTimeout=10 "$audit_host" "AUDIT_LIMIT=$audit_limit bash -s" <<'REMOTE'
set -euo pipefail

production_dir=/opt/stock-market-monitoring
[[ -d "$production_dir" && ! -L "$production_dir" ]] || {
  echo "production directory is missing or is a symbolic link" >&2
  exit 1
}
cd "$production_dir"

compose=(docker compose --env-file .env.production -f compose.production.yaml --profile shadow)
"${compose[@]}" config --quiet
postgres_id="$("${compose[@]}" ps -q postgres)"
[[ -n "$postgres_id" ]] || { echo "production postgres container is not running" >&2; exit 1; }

"${compose[@]}" exec -T postgres /bin/sh -c \
  'exec psql -X -qAt -v ON_ERROR_STOP=1 -v audit_limit="$1" -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  sh "$AUDIT_LIMIT" <<'SQL'
BEGIN TRANSACTION READ ONLY;

WITH latest AS (
    SELECT d.*
    FROM app.deliveries d
    WHERE d.status = 'succeeded'
      AND d.target_type = 'discord'
      AND d.message_type IN ('fact_flash', 'analysis_update', 'complete')
    ORDER BY d.last_response_at DESC, d.delivery_id DESC
    LIMIT :audit_limit
), report AS (
    SELECT
        l.last_response_at AS delivered_at,
        l.message_type,
        l.attempt_count AS delivery_attempts,
        left(encode(l.content_hash, 'hex'), 16) AS content_hash_prefix,
        sd.source_id,
        sd.source_external_id,
        sd.title,
        sd.document_type,
        sd.source_published_at,
        sd.source_published_date,
        sd.first_seen_at,
        extract(epoch FROM (l.last_response_at - sd.first_seen_at))::bigint AS first_seen_to_delivery_seconds,
        CASE WHEN sd.source_published_date IS NULL THEN NULL
             ELSE sd.first_seen_at::date - sd.source_published_date END AS publication_to_first_seen_days,
        e.event_type,
        ee.source_version,
        ev.version AS event_version,
        md5((ev.standardized_input - 'version' - 'unknown_fields')::text) AS core_fact_fingerprint,
        ev.standardized_input #>> '{unknown_fields,page_views,last_updated}' AS page_views_last_updated,
        av.decision AS alert_decision,
        av.reason_code AS alert_reason,
        analysis.status AS analysis_status,
        analysis.degradation_reason AS analysis_reason,
        analysis.attempts AS analysis_attempts,
        analysis.attempt_error_codes,
        analysis.cost_microunits,
        (
            SELECT count(*)
            FROM app.deliveries d2
            JOIN app.event_evidence ee2 USING (event_evidence_id, event_version_id)
            WHERE d2.status = 'succeeded'
              AND d2.message_type = l.message_type
              AND ee2.source_id = ee.source_id
              AND ee2.source_external_id IS NOT DISTINCT FROM ee.source_external_id
        ) AS same_document_successes,
        (
            SELECT count(*)
            FROM app.deliveries d2
            JOIN app.event_versions ev2 USING (event_version_id)
            WHERE d2.status = 'succeeded'
              AND d2.message_type = l.message_type
              AND md5((ev2.standardized_input - 'version' - 'unknown_fields')::text)
                  = md5((ev.standardized_input - 'version' - 'unknown_fields')::text)
        ) AS same_core_fact_successes,
        (
            SELECT count(*)
            FROM app.deliveries d2
            JOIN app.event_versions ev2 USING (event_version_id)
            WHERE d2.status = 'succeeded'
              AND d2.message_type = l.message_type
              AND ev2.event_id = ev.event_id
        ) AS same_event_successes
    FROM latest l
    JOIN app.event_versions ev USING (event_version_id)
    JOIN app.events e USING (event_id)
    JOIN app.event_evidence ee
      ON ee.event_evidence_id = l.event_evidence_id
     AND ee.event_version_id = l.event_version_id
    JOIN app.document_versions dv USING (document_version_id)
    JOIN app.document_artifacts da USING (artifact_id)
    JOIN app.source_documents sd USING (document_id)
    JOIN app.alert_versions av USING (alert_version_id)
    LEFT JOIN LATERAL (
        SELECT
            a.status,
            a.degradation_reason,
            count(at.analysis_attempt_id) AS attempts,
            coalesce(
                jsonb_agg(DISTINCT at.error_code) FILTER (WHERE at.error_code IS NOT NULL),
                '[]'::jsonb
            ) AS attempt_error_codes,
            coalesce(sum(at.cost_microunits), 0) AS cost_microunits
        FROM app.analyses a
        LEFT JOIN app.analysis_attempts at USING (analysis_id)
        WHERE a.event_version_id = l.event_version_id
        GROUP BY a.analysis_id, a.status, a.degradation_reason, a.created_at
        ORDER BY a.created_at DESC
        LIMIT 1
    ) analysis ON true
), status_counts AS (
    SELECT coalesce(jsonb_object_agg(status, amount), '{}'::jsonb) AS value
    FROM (
        SELECT coalesce(analysis_status, 'none') AS status, count(*) AS amount
        FROM report
        GROUP BY 1
        ORDER BY 1
    ) counts
)
SELECT jsonb_pretty(jsonb_build_object(
    'schema_version', 1,
    'generated_at', now(),
    'limit', :audit_limit,
    'summary', jsonb_build_object(
        'selected_pushes', count(*),
        'distinct_documents', count(DISTINCT (source_id, source_external_id)),
        'distinct_core_facts', count(DISTINCT core_fact_fingerprint),
        'rows_repeating_document', count(*) FILTER (WHERE same_document_successes > 1),
        'rows_repeating_core_facts', count(*) FILTER (WHERE same_core_fact_successes > 1),
        'analysis_statuses', (SELECT value FROM status_counts)
    ),
    'pushes', coalesce(
        jsonb_agg(to_jsonb(report) ORDER BY delivered_at DESC),
        '[]'::jsonb
    )
))
FROM report;

COMMIT;
SQL
REMOTE
