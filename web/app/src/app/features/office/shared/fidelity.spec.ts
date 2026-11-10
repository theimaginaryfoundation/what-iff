import { FidelityCollector, describeLosses } from './fidelity';

describe('FidelityCollector', () => {
  it('counts repeats per feature and place, and describes losses', () => {
    const c = new FidelityCollector();
    expect(c.hasLosses).toBe(false);
    c.drop('Charts', 'Sales');
    c.drop('Charts', 'Sales');
    c.drop('Pivot tables');
    c.warn('Fonts', 'Sales');
    expect(c.hasLosses).toBe(true);
    const report = c.report();
    expect(report.dropped).toEqual([
      { feature: 'Charts', where: 'Sales', count: 2 },
      { feature: 'Pivot tables', where: undefined, count: 1 },
    ]);
    expect(report.warnings).toEqual([{ feature: 'Fonts', where: 'Sales', count: 1 }]);
    expect(describeLosses(report)).toEqual(['Charts (Sales, 2)', 'Pivot tables']);
  });
});
