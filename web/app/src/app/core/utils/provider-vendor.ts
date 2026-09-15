/**
 * Vendor names for the surfaces that talk about API keys.
 *
 * Deliberately not the picker's labels. Those name the model family — GPT,
 * Claude, GLM, Gemini — which is what you want when choosing a model, but wrong
 * on a page about whose key you are paying for: "GPT — Required" sitting beside
 * an "OpenAI" tab reads as two different things.
 */
const VENDOR_LABELS: Record<string, string> = {
  openai: 'OpenAI',
  anthropic: 'Anthropic',
  google: 'Google Gemini',
  zai: 'z.ai',
};

export function vendorLabel(provider: string): string {
  const normalized = provider.trim().toLowerCase();
  return VENDOR_LABELS[normalized] ?? provider;
}
