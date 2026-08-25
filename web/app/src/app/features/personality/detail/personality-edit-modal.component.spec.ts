import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { of } from 'rxjs';
import { Router } from '@angular/router';

import { Personality } from '../../../core/models/personality.model';
import { PersonalityService } from '../../../core/services/personality.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { FileAttachmentService } from '../../../core/services/file-attachment.service';
import { PersonalityEditModalComponent } from './personality-edit-modal.component';
import { TEXT_LIMIT_HARD_MAX, TEXT_LIMIT_WARNING_THRESHOLD, } from '../../../core/constants/text-limits.constants';

describe('PersonalityEditModalComponent', () => {
    let fixture: ComponentFixture<PersonalityEditModalComponent>;
    let component: PersonalityEditModalComponent;
    let personalityService: Pick<MockedObject<PersonalityService>, 'updatePersonality' | 'deletePersonality'>;
    let router: Pick<MockedObject<Router>, 'navigate'>;
    let confirmDiscardResult = true;
    let confirmDiscardCalls = 0;

    const personality: Personality = {
        id: 'p-1',
        name: 'Vera',
        system_prompt: 'Prompt',
        scratchpad: 'Scratchpad',
        auto_pin_memories: false,
        expressions_enabled: true,
        image_style: 'auto', cover_image_id: null,
        cover_image_url: null,
        accent_color: '#C2572A',
        thumbnail_circle: { cx: 0.5, cy: 0.42, r: 0.34 },
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
        stats: { chat_count: 0, last_used_at: null },
    };

    beforeEach(async () => {
        personalityService = {
            updatePersonality: vi.fn().mockName("PersonalityService.updatePersonality"),
            deletePersonality: vi.fn().mockName("PersonalityService.deletePersonality")
        } as unknown as Pick<MockedObject<PersonalityService>, 'updatePersonality' | 'deletePersonality'>;
        personalityService.updatePersonality.mockReturnValue(of(personality));
        personalityService.deletePersonality.mockReturnValue(of(void 0));
        router = {
            navigate: vi.fn().mockName("Router.navigate")
        } as unknown as Pick<MockedObject<Router>, 'navigate'>;

        await TestBed.configureTestingModule({
            imports: [PersonalityEditModalComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                { provide: PersonalityService, useValue: personalityService },
                {
                    provide: ImageGalleryService,
                    useValue: {
                        listImages: () => of({ results: [], total_count: 0, page: 1, page_size: 40 }),
                        getImageUrl: () => '/img',
                    },
                },
                {
                    provide: FileAttachmentService,
                    useValue: {
                        listFileAttachments: () => of({ results: [], total_count: 0, page: 1, page_size: 40 }),
                        uploadPersonalityFileAttachment: () => of(null),
                        deleteFileAttachment: () => of(void 0),
                    },
                },
                {
                    provide: ConfirmationService,
                    useValue: {
                        confirm: async () => true,
                        alert: async () => undefined,
                        confirmDiscardChanges: async () => {
                            confirmDiscardCalls += 1;
                            return confirmDiscardResult;
                        },
                    },
                },
                { provide: Router, useValue: router },
            ],
        }).compileComponents();

        confirmDiscardResult = true;
        confirmDiscardCalls = 0;

        fixture = TestBed.createComponent(PersonalityEditModalComponent);
        component = fixture.componentInstance;
        fixture.componentRef.setInput('open', true);
        fixture.componentRef.setInput('personality', personality);
        fixture.detectChanges();
    });

    it('hydrates draft state when opened', () => {
        expect(component.draft()?.name).toBe('Vera');
        expect(component.draft()?.accent_color).toBe('#C2572A');
        expect(component.draft()?.thumbnail_circle?.r).toBeCloseTo(0.34, 5);
    });

    it('emits dismissed on cancel', () => {
        vi.spyOn(component.dismissed, 'emit').mockReturnValue(undefined);
        component.cancel();
        expect(component.dismissed.emit).toHaveBeenCalled();
    });

    it('is not dirty when freshly hydrated and dirty after an edit', () => {
        expect(component.isDirty()).toBe(false);
        component.setDraftField('name', 'Vera 2');
        expect(component.isDirty()).toBe(true);
    });

    it('closes immediately on the close button even when dirty', async () => {
        vi.spyOn(component.dismissed, 'emit').mockReturnValue(undefined);
        component.setDraftField('name', 'Vera 2');

        component.onModalDismiss('close-button');
        await Promise.resolve();

        expect(confirmDiscardCalls).toBe(0);
        expect(component.dismissed.emit).toHaveBeenCalled();
    });

    it('closes a clean form on backdrop dismiss without confirming', async () => {
        vi.spyOn(component.dismissed, 'emit').mockReturnValue(undefined);

        component.onModalDismiss('backdrop');
        await Promise.resolve();

        expect(confirmDiscardCalls).toBe(0);
        expect(component.dismissed.emit).toHaveBeenCalled();
    });

    it('confirms before discarding a dirty form on backdrop/escape', async () => {
        vi.spyOn(component.dismissed, 'emit').mockReturnValue(undefined);
        component.setDraftField('system_prompt', 'Changed prompt');

        confirmDiscardResult = false;
        component.onModalDismiss('escape');
        await Promise.resolve();
        await Promise.resolve();
        expect(confirmDiscardCalls).toBe(1);
        expect(component.dismissed.emit).not.toHaveBeenCalled();

        confirmDiscardResult = true;
        component.onModalDismiss('backdrop');
        await Promise.resolve();
        await Promise.resolve();
        expect(confirmDiscardCalls).toBe(2);
        expect(component.dismissed.emit).toHaveBeenCalled();
    });

    it('saves updates and emits saved + dismissed', () => {
        vi.spyOn(component.saved, 'emit').mockReturnValue(undefined);
        vi.spyOn(component.dismissed, 'emit').mockReturnValue(undefined);
        component.setDraftField('accent_color', '#7A5AF8');
        component.setDraftField('auto_pin_memories', true);
        component.save();

        expect(personalityService.updatePersonality).toHaveBeenCalled();
        const request = vi.mocked(personalityService.updatePersonality).mock.lastCall![1];
        expect(request.auto_pin_memories).toBe(true);
        expect(request.image_style).toBe('auto');
        expect(request.expressions_enabled).toBe(true);
        expect(component.saved.emit).toHaveBeenCalled();
        expect(component.dismissed.emit).toHaveBeenCalled();
    });

    it('routes view memories action to memories list', () => {
        component.viewMemories();
        expect(router.navigate).toHaveBeenCalledWith(['/memories'], {
            queryParams: { personality_id: 'p-1' },
        });
    });

    it('deletes personality after confirmation and emits deleted + dismissed', async () => {
        vi.spyOn(component.deleted, 'emit').mockReturnValue(undefined);
        vi.spyOn(component.dismissed, 'emit').mockReturnValue(undefined);
        await component.confirmDelete();
        expect(personalityService.deletePersonality).toHaveBeenCalledWith('p-1');
        expect(component.deleted.emit).toHaveBeenCalledWith('p-1');
        expect(component.dismissed.emit).toHaveBeenCalled();
    });

    it('blocks save when system prompt exceeds hard limit', () => {
        component.setDraftField('system_prompt', 'x'.repeat(TEXT_LIMIT_HARD_MAX + 1));
        component.save();

        expect(personalityService.updatePersonality).not.toHaveBeenCalled();
        expect(component.systemPromptOverLimit()).toBe(true);
    });

    it('shows warning modal for near-limit prompt and saves after confirmation', () => {
        component.setDraftField('system_prompt', 'x'.repeat(TEXT_LIMIT_WARNING_THRESHOLD));
        component.save();

        expect(component.systemPromptWarningOpen()).toBe(true);
        expect(personalityService.updatePersonality).not.toHaveBeenCalled();

        component.confirmWarningAndSave();
        expect(personalityService.updatePersonality).toHaveBeenCalled();
    });
});
