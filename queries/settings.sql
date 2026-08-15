-- name: UpsertSetting :one
INSERT INTO app_settings (setting_key, setting_value, is_secret)
VALUES ($1, $2, $3)
ON CONFLICT (setting_key) DO UPDATE
SET setting_value = EXCLUDED.setting_value,
    is_secret = EXCLUDED.is_secret,
    version = app_settings.version + 1,
    updated_at = now()
RETURNING *;

-- name: GetSetting :one
SELECT *
FROM app_settings
WHERE setting_key = $1;
