import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { Router } from '@angular/router';
import { of } from 'rxjs';
import { provideMarkdown } from 'ngx-markdown';
import { AnnouncementModalComponent } from './announcement-modal.component';
import { AnnouncementService } from '../../services/announcement.service';
import { UserPreferencesService } from '../../services/user-preferences.service';

describe('AnnouncementModalComponent', () => {
    let fixture: ComponentFixture<AnnouncementModalComponent>;
    let announcementService: AnnouncementService;
    let userPreferencesService: Pick<MockedObject<UserPreferencesService>, 'updateUserPreferences' | 'preferences$'>;

    beforeEach(async () => {
        userPreferencesService = {
            updateUserPreferences: vi.fn().mockName("UserPreferencesService.updateUserPreferences"),
            preferences$: of({
                user_id: 'user-1',
                default_model_id: 'model-1',
                default_personality_id: 'personality-1',
            })
        } as unknown as Pick<MockedObject<UserPreferencesService>, 'updateUserPreferences' | 'preferences$'>;
        userPreferencesService.updateUserPreferences.mockReturnValue(of({
            user_id: 'user-1',
            default_model_id: 'model-1',
            default_personality_id: 'personality-1',
            last_seen_announcement: 'announcement-1',
        }));

        await TestBed.configureTestingModule({
            imports: [AnnouncementModalComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideMarkdown(),
                { provide: UserPreferencesService, useValue: userPreferencesService },
                { provide: Router, useValue: {
                        navigate: vi.fn().mockName("Router.navigate")
                    } },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(AnnouncementModalComponent);
        announcementService = TestBed.inject(AnnouncementService);
    });

    afterEach(() => {
        announcementService.close();
        document.body.classList.remove('overflow-hidden');
    });

    it('renders through the shared modal primitive and delegates dismiss', async () => {
        announcementService.currentAnnouncement.set({
            id: 'announcement-1',
            title: 'New thing',
            body: 'Body text',
        });
        announcementService.isOpen.set(true);
        fixture.detectChanges();
        await new Promise(resolve => setTimeout(resolve, 0));

        const dialog = fixture.nativeElement.querySelector('[role="dialog"]') as HTMLElement;
        expect(dialog).toBeTruthy();
        expect(dialog.textContent).toContain('New thing');

        dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

        expect(announcementService.isOpen()).toBe(false);
        expect(userPreferencesService.updateUserPreferences).toHaveBeenCalled();
    });
});
