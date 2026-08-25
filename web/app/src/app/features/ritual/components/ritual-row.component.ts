import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { parseHotkeyKeys, formatKeyForDisplay } from '../../../core/utils/hotkey.utils';
import { RitualRowVm } from '../helpers/ritual-vm.helpers';

@Component({
  selector: 'app-ritual-row',
  standalone: true,
  imports: [DatePipe],
  templateUrl: './ritual-row.component.html',
  styleUrl: './ritual-row.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RitualRowComponent {
  readonly ritual = input.required<RitualRowVm>();
  readonly selected = input(false);
  readonly dense = input(false);

  readonly open = output<string>();
  readonly delete = output<string>();
  readonly toggleSelected = output<string>();

  hotkeyParts(hotkeys: string): string[] {
    return parseHotkeyKeys(hotkeys);
  }

  displayKey(key: string): string {
    return formatKeyForDisplay(key);
  }
}
