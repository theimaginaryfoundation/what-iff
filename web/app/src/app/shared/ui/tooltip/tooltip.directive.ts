import { DOCUMENT } from '@angular/common';
import { Directive, ElementRef, HostListener, OnDestroy, Renderer2, computed, inject, input } from '@angular/core';

export type TooltipPlacement = 'top' | 'bottom' | 'left' | 'right';

let tooltipId = 0;

@Directive({
  selector: '[uiTooltip]',
  standalone: true,
})
export class TooltipDirective implements OnDestroy {
  readonly uiTooltip = input<string>('');
  readonly placement = input<TooltipPlacement>('top');
  readonly disabledOnTouch = input(true);

  private readonly elementRef = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly renderer = inject(Renderer2);
  private readonly document = inject(DOCUMENT);
  private readonly id = `ui-tooltip-${++tooltipId}`;
  private readonly tooltipOffset = 10;
  private tooltipElement: HTMLElement | null = null;
  private tooltipText: Text | null = null;

  private readonly isDisabledForTouch = computed(() => {
    const view = this.document.defaultView;
    if (!this.disabledOnTouch()) {
      return false;
    }

    // In non-browser environments, suppress touch-hostile tooltip behavior.
    if (!view?.matchMedia) {
      return true;
    }

    return view.matchMedia('(pointer: coarse)').matches;
  });

  @HostListener('mouseenter')
  @HostListener('focus')
  show(): void {
    if (!this.uiTooltip() || this.isDisabledForTouch()) {
      return;
    }

    const tooltip = this.ensureTooltipElement();

    this.updatePlacementClass(tooltip);
    this.tooltipText!.textContent = this.uiTooltip();
    this.renderer.removeAttribute(tooltip, 'hidden');
    this.renderer.setAttribute(this.elementRef.nativeElement, 'aria-describedby', this.id);
    this.positionTooltip(tooltip);
  }

  @HostListener('mouseleave')
  @HostListener('blur')
  hide(): void {
    if (this.tooltipElement) {
      this.renderer.setAttribute(this.tooltipElement, 'hidden', '');
    }

    this.renderer.removeAttribute(this.elementRef.nativeElement, 'aria-describedby');
  }

  ngOnDestroy(): void {
    this.hide();
    if (this.tooltipElement?.parentNode) {
      this.renderer.removeChild(this.tooltipElement.parentNode, this.tooltipElement);
    }
    this.tooltipElement = null;
    this.tooltipText = null;
  }

  @HostListener('keydown.escape')
  onEscape(): void {
    this.hide();
  }

  private ensureTooltipElement(): HTMLElement {
    if (!this.tooltipElement) {
      const tooltip = this.renderer.createElement('span') as HTMLElement;
      const text = this.renderer.createText(this.uiTooltip());

      this.renderer.setAttribute(tooltip, 'id', this.id);
      this.renderer.setAttribute(tooltip, 'role', 'tooltip');
      this.renderer.addClass(tooltip, 'ui-tooltip');
      this.renderer.appendChild(tooltip, text);
      this.tooltipElement = tooltip;
      this.tooltipText = text;
    }

    if (!this.tooltipElement.parentNode) {
      this.renderer.appendChild(this.document.body, this.tooltipElement);
    }

    return this.tooltipElement;
  }

  private updatePlacementClass(tooltip: HTMLElement): void {
    this.renderer.removeClass(tooltip, 'ui-tooltip--top');
    this.renderer.removeClass(tooltip, 'ui-tooltip--right');
    this.renderer.removeClass(tooltip, 'ui-tooltip--bottom');
    this.renderer.removeClass(tooltip, 'ui-tooltip--left');
    this.renderer.addClass(tooltip, `ui-tooltip--${this.placement()}`);
  }

  private positionTooltip(tooltip: HTMLElement): void {
    const hostRect = this.elementRef.nativeElement.getBoundingClientRect();
    let left = hostRect.left + (hostRect.width / 2);
    let top = hostRect.top - this.tooltipOffset;

    switch (this.placement()) {
      case 'bottom':
        top = hostRect.bottom + this.tooltipOffset;
        break;
      case 'left':
        left = hostRect.left - this.tooltipOffset;
        top = hostRect.top + (hostRect.height / 2);
        break;
      case 'right':
        left = hostRect.right + this.tooltipOffset;
        top = hostRect.top + (hostRect.height / 2);
        break;
      default:
        top = hostRect.top - this.tooltipOffset;
        break;
    }

    this.renderer.setStyle(tooltip, 'left', `${left}px`);
    this.renderer.setStyle(tooltip, 'top', `${top}px`);
  }
}
