import { LEARNING_GUIDES, LEARNING_OBJECTIVES, MAIN_BRANCHES } from './constellation.data';
import { NODE_POSITIONS, ORIGIN } from './constellation-layout';
import { completedObjectives, learningRank, objectiveReady, parseLearningProgress } from './constellation-progress';
import type { LearningProgress } from './constellation.model';

const completion = { note: 'I checked the result against the objective.', completedAt: '2026-11-20T12:00:00Z' };
const progressFor = (ids: string[]): LearningProgress => Object.fromEntries(ids.map(id => [id, completion]));

describe('constellation curriculum and progression', () => {
  it('places Create Personality at the epicenter and gives every task valid prerequisites and named guides', () => {
    const ids = new Set(LEARNING_OBJECTIVES.map(node => node.id));
    const guides = new Set(LEARNING_GUIDES.map(guide => guide.slug));
    expect(ids.size).toBe(46);
    expect(guides.size).toBe(19);
    expect(NODE_POSITIONS['S1']).toEqual(ORIGIN);
    for (const node of LEARNING_OBJECTIVES) {
      expect(NODE_POSITIONS[node.id]).toBeDefined();
      expect(node.guides.length).toBeGreaterThan(0);
      expect(node.guides.every(guide => guides.has(guide))).toBe(true);
      expect(node.requires.every(id => ids.has(id))).toBe(true);
    }
    expect(completedObjectives(progressFor([...ids])).size).toBe(ids.size);
  });

  it('opens six learning branches after S1 while retaining the full foundation for Builder', () => {
    const completed = completedObjectives(progressFor(['S1']));
    for (const branch of MAIN_BRANCHES) {
      expect(
        objectiveReady(
          LEARNING_OBJECTIVES.find(node => node.branch === branch)!,
          completed,
        ),
      ).toBe(true);
    }
    expect(learningRank(completed)).toBe('Noob');
  });

  it('rejects out-of-order stored completions, including a forged Builder award', () => {
    expect([...completedObjectives(progressFor(['S2', 'C2', 'B1', 'B2', 'B3']))]).toEqual([]);
  });

  it('requires two distinct completed branches and the full foundation before the capstone', () => {
    const foundation = ['S1', 'S2', 'S3', 'S4', 'S5', 'S6'];
    const continuity = ['C1', 'C2', 'C3', 'C4'];
    const personality = ['P1', 'P2', 'P3', 'P4'];
    const capstone = LEARNING_OBJECTIVES.find(node => node.id === 'B1')!;
    const one = completedObjectives(progressFor([...foundation, ...continuity]));
    expect(objectiveReady(capstone, one)).toBe(false);
    expect(learningRank(one)).toBe('Maker');
    const two = completedObjectives(progressFor([...foundation, ...continuity, ...personality]));
    expect(objectiveReady(capstone, two)).toBe(true);
    expect(learningRank(two)).toBe('Builder in training');
    expect(learningRank(completedObjectives(progressFor([...foundation, ...continuity, ...personality, 'B1', 'B2', 'B3'])))).toBe(
      'Builder',
    );
  });

  it('ignores malformed, unknown, oversized and unsubstantiated saved records', () => {
    for (const raw of ['{', 'null', '[]', '42']) expect(parseLearningProgress(raw)).toEqual({});
    expect(parseLearningProgress(JSON.stringify({ S1: { ...completion, note: 'done' }, unknown: completion }))).toEqual({});
    expect(parseLearningProgress(JSON.stringify({ S1: { ...completion, note: 'x'.repeat(2001) } }))).toEqual({});
    expect(parseLearningProgress(JSON.stringify({ S1: completion }))).toEqual({ S1: completion });
  });
});
