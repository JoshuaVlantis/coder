# 0001 Personal Skills, DB-backed Chatd Integration

## Status

Accepted.

## Context

Personal skills let users carry reusable instructions across Coder chats and
workspaces. Workspace skills already come from filesystem discovery, but
personal skills need durable user-owned storage, predictable lookup behavior,
and a way for chatd to present them alongside workspace skills.

The design also needs to make collisions explicit. A personal skill and a
workspace skill can share the same kebab-case name, and silently choosing one
source would hide useful context from the user.

Personal skill content is user-authored Markdown stored by Coder. Site admins
need enough access to operate the system, investigate abuse, and recover from
support issues, but audit records should avoid copying raw skill content.

## Decision

Store personal skills in a dedicated `user_skills` table. For each chat turn,
fetch personal skill metadata fresh, combine it with workspace skill metadata,
and inject the available skills into the existing chatd skill prompt. When chatd
needs skill content, resolve personal skills through the existing `read_skill`
flow instead of syncing files into workspace filesystems.

When a personal skill and a workspace skill share a kebab-case name, expose both
with collision aliases: `personal/<name>` for the personal skill and
`workspace/<name>` for the workspace skill. Never silently override one source
with another.

Site admins can read and modify personal skill content. This is a deliberate
privacy trade-off for operability, support, and abuse handling. Audits
intentionally exclude raw Markdown content, but record the actor, target user,
and relevant metadata.

Personal skill edits affect the next chat turn. Old chat turns are not exact
snapshots of the personal skill state that existed when they ran.

The v1 design does not include CLI support, web UI support, supporting files,
organization-scoped personal skills, syncing personal skills into workspace
filesystems, or stable public API documentation.

## Consequences

Chatd can use personal and workspace skills through one prompt and one read
path, while storage remains owned by Coder instead of individual workspace
filesystems. Fresh metadata keeps skill changes responsive, but chat history is
less reproducible because old turns do not capture an exact copy of personal
skill content.

Explicit collision aliases make ambiguous names visible to users and tools.
Admin access improves operability and abuse handling, but it creates a privacy
trade-off that must remain clear in product and support expectations.
