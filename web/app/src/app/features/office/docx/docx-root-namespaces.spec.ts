import { rootNamespaces } from './docx-export';

describe('rootNamespaces', () => {
  it('keeps the original root declarations and mc:Ignorable alongside ours', () => {
    const original =
      '<w:document xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape" ' +
      'xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" mc:Ignorable="w14 wp14"><w:body/></w:document>';
    const root = rootNamespaces(original);
    expect(root).toContain('xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"');
    expect(root).toContain('mc:Ignorable="w14 wp14"');
    expect(root).toContain('xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"');
    expect(root).toContain('xmlns:mc=');
  });

  it('is just ours without an original', () => {
    expect(rootNamespaces(null)).not.toContain('Ignorable');
  });
});
