INSERT INTO app_settings (setting_key, setting_value, is_secret) VALUES
    ('catalyst.dart.rss_url', 'https://dart.fss.or.kr/api/companyRSS.xml', false),
    ('catalyst.dart.corp_code', '00164779', false),
    ('catalyst.target_symbol', 'xyz:SKHY', false),
    ('catalyst.benchmark_symbol', 'xyz:SMSN', false),
    ('catalyst.dart.request_timeout', '15s', false)
ON CONFLICT (setting_key) DO NOTHING;

---- create above / drop below ----

DELETE FROM app_settings
WHERE setting_key IN (
    'catalyst.dart.rss_url',
    'catalyst.dart.corp_code',
    'catalyst.target_symbol',
    'catalyst.benchmark_symbol',
    'catalyst.dart.request_timeout'
);
