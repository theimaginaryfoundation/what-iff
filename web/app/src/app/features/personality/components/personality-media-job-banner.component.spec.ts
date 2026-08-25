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
});
