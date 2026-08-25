export interface Announcement {
  id: string;
  title: string;
  body: string;
  ctaText?: string;
  ctaRoute?: string;
}

/**
 * The single active announcement. To ship a new one, replace this object entirely.
 * Users whose last-seen ID differs from CURRENT_ANNOUNCEMENT.id will see the modal
 * on their next login.
 */
export const CURRENT_ANNOUNCEMENT: Announcement = {
  id: 'new-model-providers-2026-07',
  title: 'Six new model providers!',
  body: `**WhatIff now includes six new model providers:** Gemini, Qwen, GLM, Mistral, MiMo, and DeepSeek.

These providers are **experimental** — they may be rough around the edges. If something breaks or looks off, please report it so we can harden them.
If there is a missing model or provider you'd like to see, please let us know!
Open the model picker in any chat to try them out.`,
  ctaText: 'Go to Chats',
  ctaRoute: '/chat',
};
