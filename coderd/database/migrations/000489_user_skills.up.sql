-- Creates the user_skills table and indexes.
CREATE TABLE user_skills (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    content text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX user_skills_user_id_name_idx ON user_skills (user_id, name);

-- Adds the user skill audit resource type.
ALTER TYPE resource_type ADD VALUE IF NOT EXISTS 'user_skill';

-- Adds API key scopes for managing user skills.
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'user_skill:create';
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'user_skill:read';
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'user_skill:update';
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'user_skill:delete';
ALTER TYPE api_key_scope ADD VALUE IF NOT EXISTS 'user_skill:*';
