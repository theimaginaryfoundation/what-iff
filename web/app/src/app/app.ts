import { Component, OnInit, inject, effect, ChangeDetectionStrategy } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { initFlowbite } from 'flowbite';
import { take } from 'rxjs/operators';
import { AuthService } from './core/services/auth.service';
import { ConfirmationModalComponent } from './core/components/confirmation-modal/confirmation-modal.component';
import { AnnouncementModalComponent } from './core/components/announcement-modal/announcement-modal.component';
import { AnnouncementService } from './core/services/announcement.service';
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

  constructor() {
    // Check for unseen announcements whenever the user logs in.
    effect(() => {
      if (this.authService.isLoggedIn()) {
        this.userPreferencesService.getUserPreferences().pipe(take(1)).subscribe(prefs => {
          this.announcementService.checkAnnouncements(prefs.last_seen_announcement);
        });
      }
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
