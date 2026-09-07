import { fitCircleWithinViewport } from './personality-portrait-focus-editor.component';

describe('fitCircleWithinViewport', () => {
  it('continues growing a circle after it reaches one horizontal edge', () => {
    const result = fitCircleWithinViewport(40, 120, 90, 200, 267);

    expect(result).toEqual({ cx: 90, cy: 120, r: 90 });
  });

  it('stops growing only when the portrait width cannot contain a larger circle', () => {
    const result = fitCircleWithinViewport(100, 133.5, 180, 200, 267);

    expect(result).toEqual({ cx: 100, cy: 133.5, r: 100 });
  });
});
