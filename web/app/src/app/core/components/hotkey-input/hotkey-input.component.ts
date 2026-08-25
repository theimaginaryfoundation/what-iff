import { Component, Input, Output, EventEmitter, signal, computed, ElementRef, ViewChild, forwardRef, OnDestroy, ChangeDetectionStrategy } from '@angular/core';

import { ControlValueAccessor, NG_VALUE_ACCESSOR } from '@angular/forms';
import { parseHotkeyKeys, formatKeyForDisplay } from '../../utils/hotkey.utils';

interface KeyCombination {
  key: string;
  code: string;
  ctrlKey: boolean;
  altKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
}

@Component({
  selector: 'app-hotkey-input',
  standalone: true,
  imports: [],
  templateUrl: './hotkey-input.component.html',
  styleUrls: ['./hotkey-input.component.scss'],
  changeDetection: ChangeDetectionStrategy.Eager,
  providers: [
    {
      provide: NG_VALUE_ACCESSOR,
      useExisting: forwardRef(() => HotkeyInputComponent),
      multi: true
    }
  ]
})
export class HotkeyInputComponent implements ControlValueAccessor, OnDestroy {
  @ViewChild('hotkeyInput', { static: true }) hotkeyInput!: ElementRef<HTMLDivElement>;

  @Input() placeholder: string = '';
  @Input() disabled: boolean = false;
  @Input() showHelp: boolean = true;

  @Output() hotkeyChange = new EventEmitter<string>();

  private value = signal<string>('');
  protected isFocused = signal<boolean>(false);
  protected isRecording = signal<boolean>(false);
  protected errorMessage = signal<string>('');
  protected successMessage = signal<string>('');
  private currentKeys = new Set<string>();
  private keyTimeout: any;

  // Form control integration
  private onChange = (value: string) => {};
  private onTouched = () => {};

  // Computed properties
  parsedHotkeys = computed(() => {
    const value = this.value();
    if (!value) return [];
    return this.parseHotkeyString(value);
  });

  constructor() {}

  ngOnDestroy(): void {
    if (this.keyTimeout) {
      clearTimeout(this.keyTimeout);
    }
  }

  // ControlValueAccessor implementation
  writeValue(value: string): void {
    this.value.set(value || '');
  }

  registerOnChange(fn: (value: string) => void): void {
    this.onChange = fn;
  }

  registerOnTouched(fn: () => void): void {
    this.onTouched = fn;
  }

  setDisabledState(isDisabled: boolean): void {
    this.disabled = isDisabled;
  }

  // Event handlers
  onFocus(): void {
    if (this.disabled) return;
    this.isFocused.set(true);
    this.errorMessage.set('');
    this.successMessage.set('');
  }

  onBlur(): void {
    this.isFocused.set(false);
    this.isRecording.set(false);
    this.currentKeys.clear();
    this.onTouched();
  }

  onKeyDown(event: KeyboardEvent): void {
    if (this.disabled) return;

    event.preventDefault();
    event.stopPropagation();

    // Ignore single modifier keys or function keys
    if (this.isIgnoredKey(event.code)) {
      return;
    }

    // Start recording when any key is pressed
    if (!this.isRecording()) {
      this.isRecording.set(true);
      this.currentKeys.clear();
      this.errorMessage.set('');
      this.successMessage.set('');
    }

    // Build the key combination
    const combination: KeyCombination = {
      key: event.key,
      code: event.code,
      ctrlKey: event.ctrlKey,
      altKey: event.altKey,
      metaKey: event.metaKey,
      shiftKey: event.shiftKey
    };

    // Add keys to current combination
    if (combination.ctrlKey) this.currentKeys.add('ctrl');
    if (combination.altKey) this.currentKeys.add('alt');
    if (combination.metaKey) this.currentKeys.add('meta');
    if (combination.shiftKey) this.currentKeys.add('shift');
    
    // Add the main key if it's not a modifier
    if (!this.isModifierKey(event.code)) {
      this.currentKeys.add(event.code.toLowerCase());
    }

    // Clear existing timeout
    if (this.keyTimeout) {
      clearTimeout(this.keyTimeout);
    }

    // Set timeout to finalize the combination
    this.keyTimeout = setTimeout(() => {
      this.finalizeKeyCombination();
    }, 100);
  }

  clearHotkeys(): void {
    if (this.disabled) return;
    this.setValue('');
    this.successMessage.set('Hotkeys cleared');
    setTimeout(() => this.successMessage.set(''), 2000);
  }

  // Helper methods
  private finalizeKeyCombination(): void {
    const keys = Array.from(this.currentKeys);
    
    if (keys.length === 0) {
      this.isRecording.set(false);
      return;
    }

    // Validate the combination
    const validation = this.validateKeyCombination(keys);
    
    if (!validation.isValid) {
      this.errorMessage.set(validation.error);
      this.isRecording.set(false);
      setTimeout(() => this.errorMessage.set(''), 3000);
      return;
    }

    // Format and set the hotkey
    const formattedHotkey = this.formatKeyCombination(keys);
    this.setValue(formattedHotkey);
    this.successMessage.set('Hotkey set successfully');
    this.isRecording.set(false);
    
    setTimeout(() => this.successMessage.set(''), 2000);
  }

  private validateKeyCombination(keys: string[]): { isValid: boolean; error: string } {
    // Must have at least 2 keys
    if (keys.length < 2) {
      return { isValid: false, error: 'Must press at least 2 keys' };
    }

    // Must have at least one modifier key
    const hasModifier = keys.some(key => 
      key === 'ctrl' || key === 'alt' || key === 'meta'
    );

    if (!hasModifier) {
      return { isValid: false, error: 'Must include Ctrl, Alt, or Cmd/Win key' };
    }

    // Must have at least one non-modifier key (excluding shift)
    const nonModifierKeys = keys.filter(key => 
      !['ctrl', 'alt', 'meta', 'shift'].includes(key)
    );

    if (nonModifierKeys.length === 0) {
      return { isValid: false, error: 'Must include at least one letter or number key' };
    }

    return { isValid: true, error: '' };
  }

  private formatKeyCombination(keys: string[]): string {
    const modifierOrder = ['ctrl', 'alt', 'meta', 'shift'];
    const modifiers: string[] = [];
    const others: string[] = [];

    keys.forEach(key => {
      if (modifierOrder.includes(key)) {
        modifiers.push(key);
      } else {
        others.push(key);
      }
    });

    // Sort modifiers by preferred order
    modifiers.sort((a, b) => modifierOrder.indexOf(a) - modifierOrder.indexOf(b));

    // Combine all keys
    const allKeys = [...modifiers, ...others];
    
    // Format for display
    return allKeys.map(key => this.formatKeyDisplay(key)).join('+');
  }

  private parseHotkeyString(hotkey: string): string[] {
    return parseHotkeyKeys(hotkey);
  }

  formatKeyDisplay(key: string): string {
    return formatKeyForDisplay(key);
  }

  isModifierKey(key: string): boolean {
    const lowerKey = key.toLowerCase();
    return ['ctrl', 'alt', 'meta', 'shift'].includes(lowerKey);
  }

  private isIgnoredKey(code: string): boolean {
    const ignoredKeys = [
      'ControlLeft', 'ControlRight',
      'AltLeft', 'AltRight',
      'MetaLeft', 'MetaRight',
      'ShiftLeft', 'ShiftRight',
      'CapsLock', 'NumLock', 'ScrollLock',
      'Fn', 'FnLock'
    ];
    
    return ignoredKeys.includes(code);
  }

  private setValue(value: string): void {
    this.value.set(value);
    this.onChange(value);
    this.hotkeyChange.emit(value);
  }
}
