import { createUniver, defaultTheme, LocaleType, merge } from '@univerjs/presets';
import { UniverSheetsCorePreset } from '@univerjs/preset-sheets-core';
import sheetsCoreEnUS from '@univerjs/preset-sheets-core/locales/en-US';
import '@univerjs/preset-sheets-core/lib/index.css';
const { univerAPI } = createUniver({ locale: LocaleType.EN_US, locales: { [LocaleType.EN_US]: merge({}, sheetsCoreEnUS) }, theme: defaultTheme, presets: [UniverSheetsCorePreset({ container: 'app' })] });
window.univerLoad = (data) => { window.wb = univerAPI.createWorkbook(data); return true; };
window.univerRead = (sheet, a1) => window.wb.getSheetByName(sheet).getRange(a1).getValue();
window.univerSet = (sheet, a1, v) => window.wb.getSheetByName(sheet).getRange(a1).setValue(v);
window.univerSnapshot = () => window.wb.save();
