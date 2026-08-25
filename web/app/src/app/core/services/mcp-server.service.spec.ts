import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { environment } from '@environments/environment';
import { MCPServerService } from './mcp-server.service';

describe('MCPServerService', () => {
    let service: MCPServerService;
    let httpMock: HttpTestingController;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                MCPServerService
            ]
        });

        service = TestBed.inject(MCPServerService);
        httpMock = TestBed.inject(HttpTestingController);
    });

    afterEach(() => {
        httpMock.verify();
    });

    it('should list MCP servers with search and pagination', () => {
        service.listMCPServers(2, 15, { search: 'stripe' }).subscribe();

        const req = httpMock.expectOne((r) => r.url === `${environment.apiUrl}/mcp-servers` &&
            r.params.get('page') === '2' &&
            r.params.get('limit') === '15' &&
            r.params.get('search') === 'stripe');
        expect(req.request.method).toBe('GET');
        req.flush({ results: [], total_count: 0, page: 2 });
    });

    it('should list available MCP servers for a chat', () => {
        service.listAvailableForChat('chat-1', 1, 10, { search: 'docs' }).subscribe();

        const req = httpMock.expectOne((r) => r.url === `${environment.apiUrl}/chat/chat-1/available-mcp-servers` &&
            r.params.get('search') === 'docs');
        expect(req.request.method).toBe('GET');
        req.flush({ results: [], total_count: 0, page: 1 });
    });

    it('should add and remove MCP server from chat', () => {
        service.addToChat('chat-1', 'mcp-1').subscribe();
        const addReq = httpMock.expectOne(`${environment.apiUrl}/chat/chat-1/mcp-servers/mcp-1`);
        expect(addReq.request.method).toBe('POST');
        addReq.flush({});

        service.removeFromChat('chat-1', 'mcp-1').subscribe();
        const removeReq = httpMock.expectOne(`${environment.apiUrl}/chat/chat-1/mcp-servers/mcp-1`);
        expect(removeReq.request.method).toBe('DELETE');
        removeReq.flush({});
    });
});

