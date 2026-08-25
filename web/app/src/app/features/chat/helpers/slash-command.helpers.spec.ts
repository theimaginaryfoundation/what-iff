import { matchCommands, parseSlash, resolveSlashCommand, SlashCommand } from './slash-command.helpers';

describe('slash-command helpers', () => {
    describe('parseSlash', () => {
        it('parses a command and args', () => {
            expect(parseSlash('/ritual morning focus')).toEqual({
                command: 'ritual',
                args: ['morning', 'focus'],
            });
        });

        it('returns an empty command for a bare slash', () => {
            expect(parseSlash('/')).toEqual({ command: '', args: [] });
        });

        it('returns null for non-slash input', () => {
            expect(parseSlash('hello')).toEqual({ command: null, args: [] });
        });
    });

    describe('matchCommands', () => {
        const commands: SlashCommand[] = [
            { id: 'attach', label: 'Attach file', keywords: ['upload'] },
            { id: 'ritual', label: 'Run ritual', description: 'Start a saved ritual' },
            { id: 'gallery', label: 'Gallery image', keywords: ['photo', 'image'] },
        ];

        it('matches labels, descriptions and keywords', () => {
            expect(matchCommands('/rit', commands).map(c => c.id)).toEqual(['ritual']);
            expect(matchCommands('upload', commands).map(c => c.id)).toEqual(['attach']);
            expect(matchCommands('photo', commands).map(c => c.id)).toEqual(['gallery']);
        });

        it('returns commands ranked by match strength', () => {
            expect(matchCommands('gal', commands).map(c => c.id)[0]).toBe('gallery');
        });

        it('returns no matches for unrelated queries', () => {
            expect(matchCommands('xyz', commands)).toEqual([]);
        });

        it('does not append keywords onto the returned description', () => {
            const modeCommands: SlashCommand[] = [
                {
                    id: 'mode',
                    label: 'Mode',
                    description: 'Set the generation mode',
                    keywords: ['emotion', 'mood'],
                },
            ];
            const [match] = matchCommands('mode', modeCommands);
            expect(match.description).toBe('Set the generation mode');
        });
    });

    describe('resolveSlashCommand', () => {
        const commands: SlashCommand[] = [
            { id: 'emoji', label: 'Emoji', keywords: ['reaction'] },
            { id: 'skill', label: 'Skill', keywords: ['ritual', 'skills'] },
        ];

        it('resolves by id and keyword alias', () => {
            expect(resolveSlashCommand('emoji', commands)?.id).toBe('emoji');
            expect(resolveSlashCommand('skills', commands)?.id).toBe('skill');
        });

        it('returns null for unknown tokens', () => {
            expect(resolveSlashCommand('unknown', commands)).toBeNull();
        });
    });
});
