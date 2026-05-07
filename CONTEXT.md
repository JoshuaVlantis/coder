# Context

`CONTEXT.md` is a living glossary of cross-cutting domain terms used by Coder
agents. It is intentionally short, stable, and free of volatile file paths so
agent context can stay focused on durable product language.

## Glossary

- **Personal skill**: A user-owned skill that follows the user across Coder
  chats and workspaces, stored by Coder rather than discovered from a workspace
  filesystem.
- **Workspace skill**: A skill discovered from the workspace filesystem,
  currently under `.agents/skills` by default.
- **Skill source**: The origin of a skill available to chatd, such as personal
  storage or workspace filesystem discovery.
- **Skill alias**: A chat or tool lookup name for a skill. Bare aliases use the
  skill name. Collision aliases use `personal/<name>` or `workspace/<name>`.
