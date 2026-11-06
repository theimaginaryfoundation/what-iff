import { renderAsync } from 'docx-preview';
window.renderDocx = async (url) => {
  const buf = await (await fetch(url)).arrayBuffer();
  await renderAsync(buf, document.getElementById('app'), undefined, { inWrapper: true, renderHeaders: true, renderFooters: true, renderFootnotes: true, breakPages: true });
  return document.getElementById('app').innerText.length;
};
