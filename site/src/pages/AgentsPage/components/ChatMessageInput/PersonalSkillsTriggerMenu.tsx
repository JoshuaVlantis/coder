import type * as TypesGen from "#/api/typesGenerated";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandItem,
	CommandList,
} from "#/components/Command/Command";
import {
	Popover,
	PopoverAnchor,
	PopoverContent,
} from "#/components/Popover/Popover";
import { cn } from "#/utils/cn";

export type CaretAnchorRect = {
	top: number;
	left: number;
	height: number;
};

export const personalSkillTriggerText = (
	skill: TypesGen.UserSkillMetadata,
): string => {
	const description = skill.description.trim();
	return description ? `/${skill.name} (${description})` : `/${skill.name}`;
};

export const filterPersonalSkills = (
	skills: readonly TypesGen.UserSkillMetadata[],
	query: string,
): TypesGen.UserSkillMetadata[] => {
	const normalizedQuery = query.toLocaleLowerCase("en-US");
	if (!normalizedQuery) {
		return [...skills].sort((a, b) => a.name.localeCompare(b.name, "en-US"));
	}

	return skills
		.map((skill, index) => {
			const name = skill.name.toLocaleLowerCase("en-US");
			const description = skill.description.toLocaleLowerCase("en-US");
			let rank = Number.POSITIVE_INFINITY;
			if (name.startsWith(normalizedQuery)) {
				rank = 0;
			} else if (name.includes(normalizedQuery)) {
				rank = 1;
			} else if (description.includes(normalizedQuery)) {
				rank = 2;
			}

			return { skill, rank, index };
		})
		.filter(({ rank }) => Number.isFinite(rank))
		.sort((a, b) => {
			if (a.rank !== b.rank) {
				return a.rank - b.rank;
			}
			const nameOrder = a.skill.name.localeCompare(b.skill.name, "en-US");
			return nameOrder === 0 ? a.index - b.index : nameOrder;
		})
		.map(({ skill }) => skill);
};

type PersonalSkillsTriggerMenuProps = {
	open: boolean;
	anchorRect: CaretAnchorRect | null;
	query: string;
	skills: readonly TypesGen.UserSkillMetadata[];
	isLoading?: boolean;
	isError?: boolean;
	selectedIndex: number;
	onSelect: (skill: TypesGen.UserSkillMetadata) => void;
	onClose: () => void;
};

export const PersonalSkillsTriggerMenu = ({
	open,
	anchorRect,
	query,
	skills,
	isLoading,
	isError,
	selectedIndex,
	onSelect,
	onClose,
}: PersonalSkillsTriggerMenuProps) => {
	const shouldRender = open && anchorRect;

	return (
		<Popover
			open={Boolean(shouldRender)}
			onOpenChange={(nextOpen) => {
				if (!nextOpen) {
					onClose();
				}
			}}
		>
			{shouldRender && (
				<PopoverAnchor asChild>
					<span
						aria-hidden="true"
						style={{
							position: "fixed",
							top: anchorRect.top,
							left: anchorRect.left,
							width: 1,
							height: Math.max(anchorRect.height, 16),
							pointerEvents: "none",
						}}
					/>
				</PopoverAnchor>
			)}
			<PopoverContent
				align="start"
				side="bottom"
				className="w-80 p-1"
				onMouseDown={(event) => event.preventDefault()}
				onOpenAutoFocus={(event) => event.preventDefault()}
				onCloseAutoFocus={(event) => event.preventDefault()}
			>
				<Command shouldFilter={false} loop={false}>
					<CommandList className="max-h-72 border-t-0">
						{isLoading ? (
							<CommandItem value="loading" disabled>
								Loading personal skills...
							</CommandItem>
						) : isError ? (
							<CommandItem value="error" disabled>
								Could not load personal skills.
							</CommandItem>
						) : skills.length === 0 ? (
							<CommandEmpty>
								{query
									? "No personal skills match that query."
									: "No personal skills found."}
							</CommandEmpty>
						) : (
							<CommandGroup heading="Personal skills">
								{skills.map((skill, index) => (
									<CommandItem
										key={skill.id}
										value={skill.name}
										aria-selected={index === selectedIndex}
										className={cn(
											"items-start",
											index === selectedIndex &&
												"bg-surface-secondary text-content-primary",
										)}
										onMouseDown={(event) => event.preventDefault()}
										onSelect={() => onSelect(skill)}
									>
										<div className="min-w-0 space-y-1">
											<div className="truncate font-mono text-content-primary text-xs">
												/{skill.name}
											</div>
											{skill.description.trim() && (
												<div className="line-clamp-2 text-content-secondary text-xs leading-snug">
													{skill.description}
												</div>
											)}
										</div>
									</CommandItem>
								))}
							</CommandGroup>
						)}
					</CommandList>
				</Command>
			</PopoverContent>
		</Popover>
	);
};
