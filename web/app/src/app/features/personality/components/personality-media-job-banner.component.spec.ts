import { mediaJobBannerCopy } from './personality-media-job-banner.component';

describe('mediaJobBannerCopy', () => {
  it('returns null for terminal jobs', () => {
    expect(
      mediaJobBannerCopy({
        job_id: 'j1',
        job_type: 'expression_grid',
        reference: 'p1',
        status: 'complete',
        personality_id: 'p1',
      }),
    ).toBeNull();
  });

  it('describes expression grid for the current personality', () => {
    const copy = mediaJobBannerCopy(
      {
        job_id: 'j1',
        job_type: 'expression_grid',
        reference: 'p1',
        status: 'processing',
        personality_id: 'p1',
        personality_name: 'Fox',
      },
      'p1',
    );
    expect(copy?.title).toContain('Fox');
    expect(copy?.hint).toContain('leave this page');
  });

  const candidateRun = {
    job_id: 'j1',
    job_type: 'expression_grid' as const,
    reference: 'p1',
    status: 'processing',
    personality_id: 'p1',
    personality_name: 'Fox',
    expression_mode: 'candidates' as const,
  };

  it('hides candidate runs on the owning personality page (the Generate modal shows them)', () => {
    expect(mediaJobBannerCopy(candidateRun, 'p1')).toBeNull();
  });

  it('shows candidate runs elsewhere without calling them default expressions', () => {
    const global = mediaJobBannerCopy(candidateRun);
    expect(global?.title).toBe('Generating expressions for Fox…');

    const other = mediaJobBannerCopy(candidateRun, 'p2');
    expect(other?.title).toContain('Fox');
    expect(other?.title).not.toContain('default');
    expect(other?.hint).toContain('Only one image job');
  });

  it('keeps default-grid copy for default runs', () => {
    const copy = mediaJobBannerCopy({ ...candidateRun, expression_mode: 'default' }, 'p1');
    expect(copy?.title).toContain('default expressions');
  });

  it('describes portrait jobs', () => {
    expect(mediaJobBannerCopy({ ...candidateRun, job_type: 'personality_portrait' })?.title).toBe('Generating personality portrait…');
  });

  it('describes default runs for another personality with the one-job hint', () => {
    const copy = mediaJobBannerCopy({ ...candidateRun, expression_mode: 'default', personality_name: '  ' }, 'p2');
    expect(copy?.title).toBe('Generating expressions for this personality');
    expect(copy?.hint).toContain('Only one image job');
  });
});
