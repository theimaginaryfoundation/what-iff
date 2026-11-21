/** A public link to one of the user's files, as its owner sees it (ADR 0x051). */
export interface ShareLink {
  /** The link's own part: the share site's `/s/<key>`. */
  key: string;
  file_id: string;
  file_name: string;
  /** The file's gallery folder; for a web app, the project's root. */
  folder?: string;
  file_type: string;
  /** The link shares the page's folder as a web app (an HTML file in a folder). */
  web_app: boolean;
  passcode_required: boolean;
  passcode?: string;
  /** The stored passcode could not be read (the server's key changed): make a new one. */
  passcode_unreadable?: boolean;
  created_at: string;
  updated_at: string;
}

/** What sharing a file shares, from its original (ADR 0x051 §1): a chat's copy of a page in a folder shares the folder. */
export interface ShareScope {
  file_name: string;
  web_app: boolean;
  /** The folder a web app shares. */
  folder?: string;
}

/** A file's share state: what sharing it shares, and its link (null when it is not shared). */
export interface ShareState {
  scope: ShareScope;
  link: ShareLink | null;
}
