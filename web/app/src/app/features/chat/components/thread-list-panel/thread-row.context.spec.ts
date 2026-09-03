import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { Chat } from '../../../../core/models/chat.model';
import { ImageGalleryService } from '../../../../core/services/image-gallery.service';
import { ThreadRowComponent } from './thread-row.component';

describe('ThreadRowComponent thread context action', () => {
  let fixture: ComponentFixture<ThreadRowComponent>;

  const thread: Chat = {
    id: 'thread-history-1',
    user_id: 'user-1',
    name: 'Old research thread',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-02T00:00:00Z',
  };

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ThreadRowComponent],
      providers: [
        provideZonelessChangeDetection(),
        {
          provide: ImageGalleryService,
          useValue: { getImageUrl: vi.fn() },
        },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(ThreadRowComponent);
    fixture.componentRef.setInput('thread', thread);
    fixture.detectChanges();
  });

  it('offers an add-to-context action and emits the selected thread without opening it', () => {
    const selected: Chat[] = [];
    (fixture.componentInstance as any).addToContext.subscribe((value: Chat) => selected.push(value));

    const button = fixture.nativeElement.querySelector(
      '[aria-label="Add thread Old research thread to context"]',
    ) as HTMLButtonElement | null;

    expect(button).not.toBeNull();
    button?.click();

    expect(selected).toEqual([thread]);
  });
});
