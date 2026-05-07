package skills

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/codersdk/workspacesdk"
)

// MaxPersonalSkillSizeBytes is the maximum raw Markdown size accepted for a
// personal skill upload.
const MaxPersonalSkillSizeBytes = 64 * 1024

// Source identifies where a skill came from.
type Source string

const (
	// SourcePersonal identifies a user-owned, DB-backed skill.
	SourcePersonal Source = "personal"
	// SourceWorkspace identifies a filesystem-discovered workspace skill.
	SourceWorkspace Source = "workspace"
)

var (
	// ErrInvalidSkillName indicates that a parsed skill name is not kebab-case.
	ErrInvalidSkillName = xerrors.New("invalid skill name")
	// ErrSkillBodyRequired indicates that the skill has no body after frontmatter.
	ErrSkillBodyRequired = xerrors.New("skill body is required")
	// ErrSkillTooLarge indicates that the raw skill Markdown is too large.
	ErrSkillTooLarge = xerrors.New("skill is too large")
	// ErrSkillNotFound indicates that a skill lookup did not match any alias.
	ErrSkillNotFound = xerrors.New("skill not found")
)

// skillNamePattern validates kebab-case skill names. Keep this in sync with
// agent/agentcontextconfig/api.go so personal and workspace names agree.
var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Skill is the source-aware metadata needed to list and resolve a skill.
type Skill struct {
	Name        string
	Description string
	Source      Source
}

// SkillContent is a validated skill with the Markdown body after frontmatter.
type SkillContent struct {
	Skill
	Body string
}

// ResolvedSkill is a skill with the alias exposed to chat tools.
type ResolvedSkill struct {
	Skill
	Alias string
}

// ValidatePersonalSkillMarkdown parses and validates raw personal skill
// Markdown. It returns source-aware metadata and the body after frontmatter.
func ValidatePersonalSkillMarkdown(raw []byte) (SkillContent, error) {
	if len(raw) > MaxPersonalSkillSizeBytes {
		return SkillContent{}, xerrors.Errorf(
			"%w: got %d bytes, maximum is %d bytes",
			ErrSkillTooLarge,
			len(raw),
			MaxPersonalSkillSizeBytes,
		)
	}

	name, description, body, err := workspacesdk.ParseSkillFrontmatter(string(raw))
	if err != nil {
		return SkillContent{}, xerrors.Errorf("parse skill frontmatter: %w", err)
	}
	if !skillNamePattern.MatchString(name) {
		return SkillContent{}, xerrors.Errorf(
			"%w: %q must match %s",
			ErrInvalidSkillName,
			name,
			skillNamePattern.String(),
		)
	}
	if strings.TrimSpace(body) == "" {
		return SkillContent{}, ErrSkillBodyRequired
	}

	return SkillContent{
		Skill: Skill{
			Name:        name,
			Description: description,
			Source:      SourcePersonal,
		},
		Body: body,
	}, nil
}

// MergeSkills combines personal and workspace skills into a deterministic list
// with aliases for chat tool display and lookup.
func MergeSkills(personalSkills, workspaceSkills []Skill) []ResolvedSkill {
	personalByName := skillsByName(personalSkills, SourcePersonal)
	workspaceByName := skillsByName(workspaceSkills, SourceWorkspace)

	names := make(map[string]struct{}, len(personalByName)+len(workspaceByName))
	for name := range personalByName {
		names[name] = struct{}{}
	}
	for name := range workspaceByName {
		names[name] = struct{}{}
	}

	resolved := make([]ResolvedSkill, 0, len(personalByName)+len(workspaceByName))
	for _, name := range slices.Sorted(maps.Keys(names)) {
		personal, hasPersonal := personalByName[name]
		workspace, hasWorkspace := workspaceByName[name]
		if hasPersonal && hasWorkspace {
			resolved = append(resolved,
				ResolvedSkill{
					Skill: personal,
					Alias: QualifiedAlias(SourcePersonal, name),
				},
				ResolvedSkill{
					Skill: workspace,
					Alias: QualifiedAlias(SourceWorkspace, name),
				},
			)
			continue
		}
		if hasPersonal {
			resolved = append(resolved, ResolvedSkill{
				Skill: personal,
				Alias: name,
			})
			continue
		}
		resolved = append(resolved, ResolvedSkill{
			Skill: workspace,
			Alias: name,
		})
	}
	return resolved
}

// Lookup finds a resolved skill by bare alias or qualified source alias.
func Lookup(resolved []ResolvedSkill, lookup string) (ResolvedSkill, error) {
	for _, skill := range resolved {
		if lookup == skill.Alias || lookup == QualifiedAlias(skill.Source, skill.Name) {
			return skill, nil
		}
	}
	return ResolvedSkill{}, xerrors.Errorf("%w: %q", ErrSkillNotFound, lookup)
}

// QualifiedAlias returns the stable source-qualified alias for a skill name.
func QualifiedAlias(source Source, name string) string {
	return string(source) + "/" + name
}

func skillsByName(skills []Skill, source Source) map[string]Skill {
	byName := make(map[string]Skill, len(skills))
	for _, skill := range skills {
		if _, ok := byName[skill.Name]; ok {
			continue
		}
		skill.Source = source
		byName[skill.Name] = skill
	}
	return byName
}
