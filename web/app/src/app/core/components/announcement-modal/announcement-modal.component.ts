import { Component, inject, ChangeDetectionStrategy } from '@angular/core';

import { Router } from '@angular/router';
import { MarkdownModule } from 'ngx-markdown';
import { AnnouncementService } from '../../services/announcement.service';
import { UserPreferencesService } from '../../services/user-preferences.service';
import { take } from 'rxjs/operators';
import { ModalComponent } from '../../../shared/ui/modal/modal.component';

@Component({
  selector: 'app-announcement-modal',
  standalone: true,
  imports: [MarkdownModule, ModalComponent],
  templateUrl: './announcement-modal.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./announcement-modal.component.scss']
})
export class AnnouncementModalComponent {
  public announcementService = inject(AnnouncementService);
  private userPreferencesService = inject(UserPreferencesService);
  private router = inject(Router);

  onCta(): void {
    const ann = this.announcementService.currentAnnouncement();
    if (ann?.ctaRoute) {
      this.markSeen(ann.id);
      this.announcementService.close();
      this.router.navigate([ann.ctaRoute]);
    } else {
      this.dismiss();
    }
  }

  dismiss(): void {
    const ann = this.announcementService.currentAnnouncement();
    if (ann) {
      this.markSeen(ann.id);
    }
    this.announcementService.close();
  }

  private markSeen(announcementId: string): void {
    this.userPreferencesService.preferences$.pipe(take(1)).subscribe(prefs => {
      if (prefs) {
        this.userPreferencesService.updateUserPreferences({
          ...prefs,
          last_seen_announcement: announcementId,
        }).subscribe();
      }
    });
  }
}
