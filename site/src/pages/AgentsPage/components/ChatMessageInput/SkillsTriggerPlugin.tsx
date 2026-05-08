import { useLexicalComposerContext } from "@lexical/react/LexicalComposerContext";
import {
	$getSelection,
	$isRangeSelection,
	$isTextNode,
	COMMAND_PRIORITY_CRITICAL,
	KEY_ARROW_DOWN_COMMAND,
	KEY_ARROW_UP_COMMAND,
	KEY_ENTER_COMMAND,
	KEY_ESCAPE_COMMAND,
	KEY_TAB_COMMAND,
	type NodeKey,
} from "lexical";
import { useEffect, useRef } from "react";
import type * as TypesGen from "#/api/typesGenerated";
import type { CaretAnchorRect } from "./PersonalSkillsTriggerMenu";

export type ActiveSkillsTrigger = {
	nodeKey: NodeKey;
	slashOffset: number;
	caretOffset: number;
	query: string;
	anchorRect: CaretAnchorRect | null;
};

type SkillsTriggerPluginProps = {
	open: boolean;
	skills: readonly TypesGen.UserSkillMetadata[];
	selectedIndex: number;
	onSelectedIndexChange: (index: number) => void;
	onTriggerChange: (trigger: ActiveSkillsTrigger | null) => void;
	onSkillSelected: (skill: TypesGen.UserSkillMetadata) => void;
};

const currentCaretRect = (): CaretAnchorRect | null => {
	const selection = getSelection();
	if (!selection || selection.rangeCount === 0) {
		return null;
	}

	const range = selection.getRangeAt(0);
	let rect = range.getBoundingClientRect();
	if ((rect.width === 0 && rect.height === 0) || Number.isNaN(rect.top)) {
		const fallbackRange = range.cloneRange();
		if (fallbackRange.startOffset > 0) {
			fallbackRange.setStart(
				fallbackRange.startContainer,
				fallbackRange.startOffset - 1,
			);
		}
		rect = fallbackRange.getBoundingClientRect();
	}

	if (Number.isNaN(rect.top)) {
		return null;
	}

	return {
		top: rect.top,
		left: rect.left,
		height: rect.height,
	};
};

const activeTriggerFromSelection = (): Omit<
	ActiveSkillsTrigger,
	"anchorRect"
> | null => {
	const selection = $getSelection();
	if (!$isRangeSelection(selection) || !selection.isCollapsed()) {
		return null;
	}

	const anchor = selection.anchor;
	if (anchor.type !== "text") {
		return null;
	}

	const node = anchor.getNode();
	if (!$isTextNode(node)) {
		return null;
	}

	const caretOffset = anchor.offset;
	const textBeforeCaret = node.getTextContent().slice(0, caretOffset);
	const lineStart = textBeforeCaret.lastIndexOf("\n") + 1;
	const linePrefix = textBeforeCaret.slice(lineStart);
	const match = /(?:^|\s)\/(\S*)$/.exec(linePrefix);
	if (!match) {
		return null;
	}

	const slashIndex = linePrefix.lastIndexOf("/");
	if (slashIndex === -1) {
		return null;
	}

	return {
		nodeKey: node.getKey(),
		slashOffset: lineStart + slashIndex,
		caretOffset,
		query: match[1] ?? "",
	};
};

export const SkillsTriggerPlugin = ({
	open,
	skills,
	selectedIndex,
	onSelectedIndexChange,
	onTriggerChange,
	onSkillSelected,
}: SkillsTriggerPluginProps) => {
	const [editor] = useLexicalComposerContext();
	const propsRef = useRef({
		open,
		skills,
		selectedIndex,
		onSelectedIndexChange,
		onTriggerChange,
		onSkillSelected,
	});

	useEffect(() => {
		propsRef.current = {
			open,
			skills,
			selectedIndex,
			onSelectedIndexChange,
			onTriggerChange,
			onSkillSelected,
		};
	}, [
		open,
		skills,
		selectedIndex,
		onSelectedIndexChange,
		onTriggerChange,
		onSkillSelected,
	]);

	useEffect(() => {
		return editor.registerUpdateListener(({ editorState }) => {
			const trigger = editorState.read(() => {
				return editor.isEditable() ? activeTriggerFromSelection() : null;
			});

			if (!trigger) {
				propsRef.current.onTriggerChange(null);
				return;
			}

			propsRef.current.onTriggerChange({
				...trigger,
				anchorRect: currentCaretRect(),
			});
		});
	}, [editor]);

	useEffect(() => {
		const moveSelection = (event: KeyboardEvent, delta: number) => {
			if (!propsRef.current.open) {
				return false;
			}
			event.preventDefault();
			const count = propsRef.current.skills.length;
			if (count === 0) {
				return true;
			}
			const currentIndex = Math.max(0, propsRef.current.selectedIndex);
			propsRef.current.onSelectedIndexChange(
				(currentIndex + delta + count) % count,
			);
			return true;
		};

		const selectHighlightedSkill = (event: KeyboardEvent | null) => {
			if (!propsRef.current.open) {
				return false;
			}
			event?.preventDefault();
			const skill = propsRef.current.skills[propsRef.current.selectedIndex];
			if (skill) {
				propsRef.current.onSkillSelected(skill);
			}
			return true;
		};

		const unregisterArrowDown = editor.registerCommand(
			KEY_ARROW_DOWN_COMMAND,
			(event: KeyboardEvent) => moveSelection(event, 1),
			COMMAND_PRIORITY_CRITICAL,
		);
		const unregisterArrowUp = editor.registerCommand(
			KEY_ARROW_UP_COMMAND,
			(event: KeyboardEvent) => moveSelection(event, -1),
			COMMAND_PRIORITY_CRITICAL,
		);
		const unregisterEnter = editor.registerCommand(
			KEY_ENTER_COMMAND,
			selectHighlightedSkill,
			COMMAND_PRIORITY_CRITICAL,
		);
		const unregisterTab = editor.registerCommand(
			KEY_TAB_COMMAND,
			selectHighlightedSkill,
			COMMAND_PRIORITY_CRITICAL,
		);
		const unregisterEscape = editor.registerCommand(
			KEY_ESCAPE_COMMAND,
			(event: KeyboardEvent) => {
				if (!propsRef.current.open) {
					return false;
				}
				event.preventDefault();
				propsRef.current.onTriggerChange(null);
				return true;
			},
			COMMAND_PRIORITY_CRITICAL,
		);

		return () => {
			unregisterArrowDown();
			unregisterArrowUp();
			unregisterEnter();
			unregisterTab();
			unregisterEscape();
		};
	}, [editor]);

	return null;
};
