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
  last_seen_announcement?: string;
  /**
   * Model IDs starred in the model picker. User-global, not per-personality.
   *
   * Always present on a response. On update the field is optional and means
   * "leave the stored list alone" when omitted; when present the array replaces
   * the list wholesale, so send the full set and use `[]` to clear all favorites.
   */
  favorite_model_ids?: string[];
}
