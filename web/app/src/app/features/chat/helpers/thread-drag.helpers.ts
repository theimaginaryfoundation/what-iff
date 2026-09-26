/** DataTransfer type carrying a thread id when a sidebar thread is dragged onto the composer. */
export const THREAD_DRAG_MIME = 'text/thread-id';

export function startThreadDrag(event: DragEvent, threadId: string): void {
  if (!event.dataTransfer) return;
  event.dataTransfer.setData(THREAD_DRAG_MIME, threadId);
  event.dataTransfer.effectAllowed = 'copy';
}

export function isThreadDrag(event: DragEvent): boolean {
  return Array.from(event.dataTransfer?.types ?? []).includes(THREAD_DRAG_MIME);
}
