/// <reference lib="webworker" />
/**
 * The formula engine for Univer Sheets, off the main thread (ADR 0x043, office suite). The
 * sheets preset on the main thread connects to it over Univer's RPC channel and leaves
 * calculation to it; the sheets editor creates one per instance and terminates it on dispose.
 */
import { UniverSheetsCoreWorkerPreset } from '@univerjs/preset-sheets-core/worker';
import { createUniver } from '@univerjs/presets';

createUniver({ presets: [UniverSheetsCoreWorkerPreset()] });
