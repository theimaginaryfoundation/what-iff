export interface UserRegisterRequest {
  username: string;
  email: string;
  password: string;
  first_name?: string;
  last_name?: string;
  // Optional: set when the user accepts terms of service.
  terms_accepted?: boolean;
}

export interface UserLoginRequest {
  username: string;
  password: string;
}

export interface UserResponse {
  id: string;
  username: string;
  email: string;
  first_name?: string;
  last_name?: string;
  timezone?: string;
  /** Timezone the user's browser last reported; jobs that follow the user run in it. */
  current_timezone?: string;
  theme?: 'light' | 'dark' | 'system';
  created_at: string;
  updated_at: string;
}

export interface LoginResponse {
  access_token: string;
  refresh_token: string;
  user: UserResponse;
}

export interface RefreshTokenRequest {
  refresh_token: string;
}

export interface RefreshTokenResponse {
  access_token: string;
  refresh_token: string;
}

export interface UpdateUserRequest {
  email?: string;
  first_name?: string;
  last_name?: string;
  timezone?: string;
}

export interface UpdatePasswordRequest {
  current_password: string;
  new_password: string;
}

export interface UsageStats {
  chats: number;
  messages: number;
  input_tokens: number;
  output_tokens: number;
}

export type UsagePeriod = 'day' | 'week' | 'month';

export interface UserPreferences {
  id?: string;
  user_id: string;
  default_model_id: string;
  default_personality_id?: string;
  theme?: 'light' | 'dark' | 'system';
  /**
   * Model IDs starred in the model picker. User-global, not per-personality.
   *
   * Always present on a response. On update the field is optional and means
   * "leave the stored list alone" when omitted; when present the array replaces
   * the list wholesale, so send the full set and use `[]` to clear all favorites.
   */
  favorite_model_ids?: string[];
  /**
   * The vendor the generate_image tool and image ritual call. Always present on a
   * response (`openai` when never chosen); omitted on update leaves it unchanged.
   */
  image_generation_provider?: ImageGenerationProvider;
  /** Read-only: the image vendors this deployment has keys for, in display order. */
  image_generation_providers?: ImageGenerationProvider[];
}

export type ImageGenerationProvider = 'openai' | 'google' | 'zai' | 'qwen';

/** Display names for the image vendors, naming the model each one uses. */
export const IMAGE_GENERATION_PROVIDER_LABELS: Record<ImageGenerationProvider, string> = {
  openai: 'OpenAI (GPT Image)',
  google: 'Google (Gemini image)',
  zai: 'Z.ai (GLM-Image)',
  qwen: 'Qwen (Qwen-Image)',
};
