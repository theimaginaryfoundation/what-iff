import { Component, provideZonelessChangeDetection, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it } from 'vitest';

import { BubbleResizeDirective, BubbleWidths, MIN_BUBBLE_WIDTH } from './bubble-resize.directive';

@Component({
  standalone: true,
  imports: [BubbleResizeDirective],
  template: `<div class="message-group" style="width: 1000px">
    <div>
      <div><div class="bubble__body" [appBubbleResize]="id()" [resizeEdge]="edge()">Hello</div></div>
    </div>
  </div>`,
})
class HostComponent {
  readonly id = signal('m-1');
  readonly edge = signal<'left' | 'right'>('right');
}

describe('BubbleResizeDirective', () => {
  beforeEach(() => {
    localStorage.removeItem('whatiff.bubbleWidths');
    TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection()] });
  });

  function mount() {
    const fixture = TestBed.createComponent(HostComponent);
    fixture.detectChanges();
    const body = fixture.nativeElement.querySelector('.bubble__body') as HTMLElement;
    const handle = body.querySelector('.bubble__resize') as HTMLElement;
    // jsdom lays nothing out: the bubble is 400px wide, its row 1000px.
    body.getBoundingClientRect = () => ({ width: 400 }) as DOMRect;
    (fixture.nativeElement.querySelector('.message-group') as HTMLElement).getBoundingClientRect = () => ({ width: 1000 }) as DOMRect;
    return { fixture, body, handle };
  }

  const key = (el: HTMLElement, k: string) => el.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true }));

  it('adds a separator handle on the outer edge, and no width until resized', () => {
    const { body, handle } = mount();
    expect(handle.getAttribute('role')).toBe('separator');
    expect(handle.dataset['edge']).toBe('right');
    expect(body.style.width).toBe('');
    expect(body.classList.contains('bubble--sized')).toBe(false);
  });

  it('resizes with the arrow keys, keeps the width per message, and resets', () => {
    const { fixture, body, handle } = mount();
    key(handle, 'ArrowRight');
    fixture.detectChanges();
    expect(body.style.width).toBe('440px');
    expect(body.classList.contains('bubble--sized')).toBe(true);
    expect(TestBed.inject(BubbleWidths).get('m-1')).toBe(440);

    // Another bubble of the same message, as after a reload, starts at the kept width.
    const again = mount();
    expect(again.body.style.width).toBe('440px');

    handle.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    fixture.detectChanges();
    expect(body.style.width).toBe('');
    expect(TestBed.inject(BubbleWidths).get('m-1')).toBeNull();
  });

  it('keeps the width between the minimum and what the row leaves', () => {
    const { fixture, body, handle } = mount();
    for (let i = 0; i < 30; i++) key(handle, 'ArrowLeft');
    fixture.detectChanges();
    expect(body.style.width).toBe(`${MIN_BUBBLE_WIDTH}px`);
    for (let i = 0; i < 40; i++) key(handle, 'ArrowRight');
    fixture.detectChanges();
    expect(body.style.width).toBe('1000px');
  });

  it('grows a bubble on the left edge (the user’s) by moving left', () => {
    const { fixture, body, handle } = mount();
    fixture.componentInstance.edge.set('left');
    fixture.detectChanges();
    expect(handle.dataset['edge']).toBe('left');
    key(handle, 'ArrowLeft');
    fixture.detectChanges();
    expect(body.style.width).toBe('440px');
  });
});
