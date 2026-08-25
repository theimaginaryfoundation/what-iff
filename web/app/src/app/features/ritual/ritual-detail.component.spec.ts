import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { ActivatedRoute, Router } from '@angular/router';
import { of, throwError } from 'rxjs';
import { RitualDetailComponent } from './ritual-detail.component';
import { RitualService } from '../../core/services/ritual.service';
import { PersonalityService } from '../../core/services/personality.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { MCPServerService } from '../../core/services/mcp-server.service';
import { Ritual, UpdateRitualRequest } from '../../core/models/ritual.model';
import { Personality } from '../../core/models/personality.model';
import { NULL_PERSONALITY_ID } from '../../core/constants/app.constants';

describe('RitualDetailComponent', () => {
    let component: RitualDetailComponent;
    let fixture: ComponentFixture<RitualDetailComponent>;
    let mockRitualService: Pick<MockedObject<RitualService>, 'listSystemRituals' | 'getRitual' | 'updateRitual' | 'deleteRitual'>;
    let mockPersonalityService: Pick<MockedObject<PersonalityService>, 'listPersonalities'>;
    let mockConfirmationService: Pick<MockedObject<ConfirmationService>, 'confirm' | 'alert' | 'setLoading' | 'close'>;
    let mockMcpServerService: Pick<MockedObject<MCPServerService>, 'listMCPServers'>;
    let mockRouter: Pick<MockedObject<Router>, 'navigate' | 'createUrlTree' | 'serializeUrl' | 'events'>;
    let mockActivatedRoute: any;

    const mockRitual: Ritual = {
        id: '123',
        name: 'Test Ritual',
        description: 'Test Description',
        content: 'Test Content',
        hotkeys: 'ctrl+shift+r',
        personality_id: '456',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z'
    };

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
        },
        {
            id: '789',
            name: 'Another Personality',
            system_prompt: 'Another prompt',
            auto_pin_memories: false,
            expressions_enabled: true,
            image_style: 'auto', cover_image_id: null,
            cover_image_url: null,
            created_at: '2024-01-01T00:00:00Z',
            updated_at: '2024-01-01T00:00:00Z',
            stats: { chat_count: 0, last_used_at: null }
        }
    ];

    beforeEach(async () => {
        mockRitualService = {
            listSystemRituals: vi.fn().mockName("RitualService.listSystemRituals"),
            getRitual: vi.fn().mockName("RitualService.getRitual"),
            updateRitual: vi.fn().mockName("RitualService.updateRitual"),
            deleteRitual: vi.fn().mockName("RitualService.deleteRitual")
        } as unknown as Pick<MockedObject<RitualService>, 'listSystemRituals' | 'getRitual' | 'updateRitual' | 'deleteRitual'>;
        mockPersonalityService = {
            listPersonalities: vi.fn().mockName("PersonalityService.listPersonalities")
        } as unknown as Pick<MockedObject<PersonalityService>, 'listPersonalities'>;
        mockMcpServerService = {
            listMCPServers: vi.fn().mockName("MCPServerService.listMCPServers")
        } as unknown as Pick<MockedObject<MCPServerService>, 'listMCPServers'>;

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
        mockActivatedRoute = {
            snapshot: {
                paramMap: {
                    get: vi.fn().mockName('get').mockReturnValue('123')
                }
            }
        };

        await TestBed.configureTestingModule({
            imports: [RitualDetailComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                { provide: RitualService, useValue: mockRitualService },
                { provide: PersonalityService, useValue: mockPersonalityService },
                { provide: MCPServerService, useValue: mockMcpServerService },
                { provide: ConfirmationService, useValue: mockConfirmationService },
                { provide: Router, useValue: mockRouter },
                { provide: ActivatedRoute, useValue: mockActivatedRoute }
            ]
        }).compileComponents();

        mockRitualService.listSystemRituals.mockReturnValue(of([]));
        mockRitualService.getRitual.mockReturnValue(of(mockRitual));
        mockPersonalityService.listPersonalities.mockReturnValue(of({
            results: mockPersonalities,
            total_count: mockPersonalities.length,
            page: 1
        }));
        mockMcpServerService.listMCPServers.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));

        fixture = TestBed.createComponent(RitualDetailComponent);
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
        it('should load ritual and personalities when ritual ID is provided', () => {
            fixture.detectChanges();

            expect(mockRitualService.getRitual).toHaveBeenCalledWith('123');
            expect(mockPersonalityService.listPersonalities).toHaveBeenCalledWith(1, 100);
            expect(mockMcpServerService.listMCPServers).toHaveBeenCalledWith(1, 200);
            expect(component.ritual()).toEqual(mockRitual);
            expect(component.personalities()).toEqual(mockPersonalities);
            expect(component.isLoading()).toBe(false);
        });

        it('should navigate to ritual list if no ritual ID is provided', () => {
            mockActivatedRoute.snapshot.paramMap.get.mockReturnValue(null);

            fixture.detectChanges();

            expect(mockRouter.navigate).toHaveBeenCalledWith(['/skills']);
        });

        it('should handle error when loading ritual fails', () => {
            const error = new Error('Failed to load');
            mockRitualService.getRitual.mockReturnValue(throwError(() => error));

            fixture.detectChanges();

            expect(component.errorMessage()).toBe('Failed to load skill. Please try again.');
            expect(component.isLoading()).toBe(false);
        });

        it('should handle error when loading personalities fails', () => {
            const error = new Error('Failed to load personalities');
            mockPersonalityService.listPersonalities.mockReturnValue(throwError(() => error));

            vi.spyOn(console, 'error').mockReturnValue(undefined);
            fixture.detectChanges();

            expect(console.error).toHaveBeenCalledWith('Failed to load personalities:', error);
        });
    });

    describe('startEditing', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        it('should populate edit form with ritual data and enter edit mode', () => {
            component.startEditing();

            expect(component.isEditing()).toBe(true);
            expect(component.editForm()).toEqual({
                name: mockRitual.name,
                description: mockRitual.description,
                content: mockRitual.content,
                hotkeys: mockRitual.hotkeys,
                personality_id: mockRitual.personality_id || '',
                mcp_server_ids: []
            });
            expect(component.errorMessage()).toBeNull();
            expect(component.successMessage()).toBeNull();
        });

        it('should not enter edit mode if ritual is null', () => {
            component.ritual.set(null);
            component.startEditing();

            expect(component.isEditing()).toBe(false);
        });
    });

    describe('cancelEditing', () => {
        beforeEach(() => {
            fixture.detectChanges();
            component.startEditing();
        });

        it('should reset edit form and exit edit mode', () => {
            component.editForm.set({
                name: 'Changed Name',
                description: 'Changed Description',
                content: 'Changed Content',
                hotkeys: 'ctrl+x',
                personality_id: '789',
                mcp_server_ids: []
            });
            component.errorMessage.set('Some error');

            component.cancelEditing();

            expect(component.isEditing()).toBe(false);
            expect(component.editForm()).toEqual({
                name: mockRitual.name,
                description: mockRitual.description,
                content: mockRitual.content,
                hotkeys: mockRitual.hotkeys,
                personality_id: mockRitual.personality_id || '',
                mcp_server_ids: []
            });
            expect(component.errorMessage()).toBeNull();
        });
    });

    describe('saveRitual', () => {
        beforeEach(() => {
            fixture.detectChanges();
            component.startEditing();
        });

        it('should successfully update ritual', async () => {
            const updatedRitual = { ...mockRitual, name: 'Updated Name' };
            mockRitualService.updateRitual.mockReturnValue(of(updatedRitual));

            component.editForm.set({
                name: 'Updated Name',
                description: mockRitual.description,
                content: mockRitual.content,
                hotkeys: mockRitual.hotkeys,
                personality_id: mockRitual.personality_id || '',
                mcp_server_ids: []
            });

            component.saveRitual();

            // Observables with of() complete synchronously, so isSaving is already false
            expect(mockRitualService.updateRitual).toHaveBeenCalledWith('123', {
                name: 'Updated Name',
                description: mockRitual.description,
                content: mockRitual.content,
                hotkeys: mockRitual.hotkeys,
                personality_id: mockRitual.personality_id,
                mcp_server_ids: []
            } as UpdateRitualRequest);

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.ritual()).toEqual(updatedRitual);
            expect(component.isEditing()).toBe(false);
            expect(component.isSaving()).toBe(false);
            expect(component.successMessage()).toBe('Skill updated successfully!');
        });

        it('should set error message if name is empty', () => {
            component.editForm.set({
                name: '  ',
                description: mockRitual.description,
                content: mockRitual.content,
                hotkeys: mockRitual.hotkeys,
                personality_id: mockRitual.personality_id || '',
                mcp_server_ids: []
            });

            component.saveRitual();

            expect(component.errorMessage()).toBe('Name, description, and content are required.');
            expect(mockRitualService.updateRitual).not.toHaveBeenCalled();
        });

        it('should set error message if description is empty', () => {
            component.editForm.set({
                name: mockRitual.name,
                description: '  ',
                content: mockRitual.content,
                hotkeys: mockRitual.hotkeys,
                personality_id: mockRitual.personality_id || '',
                mcp_server_ids: []
            });

            component.saveRitual();

            expect(component.errorMessage()).toBe('Name, description, and content are required.');
            expect(mockRitualService.updateRitual).not.toHaveBeenCalled();
        });

        it('should set error message if content is empty', () => {
            component.editForm.set({
                name: mockRitual.name,
                description: mockRitual.description,
                content: '  ',
                hotkeys: mockRitual.hotkeys,
                personality_id: mockRitual.personality_id || '',
                mcp_server_ids: []
            });

            component.saveRitual();

            expect(component.errorMessage()).toBe('Name, description, and content are required.');
            expect(mockRitualService.updateRitual).not.toHaveBeenCalled();
        });

        it('should handle update error', async () => {
            const error = { message: 'Update failed' };
            mockRitualService.updateRitual.mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            component.saveRitual();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.errorMessage()).toBe('Update failed');
            expect(component.isSaving()).toBe(false);
            expect(console.error).toHaveBeenCalledWith('Failed to update ritual:', error);
        });

        it('should not save if ritual is null', () => {
            component.ritual.set(null);
            component.saveRitual();

            expect(mockRitualService.updateRitual).not.toHaveBeenCalled();
        });

        it('should set success message', async () => {
            const updatedRitual = { ...mockRitual };
            mockRitualService.updateRitual.mockReturnValue(of(updatedRitual));

            component.saveRitual();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.successMessage()).toBe('Skill updated successfully!');
            // Note: We don't test setTimeout clearing - that's testing JavaScript, not our component
        });
    });

    describe('delete functionality', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        it('should show delete modal and call confirmation service', async () => {
            mockConfirmationService.confirm.mockResolvedValue(false);

            await component.showDeleteModal();

            expect(mockConfirmationService.confirm).toHaveBeenCalledWith({
                title: 'Delete Skill',
                message: expect.stringContaining('Test Ritual'),
                type: 'danger',
                confirmText: 'Delete',
                cancelText: 'Cancel',
                keepOpen: true
            });
        });

        it('should delete ritual and navigate to list when confirmed', async () => {
            mockConfirmationService.confirm.mockResolvedValue(true);
            mockRitualService.deleteRitual.mockReturnValue(of(void 0));

            await component.showDeleteModal();

            expect(mockConfirmationService.setLoading).toHaveBeenCalledWith(true, 'Deleting...');
            expect(mockRitualService.deleteRitual).toHaveBeenCalledWith('123');

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(mockConfirmationService.close).toHaveBeenCalled();
            expect(mockRouter.navigate).toHaveBeenCalledWith(['/skills']);
        });

        it('should not delete if user cancels', async () => {
            mockConfirmationService.confirm.mockResolvedValue(false);

            await component.showDeleteModal();

            expect(mockRitualService.deleteRitual).not.toHaveBeenCalled();
        });

        it('should handle delete error', async () => {
            const error = new Error('Delete failed');
            mockConfirmationService.confirm.mockResolvedValue(true);
            mockRitualService.deleteRitual.mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            await component.showDeleteModal();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.errorMessage()).toBe('Failed to delete skill. Please try again.');
            expect(mockConfirmationService.setLoading).toHaveBeenCalledWith(false);
            expect(mockConfirmationService.close).toHaveBeenCalled();
            expect(mockConfirmationService.alert).toHaveBeenCalledWith({
                message: 'Failed to delete skill. Please try again.',
                type: 'danger'
            });
            expect(console.error).toHaveBeenCalledWith('Failed to delete ritual:', error);
        });

        it('should not delete if ritual is null', async () => {
            component.ritual.set(null);

            await component.showDeleteModal();

            expect(mockConfirmationService.confirm).not.toHaveBeenCalled();
            expect(mockRitualService.deleteRitual).not.toHaveBeenCalled();
        });
    });

    describe('copyToClipboard', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        it('should copy text to clipboard and show success message', async () => {
            vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue();

            component.copyToClipboard('test content');

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(navigator.clipboard.writeText).toHaveBeenCalledWith('test content');
            expect(component.successMessage()).toBe('Copied to clipboard!');
        });

        it('should handle clipboard error', async () => {
            const error = new Error('Clipboard failed');
            vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(error);
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            component.copyToClipboard('test content');

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(console.error).toHaveBeenCalledWith('Failed to copy to clipboard:', error);
            expect(component.errorMessage()).toBe('Failed to copy to clipboard');
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

    describe('goBack', () => {
        it('should navigate to ritual list', () => {
            component.goBack();
            expect(mockRouter.navigate).toHaveBeenCalledWith(['/skills']);
        });
    });

    describe('formatDate', () => {
        it('should format date correctly', () => {
            const formatted = component.formatDate('2024-01-15T10:30:00Z');
            expect(formatted).toContain('January');
            expect(formatted).toContain('15');
            expect(formatted).toContain('2024');
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
            expect(component.isModifierKey('cmd')).toBe(true);
        });

        it('should return false for non-modifier keys', () => {
            expect(component.isModifierKey('a')).toBe(false);
            expect(component.isModifierKey('r')).toBe(false);
            expect(component.isModifierKey('1')).toBe(false);
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

    describe('nullPersonalityId getter', () => {
        it('should return NULL_PERSONALITY_ID constant', () => {
            expect(component.nullPersonalityId).toBe(NULL_PERSONALITY_ID);
        });
    });
});
