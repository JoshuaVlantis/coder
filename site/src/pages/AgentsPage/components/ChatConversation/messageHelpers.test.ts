import { describe, expect, it } from "vitest";
import type * as TypesGen from "#/api/typesGenerated";
import { groupSequentialReadFileMessages } from "./messageHelpers";
import type {
	MergedTool,
	ParsedMessageContent,
	ParsedMessageEntry,
} from "./types";

const baseMessage = {
	chat_id: "chat",
	created_at: "2026-03-10T00:00:00.000Z",
} as const;

const parsed = (
	overrides: Partial<ParsedMessageContent> = {},
): ParsedMessageContent => ({
	markdown: "",
	reasoning: "",
	toolCalls: [],
	toolResults: [],
	tools: [],
	blocks: [],
	sources: [],
	...overrides,
});

const entry = ({
	messageID,
	role = "assistant",
	content = [],
	parsedOverrides,
}: {
	messageID: number;
	role?: TypesGen.ChatMessageRole;
	content?: TypesGen.ChatMessagePart[];
	parsedOverrides: Partial<ParsedMessageContent>;
}): ParsedMessageEntry => ({
	message: { ...baseMessage, id: messageID, role, content },
	parsed: parsed(parsedOverrides),
});

const readFileArgs = (id: string) => ({ path: `${id}.ts` });

const readFileTool = (id: string): MergedTool => ({
	id,
	name: "read_file",
	args: readFileArgs(id),
	result: { content: id },
	isError: false,
	status: "completed",
});

const readFileToolResult = (id: string) => ({
	id,
	name: "read_file" as const,
	result: { content: id },
	isError: false,
});

const readFileMessage = (
	messageID: number,
	toolID: string,
	parsedOverrides: Partial<ParsedMessageContent> = {},
): ParsedMessageEntry => {
	const args = readFileArgs(toolID);
	return entry({
		messageID,
		parsedOverrides: {
			toolCalls: [{ id: toolID, name: "read_file", args }],
			toolResults: [readFileToolResult(toolID)],
			tools: [readFileTool(toolID)],
			blocks: [{ type: "tool", id: toolID }],
			...parsedOverrides,
		},
	});
};

const hiddenToolResultMessage = (
	messageID: number,
	toolID: string,
): ParsedMessageEntry =>
	entry({
		messageID,
		role: "tool",
		parsedOverrides: {
			toolResults: [readFileToolResult(toolID)],
			tools: [readFileTool(toolID)],
			blocks: [{ type: "tool", id: toolID }],
		},
	});

const textMessage = (messageID: number, text: string): ParsedMessageEntry =>
	entry({
		messageID,
		content: [{ type: "text", text }],
		parsedOverrides: {
			markdown: text,
			blocks: [{ type: "response", text }],
		},
	});

const executeMessage = (messageID: number): ParsedMessageEntry => {
	const tool: MergedTool = {
		id: "execute-1",
		name: "execute",
		isError: false,
		status: "completed",
	};
	return entry({
		messageID,
		parsedOverrides: {
			toolCalls: [{ id: tool.id, name: tool.name }],
			tools: [tool],
			blocks: [{ type: "tool", id: tool.id }],
		},
	});
};

describe("groupSequentialReadFileMessages", () => {
	it("returns a single read_file-only message unchanged", () => {
		const readFile = readFileMessage(1, "read-1");

		const result = groupSequentialReadFileMessages([readFile]);

		expect(result).toHaveLength(1);
		expect(result[0]).toBe(readFile);
	});

	it("collapses read_file-only assistant messages across hidden tool results", () => {
		const result = groupSequentialReadFileMessages([
			readFileMessage(1, "read-1"),
			hiddenToolResultMessage(2, "read-1"),
			readFileMessage(3, "read-2"),
			hiddenToolResultMessage(4, "read-2"),
		]);

		expect(result).toHaveLength(1);
		expect(result[0].message.id).toBe(1);
		expect(result[0].parsed.toolCalls).toEqual([
			{ id: "read-1", name: "read_file", args: { path: "read-1.ts" } },
			{ id: "read-2", name: "read_file", args: { path: "read-2.ts" } },
		]);
		expect(result[0].parsed.toolResults).toEqual([
			readFileToolResult("read-1"),
			readFileToolResult("read-2"),
		]);
		expect(result[0].parsed.blocks).toEqual([
			{ type: "tool", id: "read-1" },
			{ type: "tool", id: "read-2" },
		]);
		expect(result[0].parsed.tools.map((tool) => tool.id)).toEqual([
			"read-1",
			"read-2",
		]);
	});

	it("does not collapse read_file messages across visible content", () => {
		const result = groupSequentialReadFileMessages([
			readFileMessage(1, "read-1"),
			textMessage(2, "middle"),
			readFileMessage(3, "read-2"),
		]);

		expect(result.map((entry) => entry.message.id)).toEqual([1, 2, 3]);
		expect(result[0].parsed.blocks).toEqual([{ type: "tool", id: "read-1" }]);
		expect(result[2].parsed.blocks).toEqual([{ type: "tool", id: "read-2" }]);
	});

	it.each([
		["markdown", { markdown: "Visible markdown" }],
		["reasoning", { reasoning: "Visible reasoning" }],
		[
			"sources",
			{
				sources: [
					{ url: "https://example.com/read-2", title: "Read 2 source" },
				],
			},
		],
	] satisfies Array<
		[string, Partial<ParsedMessageContent>]
	>)("does not collapse read_file messages with visible %s", (_, overrides) => {
		const result = groupSequentialReadFileMessages([
			readFileMessage(1, "read-1"),
			readFileMessage(2, "read-2", overrides),
			readFileMessage(3, "read-3"),
		]);

		expect(result.map((entry) => entry.message.id)).toEqual([1, 2, 3]);
	});

	it("does not collapse read_file messages across another visible tool", () => {
		const result = groupSequentialReadFileMessages([
			readFileMessage(1, "read-1"),
			executeMessage(2),
			readFileMessage(3, "read-2"),
		]);

		expect(result.map((entry) => entry.message.id)).toEqual([1, 2, 3]);
	});
});
