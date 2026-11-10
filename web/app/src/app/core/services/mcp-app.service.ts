import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable, Subject } from 'rxjs';

import { environment } from '../../../environments/environment';

/** A tool call's MCP App view, as `GET /chat/{chatId}/tool-calls/{toolCallId}/app` returns it. */
export interface McpAppView {
  tool_call_id: string;
  server_name: string;
  tool_name: string;
  resource_uri: string;
  /** The view's document, its Content-Security-Policy already at the top. */
  html: string;
  csp: string;
  prefers_border?: boolean;
  tool_input: unknown;
  tool_result?: unknown;
}

/**
 * MCP Apps (ADR 0x042): loads a tool call's view and runs the tools the view calls on its own
 * connector. Also carries text a view asks to send as the user's message (ui/message) to the chat
 * page, which puts it in the composer for the person to send.
 */
@Injectable({ providedIn: 'root' })
export class McpAppService {
  private readonly http = inject(HttpClient);
  private readonly apiUrl = `${environment.apiUrl}/chat`;
  private readonly composerText = new Subject<string>();
  /** Text an app asked to send as the user's message. */
  readonly composerText$: Observable<string> = this.composerText.asObservable();

  getView(chatId: string, toolCallId: string): Observable<McpAppView> {
    return this.http.get<McpAppView>(`${this.apiUrl}/${chatId}/tool-calls/${toolCallId}/app`);
  }

  callTool(chatId: string, toolCallId: string, name: string, args: unknown): Observable<unknown> {
    return this.http.post(`${this.apiUrl}/${chatId}/tool-calls/${toolCallId}/app/tools/call`, { name, arguments: args ?? {} });
  }

  proposeMessage(text: string): void {
    const trimmed = text.trim();
    if (trimmed) this.composerText.next(trimmed);
  }
}
