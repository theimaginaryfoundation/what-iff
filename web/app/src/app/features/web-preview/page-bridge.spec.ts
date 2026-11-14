import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';

import { PAGE_BRIDGE_SCRIPT, PageBridgeEvent, readBridgeEvent } from './page-bridge';

/**
 * The bridge script run in this test's document, as in a page: it talks to `window.parent`, which
 * is this window here, so what it posts arrives as messages to it, and what the app sends is a
 * message from it.
 */
describe('the page bridge script', () => {
  const posted: PageBridgeEvent[] = [];
  const onMessage = (event: MessageEvent) => {
    const data = event.data as { wiBridge?: unknown } | null;
    const e = data?.wiBridge ? readBridgeEvent(data.wiBridge) : null;
    if (e) posted.push(e);
  };
  const app = (message: unknown) => window.dispatchEvent(new MessageEvent('message', { data: { wiApp: message }, source: window }));
  const frame = () => new Promise(resolve => requestAnimationFrame(() => setTimeout(resolve, 0)));

  beforeAll(() => {
    window.addEventListener('message', onMessage);
    document.body.innerHTML = `
      <ul id="list">
        <li data-wi-src="f:10-14">One</li>
        <li data-wi-src="f:10-14">Two</li>
        <li data-wi-src="f:10-14">Three</li>
      </ul>
      <h1 id="title" data-wi-src="f:0-15">Hello</h1>
      <button id="menu">Menu</button>`;
    new Function(PAGE_BRIDGE_SCRIPT)();
  });
  afterAll(async () => {
    // No marks left to follow the document as the environment tears it down.
    app({ type: 'marks', marks: [] });
    await frame();
    window.removeEventListener('message', onMessage);
  });

  it('says it is ready, and where (null for a page shown on its own)', async () => {
    await vi.waitFor(() => expect(posted.find(e => e.type === 'ready')).toEqual({ type: 'ready', path: null }));
  });

  it('marks, among elements that share a source range (the rows of one tag), the one the thread is on', async () => {
    app({
      type: 'marks',
      marks: [{ id: 't2', state: 'open', src: 'f:10-14', selector: '#list > li:nth-of-type(2)', fingerprint: { tag: 'li', text: 'Two' } }],
    });
    await frame();
    const items = document.querySelectorAll('li');
    posted.length = 0;
    items[0]!.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
    items[1]!.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
    await frame();
    expect(posted).toEqual([{ type: 'menu', id: 't2', x: 0, y: 0 }]);
  });

  it('keeps a thread on an element its id names, though the text it was on has changed', async () => {
    app({ type: 'marks', marks: [{ id: 'h', state: 'open', selector: '#title', fingerprint: { tag: 'h1', text: 'Goodbye' } }] });
    await frame();
    posted.length = 0;
    document.getElementById('title')!.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
    await frame();
    expect(posted).toEqual([{ type: 'menu', id: 'h', x: 0, y: 0 }]);
  });

  it('picks an element without the page’s own handlers seeing the press, and restores the cursor', async () => {
    document.documentElement.style.cursor = 'help';
    const pagePress = vi.fn();
    const button = document.getElementById('menu')!;
    button.addEventListener('mousedown', pagePress);
    button.addEventListener('pointerdown', pagePress);
    app({ type: 'pick' });
    expect(document.documentElement.style.cursor).toBe('crosshair');
    posted.length = 0;
    button.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true, cancelable: true }));
    button.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }));
    button.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    expect(pagePress).not.toHaveBeenCalled();
    await frame();
    expect(posted).toEqual([
      {
        type: 'picked',
        selector: '#menu',
        fingerprint: { tag: 'button', text: 'Menu', attrs: { id: 'menu' } },
        src: null,
        created: null,
        label: 'button#menu “Menu”',
      },
    ]);
    expect(document.documentElement.style.cursor).toBe('help');
  });

  it('records where a script made elements only for what it inserted, not their neighbours', async () => {
    const list = document.getElementById('list')!;
    list.insertAdjacentHTML('beforeend', '<li id="added">Four</li>');
    app({ type: 'pick' });
    posted.length = 0;
    document.getElementById('added')!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    await frame();
    // A page on its own has no project scripts to name: no call site, but the element is picked.
    expect(posted[0]).toMatchObject({ type: 'picked', selector: '#added', created: null });
  });
});
