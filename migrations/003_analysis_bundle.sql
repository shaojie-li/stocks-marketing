ALTER TABLE analysis_runs
    ALTER COLUMN report DROP NOT NULL,
    ADD COLUMN phase text,
    ADD COLUMN primary_asset text,
    ADD COLUMN window_type text,
    ADD COLUMN window_start timestamptz,
    ADD COLUMN window_end timestamptz,
    ADD COLUMN as_of_bucket timestamptz,
    ADD COLUMN bundle jsonb,
    ADD CONSTRAINT analysis_runs_payload_present CHECK (report IS NOT NULL OR bundle IS NOT NULL),
    ADD CONSTRAINT analysis_runs_bundle_identity_complete CHECK (
        bundle IS NULL OR (
            phase IS NOT NULL AND primary_asset IS NOT NULL AND window_type IS NOT NULL
            AND window_start IS NOT NULL AND window_end IS NOT NULL AND as_of_bucket IS NOT NULL
        )
    );

CREATE UNIQUE INDEX analysis_runs_stable_identity_idx
ON analysis_runs (rule_version, phase, primary_asset, window_type, window_start, window_end, as_of_bucket)
WHERE bundle IS NOT NULL;

CREATE TABLE analysis_scores (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    analysis_run_id bigint NOT NULL REFERENCES analysis_runs(id),
    score_type text NOT NULL CHECK (score_type IN ('FUNDAMENTAL', 'TREND', 'ENTRY')),
    rule_version text NOT NULL,
    primary_asset text NOT NULL,
    phase text NOT NULL,
    window_type text NOT NULL,
    component_set text[] NOT NULL CHECK (cardinality(component_set) > 0),
    value numeric(3, 1) NOT NULL CHECK (value >= 0 AND value <= 10),
    direction text CHECK (direction IN ('UP', 'FLAT', 'DOWN')),
    coverage_pct smallint NOT NULL CHECK (coverage_pct BETWEEN 0 AND 100),
    confidence_max text NOT NULL CHECK (confidence_max IN ('HIGH', 'MEDIUM', 'LOW')),
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (analysis_run_id, score_type)
);

CREATE INDEX analysis_scores_comparable_idx
ON analysis_scores (score_type, rule_version, primary_asset, phase, window_type, analysis_run_id);

CREATE TABLE memory_trend_history (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    analysis_run_id bigint NOT NULL REFERENCES analysis_runs(id),
    rule_version text NOT NULL,
    primary_asset text NOT NULL,
    session_date date NOT NULL,
    previous_state text NOT NULL CHECK (previous_state IN ('WEAK', 'IMPROVING', 'CONFIRMED', 'STRONG', 'PERSISTENT_STRONG')),
    state text NOT NULL CHECK (state IN ('WEAK', 'IMPROVING', 'CONFIRMED', 'STRONG', 'PERSISTENT_STRONG')),
    day_classification text NOT NULL CHECK (day_classification IN ('SUPPORTIVE', 'NEUTRAL_DAY', 'ADVERSE', 'HARD_FAILURE', 'MARKET_CLOSED', 'DATA_UNAVAILABLE')),
    transitioned boolean NOT NULL,
    supportive_streak integer NOT NULL CHECK (supportive_streak >= 0),
    adverse_streak integer NOT NULL CHECK (adverse_streak >= 0),
    reason text NOT NULL,
    confidence_max text NOT NULL CHECK (confidence_max IN ('HIGH', 'MEDIUM', 'LOW')),
    evidence_refs jsonb NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (rule_version, primary_asset, session_date)
);

CREATE INDEX memory_trend_history_latest_idx
ON memory_trend_history (rule_version, primary_asset, session_date DESC);

---- create above / drop below ----

DROP TABLE memory_trend_history;
DROP TABLE analysis_scores;
DROP INDEX analysis_runs_stable_identity_idx;
ALTER TABLE analysis_runs
    DROP CONSTRAINT analysis_runs_bundle_identity_complete,
    DROP CONSTRAINT analysis_runs_payload_present,
    DROP COLUMN bundle,
    DROP COLUMN as_of_bucket,
    DROP COLUMN window_end,
    DROP COLUMN window_start,
    DROP COLUMN window_type,
    DROP COLUMN primary_asset,
    DROP COLUMN phase,
    ALTER COLUMN report SET NOT NULL;
