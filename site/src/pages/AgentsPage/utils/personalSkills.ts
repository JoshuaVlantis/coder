import frontMatter from "front-matter";

export const PERSONAL_SKILL_MAX_SIZE_BYTES = 64 * 1024;
export const PERSONAL_SKILLS_MAX_PER_USER = 100;

const personalSkillNamePattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const htmlCommentPattern = /<!--[\s\S]*?-->/g;

type SkillFrontmatter = {
	name?: unknown;
	description?: unknown;
};

export type PersonalSkillFormValues = {
	name: string;
	description: string;
	body: string;
};

class PersonalSkillMarkdownError extends Error {}

const frontmatterValue = (value: unknown): string => {
	if (value === undefined || value === null) {
		return "";
	}
	return String(value).trim();
};

const parseSimpleSkillMarkdown = (content: string): PersonalSkillFormValues => {
	const lines = content.replace(/^\uFEFF/, "").split("\n");
	if (lines.length === 0 || lines[0]?.trim() !== "---") {
		throw new PersonalSkillMarkdownError(
			"Missing opening frontmatter delimiter.",
		);
	}

	const closingIndex = lines.findIndex(
		(line, index) => index > 0 && line.trim() === "---",
	);
	if (closingIndex < 0) {
		throw new PersonalSkillMarkdownError(
			"Missing closing frontmatter delimiter.",
		);
	}

	let name = "";
	let description = "";
	for (const line of lines.slice(1, closingIndex)) {
		const separatorIndex = line.indexOf(":");
		if (separatorIndex < 0) {
			continue;
		}
		const key = line.slice(0, separatorIndex).trim().toLowerCase();
		let value = line.slice(separatorIndex + 1).trim();
		if (value.length >= 2) {
			const first = value[0];
			const last = value[value.length - 1];
			if ((first === '"' && last === '"') || (first === "'" && last === "'")) {
				value = value.slice(1, -1);
			}
		}
		if (key === "name") {
			name = value;
		} else if (key === "description") {
			description = value;
		}
	}

	const body = lines
		.slice(closingIndex + 1)
		.join("\n")
		.replace(htmlCommentPattern, "")
		.trim();

	if (!name) {
		throw new PersonalSkillMarkdownError("Skill name is required.");
	}

	return { name, description, body };
};

export const parsePersonalSkillMarkdown = (
	content: string,
): PersonalSkillFormValues => {
	try {
		const parsed = frontMatter<SkillFrontmatter>(content);
		const name = frontmatterValue(parsed.attributes.name);
		if (!name) {
			throw new PersonalSkillMarkdownError("Skill name is required.");
		}
		return {
			name,
			description: frontmatterValue(parsed.attributes.description),
			body: parsed.body.replace(htmlCommentPattern, "").trim(),
		};
	} catch (error) {
		if (error instanceof PersonalSkillMarkdownError) {
			throw error;
		}
		return parseSimpleSkillMarkdown(content);
	}
};

const frontmatterLineValue = (value: string): string =>
	value.replace(/\r?\n/g, " ").trim();

export const buildPersonalSkillMarkdown = (
	values: PersonalSkillFormValues,
): string => {
	const name = frontmatterLineValue(values.name);
	const description = frontmatterLineValue(values.description);
	const body = values.body.trim();
	const frontmatter = ["---", `name: ${name}`];
	if (description) {
		frontmatter.push(`description: ${description}`);
	}
	frontmatter.push("---");

	return `${frontmatter.join("\n")}\n${body}\n`;
};

export const getPersonalSkillContentSizeBytes = (content: string): number =>
	new TextEncoder().encode(content).length;

export const isValidPersonalSkillName = (name: string): boolean =>
	personalSkillNamePattern.test(name);
