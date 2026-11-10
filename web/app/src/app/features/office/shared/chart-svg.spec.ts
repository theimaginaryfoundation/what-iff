import { ChartData, chartSvg, chartSvgDataUrl } from './chart-svg';

const series = [
  { name: 'North', labels: ['Q1', 'Q2', 'Q3'], values: [3, 5, 2] },
  { name: 'South', labels: ['Q1', 'Q2', 'Q3'], values: [1, 4, 6] },
];
const opts = { width: 400, height: 300 };

function chart(type: ChartData['type'], extra: Partial<ChartData> = {}): ChartData {
  return { type, series, colors: ['#112233', '#445566'], ...extra };
}

describe('chartSvg', () => {
  it.each(['bar', 'line', 'pie', 'doughnut', 'area', 'scatter', 'radar', 'bubble'] as const)(
    'draws a %s chart in the requested size and in well-formed XML',
    type => {
      const svg = chartSvg(chart(type), opts);
      const doc = new DOMParser().parseFromString(svg, 'image/svg+xml');
      expect(doc.querySelector('parsererror')).toBeNull();
      expect(doc.documentElement.getAttribute('width')).toBe('400');
      expect(doc.documentElement.children.length).toBeGreaterThan(1);
    },
  );

  it('uses the chart colors for its series', () => {
    const svg = chartSvg(chart('bar'), opts);
    expect(svg).toContain('#112233');
    expect(svg).toContain('#445566');
  });

  it('escapes text taken from the workbook', () => {
    const svg = chartSvg(chart('bar', { title: '<script>a&b</script>' }), opts);
    expect(svg).not.toContain('<script>');
    expect(svg).toContain('&lt;script&gt;a&amp;b');
  });

  it('survives a chart with no data', () => {
    const svg = chartSvg({ type: 'bar', series: [], colors: [] }, opts);
    expect(new DOMParser().parseFromString(svg, 'image/svg+xml').querySelector('parsererror')).toBeNull();
  });

  it('wraps the drawing in a data URL', () => {
    expect(chartSvgDataUrl(chart('line'), opts)).toMatch(/^data:image\/svg\+xml/);
  });
});
