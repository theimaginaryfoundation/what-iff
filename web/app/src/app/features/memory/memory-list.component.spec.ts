import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { Router, ActivatedRoute, convertToParamMap } from '@angular/router';
import { of, throwError } from 'rxjs';
import { MemoryListComponent } from './memory-list.component';
import { MemoryService } from '../../core/services/memory.service';
import { Memory, PaginatedMemoryResponse } from '../../core/models/memory.model';
import { AuthService } from '../../core/services/auth.service';
import { UserResponse } from '../../core/models/user.model';

describe('MemoryListComponent', () => {
    let component: MemoryListComponent;
    let fixture: ComponentFixture<MemoryListComponent>;
    let mockMemoryService: Pick<MockedObject<MemoryService>, 'getMemories' | 'getMemoryById' | 'deleteMemory'>;
    let mockRouter: Pick<MockedObject<Router>, 'navigate' | 'createUrlTree' | 'serializeUrl' | 'events'>;
    let mockActivatedRoute: any;
    let mockAuthService: any;

    const mockMemories: Memory[] = [
        {
            id: '1',
            content: 'User likes coffee',
            level: 'global',
            type: 'Context',
            status: 'active',
            confidence: 0.6,
            starred: false,
            created_at: '2024-01-01T00:00:00Z',
            updated_at: '2024-01-01T00:00:00Z',
            pinned_personality_id: null
        },
        {
            id: '2',
            chat_id: 'chat-123',
            chat_name: 'Work Chat',
            content: 'Project deadline is next week',
            level: 'thread',
            type: 'Context',
            status: 'active',
            confidence: 0.6,
            starred: false,
            created_at: '2024-01-02T00:00:00Z',
            updated_at: '2024-01-02T00:00:00Z',
            pinned_personality_id: null
        }
    ];

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
            getMemories: vi.fn().mockName("MemoryService.getMemories"),
            getMemoryById: vi.fn().mockName("MemoryService.getMemoryById"),
            deleteMemory: vi.fn().mockName("MemoryService.deleteMemory")
        } as unknown as Pick<MockedObject<MemoryService>, 'getMemories' | 'getMemoryById' | 'deleteMemory'>;

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
                    get: vi.fn().mockName('get')
                }
            },
            params: of({}),
            queryParams: of({}),
            queryParamMap: of(convertToParamMap({}))
        };

        mockAuthService = {
            getUserProfile: vi.fn().mockName("AuthService.getUserProfile"),
            currentUser: signal<UserResponse | null>(mockUser), isLoggedIn: signal(true)
        };

        await TestBed.configureTestingModule({
            imports: [MemoryListComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                { provide: MemoryService, useValue: mockMemoryService },
                { provide: Router, useValue: mockRouter },
                { provide: ActivatedRoute, useValue: mockActivatedRoute },
                { provide: AuthService, useValue: mockAuthService }
            ]
        }).compileComponents();

        mockMemoryService.getMemories.mockReturnValue(of({
            results: mockMemories,
            total_count: mockMemories.length,
            page: 1
        }));

        fixture = TestBed.createComponent(MemoryListComponent);
        component = fixture.componentInstance;
    });

    it('should create', () => {
        expect(component).toBeTruthy();
    });

    describe('ngOnInit', () => {
        it('should load memories on initialization', () => {
            fixture.detectChanges();

            expect(mockMemoryService.getMemories).toHaveBeenCalledWith(1, 10, {});
            expect(component.memories()).toEqual(mockMemories);
            expect(component.totalCount()).toBe(2);
            expect(component.loading()).toBe(false);
        });

        it('should handle error when loading memories fails', async () => {
            const error = new Error('Failed to load');
            mockMemoryService.getMemories.mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            fixture.detectChanges();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.loading()).toBe(false);
            expect(component.error()).toBe('Failed to load');
            expect(console.error).toHaveBeenCalledWith('Error loading memories:', error);
        });
    });

    describe('pagination', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        it('should calculate total pages correctly', () => {
            component.totalCount.set(25);
            component.pageSize.set(10);

            expect(component.totalPages()).toBe(3);
        });

        it('should navigate to next page', () => {
            mockMemoryService.getMemories.mockClear();
            component.totalCount.set(25);

            component.nextPage();

            expect(component.currentPage()).toBe(2);
            expect(mockMemoryService.getMemories).toHaveBeenCalledWith(2, 10, {});
        });

        it('should navigate to previous page', () => {
            component.currentPage.set(2);
            mockMemoryService.getMemories.mockClear();

            component.previousPage();

            expect(component.currentPage()).toBe(1);
            expect(mockMemoryService.getMemories).toHaveBeenCalledWith(1, 10, {});
        });

        it('should go to specific page', () => {
            component.totalCount.set(50);
            component.pageSize.set(10);
            mockMemoryService.getMemories.mockClear();

            component.goToPage(3);

            expect(component.currentPage()).toBe(3);
            expect(mockMemoryService.getMemories).toHaveBeenCalledWith(3, 10, {});
        });

        it('should not go to page less than 1', () => {
            const initialPage = component.currentPage();
            mockMemoryService.getMemories.mockClear();

            component.goToPage(0);

            expect(component.currentPage()).toBe(initialPage);
            expect(mockMemoryService.getMemories).not.toHaveBeenCalled();
        });

        it('should not go to page greater than total pages', () => {
            component.totalCount.set(10);
            component.pageSize.set(10);
            const initialPage = component.currentPage();
            mockMemoryService.getMemories.mockClear();

            component.goToPage(5);

            expect(component.currentPage()).toBe(initialPage);
            expect(mockMemoryService.getMemories).not.toHaveBeenCalled();
        });

        it('should not go to next page when on last page', () => {
            component.currentPage.set(1);
            component.totalCount.set(10);
            component.pageSize.set(10);
            mockMemoryService.getMemories.mockClear();

            component.nextPage();

            expect(component.currentPage()).toBe(1);
            expect(mockMemoryService.getMemories).not.toHaveBeenCalled();
        });

        it('should not go to previous page when on first page', () => {
            component.currentPage.set(1);
            mockMemoryService.getMemories.mockClear();

            component.previousPage();

            expect(component.currentPage()).toBe(1);
            expect(mockMemoryService.getMemories).not.toHaveBeenCalled();
        });

        it('should calculate pagination from correctly', () => {
            component.currentPage.set(2);
            component.pageSize.set(10);

            expect(component.getPaginationFrom()).toBe(11);
        });

        it('should calculate pagination to correctly', () => {
            component.currentPage.set(2);
            component.pageSize.set(10);
            component.totalCount.set(25);

            expect(component.getPaginationTo()).toBe(20);
        });

        it('should handle last page pagination to correctly', () => {
            component.currentPage.set(3);
            component.pageSize.set(10);
            component.totalCount.set(25);

            expect(component.getPaginationTo()).toBe(25);
        });

        it('should compute page numbers range', () => {
            component.currentPage.set(3);
            component.totalCount.set(100);
            component.pageSize.set(10);

            const pageNumbers = component.pageNumbers();
            expect(pageNumbers).toContain(1);
            expect(pageNumbers).toContain(3);
            expect(pageNumbers).toContain(5);
        });

        it('should identify first page correctly', () => {
            component.currentPage.set(1);
            expect(component.isFirstPage()).toBe(true);

            component.currentPage.set(2);
            expect(component.isFirstPage()).toBe(false);
        });

        it('should identify last page correctly', () => {
            component.currentPage.set(1);
            component.totalCount.set(10);
            component.pageSize.set(10);
            expect(component.isLastPage()).toBe(true);

            component.totalCount.set(20);
            expect(component.isLastPage()).toBe(false);
        });
    });

    describe('search and filtering', () => {
        beforeEach(() => {
            fixture.detectChanges();
            mockMemoryService.getMemories.mockClear();
        });

        it('should search by query', () => {
            component.searchQuery.set('coffee');
            component.onSearch();

            expect(component.currentPage()).toBe(1);
            expect(mockMemoryService.getMemories).toHaveBeenCalledWith(1, 10, { query: 'coffee' });
        });

        it('should filter by level', () => {
            component.selectedLevel.set('global');
            component.onSearch();

            expect(mockMemoryService.getMemories).toHaveBeenCalledWith(1, 10, { level: 'global' });
        });

        it('should filter by chat ID', () => {
            component.selectedChatId.set('chat-123');
            component.onSearch();

            expect(mockMemoryService.getMemories).toHaveBeenCalledWith(1, 10, { chat_id: 'chat-123' });
        });

        it('should filter by date range', () => {
            component.startDate.set('2024-01-01');
            component.endDate.set('2024-12-31');
            component.onSearch();

            const filters = mockMemoryService.getMemories.mock.lastCall?.[2];
            expect(filters).toBeDefined();
            expect(filters?.min_date).toBeDefined();
            expect(filters?.max_date).toBeDefined();
        });

        it('should apply multiple filters', () => {
            component.searchQuery.set('coffee');
            component.selectedLevel.set('global');
            component.startDate.set('2024-01-01');
            component.onSearch();

            const filters = mockMemoryService.getMemories.mock.lastCall?.[2];
            expect(filters).toBeDefined();
            expect(filters?.query).toBe('coffee');
            expect(filters?.level).toBe('global');
            expect(filters?.min_date).toBeDefined();
        });

        it('should reset page to 1 when searching', () => {
            component.currentPage.set(3);
            component.onSearch();

            expect(component.currentPage()).toBe(1);
        });

        it('should clear all filters', () => {
            component.searchQuery.set('test');
            component.selectedLevel.set('global');
            component.selectedChatId.set('chat-123');
            component.startDate.set('2024-01-01');
            component.endDate.set('2024-12-31');
            component.currentPage.set(3);

            component.clearFilters();

            expect(component.searchQuery()).toBe('');
            expect(component.selectedLevel()).toBe('');
            expect(component.selectedChatId()).toBe('');
            expect(component.startDate()).toBe('');
            expect(component.endDate()).toBe('');
            expect(component.currentPage()).toBe(1);
            expect(mockMemoryService.getMemories).toHaveBeenCalledWith(1, 10, {});
        });
    });

    describe('navigation', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        it('should navigate to memory detail page', () => {
            component.viewMemory(mockMemories[0]);

            expect(mockRouter.navigate).toHaveBeenCalledWith(['/memory', '1']);
        });
    });

    describe('helper methods', () => {
        it('should track memories by id', () => {
            const id = component.trackByMemoryId(0, mockMemories[0]);
            expect(id).toBe('1');
        });

        it('should format date correctly', () => {
            const formatted = component.formatDate('2024-01-15T10:30:00Z');
            // The format includes date and time in locale format
            expect(formatted).toContain('2024');
            expect(formatted.length).toBeGreaterThan(0);
        });

        it('should return correct level badge class for global', () => {
            const badgeClass = component.getLevelBadgeClass('global');
            expect(badgeClass).toContain('bg-blue-100');
            expect(badgeClass).toContain('text-blue-800');
        });

        it('should return correct level badge class for thread', () => {
            const badgeClass = component.getLevelBadgeClass('thread');
            expect(badgeClass).toContain('bg-green-100');
            expect(badgeClass).toContain('text-green-800');
        });

        it('should truncate long content', () => {
            const longContent = 'a'.repeat(200);
            const truncated = component.truncateContent(longContent, 150);

            expect(truncated.length).toBe(153); // 150 + '...'
            expect(truncated.endsWith('...')).toBe(true);
        });

        it('should not truncate short content', () => {
            const shortContent = 'Short text';
            const result = component.truncateContent(shortContent, 150);

            expect(result).toBe(shortContent);
        });

        it('should compute hasMemories correctly', () => {
            component.memories.set([]);
            expect(component.hasMemories()).toBe(false);

            component.memories.set(mockMemories);
            expect(component.hasMemories()).toBe(true);
        });
    });

    describe('pinned memory display', () => {
        const pinnedMemory: Memory = {
            id: '3',
            content: 'Pinned memory to personality',
            level: 'personality',
            type: 'Context',
            status: 'active',
            confidence: 0.6,
            starred: false,
            pinned_personality_id: 'personality-123',
            created_at: '2024-01-03T00:00:00Z',
            updated_at: '2024-01-03T00:00:00Z'
        };

        const unpinnedMemory: Memory = {
            id: '4',
            content: 'Unpinned memory',
            level: 'global',
            type: 'Context',
            status: 'active',
            confidence: 0.6,
            starred: false,
            pinned_personality_id: null,
            created_at: '2024-01-04T00:00:00Z',
            updated_at: '2024-01-04T00:00:00Z'
        };

        it('should display pinned memories correctly', () => {
            mockMemoryService.getMemories.mockReturnValue(of({
                results: [pinnedMemory, unpinnedMemory],
                total_count: 2,
                page: 1
            }));

            fixture.detectChanges();

            expect(component.memories().length).toBe(2);
            expect(component.memories()[0].pinned_personality_id).toBe('personality-123');
            expect(component.memories()[1].pinned_personality_id).toBeNull();
        });

        it('should show pinned badge for personality-level pinned memories', () => {
            mockMemoryService.getMemories.mockReturnValue(of({
                results: [pinnedMemory],
                total_count: 1,
                page: 1
            }));

            fixture.detectChanges();

            const memory = component.memories()[0];
            expect(memory.level).toBe('personality');
            expect(memory.pinned_personality_id).toBeTruthy();
        });

        it('should not show pinned badge for unpinned memories', () => {
            mockMemoryService.getMemories.mockReturnValue(of({
                results: [unpinnedMemory],
                total_count: 1,
                page: 1
            }));

            fixture.detectChanges();

            const memory = component.memories()[0];
            expect(memory.level).toBe('global');
            expect(memory.pinned_personality_id).toBeNull();
        });

        it('should not show pinned badge for thread-level memories', () => {
            const chatMemory: Memory = {
                id: '5',
                chat_id: 'chat-456',
                chat_name: 'Team Chat',
                content: 'Chat memory',
                level: 'thread',
                type: 'Context',
                status: 'active',
                confidence: 0.6,
                starred: false,
                pinned_personality_id: null,
                created_at: '2024-01-05T00:00:00Z',
                updated_at: '2024-01-05T00:00:00Z'
            };

            mockMemoryService.getMemories.mockReturnValue(of({
                results: [chatMemory],
                total_count: 1,
                page: 1
            }));

            fixture.detectChanges();

            const memory = component.memories()[0];
            expect(memory.level).toBe('thread');
            expect(memory.pinned_personality_id).toBeNull();
        });

        it('should load mixed pinned and unpinned memories', () => {
            const mixedMemories: Memory[] = [
                pinnedMemory,
                unpinnedMemory,
                mockMemories[0],
                mockMemories[1]
            ];

            mockMemoryService.getMemories.mockReturnValue(of({
                results: mixedMemories,
                total_count: 4,
                page: 1
            }));

            fixture.detectChanges();

            expect(component.memories().length).toBe(4);

            const pinnedCount = component.memories().filter(m => m.level === 'personality' && m.pinned_personality_id).length;

            expect(pinnedCount).toBe(1);
        });
    });
});
