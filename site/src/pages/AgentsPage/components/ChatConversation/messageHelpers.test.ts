import { describe, expect, it } from "vitest";
import { groupSequentialReadFileMessages } from "./messageHelpers";
import type { MergedTool, ParsedMessageEntry } from "./types";

const baseMessage = {
	chat_id: "chat",
	created_at: "2026-03-10T00:00:00.000Z",
} as const;

const readFileTool = (id: string): MergedTool => ({
	id,
	name: "read_file",
	args: { path: `${id}.ts` },
	result: { content: id },
	isError: false,
	status: "completed",
});

const readFileMessage = (
	messageID: number,
	toolID: string,
): ParsedMessageEntry => {
	const args = { path: `${toolID}.ts` };
	const tool = readFileTool(toolID);
	return {
		message: {
			...baseMessage,
			id: messageID,
			role: "assistant",
			content: [
				{
					type: "tool-call",
					tool_call_id: toolID,
					tool_name: "read_file",
					args,
				},
			],
		},
		parsed: {
			markdown: "",
			reasoning: "",
			toolCalls: [{ id: toolID, name: "read_file", args }],
			toolResults: [],
			tools: [tool],
			blocks: [{ type: "tool", id: toolID }],
			sources: [],
		},
	};
};

const textMessage = (messageID: number, text: string): ParsedMessageEntry => ({
	message: {
		...baseMessage,
		id: messageID,
		role: "assistant",
		content: [{ type: "text", text }],
	},
	parsed: {
		markdown: text,
		reasoning: "",
		toolCalls: [],
		toolResults: [],
		tools: [],
		blocks: [{ type: "response", text }],
		sources: [],
	},
});

const hiddenToolResultMessage = (
	messageID: number,
	toolID: string,
): ParsedMessageEntry => ({
	message: {
		...baseMessage,
		id: messageID,
		role: "tool",
		content: [
			{
				type: "tool-result",
				tool_call_id: toolID,
				tool_name: "read_file",
				result: { content: toolID },
			},
		],
	},
	parsed: {
		markdown: "",
		reasoning: "",
		toolCalls: [],
		toolResults: [
			{
				id: toolID,
				name: "read_file",
				result: { content: toolID },
				isError: false,
			},
		],
		tools: [readFileTool(toolID)],
		blocks: [{ type: "tool", id: toolID }],
		sources: [],
	},
});

const executeMessage = (messageID: number): ParsedMessageEntry => {
	const args = { command: "pwd" };
	const tool: MergedTool = {
		id: "execute-1",
		name: "execute",
		args,
		result: { output: "/home/coder" },
		isError: false,
		status: "completed",
	};
	return {
		message: {
			...baseMessage,
			id: messageID,
			role: "assistant",
			content: [
				{
					type: "tool-call",
					tool_call_id: tool.id,
					tool_name: tool.name,
					args,
				},
			],
		},
		parsed: {
			markdown: "",
			reasoning: "",
			toolCalls: [{ id: tool.id, name: tool.name, args }],
			toolResults: [],
			tools: [tool],
			blocks: [{ type: "tool", id: tool.id }],
			sources: [],
		},
	};
};

describe("groupSequentialReadFileMessages", () => {
	it("returns a single read_file-only message unchanged", () => {
		const entry = readFileMessage(1, "read-1");

		const result = groupSequentialReadFileMessages([entry]);

		expect(result).toHaveLength(1);
		expect(result[0]).toBe(entry);
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

	it("does not collapse read_file messages across another visible tool", () => {
		const result = groupSequentialReadFileMessages([
			readFileMessage(1, "read-1"),
			executeMessage(2),
			readFileMessage(3, "read-2"),
		]);

		expect(result.map((entry) => entry.message.id)).toEqual([1, 2, 3]);
	});
});
