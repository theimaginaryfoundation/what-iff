/** Whether the signed-in account can reach one model provider. */
export interface ProviderKeyStatus {
  provider: string;
  /** True when the account can reach this provider, by either source. */
  configured: boolean;
  /** Where the usable credential comes from; empty when not configured. */
  source: '' | 'account' | 'deployment';
  /** Last four characters of the account's own key, when it has one. */
  key_hint?: string;
  /**
   * The app cannot function without this provider. True for OpenAI, whose key
   * is used by embeddings, summarization, chat naming, mood, expressions and
   * file attachments regardless of which chat model is selected.
   */
  required: boolean;
  /** Whether a key can be supplied per account yet. */
  supported: boolean;
}
