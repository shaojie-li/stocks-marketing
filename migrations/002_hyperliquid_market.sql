INSERT INTO app_settings (setting_key, setting_value, is_secret) VALUES
    ('market.hyperliquid.info_url', 'https://api.hyperliquid.xyz/info', false),
    ('market.hyperliquid.websocket_url', 'wss://api.hyperliquid.xyz/ws', false),
    ('market.hyperliquid.assets', '["xyz:MU","xyz:SMH","xyz:XYZ100","xyz:AMD","xyz:NVDA","xyz:SKHY","xyz:SMSN","xyz:KR200"]', false),
    ('market.hyperliquid.stale_after', '5s', false),
    ('market.hyperliquid.request_timeout', '15s', false),
    ('market.hyperliquid.reconnect_min', '1s', false),
    ('market.hyperliquid.reconnect_max', '30s', false)
ON CONFLICT (setting_key) DO NOTHING;

---- create above / drop below ----

DELETE FROM app_settings
WHERE setting_key IN (
    'market.hyperliquid.info_url',
    'market.hyperliquid.websocket_url',
    'market.hyperliquid.assets',
    'market.hyperliquid.stale_after',
    'market.hyperliquid.request_timeout',
    'market.hyperliquid.reconnect_min',
    'market.hyperliquid.reconnect_max'
);
