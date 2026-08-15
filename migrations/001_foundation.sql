CREATE TABLE app_settings (
    setting_key text PRIMARY KEY,
    setting_value text NOT NULL,
    is_secret boolean NOT NULL DEFAULT false,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (NOT is_secret OR setting_value LIKE 'v1:%')
);

CREATE TABLE analysis_runs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    input_hash bytea NOT NULL UNIQUE,
    rule_version text NOT NULL,
    report jsonb NOT NULL,
    indicators jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE delivery_attempts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    analysis_run_id bigint NOT NULL REFERENCES analysis_runs(id),
    idempotency_key text NOT NULL UNIQUE,
    status text NOT NULL CHECK (status IN ('PENDING', 'SUCCEEDED', 'FAILED')),
    provider_message_id text,
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

---- create above / drop below ----

DROP TABLE delivery_attempts;
DROP TABLE analysis_runs;
DROP TABLE app_settings;
