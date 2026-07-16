import { deleteSync, getSync, patchSync, postSync, putSync } from "./envelope";

// PromptName is one of the three fixed prompt slots the daemon stores.
export type PromptName = "chat_system_prompt" | "answer_system_prompt" | "source_rules";

// GENERATION_SLOTS are the two slots that support named variants. The
// source_rules guardrail has only a single override.
export const GENERATION_SLOTS: PromptName[] = ["chat_system_prompt", "answer_system_prompt"];

// isGenerationSlot reports whether a slot supports variants.
export function isGenerationSlot(name: PromptName): boolean {
  return GENERATION_SLOTS.includes(name);
}

// VARIANT_NAME_PATTERN mirrors the daemon's validation, so the UI can reject a
// bad name before the round-trip.
export const VARIANT_NAME_PATTERN = /^[a-z0-9][a-z0-9-]{0,63}$/;

// Prompt is the API view of one prompt slot (daemon promptView): its effective
// value, the built-in default it falls back to, and whether a customization is
// active. Generation slots also carry the active variant name and the stored
// variant names. `default` is returned alongside `value` so the UI can show the
// default for comparison and reset to it without a second request.
export interface Prompt {
  name: PromptName;
  value: string;
  default: string;
  customized: boolean;
  // Populated for generation slots only.
  active?: string;
  variants?: string[];
}

// PromptVariant is the API view of one named variant's head value and metadata.
export interface PromptVariant {
  name: string;
  slot: PromptName;
  value: string;
  version: number;
  active: boolean;
}

// PromptVariantSummary is the transcript-free view of a variant in a listing.
export interface PromptVariantSummary {
  name: string;
  versions: number;
  active: boolean;
}

// PromptVersion is one entry in a variant's version history.
export interface PromptVersion {
  version: number;
  value: string;
}

// listPrompts fetches the prompt slots in the daemon's canonical order (chat,
// answer, source rules) — the same order `prompt init` presents.
export async function listPrompts(): Promise<Prompt[]> {
  const prompts = await getSync<Prompt[] | null>("/1.0/prompts");
  return prompts ?? [];
}

// savePrompt writes through the slot's current selection (the back-compat PUT).
// The daemon rejects an empty value: reset a prompt with resetPrompt instead, so
// clearing the editor cannot silently discard a customization.
export async function savePrompt(name: PromptName, value: string): Promise<Prompt> {
  return putSync<Prompt>(`/1.0/prompts/${name}`, { value });
}

// resetPrompt returns the slot to the built-in default (clears the active
// pointer; stored variants are preserved).
export async function resetPrompt(name: PromptName): Promise<Prompt> {
  return deleteSync<Prompt>(`/1.0/prompts/${name}`);
}

// activatePrompt points a slot's active pointer at a variant, or at the built-in
// default when name is empty.
export async function activatePrompt(name: PromptName, variant: string): Promise<Prompt> {
  return patchSync<Prompt>(`/1.0/prompts/${name}`, { active: variant });
}

// listVariants returns the variants of a generation slot.
export async function listVariants(slot: PromptName): Promise<PromptVariantSummary[]> {
  const variants = await getSync<PromptVariantSummary[] | null>(`/1.0/prompts/${slot}/variants`);
  return variants ?? [];
}

// getVariant returns one variant's head value and metadata.
export async function getVariant(slot: PromptName, name: string): Promise<PromptVariant> {
  return getSync<PromptVariant>(`/1.0/prompts/${slot}/variants/${name}`);
}

// createVariant stores a new variant from an initial value, failing if the name
// is already in use.
export async function createVariant(slot: PromptName, name: string, value: string): Promise<PromptVariant> {
  return postSync<PromptVariant>(`/1.0/prompts/${slot}/variants`, { name, value });
}

// saveVariant appends a new version to a variant, creating it if absent.
export async function saveVariant(slot: PromptName, name: string, value: string): Promise<PromptVariant> {
  return putSync<PromptVariant>(`/1.0/prompts/${slot}/variants/${name}`, { value });
}

// deleteVariant removes a variant. The active variant cannot be deleted.
export async function deleteVariant(slot: PromptName, name: string): Promise<void> {
  await deleteSync<unknown>(`/1.0/prompts/${slot}/variants/${name}`);
}

// variantVersions returns a variant's full version history.
export async function variantVersions(slot: PromptName, name: string): Promise<PromptVersion[]> {
  const versions = await getSync<PromptVersion[] | null>(`/1.0/prompts/${slot}/variants/${name}/versions`);
  return versions ?? [];
}

// restoreVariant appends a new head version carrying an earlier version's
// content.
export async function restoreVariant(slot: PromptName, name: string, version: number): Promise<PromptVariant> {
  return postSync<PromptVariant>(`/1.0/prompts/${slot}/variants/${name}/restore`, { version });
}
