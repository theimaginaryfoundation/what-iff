/** The API a connection speaks. */
export type ProviderFlavor = 'openai' | 'anthropic';

/** One way the account reaches one model provider, without its key (ADR 0x027). */
export interface ProviderConnection {
  id: string;
  /** The preset it was made from; a built-in provider's id, or openrouter, ollama, lmstudio, custom. */
  preset: string;
  name: string;
  /** Only Custom stores one; a preset implies its own. */
  flavor?: ProviderFlavor;
  /** The address the account chose; absent means the preset's own. */
  base_url?: string;
  /** The key's last four characters; absent when there is no key. */
  hint?: string;
  has_key: boolean;
  /** A built-in provider; the account has at most one connection to each. */
  builtin: boolean;
  /** "converted" for a key stored before connections existed. */
  source?: string;
  created_at: string;
  updated_at: string;
}

/** Details that fill in a new connection (GET /provider-connections/presets). */
export interface ProviderPreset {
  id: string;
  name: string;
  /** Absent for Custom, where the account chooses it. */
  flavor?: ProviderFlavor;
  /** The address a new connection gets, already right for this deployment. Absent for Custom. */
  base_url?: string;
  address_editable: boolean;
  key_required: boolean;
  builtin: boolean;
  /** A server expected on the same computer. */
  local: boolean;
  /** Where to create a key, when known. */
  keys_url?: string;
}

/** A model a provider offers. */
export interface ListedModel {
  id: string;
  display_name?: string;
}

/** What adding or editing a connection answers with. */
export interface ProviderConnected {
  connection: ProviderConnection;
  /** The models found; empty when an edit did not need to check the provider. */
  models: ListedModel[];
}

export interface CreateProviderConnection {
  preset: string;
  name?: string;
  flavor?: ProviderFlavor;
  base_url?: string;
  key?: string;
  current_password: string;
}

export interface UpdateProviderConnection {
  name?: string;
  base_url?: string;
  /** A new key, or empty to remove it where the preset needs none. */
  key?: string;
  current_password?: string;
}
