import { Component, OnInit, inject, effect, ChangeDetectionStrategy } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { initFlowbite } from 'flowbite';
import { take } from 'rxjs/operators';
import { AuthService } from './core/services/auth.service';
import { ConfirmationModalComponent } from './core/components/confirmation-modal/confirmation-modal.component';
import { UserPreferencesService } from './core/services/user-preferences.service';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, ConfirmationModalComponent],
  templateUrl: './app.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrl: './app.scss'
})
export class App implements OnInit {
  protected title = 'WhatIff';
  private authService = inject(AuthService);
  private userPreferencesService = inject(UserPreferencesService);

  constructor() {
    // Load preferences whenever the user logs in: services that read the cached
    // copy (UserPreferencesService.preferences$, e.g. model favourites) start from it.
    effect(() => {
      if (this.authService.isLoggedIn()) {
        this.userPreferencesService.getUserPreferences().pipe(take(1)).subscribe();
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
