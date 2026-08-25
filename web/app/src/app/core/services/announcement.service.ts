import { Injectable, signal } from '@angular/core';
import { Announcement, CURRENT_ANNOUNCEMENT } from '../constants/announcement.constants';

@Injectable({
  providedIn: 'root'
})
export class AnnouncementService {
  currentAnnouncement = signal<Announcement | null>(null);
  isOpen = signal(false);

  /**
   * Checks whether the current announcement has been seen by the user.
   * If not, opens the modal. Call this after loading user preferences on login.
   */
  checkAnnouncements(lastSeenId: string | undefined): void {
    if (CURRENT_ANNOUNCEMENT.id !== lastSeenId) {
      this.currentAnnouncement.set(CURRENT_ANNOUNCEMENT);
      this.isOpen.set(true);
    }
  }

  close(): void {
    this.isOpen.set(false);
    this.currentAnnouncement.set(null);
  }
}
