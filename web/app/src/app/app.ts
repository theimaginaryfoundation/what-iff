import { Component, OnInit, inject, effect, ChangeDetectionStrategy } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { initFlowbite } from 'flowbite';
import { catchError, take } from 'rxjs/operators';
import { of } from 'rxjs';
import { AuthService } from './core/services/auth.service';
import { ConfirmationModalComponent } from './core/components/confirmation-modal/confirmation-modal.component';
import { AnnouncementModalComponent } from './core/components/announcement-modal/announcement-modal.component';
import { AnnouncementService } from './core/services/announcement.service';
import { ProviderKeyService } from './core/services/provider-key.service';
import { ProviderKeyStatus } from './core/models/provider-key.model';
import { UserPreferencesService } from './core/services/user-preferences.service';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, ConfirmationModalComponent, AnnouncementModalComponent],
  templateUrl: './app.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrl: './app.scss'
})
export class App implements OnInit {
  protected title = 'WhatIff';
  private authService = inject(AuthService);
  private announcementService = inject(AnnouncementService);
  private userPreferencesService = inject(UserPreferencesService);
  private providerKeys = inject(ProviderKeyService);

  constructor() {
    // Check for unseen announcements whenever the user logs in — but not while
    // the account still has setup to do. An announcement modal covering the
    // screen someone was just sent to in order to finish setting up puts
    // product news ahead of the thing blocking them, and the current one
    // advertises model providers to a user who cannot reach any provider yet.
    effect(() => {
      if (!this.authService.isLoggedIn()) {
        return;
      }
      this.providerKeys
        .listStatuses()
        .pipe(
          take(1),
          catchError(() => of([] as ProviderKeyStatus[])),
        )
        .subscribe(statuses => {
          const setupPending = statuses.some(s => s.required && !s.configured);
          if (setupPending) {
            return;
          }
          this.userPreferencesService
            .getUserPreferences()
            .pipe(take(1))
            .subscribe(prefs => {
              this.announcementService.checkAnnouncements(prefs.last_seen_announcement);
            });
        });
    });
  }

  ngOnInit(): void {
    initFlowbite();
    // Initialize auth state after app is fully loaded to avoid circular dependency
    setTimeout(() => {
      this.authService.checkAuthState();
    }, 100);
  }
}
