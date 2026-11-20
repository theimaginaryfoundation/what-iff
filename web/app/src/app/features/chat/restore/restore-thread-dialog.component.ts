import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonComponent } from '../../../shared/ui/button/button.component';
import { ModalComponent } from '../../../shared/ui/modal/modal.component';
import { RestoreThreadService } from './restore-thread.service';

@Component({
  selector: 'app-restore-thread-dialog',
  standalone: true,
  imports: [FormsModule, ButtonComponent, ModalComponent],
  templateUrl: './restore-thread-dialog.component.html',
  styleUrl: './restore-thread-dialog.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RestoreThreadDialogComponent {
  readonly restore = inject(RestoreThreadService);
}
