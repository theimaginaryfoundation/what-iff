import { Component, inject, signal, OnInit, ChangeDetectionStrategy } from '@angular/core';

import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { HotkeyInputComponent } from '../../core/components/hotkey-input/hotkey-input.component';
import { RitualService } from '../../core/services/ritual.service';
import { PersonalityService } from '../../core/services/personality.service';
import { MCPServerService } from '../../core/services/mcp-server.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { Ritual, UpdateRitualRequest } from '../../core/models/ritual.model';
import { Personality } from '../../core/models/personality.model';
import { MCPServer } from '../../core/models/mcp-server.model';
import { NULL_PERSONALITY_ID } from '../../core/constants/app.constants';
import { parseHotkeyKeys, formatKeyForDisplay } from '../../core/utils/hotkey.utils';

@Component({
  selector: 'app-ritual-detail',
  standalone: true,
  imports: [FormsModule, HotkeyInputComponent],
  templateUrl: './ritual-detail.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./ritual-detail.component.scss']
})
export class RitualDetailComponent implements OnInit {
  private ritualService = inject(RitualService);
  private personalityService = inject(PersonalityService);
  private mcpServerService = inject(MCPServerService);
  private route = inject(ActivatedRoute);
  private router = inject(Router);
  private confirmationService = inject(ConfirmationService);

  ritual = signal<Ritual | null>(null);
  personalities = signal<Personality[]>([]);
  mcpServers = signal<MCPServer[]>([]);
  isLoading = signal(true);
  isEditing = signal(false);
  isSaving = signal(false);
  errorMessage = signal<string | null>(null);
  successMessage = signal<string | null>(null);

  // Edit form
  editForm = signal({
    name: '',
    description: '',
    content: '',
    hotkeys: '',
    personality_id: '',
    mcp_server_ids: [] as string[]
  });


  ngOnInit(): void {
    const ritualId = this.route.snapshot.paramMap.get('id');
    if (ritualId) {
      this.loadRitual(ritualId);
      this.loadPersonalities();
      this.loadMcpServers();
    } else {
      this.router.navigate(['/skills']);
    }
  }

  private loadRitual(id: string): void {
    this.isLoading.set(true);
    this.ritualService.getRitual(id).subscribe({
      next: (ritual) => {
        this.ritual.set(ritual);
        this.editForm.set({
          name: ritual.name,
          description: ritual.description,
          content: ritual.content,
          hotkeys: ritual.hotkeys,
          personality_id: ritual.personality_id || '',
          mcp_server_ids: ritual.mcp_server_ids ? [...ritual.mcp_server_ids] : []
        });
        this.isLoading.set(false);
      },
      error: (error) => {
        console.error('Failed to load ritual:', error);
        this.errorMessage.set('Failed to load skill. Please try again.');
        this.isLoading.set(false);
      }
    });
  }

  private loadPersonalities(): void {
    this.personalityService.listPersonalities(1, 100).subscribe({
      next: (response) => {
        this.personalities.set(response.results);
      },
      error: (error) => {
        console.error('Failed to load personalities:', error);
      }
    });
  }

  private loadMcpServers(): void {
    this.mcpServerService.listMCPServers(1, 200).subscribe({
      next: (response) => this.mcpServers.set(response.results),
      error: () => this.mcpServers.set([])
    });
  }

  startEditing(): void {
    const ritual = this.ritual();
    if (ritual) {
      this.editForm.set({
        name: ritual.name,
        description: ritual.description,
        content: ritual.content,
        hotkeys: ritual.hotkeys,
        personality_id: ritual.personality_id || '',
        mcp_server_ids: ritual.mcp_server_ids ? [...ritual.mcp_server_ids] : []
      });
      this.isEditing.set(true);
      this.errorMessage.set(null);
      this.successMessage.set(null);
    }
  }

  cancelEditing(): void {
    const ritual = this.ritual();
    if (ritual) {
      this.editForm.set({
        name: ritual.name,
        description: ritual.description,
        content: ritual.content,
        hotkeys: ritual.hotkeys,
        personality_id: ritual.personality_id || '',
        mcp_server_ids: ritual.mcp_server_ids ? [...ritual.mcp_server_ids] : []
      });
    }
    this.isEditing.set(false);
    this.errorMessage.set(null);
  }

  saveRitual(): void {
    const ritual = this.ritual();
    const form = this.editForm();

    if (!ritual) return;

    if (!form.name.trim() || !form.description.trim() || !form.content.trim()) {
      this.errorMessage.set('Name, description, and content are required.');
      return;
    }

    this.isSaving.set(true);
    this.errorMessage.set(null);

    const request: UpdateRitualRequest = {
      name: form.name.trim(),
      description: form.description.trim(),
      content: form.content.trim(),
      hotkeys: form.hotkeys.trim() || undefined,
      personality_id: form.personality_id || null,
      mcp_server_ids: [...form.mcp_server_ids]
    };

    this.ritualService.updateRitual(ritual.id, request).subscribe({
      next: (updatedRitual) => {
        this.ritual.set(updatedRitual);
        this.isEditing.set(false);
        this.isSaving.set(false);
        this.successMessage.set('Skill updated successfully!');

        // Clear success message after 3 seconds
        setTimeout(() => {
          this.successMessage.set(null);
        }, 3000);
      },
      error: (error) => {
        console.error('Failed to update ritual:', error);
        this.errorMessage.set(error.message || 'Failed to update skill. Please try again.');
        this.isSaving.set(false);
      }
    });
  }

  async showDeleteModal(): Promise<void> {
    const ritual = this.ritual();
    if (!ritual) return;

    const confirmed = await this.confirmationService.confirm({
      title: 'Delete Skill',
      message: `Are you sure you want to delete "${ritual.name}"? This action cannot be undone.`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
      keepOpen: true // Keep modal open for async operation
    });

    if (!confirmed) {
      return;
    }

    // Set loading state
    this.confirmationService.setLoading(true, 'Deleting...');

    this.ritualService.deleteRitual(ritual.id).subscribe({
      next: () => {
        this.confirmationService.close();
        this.router.navigate(['/skills']);
      },
      error: async (error) => {
        console.error('Failed to delete ritual:', error);
        this.confirmationService.setLoading(false);
        this.confirmationService.close();
        this.errorMessage.set('Failed to delete skill. Please try again.');
        // Show error alert and await it to ensure it completes
        try {
          await this.confirmationService.alert({
            message: 'Failed to delete skill. Please try again.',
            type: 'danger'
          });
        } catch (alertError) {
          // Handle any errors in alert flow to prevent inconsistent state
          console.error('Error showing alert:', alertError);
        }
      }
    });
  }

  copyToClipboard(text: string): void {
    navigator.clipboard.writeText(text).then(() => {
      this.successMessage.set('Copied to clipboard!');
      setTimeout(() => {
        this.successMessage.set(null);
      }, 2000);
    }).catch((error) => {
      console.error('Failed to copy to clipboard:', error);
      this.errorMessage.set('Failed to copy to clipboard');
    });
  }

  getPersonalityName(personalityId: string | null): string | null {
    if (!personalityId) return null;
    const personality = this.personalities().find(p => p.id === personalityId);
    return personality?.name || null;
  }

  getMcpServerName(serverId: string): string {
    const s = this.mcpServers().find((x) => x.id === serverId);
    return s?.name || serverId;
  }

  isMcpServerSelected(serverId: string): boolean {
    return this.editForm().mcp_server_ids.includes(serverId);
  }

  toggleMcpServer(serverId: string, checked: boolean): void {
    const f = this.editForm();
    const set = new Set(f.mcp_server_ids);
    if (checked) {
      set.add(serverId);
    } else {
      set.delete(serverId);
    }
    this.editForm.set({ ...f, mcp_server_ids: [...set] });
  }

  goBack(): void {
    this.router.navigate(['/skills']);
  }

  formatDate(dateString: string): string {
    const date = new Date(dateString);
    return date.toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  }

  parseHotkeys(hotkey: string): string[] {
    return parseHotkeyKeys(hotkey);
  }

  isModifierKey(key: string): boolean {
    const lowerKey = key.toLowerCase();
    return ['ctrl', 'alt', 'meta', 'shift', 'cmd', 'win'].includes(lowerKey);
  }

  formatKeyDisplay(key: string): string {
    return formatKeyForDisplay(key);
  }

  /**
   * Getter to expose the null personality ID constant for use in the template
   */
  get nullPersonalityId(): string {
    return NULL_PERSONALITY_ID;
  }
}
