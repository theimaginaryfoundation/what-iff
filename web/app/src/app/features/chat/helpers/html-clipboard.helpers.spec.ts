import { sanitizeClipboardContainer } from './html-clipboard.helpers';

describe('html-clipboard.helpers', () => {
    it('strips unsafe clipboard markup while preserving readable text', () => {
        const container = document.createElement('div');
        container.innerHTML =
            '<p onclick="alert(1)">Hi</p><svg onload="alert(1)"></svg><a href="javascript:alert(1)">Bad</a>';
        sanitizeClipboardContainer(container);

        expect(container.innerHTML).not.toContain('onclick');
        expect(container.innerHTML.toLowerCase()).not.toContain('<svg');
        expect(container.innerHTML).not.toContain('javascript:');
        expect(container.textContent).toContain('Hi');
        expect(container.textContent).toContain('Bad');
    });

    it('strips disallowed attributes such as class and style from copy html', () => {
        const container = document.createElement('div');
        container.innerHTML = '<ul class="x"><li style="background:#000">First</li><li>Second</li></ul>';
        sanitizeClipboardContainer(container);

        expect(container.innerHTML).toBe('<ul><li>First</li><li>Second</li></ul>');
    });

    it('converts task-list checkbox inputs to text markers', () => {
        const container = document.createElement('div');
        container.innerHTML = '<ul><li><input type="checkbox" checked> Done</li><li><input type="checkbox"> Todo</li></ul>';
        sanitizeClipboardContainer(container);

        expect(container.innerHTML).toContain('[x]');
        expect(container.innerHTML).toContain('[ ]');
    });
});
