ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'aibridge_provider:*';
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'aibridge_provider:create';
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'aibridge_provider:delete';
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'aibridge_provider:read';
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'aibridge_provider:update';
