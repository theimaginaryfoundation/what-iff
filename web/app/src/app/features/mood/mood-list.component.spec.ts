import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { provideRouter, convertToParamMap } from '@angular/router';
import { of } from 'rxjs';

import { MoodListComponent } from './mood-list.component';
import { MoodService } from '../../core/services/mood.service';
import { PersonalityService } from '../../core/services/personality.service';
import { RitualService } from '../../core/services/ritual.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { ModeViewService } from '../../core/services/mode-view.service';
import { ModelService } from '../../core/services/model.service';
import { MCPServerService } from '../../core/services/mcp-server.service';
import { ActivatedRoute } from '@angular/router';

describe('MoodListComponent', () => {
    let moodServiceSpy: Pick<MockedObject<MoodService>, 'listMoods' | 'getMood' | 'createMood' | 'updateMood' | 'attachToPersonalities' | 'deleteMood'>;

    const modeViewStub = {
        associationFilterMode: signal<'all' | 'personality'>('all'),
        selectedPersonalityIds: signal<string[]>([]),
        createRequestTick: signal(0),
        setSelectedPersonalityIds(ids: readonly string[]) {
            this.selectedPersonalityIds.set([...ids]);
            this.associationFilterMode.set(ids.length > 0 ? 'personality' : 'all');
        },
        selectAllAssociations() {
            this.selectedPersonalityIds.set([]);
            this.associationFilterMode.set('all');
        },
        requestCreateModalOpen() {
            this.createRequestTick.update(value => value + 1);
        },
    };

    beforeEach(async () => {
        moodServiceSpy = {
            listMoods: vi.fn().mockName("MoodService.listMoods"),
            getMood: vi.fn().mockName("MoodService.getMood"),
            createMood: vi.fn().mockName("MoodService.createMood"),
            updateMood: vi.fn().mockName("MoodService.updateMood"),
            attachToPersonalities: vi.fn().mockName("MoodService.attachToPersonalities"),
            deleteMood: vi.fn().mockName("MoodService.deleteMood")
        } as unknown as Pick<MockedObject<MoodService>, 'listMoods' | 'getMood' | 'createMood' | 'updateMood' | 'attachToPersonalities' | 'deleteMood'>;
        moodServiceSpy.listMoods.mockReturnValue(of({
            results: [
                {
                    id: 'mode-1',
                    name: 'Focused',
                    description: 'Concise and direct',
                    prompt_snippet: '',
                    image_ids: [],
                    ritual_ids: [],
                    personality_ids: ['persona-1'],
                    created_at: '',
                    updated_at: '',
                },
                {
                    id: 'mode-2',
                    name: 'Creative',
                    description: 'Open-ended style',
                    prompt_snippet: '',
                    image_ids: [],
                    ritual_ids: [],
                    personality_ids: ['persona-2'],
                    created_at: '',
                    updated_at: '',
                },
            ],
            total_count: 2,
            page: 1,
            page_size: 20,
        } as any));
        moodServiceSpy.getMood.mockImplementation((id: string) => of({
            id,
            name: id === 'mode-1' ? 'Focused' : 'Creative',
            description: '',
            prompt_snippet: '',
            image_ids: [],
            ritual_ids: [],
            personality_ids: id === 'mode-1' ? ['persona-1'] : ['persona-2'],
            created_at: '',
            updated_at: '',
        } as any));
        moodServiceSpy.createMood.mockReturnValue(of({
            id: 'mode-3',
            name: 'New',
            description: '',
            prompt_snippet: '',
            image_ids: [],
            ritual_ids: [],
            personality_ids: [],
            created_at: '',
            updated_at: '',
        } as any));
        moodServiceSpy.updateMood.mockImplementation((_id: string, req: any) => of({
            id: 'mode-1',
            name: req.name,
            description: req.description,
            prompt_snippet: req.prompt_snippet,
            recommended_model: req.recommended_model,
            image_ids: req.image_ids ?? [],
            ritual_ids: req.ritual_ids ?? [],
            personality_ids: [],
            created_at: '',
            updated_at: '',
        } as any));
        moodServiceSpy.attachToPersonalities.mockReturnValue(of(void 0));
        moodServiceSpy.deleteMood.mockReturnValue(of(void 0));

        await TestBed.configureTestingModule({
            imports: [MoodListComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideRouter([]),
                { provide: MoodService, useValue: moodServiceSpy },
                {
                    provide: PersonalityService,
                    useValue: { listPersonalities: () => of({ results: [{ id: 'persona-1', name: 'Ada' }, { id: 'persona-2', name: 'Vera' }] }) },
                },
                { provide: RitualService, useValue: { listRituals: () => of({ results: [] }) } },
                { provide: ModelService, useValue: { getModels: () => of([]) } },
                { provide: MCPServerService, useValue: { listMCPServers: () => of({ results: [] }) } },
                { provide: ImageGalleryService, useValue: { getImageUrl: () => '', listImages: () => of({ results: [] }) } },
                { provide: ConfirmationService, useValue: { confirm: async () => true, alert: async () => { } } },
                { provide: ModeViewService, useValue: modeViewStub },
                { provide: ActivatedRoute, useValue: { paramMap: of(convertToParamMap({})) } },
            ],
        }).compileComponents();
    });

    it('filters visible mode cards by selected personalities', () => {
        const fixture = TestBed.createComponent(MoodListComponent);
        fixture.detectChanges();
        expect(fixture.componentInstance.vm.visibleMoods().length).toBe(2);

        modeViewStub.setSelectedPersonalityIds(['persona-1']);
        fixture.detectChanges();
        expect(fixture.componentInstance.vm.visibleMoods().length).toBe(1);
        expect(fixture.componentInstance.vm.visibleMoods()[0].name).toBe('Focused');
    });

    it('opens create modal when sidebar requests create mode', () => {
        const fixture = TestBed.createComponent(MoodListComponent);
        fixture.detectChanges();
        expect(fixture.componentInstance.vm.isEditModalOpen()).toBe(false);

        modeViewStub.requestCreateModalOpen();
        fixture.detectChanges();
        expect(fixture.componentInstance.vm.isEditModalOpen()).toBe(true);
        expect(fixture.componentInstance.vm.modalMode()).toBe('create');
    });

    it('removes one personality association from a mode card', async () => {
        const fixture = TestBed.createComponent(MoodListComponent);
        fixture.detectChanges();
        const mood = fixture.componentInstance.vm.moods()[0];
        mood.personality_ids = ['persona-1', 'persona-2'];
        await fixture.componentInstance.vm.removePersonalityFromMode(mood as any, 'persona-2', new MouseEvent('click'));
        expect(moodServiceSpy.attachToPersonalities).toHaveBeenCalledWith('mode-1', { personality_ids: ['persona-1'] });
    });

    it('creates a mode and persists selected personality associations', () => {
        moodServiceSpy.attachToPersonalities.mockClear();
        const fixture = TestBed.createComponent(MoodListComponent);
        fixture.detectChanges();
        fixture.componentInstance.vm.openCreateModeModal();
        fixture.componentInstance.vm.formName.set('New mode');
        fixture.componentInstance.vm.formDescription.set('desc');
        fixture.componentInstance.vm.saveEditMood();
        expect(moodServiceSpy.createMood).toHaveBeenCalled();
        expect(moodServiceSpy.attachToPersonalities).not.toHaveBeenCalled();
    });

    it('adds personality association from mode card picker', () => {
        const fixture = TestBed.createComponent(MoodListComponent);
        fixture.detectChanges();
        const mood = fixture.componentInstance.vm.moods()[0];
        mood.personality_ids = ['persona-1'];
        fixture.componentInstance.vm.addPersonalityToMode(mood as any, 'persona-2', new MouseEvent('click'));
        expect(moodServiceSpy.attachToPersonalities).toHaveBeenCalledWith('mode-1', { personality_ids: ['persona-1', 'persona-2'] });
    });
});
