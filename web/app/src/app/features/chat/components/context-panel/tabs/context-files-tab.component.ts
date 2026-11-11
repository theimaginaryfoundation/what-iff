import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, computed, effect, inject, input, signal, untracked, viewChild } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Subscription } from 'rxjs';

import { ChatContextFile, ChatContextFilesResponse, ChatContextReason, FileAttachment } from '../../../../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../../../../core/services/file-attachment.service';
import { apiErrorMessage } from '../../../../../core/utils/api-error.helpers';
import { HelpHintComponent } from '../../../../../shared/ui/help-hint/help-hint.component';
import { TooltipDirective } from '../../../../../shared/ui/tooltip/tooltip.directive';
import { FileWorkspaceService } from '../../../../file-workspace/file-workspace.service';
import { fileBadge, fileTypeLabel } from '../../../../gallery/helpers/file-preview.helpers';
import { isMimeOrExtensionAllowedForChatUpload, MAX_FILE_SIZE_BYTES } from '../../../chat.constants';
import { ContextPanelService } from '../../../services/context-panel.service';

const REASON_LABELS: Record<string, string> = {
  attached_recent: 'Attached',
  attached_since_checkpoint: 'Attached',
  recalled: 'Recalled',
  produced_this_turn: 'Produced',
  agent_written: 'Agent file',
  skill_bundle: 'Skill',
};

/**
 * The Files tab (ADR 0x043 §4): the files the agent working in this thread sees, by the same rule
 * the private build hydrates its container with. A row opens the file in the workspace; Upload and
 * From gallery add a file to the next message so it joins the context.
 */
@Component({
  selector: 'app-context-files-tab',
  standalone: true,
  imports: [HelpHintComponent, TooltipDirective],
  templateUrl: './context-files-tab.component.html',
  styleUrl: './context-files-tab.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ContextFilesTabComponent {
  private readonly files = inject(FileAttachmentService);
  private readonly destroyRef = inject(DestroyRef);
  private readonly contextPanel = inject(ContextPanelService);
  private readonly workspace = inject(FileWorkspaceService);
  private readonly picker = viewChild<ElementRef<HTMLInputElement>>('picker');

  readonly chatId = input<string | null>(null);

  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly response = signal<ChatContextFilesResponse | null>(null);
  readonly uploading = signal(false);
  readonly uploadError = signal<string | null>(null);

  readonly rows = computed(() => this.response()?.files ?? []);
  readonly container = computed(() => this.response()?.container ?? null);
  /** The files that hold their workspace path, then the ones a newer file shadows. */
  readonly mounted = computed(() => this.rows().filter(row => !row.shadowed_by));
  readonly shadowed = computed(() => this.rows().filter(row => !!row.shadowed_by));
  readonly openId = computed(() => this.workspace.fileId());
  /** Files attached to the message being composed: in context once it is sent. */
  readonly pending = computed(() => {
    const listed = new Set(this.rows().map(row => row.file.id));
    return this.contextPanel.composerPending().filter(file => !listed.has(file.id));
  });

  private loadSubscription: Subscription | null = null;

  constructor() {
    effect(() => {
      const chatId = this.chatId();
      this.contextPanel.filesVersion();
      untracked(() => void this.load(chatId));
    });
  }

  badge(file: FileAttachment): string {
    return fileBadge(file);
  }

  typeLabel(file: FileAttachment): string {
    return fileTypeLabel(file);
  }

  reasonLabels(row: ChatContextFile): string[] {
    const seen = new Set<string>();
    const out: string[] = [];
    for (const reason of row.reasons) {
      const label = REASON_LABELS[reason as string] ?? humanize(reason);
      if (!seen.has(label)) {
        seen.add(label);
        out.push(label);
      }
    }
    return out;
  }

  containerState(row: ChatContextFile): string | null {
    return this.container()?.files?.[row.workspace_path] ?? null;
  }

  shadowedByName(row: ChatContextFile): string {
    const winner = this.rows().find(r => r.file.id === row.shadowed_by);
    return winner ? winner.file.name : 'a newer file';
  }

  open(row: ChatContextFile): void {
    this.openFile(row.file);
  }

  /** The panel closes on every click, including one on the file already open behind it. */
  openFile(file: FileAttachment): void {
    this.contextPanel.hideForOpenFile();
    this.workspace.requestOpen(file.id);
  }

  reload(): void {
    this.contextPanel.refreshFiles();
  }

  pickFiles(): void {
    this.picker()?.nativeElement.click();
  }

  fromGallery(): void {
    this.contextPanel.requestGalleryPicker();
  }

  /** A blank page to sketch on (ADR 0x045 §7); the chat page opens it. */
  newCanvas(): void {
    this.contextPanel.requestSketch();
  }

  onPicked(event: Event): void {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    const chatId = this.chatId();
    if (!file || !chatId) return;
    if (!isMimeOrExtensionAllowedForChatUpload(file)) {
      this.uploadError.set(`"${file.name}" is not a supported file type.`);
      return;
    }
    if (file.size > MAX_FILE_SIZE_BYTES) {
      this.uploadError.set(`"${file.name}" is over the ${Math.round(MAX_FILE_SIZE_BYTES / (1024 * 1024))} MB limit.`);
      return;
    }
    this.uploadError.set(null);
    this.uploading.set(true);
    this.files.uploadChatFileAttachment(chatId, file).pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: attachment => {
        this.uploading.set(false);
        this.contextPanel.requestComposerAttachment(attachment);
      },
      error: error => {
        this.uploading.set(false);
        this.uploadError.set(apiErrorMessage(error, 'Upload failed'));
      },
    });
  }

  private load(chatId: string | null): void {
    // A newer load replaces whatever is in flight, including when there is no thread any more.
    this.loadSubscription?.unsubscribe();
    this.error.set(null);
    if (!chatId) {
      this.response.set(null);
      this.loading.set(false);
      return;
    }
    this.loading.set(true);
    this.loadSubscription = this.files.listChatContextFiles(chatId).pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: response => {
        this.response.set(response);
        this.loading.set(false);
      },
      error: error => {
        this.error.set(apiErrorMessage(error, "Failed to load the thread's files"));
        this.loading.set(false);
      },
    });
  }
}

function humanize(reason: ChatContextReason): string {
  const text = String(reason).replace(/_/g, ' ');
  return text.charAt(0).toUpperCase() + text.slice(1);
}
