-- name: InsertUserSkill :one
INSERT INTO user_skills (user_id, name, description, content)
SELECT @user_id::uuid, @name::text, @description::text, @content::text
WHERE (
    SELECT count(*) FROM user_skills WHERE user_id = @user_id::uuid
) < @max_skills::int
RETURNING *;

-- name: GetUserSkillByUserIDAndName :one
SELECT *
FROM user_skills
WHERE user_id = @user_id AND name = @name;

-- name: ListUserSkillMetadataByUserID :many
SELECT
    id, user_id, name, description, created_at, updated_at
FROM user_skills
WHERE user_id = @user_id
ORDER BY name ASC;

-- name: UpdateUserSkillByUserIDAndName :one
UPDATE user_skills
SET
    description = @description,
    content     = @content,
    updated_at  = now()
WHERE user_id = @user_id AND name = @name
RETURNING *;

-- name: DeleteUserSkillByUserIDAndName :one
DELETE FROM user_skills
WHERE user_id = @user_id AND name = @name
RETURNING *;
