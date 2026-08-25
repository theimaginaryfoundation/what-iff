/**
 * Hotkey utility functions for parsing and formatting keyboard shortcuts
 */

export interface ParsedHotkey {
  modifiers: {
    ctrl: boolean;
    shift: boolean;
    alt: boolean;
    meta: boolean;
  };
  keys: string[];
  originalString: string;
}

/**
 * Parses a hotkey string into individual keys
 * @param hotkey - The hotkey string (e.g., "Ctrl+Shift+P" or "Ctrl+J+K")
 * @returns Array of trimmed, lowercase keys
 */
export function parseHotkeyKeys(hotkey: string): string[] {
  if (!hotkey) return [];
  return hotkey.split('+').map(key => key.trim().toLowerCase());
}

/**
 * Parses a hotkey string into modifiers and main keys
 * @param hotkeyString - The hotkey string to parse
 * @returns Parsed hotkey object with modifiers and keys
 */
export function parseHotkey(hotkeyString: string): ParsedHotkey {
  const keys = parseHotkeyKeys(hotkeyString);
  
  const modifiers = {
    ctrl: keys.includes('ctrl') || keys.includes('control'),
    shift: keys.includes('shift'),
    alt: keys.includes('alt'),
    meta: keys.includes('meta') || keys.includes('cmd') || keys.includes('command')
  };
  
  const mainKeys = keys.filter(k => 
    !['ctrl', 'control', 'shift', 'alt', 'meta', 'cmd', 'command'].includes(k)
  );
  
  return {
    modifiers,
    keys: mainKeys,
    originalString: hotkeyString
  };
}

/**
 * Formats a key for display purposes
 * @param key - The key to format
 * @returns Formatted key string
 */
export function formatKeyForDisplay(key: string): string {
  const keyMap: { [key: string]: string } = {
    ' ': 'Space',
    'arrowup': '↑',
    'arrowdown': '↓',
    'arrowleft': '←',
    'arrowright': '→',
    'enter': 'Enter',
    'escape': 'Esc',
    'backspace': 'Backspace',
    'delete': 'Delete',
    'tab': 'Tab',
    'capslock': 'Caps Lock',
    'shift': 'Shift',
    'control': 'Ctrl',
    'ctrl': 'Ctrl',
    'alt': 'Alt',
    'meta': 'Cmd',
    'cmd': 'Cmd',
    'command': 'Cmd'
  };

  // Handle function keys
  if (key.startsWith('f') && key.length <= 3) {
    const fNumber = key.substring(1);
    if (!isNaN(Number(fNumber))) {
      return `F${fNumber}`;
    }
  }

  // Handle key codes like 'keya', 'digit1', etc.
  if (key.startsWith('key')) {
    return key.substring(3).toUpperCase();
  }
  if (key.startsWith('digit')) {
    return key.substring(5);
  }
  if (key.startsWith('numpad')) {
    return `Num${key.substring(6)}`;
  }

  // Return mapped key or capitalize first letter
  return keyMap[key.toLowerCase()] || key.charAt(0).toUpperCase() + key.slice(1);
}

/**
 * Normalizes a key for comparison purposes
 * @param key - The key to normalize
 * @returns Normalized key string
 */
export function normalizeKey(key: string): string {
  const normalized = key.toLowerCase();
  // Handle special cases
  if (normalized === ' ') return 'space';
  return normalized;
}
