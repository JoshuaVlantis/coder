ALTER TABLE chats DROP CONSTRAINT IF EXISTS chat_acl_only_on_root_chats;
ALTER TABLE chats DROP CONSTRAINT IF EXISTS chat_group_acl_not_null_jsonb;
ALTER TABLE chats DROP CONSTRAINT IF EXISTS chat_user_acl_not_null_jsonb;
ALTER TABLE chats DROP COLUMN IF EXISTS group_acl;
ALTER TABLE chats DROP COLUMN IF EXISTS user_acl;

-- No-op: keep api_key_scope values to avoid dependency churn.
