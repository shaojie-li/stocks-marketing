INSERT INTO app_settings (setting_key, setting_value, is_secret) VALUES
    ('fundamental.opendart.base_url', 'https://opendart.fss.or.kr/api', false),
    ('fundamental.opendart.corp_code', '00164779', false),
    ('fundamental.target_symbol', 'xyz:SKHY', false),
    ('fundamental.opendart.request_timeout', '30s', false)
ON CONFLICT (setting_key) DO NOTHING;

---- create above / drop below ----

DELETE FROM app_settings
WHERE setting_key IN (
    'fundamental.opendart.base_url',
    'fundamental.opendart.corp_code',
    'fundamental.target_symbol',
    'fundamental.opendart.request_timeout'
);
