export function hasModifier(event: KeyboardEvent): boolean {
  return event.altKey || event.ctrlKey || event.metaKey || event.shiftKey;
}

export function isActivationKey(event: KeyboardEvent): boolean {
  return (event.key === 'Enter' || event.key === ' ') && !hasModifier(event);
}

export function isEscapeKey(event: KeyboardEvent): boolean {
  return event.key === 'Escape' || event.key === 'Esc';
}

export function isTabKey(event: KeyboardEvent): boolean {
  return event.key === 'Tab';
}
