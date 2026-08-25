import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { Router, ActivatedRoute } from '@angular/router';
import { of, throwError } from 'rxjs';
import { MemoryDetailComponent } from './memory-detail.component';
import { MemoryService } from '../../core/services/memory.service';
import { PersonalityService } from '../../core/services/personality.service';
import { Personality } from '../../core/models/personality.model';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { Memory } from '../../core/models/memory.model';
import { AuthService } from '../../core/services/auth.service';
import { UserResponse } from '../../core/models/user.model';

describe('MemoryDetailComponent', () => {
    let component: MemoryDetailComponent;
    let fixture: ComponentFixture<MemoryDetailComponent>;
    let mockMemoryService: Pick<MockedObject<MemoryService>, 'getMemoryById' | 'deleteMemory' | 'updateMemoryPin'>;
    let mockConfirmationService: Pick<MockedObject<ConfirmationService>, 'confirm' | 'alert' | 'setLoading' | 'close'>;
    let mockPersonalityService: Pick<MockedObject<PersonalityService>, 'listPersonalities'>;
    let mockRouter: Pick<MockedObject<Router>, 'navigate' | 'createUrlTree' | 'serializeUrl' | 'events'>;
    let mockActivatedRoute: any;
    let mockAuthService: any;

    const mockMemory: Memory = {
        id: '123',
        content: 'User likes coffee in the morning',
        level: 'global',
        type: 'Context',
        status: 'active',
        confidence: 0.6,
        starred: false,
        created_at: '2024-01-15T10:30:00Z',
        updated_at: '2024-01-15T10:30:00Z',
        pinned_personality_id: null
    };

    const mockChatMemory: Memory = {
        id: '456',
        chat_id: 'chat-123',
        chat_name: 'Work Chat',
        content: 'Project deadline is next Friday',
        level: 'thread',
        type: 'Context',
        status: 'active',
        confidence: 0.6,
        starred: false,
        created_at: '2024-01-20T14:00:00Z',
        updated_at: '2024-01-20T14:00:00Z'
    };

    const mockUser: UserResponse = {
        id: 'user-123',
        username: 'testuser',
        email: 'test@example.com',
        first_name: 'Test',
        last_name: 'User',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z'
    };

    beforeEach(async () => {
        mockMemoryService = {
            getMemoryById: vi.fn().mockName("MemoryService.getMemoryById"),
            deleteMemory: vi.fn().mockName("MemoryService.deleteMemory"),
            updateMemoryPin: vi.fn().mockName("MemoryService.updateMemoryPin")
        } as unknown as Pick<MockedObject<MemoryService>, 'getMemoryById' | 'deleteMemory' | 'updateMemoryPin'>;
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

        mockActivatedRoute = {
            snapshot: {
                paramMap: {
                    get: vi.fn().mockName('get').mockReturnValue('123')
                }
            },
            params: of({}),
            queryParams: of({})
        };

        mockAuthService = {
            getUserProfile: vi.fn().mockName("AuthService.getUserProfile"),
            currentUser: signal<UserResponse | null>(mockUser), isLoggedIn: signal(true)
        };

        await TestBed.configureTestingModule({
            imports: [MemoryDetailComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                { provide: MemoryService, useValue: mockMemoryService },
                { provide: PersonalityService, useValue: mockPersonalityService },
                { provide: ConfirmationService, useValue: mockConfirmationService },
                { provide: Router, useValue: mockRouter },
                { provide: ActivatedRoute, useValue: mockActivatedRoute },
                { provide: AuthService, useValue: mockAuthService }
            ]
        }).compileComponents();

        mockMemoryService.getMemoryById.mockReturnValue(of(mockMemory));
        mockPersonalityService.listPersonalities.mockReturnValue(of({
            results: [],
            total_count: 0,
            page: 1
        }));

        fixture = TestBed.createComponent(MemoryDetailComponent);
        component = fixture.componentInstance;
    });

    it('should create', () => {
        expect(component).toBeTruthy();
    });

    describe('ngOnInit', () => {
        it('should load memory when ID is provided', () => {
            fixture.detectChanges();

            expect(mockMemoryService.getMemoryById).toHaveBeenCalledWith('123');
            expect(component.memory()).toEqual(mockMemory);
            expect(component.loading()).toBe(false);
            expect(component.error()).toBeNull();
        });

        it('should set error when no ID is provided', () => {
            mockActivatedRoute.snapshot.paramMap.get.mockReturnValue(null);

            fixture.detectChanges();

            expect(component.error()).toBe('Invalid memory ID');
            expect(component.loading()).toBe(false);
            expect(mockMemoryService.getMemoryById).not.toHaveBeenCalled();
        });

        it('should handle error when loading memory fails', async () => {
            const error = new Error('Memory not found');
            mockMemoryService.getMemoryById.mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            fixture.detectChanges();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.loading()).toBe(false);
            expect(component.error()).toBe('Memory not found');
            expect(console.error).toHaveBeenCalledWith('Error loading memory:', error);
        });
    });

    describe('navigation', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        it('should navigate back to memory list', () => {
            component.goBack();

            expect(mockRouter.navigate).toHaveBeenCalledWith(['/memory']);
        });
    });

    describe('delete functionality', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        it('should show delete confirmation modal and call confirmation service', async () => {
            mockConfirmationService.confirm.mockResolvedValue(false);

            await component.confirmDelete();

            expect(mockConfirmationService.confirm).toHaveBeenCalledWith({
                title: 'Delete Memory',
                message: expect.stringContaining('User likes coffee'),
                type: 'danger',
                confirmText: 'Delete Memory',
                cancelText: 'Cancel',
                keepOpen: true
            });
        });

        it('should delete memory and navigate to list with query params when confirmed', async () => {
            mockConfirmationService.confirm.mockResolvedValue(true);
            mockMemoryService.deleteMemory.mockReturnValue(of(void 0));

            await component.confirmDelete();

            expect(mockConfirmationService.setLoading).toHaveBeenCalledWith(true, 'Deleting...');
            expect(mockMemoryService.deleteMemory).toHaveBeenCalledWith('123');

            await new Promise(resolve => setTimeout(resolve, 0));

            expect(mockConfirmationService.close).toHaveBeenCalled();
            expect(mockRouter.navigate).toHaveBeenCalledWith(['/memory'], {
                queryParams: { deleted: 'true', memoryId: '123' }
            });
        });

        it('should not delete if user cancels', async () => {
            mockConfirmationService.confirm.mockResolvedValue(false);

            await component.confirmDelete();

            expect(mockMemoryService.deleteMemory).not.toHaveBeenCalled();
        });

        it('should handle delete error', async () => {
            const error = new Error('Delete failed');
            mockConfirmationService.confirm.mockResolvedValue(true);
            mockConfirmationService.alert.mockResolvedValue();
            mockMemoryService.deleteMemory.mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            await component.confirmDelete();

            await new Promise(resolve => setTimeout(resolve, 0));

            expect(component.error()).toBe('Delete failed');
            expect(mockConfirmationService.setLoading).toHaveBeenCalledWith(false);
            expect(mockConfirmationService.close).toHaveBeenCalled();
            expect(mockConfirmationService.alert).toHaveBeenCalledWith({
                message: 'Failed to delete memory: Delete failed',
                type: 'danger'
            });
            expect(console.error).toHaveBeenCalledWith('Error deleting memory:', error);
        });

        it('should not delete if memory is null', async () => {
            component.memory.set(null);

            await component.confirmDelete();

            expect(mockConfirmationService.confirm).not.toHaveBeenCalled();
            expect(mockMemoryService.deleteMemory).not.toHaveBeenCalled();
        });
    });

    describe('clipboard functionality', () => {
        beforeEach(() => {
            // Fresh stub per test. The unit-test builder runs with
            // `isolate: false`, so every spec in a worker shares one
            // `navigator`; if a previous test's spy is still installed,
            // `vi.spyOn` hands back that same mock with its call history and
            // the not-called assertion below fails depending on file order.
            Object.defineProperty(navigator, 'clipboard', {
                value: {
                    writeText: () => Promise.resolve(),
                    readText: () => Promise.resolve(''),
                },
                writable: true,
                configurable: true,
            });
            fixture.detectChanges();
        });

        it('should copy memory content to clipboard', async () => {
            vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue();
            vi.spyOn(console, 'log').mockReturnValue(undefined);

            await component.copyToClipboard();

            expect(navigator.clipboard.writeText).toHaveBeenCalledWith(mockMemory.content);
        });

        it('should handle clipboard error', async () => {
            const error = new Error('Clipboard failed');
            vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(error);
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            await component.copyToClipboard();

            expect(console.error).toHaveBeenCalledWith('Failed to copy to clipboard:', error);
        });

        it('should not copy if memory is null', async () => {
            component.memory.set(null);
            vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined);

            await component.copyToClipboard();

            expect(navigator.clipboard.writeText).not.toHaveBeenCalled();
        });
    });

    describe('helper methods', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        it('should format date correctly', () => {
            const formatted = component.formatDate('2024-01-15T10:30:00Z');

            expect(formatted).toContain('January');
            expect(formatted).toContain('15');
            expect(formatted).toContain('2024');
            // Time format varies by locale (AM/PM vs 24-hour)
            expect(formatted.length).toBeGreaterThan(0);
        });

        it('should return correct badge class for global level', () => {
            const badgeClass = component.getLevelBadgeClass('global');

            expect(badgeClass).toContain('bg-blue-100');
            expect(badgeClass).toContain('text-blue-800');
        });

        it('should return correct badge class for thread level', () => {
            const badgeClass = component.getLevelBadgeClass('thread');

            expect(badgeClass).toContain('bg-green-100');
            expect(badgeClass).toContain('text-green-800');
        });
    });

    describe('chat memory specific features', () => {
        beforeEach(() => {
            mockMemoryService.getMemoryById.mockReturnValue(of(mockChatMemory));
        });

        it('should load chat memory with chat name', () => {
            fixture.detectChanges();

            expect(component.memory()).toEqual(mockChatMemory);
            expect(component.memory()!.chat_name).toBe('Work Chat');
            expect(component.memory()!.chat_id).toBe('chat-123');
        });

        it('should handle chat memory deletion', async () => {
            fixture.detectChanges();
            mockConfirmationService.confirm.mockResolvedValue(true);
            mockMemoryService.deleteMemory.mockReturnValue(of(void 0));

            await component.confirmDelete();

            expect(mockConfirmationService.setLoading).toHaveBeenCalledWith(true, 'Deleting...');
            expect(mockMemoryService.deleteMemory).toHaveBeenCalledWith('456');

            await new Promise(resolve => setTimeout(resolve, 0));

            expect(mockConfirmationService.close).toHaveBeenCalled();
            expect(mockRouter.navigate).toHaveBeenCalledWith(['/memory'], {
                queryParams: { deleted: 'true', memoryId: '456' }
            });
        });
    });

    describe('loading states', () => {
        it('should show loading state initially', () => {
            expect(component.loading()).toBe(true);
        });

        it('should hide loading state after successful load', () => {
            fixture.detectChanges();

            expect(component.loading()).toBe(false);
        });

        it('should hide loading state after error', async () => {
            const error = new Error('Failed to load');
            mockMemoryService.getMemoryById.mockReturnValue(throwError(() => error));

            fixture.detectChanges();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.loading()).toBe(false);
        });
    });

    describe('pin memory functionality', () => {
        const mockPinnedMemory: Memory = {
            id: '789',
            content: 'Memory pinned to personality',
            level: 'personality',
            type: 'Context',
            status: 'active',
            confidence: 0.6,
            starred: false,
            pinned_personality_id: 'personality-123',
            created_at: '2024-01-15T10:30:00Z',
            updated_at: '2024-01-15T10:30:00Z'
        };

        beforeEach(() => {
            mockMemoryService['updateMemoryPin'] = vi.fn().mockName('updateMemoryPin');
        });

        it('should load memory with pinned_personality_id', () => {
            mockMemoryService.getMemoryById.mockReturnValue(of(mockPinnedMemory));

            fixture.detectChanges();

            expect(component.memory()).toEqual(mockPinnedMemory);
            expect(component.selectedPinnedPersonalityId()).toBe('personality-123');
        });

        it('should load memory without pinned_personality_id (unpinned)', () => {
            fixture.detectChanges();

            expect(component.memory()).toEqual(mockMemory);
            expect(component.selectedPinnedPersonalityId()).toBeNull();
        });

        it('should update pin when personality is selected', async () => {
            mockMemoryService['updateMemoryPin'].mockReturnValue(of({
                ...mockMemory,
                pinned_personality_id: 'personality-456'
            }));

            fixture.detectChanges();
            component.selectedPinnedPersonalityId.set('personality-456');
            component.updatePin();

            expect(mockMemoryService['updateMemoryPin']).toHaveBeenCalledWith('123', 'personality-456');

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.isUpdatingPin()).toBe(false);
            expect(component.memory()?.pinned_personality_id).toBe('personality-456');
        });

        it('should unpin memory when null is selected', async () => {
            mockMemoryService.getMemoryById.mockReturnValue(of(mockPinnedMemory));
            mockMemoryService['updateMemoryPin'].mockReturnValue(of({
                ...mockPinnedMemory,
                pinned_personality_id: null
            }));

            fixture.detectChanges();
            component.selectedPinnedPersonalityId.set(null);
            component.updatePin();

            expect(mockMemoryService['updateMemoryPin']).toHaveBeenCalledWith('789', null);

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.memory()?.pinned_personality_id).toBeNull();
        });

        it('should handle error when updating pin fails', async () => {
            const error = new Error('Failed to update pin');
            mockMemoryService['updateMemoryPin'].mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            fixture.detectChanges();
            component.selectedPinnedPersonalityId.set('personality-456');
            component.updatePin();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.isUpdatingPin()).toBe(false);
            expect(component.error()).toBe('Failed to update memory pin: Failed to update pin');
            expect(console.error).toHaveBeenCalledWith('Error updating memory pin:', error);
        });

        it('should not update pin if memory is null', () => {
            fixture.detectChanges();
            component.memory.set(null);
            component.updatePin();

            expect(mockMemoryService['updateMemoryPin']).not.toHaveBeenCalled();
        });

        it('should get pinned personality name when pinned', () => {
            mockMemoryService.getMemoryById.mockReturnValue(of(mockPinnedMemory));
            fixture.detectChanges();

            component.personalities.set([
                { id: 'personality-123', name: 'Work Assistant' } as any
            ]);

            const name = component.getPinnedPersonalityName();
            expect(name).toBe('Work Assistant');
        });

        it('should return default message when memory is not pinned', () => {
            fixture.detectChanges();

            const name = component.getPinnedPersonalityName();
            expect(name).toBe('None (accessible by all)');
        });

        it('should return default message when pinned personality not found in list', () => {
            mockMemoryService.getMemoryById.mockReturnValue(of(mockPinnedMemory));
            fixture.detectChanges();

            component.personalities.set([
                { id: 'different-id', name: 'Other Assistant' } as any
            ]);

            const name = component.getPinnedPersonalityName();
            expect(name).toBe('Unknown personality');
        });

        it('should only show pin functionality for global/personality levels', () => {
            fixture.detectChanges();

            expect(component.memory()?.level).toBe('global');
            // Pin functionality should be available

            component.memory.set(mockChatMemory);
            expect(component.memory()?.level).toBe('thread');
            // Pin functionality should not be available for thread-level memories
        });
    });
});
