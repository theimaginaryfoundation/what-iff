import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';

import { UserResponse } from '../../core/models/user.model';
import { AuthService } from '../../core/services/auth.service';
import { ProfileSettingsModalService } from '../../features/profile-settings/profile-settings-modal.service';
import { UserIconComponent } from '../../shared/ui/icons/icons';
import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';

type ProfileUser = Pick<UserResponse, 'username' | 'email' | 'first_name' | 'last_name'>;

/**
 * Max visible length for an email used as the footer label.
 * Fits the 300px expanded sidebar (~icon + padding) at 12px desktop and at
 * 1rem mobile, with CSS ellipsis as a hard overflow backstop.
 */
export const PROFILE_EMAIL_DISPLAY_MAX = 24;

/**
 * Truncate a long email for the visible footer label while keeping the domain
 * readable: `local…@domain.com`.
 *
 * Rule:
 * 1. If `email.length ≤ 24`, show the full address.
 * 2. Otherwise keep the full `@domain`, and give the local-part whatever budget
 *    remains after `…@` (minimum 3 local characters).
 * 3. If `@domain` alone can't fit (pathological domains), head-truncate the
 *    whole string to 23 chars + `…`.
 *
 * Visible only — accessible names always use the full email via {@link profileFullName}.
 */
export function truncateEmailForDisplay(
  email: string,
  maxLen: number = PROFILE_EMAIL_DISPLAY_MAX,
): string {
  const value = email.trim();
  if (!value || value.length <= maxLen) return value;

  const at = value.lastIndexOf('@');
  if (at <= 0 || at === value.length - 1) {
    return `${value.slice(0, Math.max(1, maxLen - 1))}…`;
  }

  const local = value.slice(0, at);
  const domain = value.slice(at + 1);
  // Reserve 2 chars for `…` + `@`.
  const localBudget = maxLen - domain.length - 2;
  if (localBudget < 3) {
    return `${value.slice(0, Math.max(1, maxLen - 1))}…`;
  }

  if (local.length <= localBudget) return value;
  return `${local.slice(0, localBudget)}…@${domain}`;
}

/** Full name for accessible labels: "First Last", falling back to username, email, then "Guest". */
export function profileFullName(user: ProfileUser | null): string {
  if (!user) return 'Guest';
  const composed = [user.first_name, user.last_name].map(part => part?.trim()).filter(Boolean).join(' ');
  return composed || user.username?.trim() || user.email?.trim() || 'Guest';
}

/**
 * Compact name for the footer: "First L.", then first name only, username,
 * truncated email, "Guest". Emails longer than {@link PROFILE_EMAIL_DISPLAY_MAX}
 * become `local…@domain`.
 */
export function profileShortName(user: ProfileUser | null): string {
  if (!user) return 'Guest';
  const first = user.first_name?.trim();
  const lastInitial = user.last_name?.trim().charAt(0).toUpperCase();
  if (first && lastInitial) return `${first} ${lastInitial}.`;
  if (first) return first;
  const username = user.username?.trim();
  if (username) return username;
  const email = user.email?.trim();
  if (email) return truncateEmailForDisplay(email);
  return 'Guest';
}

/**
 * Sidebar footer profile trigger: user icon plus a compact name when expanded,
 * icon only (with tooltip) on the collapsed rail. Opens the profile modal.
 */
@Component({
  selector: 'app-sidebar-profile',
  standalone: true,
  imports: [TooltipDirective, UserIconComponent],
  templateUrl: './app-sidebar-profile.component.html',
  styleUrl: './app-sidebar-profile.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AppSidebarProfileComponent {
  readonly collapsed = input(false);

  private readonly auth = inject(AuthService);
  private readonly profileSettingsModal = inject(ProfileSettingsModalService);

  /** Untruncated identity — used only for aria-label / collapsed tooltip. */
  readonly fullName = computed(() => profileFullName(this.auth.currentUser()));
  /** Possibly truncated email — visible only, always aria-hidden in the template. */
  readonly shortName = computed(() => profileShortName(this.auth.currentUser()));

  openProfile(): void {
    this.profileSettingsModal.open('profile');
  }
}
