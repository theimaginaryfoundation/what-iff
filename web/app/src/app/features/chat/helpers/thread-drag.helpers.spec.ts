import { THREAD_DRAG_MIME, isThreadDrag, startThreadDrag } from './thread-drag.helpers';

function dragEventWith(dataTransfer: Partial<DataTransfer> | null): DragEvent {
  return { dataTransfer } as unknown as DragEvent;
}

describe('thread drag helpers', () => {
  it('writes the thread id and copy effect on drag start', () => {
    const setData = vi.fn().mockName('setData');
    const dataTransfer = { setData, effectAllowed: 'none' } as unknown as DataTransfer;

    startThreadDrag(dragEventWith(dataTransfer), 'thread-1');

    expect(setData).toHaveBeenCalledWith(THREAD_DRAG_MIME, 'thread-1');
    expect(dataTransfer.effectAllowed).toBe('copy');
  });

  it('ignores drag start without a dataTransfer', () => {
    expect(() => startThreadDrag(dragEventWith(null), 'thread-1')).not.toThrow();
  });

  it('detects thread drags by their DataTransfer type', () => {
    expect(isThreadDrag(dragEventWith({ types: [THREAD_DRAG_MIME] }))).toBe(true);
    expect(isThreadDrag(dragEventWith({ types: ['Files'] }))).toBe(false);
    expect(isThreadDrag(dragEventWith(null))).toBe(false);
  });
});
