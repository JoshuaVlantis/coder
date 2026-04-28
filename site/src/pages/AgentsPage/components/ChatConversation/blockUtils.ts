import { asString } from "../ChatElements/runtimeTypeUtils";
import type { RenderBlock } from "./types";

export const asNonEmptyString = (value: unknown): string | undefined => {
	const next = asString(value).trim();
	return next.length > 0 ? next : undefined;
};

/**
 * Optional reasoning timestamps. Only meaningful when type is
 * "thinking"; ignored for "response" blocks.
 */
export type ThinkingTimestamps = {
	startedAt?: string;
	completedAt?: string;
};

/**
 * Append a text or thinking block to a render block list, merging
 * with the previous block when the types match.
 *
 * For "thinking" blocks, timestamps may be supplied. When merging
 * into an existing thinking block, the earliest startedAt and the
 * latest completedAt across the merged span are kept so the
 * rendered duration covers the full reasoning span.
 */
export const appendTextBlock = (
	blocks: RenderBlock[],
	type: "response" | "thinking",
	text: string,
	timestamps: ThinkingTimestamps = {},
): RenderBlock[] => {
	const hasTimestamps =
		type === "thinking" &&
		(timestamps.startedAt !== undefined ||
			timestamps.completedAt !== undefined);
	// Skip whitespace-only text unless this is a "thinking" block
	// carrying timestamps. The end-of-reasoning marker has empty
	// text but contributes a completedAt that the UI needs.
	if (!text.trim() && !hasTimestamps) {
		return blocks;
	}
	const nextBlocks = [...blocks];
	const last = nextBlocks[nextBlocks.length - 1];
	if (last && last.type === type) {
		if (type === "thinking" && last.type === "thinking") {
			nextBlocks[nextBlocks.length - 1] = {
				type,
				text: `${last.text}${text}`,
				startedAt: earliest(last.startedAt, timestamps.startedAt),
				completedAt: latest(last.completedAt, timestamps.completedAt),
			};
		} else {
			nextBlocks[nextBlocks.length - 1] = {
				type,
				text: `${last.text}${text}`,
			};
		}
		return nextBlocks;
	}
	if (type === "thinking") {
		nextBlocks.push({
			type,
			text,
			startedAt: timestamps.startedAt,
			completedAt: timestamps.completedAt,
		});
	} else {
		nextBlocks.push({ type, text });
	}
	return nextBlocks;
};

const earliest = (a?: string, b?: string): string | undefined => {
	if (!a) return b;
	if (!b) return a;
	return new Date(a).getTime() <= new Date(b).getTime() ? a : b;
};

const latest = (a?: string, b?: string): string | undefined => {
	if (!a) return b;
	if (!b) return a;
	return new Date(a).getTime() >= new Date(b).getTime() ? a : b;
};
