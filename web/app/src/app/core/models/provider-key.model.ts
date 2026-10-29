/** A model provider, as the API names it. */
export type ModelProvider = 'openai' | 'anthropic' | 'zai' | 'google' | 'mistral' | 'deepseek' | 'qwen' | 'xiaomi';

/** A stored key, without the key: write-only, only the last four characters come back. */
export interface ProviderKeySummary {
  provider: ModelProvider;
  hint: string;
  updated_at: string;
}

/**
 * Where the account's provider keys come from, and which providers it has a key
 * for right now (GET /provider-access).
 */
export interface ProviderAccess {
  keys_from: 'deployment' | 'account';
  providers: ModelProvider[];
}

/** How the key screen describes each provider, in the order it lists them. */
export interface ProviderInfo {
  id: ModelProvider;
  name: string;
  /** What this key is spent on. */
  usedFor: string;
  /** The app cannot work without it. */
  required?: boolean;
  /** Where to create a key. */
  keysUrl?: string;
}

export const PROVIDERS: readonly ProviderInfo[] = [
  {
    id: 'openai',
    name: 'OpenAI',
    usedFor: 'Memory, summaries and images, whichever model you chat with',
    required: true,
    keysUrl: 'https://platform.openai.com/api-keys',
  },
  { id: 'anthropic', name: 'Anthropic', usedFor: 'Chatting with Claude models' },
  { id: 'google', name: 'Google Gemini', usedFor: 'Chatting with Gemini models, and Gemini images' },
  { id: 'zai', name: 'z.ai', usedFor: 'Chatting with GLM models, and GLM images' },
  { id: 'mistral', name: 'Mistral', usedFor: 'Chatting with Mistral models' },
  { id: 'deepseek', name: 'DeepSeek', usedFor: 'Chatting with DeepSeek models' },
  { id: 'qwen', name: 'Qwen', usedFor: 'Chatting with Qwen models, and Qwen images' },
  { id: 'xiaomi', name: 'Xiaomi MiMo', usedFor: 'Chatting with MiMo models' },
];

/** The provider's display name, or its id when unknown. */
export function providerName(id: string): string {
  return PROVIDERS.find(p => p.id === id)?.name ?? id;
}
