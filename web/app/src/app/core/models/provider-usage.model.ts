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

/** How provider keys work on this deployment, with what each is spent on. */
export interface ProviderUsageReport {
  /**
   * Whether the person reading this supplies the keys. Travels with the job
   * list because the two are read together: what a key is spent on only tells
   * you what to expect once you know whose key it is.
   */
  accounts_supply_keys: boolean;
  providers: ProviderUsage[];
}
