import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { RightPanelService } from '../../core/services/right-panel.service';
import { ContextPanelService } from '../../features/chat/services/context-panel.service';
import { ContextPanelComponent } from '../../features/chat/components/context-panel/context-panel.component';
import { FileWorkspacePanelComponent } from '../../features/file-workspace/file-workspace-panel.component';
import { FileWorkspaceService } from '../../features/file-workspace/file-workspace.service';

/**
 * Empty right-context-panel slot. Phase 7 (Conversation Context) replaces the
 * inner template with the real content driver. For now it just reacts to
 * `RightPanelService.visible()` so feature components (Phase 4 onwards) can
 * already opt into the slot without waiting for Phase 7.
 */
@Component({
  selector: 'app-right-panel-host',
  standalone: true,
  imports: [ContextPanelComponent, FileWorkspacePanelComponent],
  templateUrl: './right-panel-host.component.html',
  styleUrl: './right-panel-host.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RightPanelHostComponent {
  private readonly service = inject(RightPanelService);
  readonly context = inject(ContextPanelService);
  readonly workspace = inject(FileWorkspaceService);
  readonly visible = computed(() => this.service.visible());
}
