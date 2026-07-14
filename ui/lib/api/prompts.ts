import { deleteSync, getSync, putSync } from "./envelope";

// PromptName is one of the three fixed prompt templates the daemon stores.
export type PromptName = "chat_system_prompt" | "answer_system_prompt" | "source_rules";

// Prompt is the API view of one prompt template (daemon promptView): its
// effective value, the built-in default it falls back to, and whether a
// customization is stored. `default` is returned alongside `value` so the UI can
// show the default for comparison and reset to it without a second request.
export interface Prompt {
  name: PromptName;
  value: string;
  default: string;
  customized: boolean;
}

// listPrompts fetches the prompt templates in the daemon's canonical order
// (chat, answer, source rules) — the same order `prompt init` presents.
export async function listPrompts(): Promise<Prompt[]> {
  const prompts = await getSync<Prompt[] | null>("/1.0/prompts");
  return prompts ?? [];
}

// savePrompt stores a customization. The daemon rejects an empty value: reset a
// prompt with resetPrompt instead, so clearing the editor cannot silently
// discard a customization.
export async function savePrompt(name: PromptName, value: string): Promise<Prompt> {
  return putSync<Prompt>(`/1.0/prompts/${name}`, { value });
}

// resetPrompt drops the customization so the prompt resolves to the built-in
// default again.
export async function resetPrompt(name: PromptName): Promise<Prompt> {
  return deleteSync<Prompt>(`/1.0/prompts/${name}`);
}
