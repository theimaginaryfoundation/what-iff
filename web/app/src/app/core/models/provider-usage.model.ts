/** One piece of work a provider key pays for, and the model that does it. */
export interface ProviderJob {
  job: string;
  model: string;
}

/** Everything one provider's key is spent on. */
export interface ProviderUsage {
  provider: string;
  /** The app itself spends this key regardless of the chat model. OpenAI only. */
  required?: boolean;
  jobs: ProviderJob[];
}
