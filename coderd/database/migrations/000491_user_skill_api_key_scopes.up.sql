ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'user_skill:create';
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'user_skill:read';
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'user_skill:update';
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'user_skill:delete';
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'user_skill:*';
