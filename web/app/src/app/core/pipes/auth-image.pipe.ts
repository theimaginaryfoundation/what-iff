import { Pipe, PipeTransform, inject, OnDestroy } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable, of } from 'rxjs';
import { catchError, map, shareReplay } from 'rxjs/operators';
import { environment } from '../../../environments/environment';

interface CacheEntry {
  observable: Observable<string | null>;
  objectUrl?: string;
}

/**
 * AuthImagePipe — fetches an image URL via HttpClient (so the auth interceptor
 * attaches the Bearer token) and returns a blob object URL safe to use in <img src>.
 *
 * Usage:  <img [src]="imageUrl | authImage | async" />
 *
 * Results are memoized per URL so each image is fetched at most once per pipe
 * instance. Object URLs are revoked on pipe destruction to avoid memory leaks.
 */
@Pipe({ name: 'authImage', standalone: true, pure: true })
export class AuthImagePipe implements PipeTransform, OnDestroy {
  private http = inject(HttpClient);
  private cache = new Map<string, CacheEntry>();

  transform(url: string | null | undefined): Observable<string | null> {
    if (!url) return of(null);
    const resolvedUrl = resolveImageUrl(url);

    const cached = this.cache.get(resolvedUrl);
    if (cached) return cached.observable;

    const entry: CacheEntry = { observable: of(null) };
    entry.observable = this.http
      .get(resolvedUrl, { responseType: 'blob' })
      .pipe(
        map((blob): string | null => {
          const objectUrl = URL.createObjectURL(blob);
          entry.objectUrl = objectUrl;
          return objectUrl;
        }),
        catchError(() => of<string | null>(null)),
        shareReplay(1)
      );

    this.cache.set(resolvedUrl, entry);
    return entry.observable;
  }

  ngOnDestroy(): void {
    this.cache.forEach((entry) => {
      if (entry.objectUrl) URL.revokeObjectURL(entry.objectUrl);
    });
    this.cache.clear();
  }
}

function resolveImageUrl(url: string): string {
  if (!url.startsWith('/api/')) {
    return url;
  }

  const apiBase = environment.apiUrl.replace(/\/$/, '');
  const apiOrigin = apiBase.endsWith('/api') ? apiBase.slice(0, -4) : apiBase;
  return `${apiOrigin}${url}`;
}
