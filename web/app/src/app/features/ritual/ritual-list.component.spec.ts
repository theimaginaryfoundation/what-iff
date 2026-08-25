import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { ActivatedRoute, Router } from '@angular/router';
import { of, throwError } from 'rxjs';
import { RitualListComponent } from './ritual-list.component';
import { RitualService } from '../../core/services/ritual.service';
import { PersonalityService } from '../../core/services/personality.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { Ritual, RitualFilters, PaginatedRitualResponse, CreateRitualRequest } from '../../core/models/ritual.model';
import { Personality } from '../../core/models/personality.model';

describe('RitualListComponent', () => {
    let component: RitualListComponent;
    let fixture: ComponentFixture<RitualListComponent>;
    let mockRitualService: Pick<MockedObject<RitualService>, 'listSystemRituals' | 'listRituals' | 'createRitual' | 'deleteRitual' | 'assignSystemRitualHotkey'>;
    let mockPersonalityService: Pick<MockedObject<PersonalityService>, 'listPersonalities'>;
    let mockConfirmationService: Pick<MockedObject<ConfirmationService>, 'confirm' | 'alert' | 'setLoading' | 'close'>;
    let mockRouter: Pick<MockedObject<Router>, 'navigate' | 'createUrlTree' | 'serializeUrl' | 'events'>;

    const mockRituals: Ritual[] = [
        {
            id: '1',
            name: 'Ritual 1',
            description: 'Description 1',
            content: 'Content 1',
            hotkeys: 'ctrl+1',
            personality_id: '456',
            created_at: '2024-01-01T00:00:00Z',
            updated_at: '2024-01-01T00:00:00Z'
        },
        {
            id: '2',
            name: 'Ritual 2',
            description: 'Description 2',
            content: 'Content 2',
            hotkeys: 'ctrl+2',
            personality_id: null,
            created_at: '2024-01-02T00:00:00Z',
            updated_at: '2024-01-02T00:00:00Z'
        }
    ];

    const mockPersonalities: Personality[] = [
        {
            id: '456',
            name: 'Test Personality',
            system_prompt: 'Test prompt',
            auto_pin_memories: false,
            expressions_enabled: true,
            image_style: 'auto', cover_image_id: null,
            cover_image_url: null,
            created_at: '2024-01-01T00:00:00Z',
            updated_at: '2024-01-01T00:00:00Z',
            stats: { chat_count: 0, last_used_at: null }
        }
    ];

    const mockPaginatedResponse: PaginatedRitualResponse = {
        results: mockRituals,
        total_count: 2,
        page: 1
    };

    beforeEach(async () => {
        mockRitualService = {
            listSystemRituals: vi.fn().mockName("RitualService.listSystemRituals"),
            listRituals: vi.fn().mockName("RitualService.listRituals"),
            createRitual: vi.fn().mockName("RitualService.createRitual"),
            deleteRitual: vi.fn().mockName("RitualService.deleteRitual"),
            assignSystemRitualHotkey: vi.fn().mockName("RitualService.assignSystemRitualHotkey")
        } as unknown as Pick<MockedObject<RitualService>, 'listSystemRituals' | 'listRituals' | 'createRitual' | 'deleteRitual' | 'assignSystemRitualHotkey'>;
        mockPersonalityService = {
            listPersonalities: vi.fn().mockName("PersonalityService.listPersonalities")
        } as unknown as Pick<MockedObject<PersonalityService>, 'listPersonalities'>;
        mockConfirmationService = {
            confirm: vi.fn().mockName("ConfirmationService.confirm"),
            alert: vi.fn().mockName("ConfirmationService.alert"),
            setLoading: vi.fn().mockName("ConfirmationService.setLoading"),
            close: vi.fn().mockName("ConfirmationService.close")
        } as unknown as Pick<MockedObject<ConfirmationService>, 'confirm' | 'alert' | 'setLoading' | 'close'>;
        mockRouter = {
            navigate: vi.fn().mockName("Router.navigate"),
            createUrlTree: vi.fn().mockName("Router.createUrlTree"),
            serializeUrl: vi.fn().mockName("Router.serializeUrl"),
            events: of()
        } as unknown as Pick<MockedObject<Router>, 'navigate' | 'createUrlTree' | 'serializeUrl' | 'events'>;
        mockRouter.createUrlTree.mockReturnValue({} as any);
        mockRouter.serializeUrl.mockReturnValue('');

        await TestBed.configureTestingModule({
            imports: [RitualListComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                { provide: RitualService, useValue: mockRitualService },
                { provide: PersonalityService, useValue: mockPersonalityService },
                { provide: ConfirmationService, useValue: mockConfirmationService },
                { provide: Router, useValue: mockRouter },
                { provide: ActivatedRoute, useValue: { snapshot: { paramMap: { get: () => null } } } }
            ]
        }).compileComponents();

        mockRitualService.listRituals.mockReturnValue(of(mockPaginatedResponse));
        mockRitualService.listSystemRituals.mockReturnValue(of([]));
        mockPersonalityService.listPersonalities.mockReturnValue(of({
            results: mockPersonalities,
            total_count: mockPersonalities.length,
            page: 1
        }));

        fixture = TestBed.createComponent(RitualListComponent);
        component = fixture.componentInstance;
    });

    afterEach(() => {
        // Clean up DOM side effects that might affect other tests
        document.body.classList.remove('overflow-hidden');
    });

    it('should create', () => {
        expect(component).toBeTruthy();
    });

    describe('ngOnInit', () => {
        it('should load rituals and personalities', () => {
            fixture.detectChanges();

            expect(mockRitualService.listRituals).toHaveBeenCalledWith(1, 10, {});
            expect(mockPersonalityService.listPersonalities).toHaveBeenCalledWith(1, 100);
            expect(component.rituals()).toEqual(mockRituals);
            expect(component.personalities()).toEqual(mockPersonalities);
            expect(component.totalCount()).toBe(2);
            expect(component.isLoading()).toBe(false);
        });
    });

    describe('loadRituals', () => {
        it('should load rituals with no filters', () => {
            component.loadRituals();

            expect(mockRitualService.listRituals).toHaveBeenCalledWith(1, 10, {});
            expect(component.rituals()).toEqual(mockRituals);
            expect(component.totalCount()).toBe(2);
            expect(component.isLoading()).toBe(false);
        });

        it('should load rituals with name filter', () => {
            component.nameFilter.set('test');
            component.loadRituals();

            expect(mockRitualService.listRituals).toHaveBeenCalledWith(1, 10, { name: 'test' });
        });

        it('should load rituals with search filter', () => {
            component.searchFilter.set('search term');
            component.loadRituals();

            expect(mockRitualService.listRituals).toHaveBeenCalledWith(1, 10, { search: 'search term' });
        });

        it('should load rituals with personality filter', () => {
            component.personalityFilter.set('456');
            component.loadRituals();

            expect(mockRitualService.listRituals).toHaveBeenCalledWith(1, 10, { personality_id: '456' });
        });

        it('should not include personality_id filter for "global"', () => {
            component.personalityFilter.set('global');
            component.loadRituals();

            expect(mockRitualService.listRituals).toHaveBeenCalledWith(1, 10, {});
        });

        it('should load rituals with date filters', () => {
            component.minDateFilter.set('2024-01-01');
            component.maxDateFilter.set('2024-12-31');
            component.loadRituals();

            expect(mockRitualService.listRituals).toHaveBeenCalledWith(1, 10, {
                min_date: '2024-01-01',
                max_date: '2024-12-31'
            });
        });

        it('should handle error when loading rituals fails', () => {
            const error = new Error('Failed to load');
            mockRitualService.listRituals.mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            component.loadRituals();

            expect(console.error).toHaveBeenCalledWith('Failed to load rituals:', error);
            expect(component.isLoading()).toBe(false);
        });
    });

    describe('loadPersonalities', () => {
        it('should load all personalities', () => {
            component.loadPersonalities();

            expect(mockPersonalityService.listPersonalities).toHaveBeenCalledWith(1, 100);
            expect(component.personalities()).toEqual(mockPersonalities);
        });

        it('should handle error when loading personalities fails', () => {
            const error = new Error('Failed to load');
            mockPersonalityService.listPersonalities.mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            component.loadPersonalities();

            expect(console.error).toHaveBeenCalledWith('Failed to load personalities:', error);
        });
    });

    describe('onPageChange', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        it('should change page and reload rituals', () => {
            mockRitualService.listRituals.mockClear();

            component.onPageChange(2);

            expect(component.currentPage()).toBe(2);
            expect(mockRitualService.listRituals).toHaveBeenCalledWith(2, 10, {});
        });
    });

    describe('onFiltersApplied', () => {
        beforeEach(() => {
            fixture.detectChanges();
            component.currentPage.set(3);
        });

        it('should reset to page 1 and reload rituals', () => {
            mockRitualService.listRituals.mockClear();

            component.onFiltersApplied();

            expect(component.currentPage()).toBe(1);
            expect(mockRitualService.listRituals).toHaveBeenCalled();
        });
    });

    describe('clearFilters', () => {
        beforeEach(() => {
            fixture.detectChanges();
            component.nameFilter.set('test');
            component.searchFilter.set('search');
            component.personalityFilter.set('456');
            component.minDateFilter.set('2024-01-01');
            component.maxDateFilter.set('2024-12-31');
        });

        it('should clear all filters and reload rituals', () => {
            mockRitualService.listRituals.mockClear();

            component.clearFilters();

            expect(component.nameFilter()).toBe('');
            expect(component.searchFilter()).toBe('');
            expect(component.personalityFilter()).toBe('');
            expect(component.minDateFilter()).toBe('');
            expect(component.maxDateFilter()).toBe('');
            expect(mockRitualService.listRituals).toHaveBeenCalledWith(1, 10, {});
        });
    });

    describe('viewRitual', () => {
        it('should navigate to ritual detail page', () => {
            component.viewRitual(mockRituals[0]);

            expect(mockRouter.navigate).toHaveBeenCalledWith(['/skills', '1']);
        });
    });

    describe('deleteRitual', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        it('should delete ritual after confirmation', async () => {
            mockConfirmationService.confirm.mockResolvedValue(true);
            mockRitualService.deleteRitual.mockReturnValue(of(void 0));
            mockRitualService.listRituals.mockClear();

            await component.deleteRitual(mockRituals[0]);

            expect(mockConfirmationService.confirm).toHaveBeenCalledWith({
                title: 'Delete Skill',
                message: 'Are you sure you want to delete "Ritual 1"?',
                type: 'danger',
                confirmText: 'Delete',
                cancelText: 'Cancel'
            });
            expect(mockRitualService.deleteRitual).toHaveBeenCalledWith('1');

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(mockRitualService.listRituals).toHaveBeenCalled();
            expect(component.successMessage()).toBe('Skill deleted successfully!');
        });

        it('should not delete if user cancels confirmation', async () => {
            mockConfirmationService.confirm.mockResolvedValue(false);

            await component.deleteRitual(mockRituals[0]);

            expect(mockRitualService.deleteRitual).not.toHaveBeenCalled();
        });

        it('should handle delete error', async () => {
            mockConfirmationService.confirm.mockResolvedValue(true);
            mockConfirmationService.alert.mockResolvedValue();
            const error = new Error('Delete failed');
            mockRitualService.deleteRitual.mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            await component.deleteRitual(mockRituals[0]);

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(console.error).toHaveBeenCalledWith('Failed to delete ritual:', error);
            expect(mockConfirmationService.alert).toHaveBeenCalledWith({
                message: 'Failed to delete skill. Please try again.',
                type: 'danger'
            });
        });
    });

    describe('copyToClipboard', () => {
        it('should copy text to clipboard and show success message', async () => {
            vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue();

            await component.copyToClipboard('test content');

            expect(navigator.clipboard.writeText).toHaveBeenCalledWith('test content');
            expect(component.successMessage()).toBe('Copied to clipboard!');
        });

        it('should handle clipboard error', async () => {
            mockConfirmationService.alert.mockResolvedValue();
            const error = new Error('Clipboard failed');
            vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(error);
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            await component.copyToClipboard('test content');

            expect(console.error).toHaveBeenCalledWith('Failed to copy to clipboard:', error);
            expect(mockConfirmationService.alert).toHaveBeenCalledWith({
                message: 'Failed to copy to clipboard',
                type: 'danger'
            });
        });
    });

    describe('create ritual modal', () => {
        it('should open create form and prevent body scroll', () => {
            component.openCreateForm();

            expect(component.isCreateFormOpen()).toBe(true);
            expect(component.createForm()).toEqual({
                name: '',
                description: '',
                content: '',
                hotkeys: '',
                personality_id: ''
            });
            expect(component.createErrorMessage()).toBeNull();
            expect(document.body.style.overflow).toBe('hidden');
        });

        it('should close create form and restore body scroll', () => {
            component.openCreateForm();
            component.createForm.set({
                name: 'test',
                description: 'test',
                content: 'test',
                hotkeys: 'test',
                personality_id: 'test'
            });

            component.closeCreateForm();

            expect(component.isCreateFormOpen()).toBe(false);
            expect(component.createForm()).toEqual({
                name: '',
                description: '',
                content: '',
                hotkeys: '',
                personality_id: ''
            });
            expect(document.body.style.overflow).toBe('');
        });
    });

    describe('createRitual', () => {
        beforeEach(() => {
            fixture.detectChanges();
            component.openCreateForm();
        });

        it('should successfully create ritual', async () => {
            const newRitual: Ritual = {
                id: '3',
                name: 'New Ritual',
                description: 'New Description',
                content: 'New Content',
                hotkeys: 'ctrl+3',
                personality_id: null,
                created_at: '2024-01-03T00:00:00Z',
                updated_at: '2024-01-03T00:00:00Z'
            };
            mockRitualService.createRitual.mockReturnValue(of(newRitual));
            mockRitualService.listRituals.mockClear();

            component.createForm.set({
                name: 'New Ritual',
                description: 'New Description',
                content: 'New Content',
                hotkeys: 'ctrl+3',
                personality_id: ''
            });

            component.createRitual();

            // Observables with of() complete synchronously, so isCreating is already false
            expect(mockRitualService.createRitual).toHaveBeenCalledWith({
                name: 'New Ritual',
                description: 'New Description',
                content: 'New Content',
                hotkeys: 'ctrl+3',
                personality_id: null
            } as CreateRitualRequest);

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.isCreating()).toBe(false);
            expect(component.isCreateFormOpen()).toBe(false);
            expect(mockRitualService.listRituals).toHaveBeenCalled();
            expect(mockRouter.navigate).toHaveBeenCalledWith(['/skills', '3']);
        });

        it('should trim whitespace from form fields', () => {
            const newRitual: Ritual = {
                id: '3',
                name: 'New Ritual',
                description: 'New Description',
                content: 'New Content',
                hotkeys: '',
                personality_id: null,
                created_at: '2024-01-03T00:00:00Z',
                updated_at: '2024-01-03T00:00:00Z'
            };
            mockRitualService.createRitual.mockReturnValue(of(newRitual));

            component.createForm.set({
                name: '  New Ritual  ',
                description: '  New Description  ',
                content: '  New Content  ',
                hotkeys: '   ',
                personality_id: ''
            });

            component.createRitual();

            expect(mockRitualService.createRitual).toHaveBeenCalledWith({
                name: 'New Ritual',
                description: 'New Description',
                content: 'New Content',
                hotkeys: undefined,
                personality_id: null
            } as CreateRitualRequest);
        });

        it('should set error message if name is empty', () => {
            component.createForm.set({
                name: '  ',
                description: 'Description',
                content: 'Content',
                hotkeys: '',
                personality_id: ''
            });

            component.createRitual();

            expect(component.createErrorMessage()).toBe('Name, description, and content are required.');
            expect(mockRitualService.createRitual).not.toHaveBeenCalled();
        });

        it('should set error message if description is empty', () => {
            component.createForm.set({
                name: 'Name',
                description: '  ',
                content: 'Content',
                hotkeys: '',
                personality_id: ''
            });

            component.createRitual();

            expect(component.createErrorMessage()).toBe('Name, description, and content are required.');
            expect(mockRitualService.createRitual).not.toHaveBeenCalled();
        });

        it('should set error message if content is empty', () => {
            component.createForm.set({
                name: 'Name',
                description: 'Description',
                content: '  ',
                hotkeys: '',
                personality_id: ''
            });

            component.createRitual();

            expect(component.createErrorMessage()).toBe('Name, description, and content are required.');
            expect(mockRitualService.createRitual).not.toHaveBeenCalled();
        });

        it('should handle create error', async () => {
            const error = { message: 'Create failed' };
            mockRitualService.createRitual.mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            component.createForm.set({
                name: 'Name',
                description: 'Description',
                content: 'Content',
                hotkeys: '',
                personality_id: ''
            });

            component.createRitual();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.isCreating()).toBe(false);
            expect(component.createErrorMessage()).toBe('Create failed');
            expect(console.error).toHaveBeenCalledWith('Failed to create ritual:', error);
        });
    });

    describe('getPersonalityName', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        it('should return personality name for valid ID', () => {
            const name = component.getPersonalityName('456');
            expect(name).toBe('Test Personality');
        });

        it('should return null for invalid ID', () => {
            const name = component.getPersonalityName('invalid-id');
            expect(name).toBeNull();
        });

        it('should return null for null ID', () => {
            const name = component.getPersonalityName(null);
            expect(name).toBeNull();
        });
    });

    describe('showSuccessMessage', () => {
        it('should set success message', () => {
            component.showSuccessMessage('Test message');

            expect(component.successMessage()).toBe('Test message');
            // Note: We don't test setTimeout clearing - that's testing JavaScript, not our component
        });
    });

    describe('trackByRitualId', () => {
        it('should return ritual ID', () => {
            const id = component.trackByRitualId(0, mockRituals[0]);
            expect(id).toBe('1');
        });
    });

    describe('pagination helpers', () => {
        beforeEach(() => {
            fixture.detectChanges();
            component.totalCount.set(25);
            component.pageSize.set(10);
        });

        it('should calculate pagination "to" correctly', () => {
            component.currentPage.set(1);
            expect(component.getPaginationTo()).toBe(10);

            component.currentPage.set(2);
            expect(component.getPaginationTo()).toBe(20);

            component.currentPage.set(3);
            expect(component.getPaginationTo()).toBe(25);
        });

        it('should calculate pagination "from" correctly', () => {
            component.currentPage.set(1);
            expect(component.getPaginationFrom()).toBe(1);

            component.currentPage.set(2);
            expect(component.getPaginationFrom()).toBe(11);

            component.currentPage.set(3);
            expect(component.getPaginationFrom()).toBe(21);
        });

        it('should return 0 for "from" when total count is 0', () => {
            component.totalCount.set(0);
            expect(component.getPaginationFrom()).toBe(0);
        });

        it('should calculate total pages correctly', () => {
            component.totalCount.set(25);
            component.pageSize.set(10);
            expect(component.getTotalPages()).toBe(3);

            component.totalCount.set(30);
            expect(component.getTotalPages()).toBe(3);

            component.totalCount.set(0);
            expect(component.getTotalPages()).toBe(0);
        });
    });

    describe('formatDate', () => {
        it('should format date correctly', () => {
            const formatted = component.formatDate('2024-01-15T10:30:00Z');
            expect(formatted).toContain('Jan');
            expect(formatted).toContain('15');
            expect(formatted).toContain('2024');
        });
    });

    describe('binding modal (system ritual hotkey)', () => {
        const mockSystemRitual: Ritual = {
            id: 'sys-1',
            name: 'Generate image',
            description: 'Generate an image from the conversation.',
            content: '',
            hotkeys: '',
            personality_id: null,
            created_at: '2024-01-01T00:00:00Z',
            updated_at: '2024-01-01T00:00:00Z'
        };

        beforeEach(() => {
            mockRitualService.listSystemRituals.mockReturnValue(of([mockSystemRitual]));
            fixture.detectChanges();
        });

        it('should open binding modal and set target and value', () => {
            component.openBindingModal(mockSystemRitual);

            expect(component.bindingTarget()).toEqual(mockSystemRitual);
            expect(component.bindingValue()).toBe('');
            expect(component.isBindingModalOpen()).toBe(true);
            expect(component.bindingError()).toBeNull();
            expect(document.body.style.overflow).toBe('hidden');
        });

        it('should prefill binding value when ritual has hotkeys', () => {
            const ritualWithHotkey = { ...mockSystemRitual, hotkeys: 'ctrl+shift+g' };
            component.openBindingModal(ritualWithHotkey);

            expect(component.bindingValue()).toBe('ctrl+shift+g');
        });

        it('should close binding modal and restore body scroll', () => {
            component.openBindingModal(mockSystemRitual);
            component.bindingValue.set('ctrl+r');

            component.closeBindingModal();

            expect(component.bindingTarget()).toBeNull();
            expect(component.bindingValue()).toBe('');
            expect(component.bindingError()).toBeNull();
            expect(component.isBindingModalOpen()).toBe(false);
            expect(document.body.style.overflow).toBe('');
        });

        it('should save binding successfully', async () => {
            component.openBindingModal(mockSystemRitual);
            component.bindingValue.set('ctrl+shift+g');
            mockRitualService.assignSystemRitualHotkey.mockReturnValue(of({}));
            mockRitualService.listSystemRituals.mockClear();

            component.saveBinding();

            expect(mockRitualService.assignSystemRitualHotkey).toHaveBeenCalledWith('sys-1', 'ctrl+shift+g');

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.isBindingSaving()).toBe(false);
            expect(component.successMessage()).toBe('Hotkey saved');
            expect(component.isBindingModalOpen()).toBe(false);
            expect(mockRitualService.listSystemRituals).toHaveBeenCalled();
        });

        it('should trim hotkey value when saving', async () => {
            component.openBindingModal(mockSystemRitual);
            component.bindingValue.set('  ctrl+r  ');
            mockRitualService.assignSystemRitualHotkey.mockReturnValue(of({}));

            component.saveBinding();

            expect(mockRitualService.assignSystemRitualHotkey).toHaveBeenCalledWith('sys-1', 'ctrl+r');
        });

        it('should not save when no binding target', () => {
            component.bindingTarget.set(null);
            component.bindingValue.set('ctrl+r');

            component.saveBinding();

            expect(mockRitualService.assignSystemRitualHotkey).not.toHaveBeenCalled();
        });

        it('should set 409 conflict error when hotkey already in use', async () => {
            component.openBindingModal(mockSystemRitual);
            component.bindingValue.set('ctrl+r');
            const conflictError = { status: 409 } as any;
            mockRitualService.assignSystemRitualHotkey.mockReturnValue(throwError(() => conflictError));

            component.saveBinding();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.isBindingSaving()).toBe(false);
            expect(component.bindingError()).toBe('Hotkey already in use. Choose another or clear the conflicting binding.');
            expect(component.isBindingModalOpen()).toBe(true);
        });

        it('should set generic error when save fails', async () => {
            component.openBindingModal(mockSystemRitual);
            component.bindingValue.set('ctrl+r');
            const error = { status: 500, message: 'Server error' } as any;
            mockRitualService.assignSystemRitualHotkey.mockReturnValue(throwError(() => error));

            component.saveBinding();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.isBindingSaving()).toBe(false);
            expect(component.bindingError()).toBe('Server error');
            expect(component.isBindingModalOpen()).toBe(true);
        });
    });

    describe('parseHotkeys', () => {
        it('should parse hotkey string into array', () => {
            const keys = component.parseHotkeys('ctrl+shift+r');
            expect(keys).toEqual(['ctrl', 'shift', 'r']);
        });
    });

    describe('isModifierKey', () => {
        it('should return true for modifier keys', () => {
            expect(component.isModifierKey('ctrl')).toBe(true);
            expect(component.isModifierKey('shift')).toBe(true);
            expect(component.isModifierKey('alt')).toBe(true);
            expect(component.isModifierKey('meta')).toBe(true);
        });

        it('should return false for non-modifier keys', () => {
            expect(component.isModifierKey('a')).toBe(false);
            expect(component.isModifierKey('r')).toBe(false);
        });

        it('should be case insensitive', () => {
            expect(component.isModifierKey('CTRL')).toBe(true);
            expect(component.isModifierKey('Shift')).toBe(true);
        });
    });

    describe('formatKeyDisplay', () => {
        it('should format key for display', () => {
            const formatted = component.formatKeyDisplay('ctrl');
            expect(formatted).toBeTruthy();
        });
    });
});
