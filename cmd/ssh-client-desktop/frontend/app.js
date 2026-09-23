import {
  GetState,
  SetLaunchAtLogin,
  SetTheme,
  CreateGroup,
  RenameGroup,
  DeleteGroup,
  CreateProfile,
  UpdateProfile,
  DeleteProfile,
  ClearSavedCredential,
  ConnectProfile,
  QuickConnect,
  WriteSession,
  ResizeSession,
  RetrySession,
  DisconnectSession,
  CloseSession,
  ResolveHostKey,
  ChoosePrivateKey,
  PreviewSSHConfig,
  ChooseSSHConfig,
  ImportSSHConfig,
  RecordCommandHistory,
  ClearCommandHistory
} from "./wailsjs/go/main/App.js";
import { ClipboardGetText, ClipboardSetText, EventsOn } from "./wailsjs/runtime/runtime.js";

const $ = (selector) => document.querySelector(selector);
const BUILTIN_THEME_PACKS = [
  { name: "aurora", display_name: "极光", description: "蓝紫冷色，适合作为通用默认主题" },
  { name: "ocean", display_name: "海洋", description: "蓝青色调，清爽明亮" },
  { name: "forest", display_name: "森林", description: "绿色系，柔和自然" },
  { name: "sunset", display_name: "落日", description: "橙粉暖色，更有生活感" }
];

const SESSION_HISTORY_KEY = "ssh-client.session-history.v1";
const SESSION_HISTORY_LIMIT = 50;
const SESSION_HISTORY_TEXT_LIMIT = 200000;

const state = {
  settings: { theme: { mode: "dark", variant: "aurora" }, groups: [], profiles: [] },
  sessions: [],
  themePacks: [...BUILTIN_THEME_PACKS],
  themeCatalog: { source: "builtin", stale: false, last_error: "" },
  selectedProfileId: "",
  activeSessionId: "",
  nav: "connections",
  terminals: new Map(),
  decoders: new Map(),
  pendingProfile: null,
  pendingHostKey: null,
  pendingQuickSaves: new Map(),
  importPreview: null,
  lastAuthPromptSessionId: "",
  launchAtLogin: false,
  launchAtLoginSupported: false,
  dataDir: "",
  search: "",
  sidebarCollapsed: false,
  terminalContextSelection: "",
  history: [],
  commandHistory: [],
  promptPrefixes: new Map()
};

let toastTimer = 0;
let commandSelection = 0;
let visibleCommandItems = [];
let terminalImeComposing = false;
let terminalImeSuppressInput = false;
let terminalFocusInside = false;


const TERMINAL_STYLE_CACHE_LIMIT = 4096;
const TERMINAL_OSC_SEQUENCE_LIMIT = 4096;
const TERMINAL_CURSOR_MARK = "\u0000cursor";

const DEC_SPECIAL_GRAPHICS = {
  "\`": "◆", "a": "▒", "b": "␉", "c": "␌", "d": "␍", "e": "␊",
  "f": "°", "g": "±", "h": "␤", "i": "␋", "j": "┘", "k": "┐",
  "l": "┌", "m": "└", "n": "┼", "o": "⎺", "p": "⎻", "q": "─",
  "r": "⎼", "s": "⎽", "t": "├", "u": "┤", "v": "┴", "w": "┬",
  "x": "│", "y": "≤", "z": "≥", "{": "π", "|": "≠", "}": "£", "~": "·"
};

function terminalCellWidth(ch) {
  if (!ch) return 0;
  const cp = ch.codePointAt(0);
  if (
    cp === 0x200d ||
    (cp >= 0xfe00 && cp <= 0xfe0f) ||
    (cp >= 0xe0100 && cp <= 0xe01ef) ||
    (cp >= 0x1f3fb && cp <= 0x1f3ff) ||
    (cp >= 0x0300 && cp <= 0x036f) ||
    (cp >= 0x1ab0 && cp <= 0x1aff) ||
    (cp >= 0x1dc0 && cp <= 0x1dff) ||
    (cp >= 0x20d0 && cp <= 0x20ff) ||
    (cp >= 0xfe20 && cp <= 0xfe2f)
  ) return 0;
  if (
    cp >= 0x1100 && (
      cp <= 0x115f ||
      cp === 0x2329 || cp === 0x232a ||
      (cp >= 0x2e80 && cp <= 0xa4cf && cp !== 0x303f) ||
      (cp >= 0xac00 && cp <= 0xd7a3) ||
      (cp >= 0xf900 && cp <= 0xfaff) ||
      (cp >= 0xfe10 && cp <= 0xfe19) ||
      (cp >= 0xfe30 && cp <= 0xfe6f) ||
      (cp >= 0xff00 && cp <= 0xff60) ||
      (cp >= 0xffe0 && cp <= 0xffe6) ||
      (cp >= 0x1f000 && cp <= 0x1faff) ||
      (cp >= 0x20000 && cp <= 0x3fffd)
    )
  ) return 2;
  return 1;
}

function terminalDefaultStyle() {
  return { fg: "", bg: "", bold: false, dim: false, italic: false, underline: false, inverse: false, hidden: false, strike: false };
}

function terminalStyleKey(style) {
  return [style.fg, style.bg, style.bold ? 1 : 0, style.dim ? 1 : 0, style.italic ? 1 : 0,
    style.underline ? 1 : 0, style.inverse ? 1 : 0, style.hidden ? 1 : 0, style.strike ? 1 : 0].join("|");
}

function terminalAnsi16Color(index, bright = false) {
  const normal = ["#000000","#cd0000","#00cd00","#cdcd00","#0000ee","#cd00cd","#00cdcd","#e5e5e5"];
  const hi = ["#7f7f7f","#ff0000","#00ff00","#ffff00","#5c5cff","#ff00ff","#00ffff","#ffffff"];
  return (bright ? hi : normal)[Math.max(0, Math.min(7, index))];
}

function terminalXterm256Color(index) {
  index = Math.max(0, Math.min(255, Number(index) || 0));
  if (index < 8) return terminalAnsi16Color(index, false);
  if (index < 16) return terminalAnsi16Color(index - 8, true);
  if (index >= 232) {
    const level = 8 + (index - 232) * 10;
    return "rgb(" + level + "," + level + "," + level + ")";
  }
  const n = index - 16;
  const r = Math.floor(n / 36);
  const g = Math.floor((n % 36) / 6);
  const b = n % 6;
  const level = (v) => v === 0 ? 0 : 55 + v * 40;
  return "rgb(" + level(r) + "," + level(g) + "," + level(b) + ")";
}

class TerminalBuffer {
  constructor(cols = 100, rows = 30, scrollbackLimit = 10000, options = {}) {
    this.cols = Math.max(20, cols);
    this.rows = Math.max(5, rows);
    this.scrollbackLimit = Math.max(100, scrollbackLimit || 10000);
    this.styleById = [terminalDefaultStyle()];
    this.styleIdByKey = new Map([[terminalStyleKey(this.styleById[0]), 0]]);
    this.currentStyle = terminalDefaultStyle();
    this.screen = Array.from({ length: this.rows }, () => this.blankRow());
    this.styleScreen = Array.from({ length: this.rows }, () => this.blankStyleRow());
    this.scrollback = [];
    this.scrollbackStyles = [];
    this.row = 0;
    this.col = 0;
    this.savedRow = 0;
    this.savedCol = 0;
    this.savedState = null;
    this.mode = "normal";
    this.sequence = "";
    this.oscSequence = "";
    this.charsetTarget = 0;
    this.charsets = ["B", "B", "B", "B"];
    this.activeCharset = 0;
    this.bracketedPaste = false;
    this.alternateScreen = false;
    this.applicationCursorKeys = false;
    this.applicationKeypad = false;
    this.cursorVisible = true;
    this.cursorStyle = options.cursorStyle || "bar";
    this.defaultCursorStyle = options.cursorStyle || "bar";
    this.colorScheme = options.colorScheme || "midnight";
    this.mainScreenState = null;
    this.scrollTop = 0;
    this.scrollBottom = this.rows - 1;
    this.originMode = false;
    this.autowrap = true;
    this.wrapPending = false;
    this.insertMode = false;
    this.mouseTracking = 0;
    this.sgrMouse = false;
    this.focusReporting = false;
    this.lastPrintable = "";
    this.responses = [];
    this.tabStops = new Set();
    this.resetTabStops();
  }

  setConfig(config = {}) {
    if (Number.isFinite(Number(config.scrollback_lines))) {
      this.scrollbackLimit = Math.max(100, Math.min(100000, Number(config.scrollback_lines)));
      this.trimScrollback();
    }
    if (["bar","block","underline"].includes(config.cursor_style)) {
      const previousDefault = this.defaultCursorStyle;
      this.defaultCursorStyle = config.cursor_style;
      if (!this.cursorStyle || this.cursorStyle === previousDefault) {
        this.cursorStyle = config.cursor_style;
      }
    }
    if (["midnight","graphite","daylight"].includes(config.color_scheme)) this.colorScheme = config.color_scheme;
  }

  blankRow(cols = this.cols) {
    return Array.from({ length: cols }, () => " ");
  }

  blankStyleRow(cols = this.cols) {
    return Array.from({ length: cols }, () => 0);
  }

  resetTabStops() {
    this.tabStops.clear();
    for (let c = 8; c < this.cols; c += 8) this.tabStops.add(c);
  }

  styleId(style = this.currentStyle) {
    const key = terminalStyleKey(style);
    const found = this.styleIdByKey.get(key);
    if (found !== undefined) return found;
    if (this.styleById.length >= TERMINAL_STYLE_CACHE_LIMIT) return 0;
    const id = this.styleById.length;
    this.styleById.push({ ...style });
    this.styleIdByKey.set(key, id);
    return id;
  }

  styleFor(id) {
    return this.styleById[id] || this.styleById[0];
  }

  rowText(row) {
    return (row || []).join("").replace(/\s+$/, "");
  }

  currentBounds() {
    return this.originMode ? [this.scrollTop, this.scrollBottom] : [0, this.rows - 1];
  }

  homeCursor() {
    this.row = this.originMode ? this.scrollTop : 0;
    this.col = 0;
    this.wrapPending = false;
  }

  saveCursor() {
    this.savedRow = this.row;
    this.savedCol = this.col;
    this.savedState = {
      row: this.row, col: this.col, style: { ...this.currentStyle },
      originMode: this.originMode, autowrap: this.autowrap,
      activeCharset: this.activeCharset, charsets: this.charsets.slice(),
      cursorVisible: this.cursorVisible, cursorStyle: this.cursorStyle
    };
  }

  restoreCursor() {
    const saved = this.savedState;
    this.row = Math.max(0, Math.min(saved ? saved.row : this.savedRow, this.rows - 1));
    this.col = Math.max(0, Math.min(saved ? saved.col : this.savedCol, this.cols - 1));
    if (saved) {
      this.currentStyle = { ...saved.style };
      this.originMode = saved.originMode;
      this.autowrap = saved.autowrap;
      this.activeCharset = saved.activeCharset;
      this.charsets = saved.charsets.slice();
      this.cursorVisible = saved.cursorVisible;
      this.cursorStyle = saved.cursorStyle;
    }
    this.wrapPending = false;
  }

  enterAlternateScreen(saveCursor = false) {
    if (this.alternateScreen) return;
    if (saveCursor) this.saveCursor();
    this.mainScreenState = {
      screen: this.screen.map((row) => row.slice()),
      styleScreen: this.styleScreen.map((row) => row.slice()),
      scrollback: this.scrollback.map((row) => row.slice()),
      scrollbackStyles: this.scrollbackStyles.map((row) => row.slice()),
      row: this.row, col: this.col, savedRow: this.savedRow, savedCol: this.savedCol,
      savedState: this.savedState ? { ...this.savedState, style: { ...this.savedState.style }, charsets: this.savedState.charsets.slice() } : null,
      scrollTop: this.scrollTop, scrollBottom: this.scrollBottom,
      originMode: this.originMode, autowrap: this.autowrap, insertMode: this.insertMode,
      wrapPending: this.wrapPending, cursorVisible: this.cursorVisible, cursorStyle: this.cursorStyle,
      charsets: this.charsets.slice(), activeCharset: this.activeCharset
    };
    this.alternateScreen = true;
    this.screen = Array.from({ length: this.rows }, () => this.blankRow());
    this.styleScreen = Array.from({ length: this.rows }, () => this.blankStyleRow());
    this.scrollback = [];
    this.scrollbackStyles = [];
    this.scrollTop = 0;
    this.scrollBottom = this.rows - 1;
    this.originMode = false;
    this.wrapPending = false;
    this.row = 0;
    this.col = 0;
  }

  exitAlternateScreen(restoreCursor = false) {
    if (!this.alternateScreen) {
      if (restoreCursor) this.restoreCursor();
      return;
    }
    const saved = this.mainScreenState;
    this.alternateScreen = false;
    this.mainScreenState = null;
    if (!saved) return;

    const resizeRow = (row, cols, blank) => row.length > cols ? row.slice(0, cols) :
      (row.length < cols ? row.concat(Array.from({ length: cols - row.length }, blank)) : row.slice());
    this.screen = saved.screen.map((row) => resizeRow(row, this.cols, () => " "));
    this.styleScreen = saved.styleScreen.map((row) => resizeRow(row, this.cols, () => 0));
    while (this.screen.length < this.rows) {
      this.screen.push(this.blankRow());
      this.styleScreen.push(this.blankStyleRow());
    }
    while (this.screen.length > this.rows) {
      this.screen.shift();
      this.styleScreen.shift();
    }
    this.scrollback = saved.scrollback.slice(-this.scrollbackLimit).map((row) => row.slice());
    this.scrollbackStyles = saved.scrollbackStyles.slice(-this.scrollbackLimit).map((row) => row.slice());
    this.row = Math.max(0, Math.min(saved.row, this.rows - 1));
    this.col = Math.max(0, Math.min(saved.col, this.cols - 1));
    this.savedRow = Math.max(0, Math.min(saved.savedRow, this.rows - 1));
    this.savedCol = Math.max(0, Math.min(saved.savedCol, this.cols - 1));
    this.savedState = saved.savedState;
    this.scrollTop = Math.max(0, Math.min(saved.scrollTop, this.rows - 1));
    this.scrollBottom = Math.max(this.scrollTop, Math.min(saved.scrollBottom, this.rows - 1));
    this.originMode = saved.originMode;
    this.autowrap = saved.autowrap;
    this.insertMode = saved.insertMode;
    this.wrapPending = saved.wrapPending;
    this.cursorVisible = saved.cursorVisible;
    this.cursorStyle = saved.cursorStyle;
    this.charsets = saved.charsets.slice();
    this.activeCharset = saved.activeCharset;
    if (restoreCursor) this.restoreCursor();
  }

  scrollRegionUp(count = 1) {
    const top = Math.max(0, Math.min(this.scrollTop, this.rows - 1));
    const bottom = Math.max(top, Math.min(this.scrollBottom, this.rows - 1));
    for (let i = 0; i < count; i++) {
      const removed = this.screen.splice(top, 1)[0];
      const removedStyles = this.styleScreen.splice(top, 1)[0];
      this.screen.splice(bottom, 0, this.blankRow());
      this.styleScreen.splice(bottom, 0, this.blankStyleRow());
      if (!this.alternateScreen && top === 0 && bottom === this.rows - 1 && removed) {
        this.scrollback.push(removed.slice());
        this.scrollbackStyles.push((removedStyles || this.blankStyleRow()).slice());
        this.trimScrollback();
      }
    }
  }

  scrollRegionDown(count = 1) {
    const top = Math.max(0, Math.min(this.scrollTop, this.rows - 1));
    const bottom = Math.max(top, Math.min(this.scrollBottom, this.rows - 1));
    for (let i = 0; i < count; i++) {
      this.screen.splice(bottom, 1);
      this.styleScreen.splice(bottom, 1);
      this.screen.splice(top, 0, this.blankRow());
      this.styleScreen.splice(top, 0, this.blankStyleRow());
    }
  }

  resize(cols, rows) {
    cols = Math.max(20, Math.floor(cols));
    rows = Math.max(5, Math.floor(rows));
    if (cols === this.cols && rows === this.rows) return;
    const oldRows = this.rows;
    const oldCols = this.cols;
    const oldScrollTop = this.scrollTop;
    const oldScrollBottom = this.scrollBottom;
    const fullRegion = oldScrollTop === 0 && oldScrollBottom === oldRows - 1;

    const fit = (row, blank) => {
      if (row.length > cols) return row.slice(0, cols);
      if (row.length < cols) return row.concat(Array.from({ length: cols - row.length }, blank));
      return row;
    };
    this.screen = this.screen.map((row) => fit(row, () => " "));
    this.styleScreen = this.styleScreen.map((row) => fit(row, () => 0));

    while (this.screen.length < rows) {
      this.screen.push(Array.from({ length: cols }, () => " "));
      this.styleScreen.push(Array.from({ length: cols }, () => 0));
    }
    while (this.screen.length > rows) {
      const removed = this.screen.shift();
      const removedStyles = this.styleScreen.shift();
      if (!this.alternateScreen) {
        this.scrollback.push(removed);
        this.scrollbackStyles.push(removedStyles);
      }
    }

    this.cols = cols;
    this.rows = rows;
    this.row = Math.max(0, Math.min(this.row, rows - 1));
    this.col = Math.max(0, Math.min(this.col, cols - 1));
    if (fullRegion) {
      this.scrollTop = 0;
      this.scrollBottom = rows - 1;
    } else {
      this.scrollTop = Math.max(0, Math.min(oldScrollTop, rows - 1));
      this.scrollBottom = Math.max(this.scrollTop, Math.min(oldScrollBottom, rows - 1));
    }
    const nextStops = new Set([...this.tabStops].filter((c) => c < cols));
    if (cols > oldCols) {
      for (let c = Math.max(8, Math.ceil((oldCols + 1) / 8) * 8); c < cols; c += 8) nextStops.add(c);
    }
    this.tabStops = nextStops;
    this.trimScrollback();
  }

  mapCharset(ch) {
    return this.charsets[this.activeCharset] === "0" ? (DEC_SPECIAL_GRAPHICS[ch] || ch) : ch;
  }

  appendCombining(ch) {
    let c = this.wrapPending ? this.col : this.col - 1;
    c = Math.max(0, Math.min(c, this.cols - 1));
    if (this.screen[this.row][c] === "" && c > 0) c--;
    if (this.screen[this.row][c] && this.screen[this.row][c] !== " ") {
      this.screen[this.row][c] += ch;
    }
  }

  clearRange(rowIndex, start, end) {
    if (rowIndex < 0 || rowIndex >= this.rows) return;
    start = Math.max(0, start);
    end = Math.min(this.cols - 1, end);
    const row = this.screen[rowIndex];
    const styles = this.styleScreen[rowIndex];
    if (row[start] === "" && start > 0) start--;
    if (end + 1 < this.cols && row[end + 1] === "") end++;
    for (let c = start; c <= end; c++) {
      row[c] = " ";
      styles[c] = 0;
    }
  }

  insertCells(count) {
    count = Math.max(1, Math.min(count, this.cols - this.col));
    const row = this.screen[this.row];
    const styles = this.styleScreen[this.row];
    row.splice(this.col, 0, ...Array.from({ length: count }, () => " "));
    styles.splice(this.col, 0, ...Array.from({ length: count }, () => 0));
    row.length = this.cols;
    styles.length = this.cols;
  }

  deleteCells(count) {
    count = Math.max(1, Math.min(count, this.cols - this.col));
    const row = this.screen[this.row];
    const styles = this.styleScreen[this.row];
    row.splice(this.col, count);
    styles.splice(this.col, count);
    while (row.length < this.cols) row.push(" ");
    while (styles.length < this.cols) styles.push(0);
  }

  writeChar(input) {
    const ch = this.mapCharset(input);
    const width = terminalCellWidth(ch);
    if (width === 0) {
      this.appendCombining(ch);
      return;
    }
    if (this.wrapPending) {
      if (this.autowrap) {
        this.col = 0;
        this.indexDown();
      }
      this.wrapPending = false;
    }
    if (width === 2 && this.col === this.cols - 1) {
      if (this.autowrap) {
        this.col = 0;
        this.indexDown();
      }
    }
    if (this.insertMode) this.insertCells(width);
    const styleId = this.styleId();
    this.screen[this.row][this.col] = ch;
    this.styleScreen[this.row][this.col] = styleId;
    if (width === 2 && this.col + 1 < this.cols) {
      this.screen[this.row][this.col + 1] = "";
      this.styleScreen[this.row][this.col + 1] = styleId;
    }
    this.lastPrintable = ch;
    const last = this.col + width - 1;
    if (last >= this.cols - 1) {
      this.col = this.cols - 1;
      this.wrapPending = this.autowrap;
    } else {
      this.col += width;
    }
  }

  backspace() {
    this.wrapPending = false;
    if (this.col > 0) this.col--;
    if (this.screen[this.row][this.col] === "" && this.col > 0) this.col--;
  }

  nextTab(count = 1) {
    this.wrapPending = false;
    for (let i = 0; i < count; i++) {
      const candidates = [...this.tabStops].filter((c) => c > this.col).sort((a,b) => a-b);
      this.col = candidates.length ? candidates[0] : this.cols - 1;
    }
  }

  previousTab(count = 1) {
    this.wrapPending = false;
    for (let i = 0; i < count; i++) {
      const candidates = [...this.tabStops].filter((c) => c < this.col).sort((a,b) => b-a);
      this.col = candidates.length ? candidates[0] : 0;
    }
  }

  reset() {
    this.screen = Array.from({ length: this.rows }, () => this.blankRow());
    this.styleScreen = Array.from({ length: this.rows }, () => this.blankStyleRow());
    this.scrollback = [];
    this.scrollbackStyles = [];
    this.row = 0; this.col = 0; this.savedRow = 0; this.savedCol = 0;
    this.savedState = null; this.scrollTop = 0; this.scrollBottom = this.rows - 1;
    this.bracketedPaste = false; this.applicationCursorKeys = false; this.applicationKeypad = false;
    this.cursorVisible = true; this.cursorStyle = this.defaultCursorStyle; this.originMode = false;
    this.autowrap = true; this.wrapPending = false; this.insertMode = false;
    this.mouseTracking = 0; this.sgrMouse = false; this.focusReporting = false;
    this.currentStyle = terminalDefaultStyle();
    this.charsets = ["B","B","B","B"]; this.activeCharset = 0;
    this.resetTabStops();
  }

  finishOSC() {
    const value = this.oscSequence;
    this.oscSequence = "";
    this.mode = "normal";
    const match = value.match(/^(10|11|12);\?$/);
    if (!match) return;
    const palette = {
      midnight: { "10": "d7dae0", "11": "10141c", "12": "d7dae0" },
      graphite: { "10": "e2e2e2", "11": "1b1b1b", "12": "ffffff" },
      daylight: { "10": "252525", "11": "f7f7f7", "12": "202020" }
    };
    const colors = palette[this.colorScheme] || palette.midnight;
    const hex = colors[match[1]];
    const rgb = hex.match(/../g).map((p) => p + p).join("/");
    this.responses.push("\u001b]" + match[1] + ";rgb:" + rgb + "\u001b\\");
  }

  feed(text) {
    this.responses = [];
    for (const input of text) {
      const ch = input;
      if (this.mode === "osc") {
        if (ch === "\u0007") {
          this.finishOSC();
        } else if (ch === "\u001b") {
          this.mode = "osc-esc";
        } else if (this.oscSequence.length < TERMINAL_OSC_SEQUENCE_LIMIT) {
          this.oscSequence += ch;
        } else {
          this.oscSequence = "";
          this.mode = "normal";
        }
        continue;
      }
      if (this.mode === "osc-esc") {
        if (ch === "\\") this.finishOSC();
        else {
          if (this.oscSequence.length < TERMINAL_OSC_SEQUENCE_LIMIT) this.oscSequence += "\u001b" + ch;
          this.mode = "osc";
        }
        continue;
      }
      if (this.mode === "charset") {
        this.charsets[this.charsetTarget] = ch;
        this.mode = "normal";
        continue;
      }
      if (this.mode === "esc") {
        if (ch === "[") {
          this.mode = "csi"; this.sequence = "";
        } else if (ch === "]") {
          this.mode = "osc"; this.oscSequence = "";
        } else if ("()*+".includes(ch)) {
          this.charsetTarget = {"(":0,")":1,"*":2,"+":3}[ch];
          this.mode = "charset";
        } else if (ch === "7") {
          this.saveCursor(); this.mode = "normal";
        } else if (ch === "8") {
          this.restoreCursor(); this.mode = "normal";
        } else if (ch === "D") {
          this.indexDown(); this.mode = "normal";
        } else if (ch === "E") {
          this.col = 0; this.indexDown(); this.mode = "normal";
        } else if (ch === "M") {
          this.reverseIndex(); this.mode = "normal";
        } else if (ch === "H") {
          this.tabStops.add(this.col); this.mode = "normal";
        } else if (ch === "=") {
          this.applicationKeypad = true; this.mode = "normal";
        } else if (ch === ">") {
          this.applicationKeypad = false; this.mode = "normal";
        } else if (ch === "n") {
          this.activeCharset = 2; this.mode = "normal";
        } else if (ch === "o") {
          this.activeCharset = 3; this.mode = "normal";
        } else if (ch === "Z") {
          this.responses.push("\u001b[?1;2c"); this.mode = "normal";
        } else if (ch === "c") {
          this.reset(); this.mode = "normal";
        } else {
          this.mode = "normal";
        }
        continue;
      }
      if (this.mode === "csi") {
        if (ch >= "@" && ch <= "~") {
          this.handleCSI(this.sequence, ch);
          this.sequence = "";
          this.mode = "normal";
        } else if (this.sequence.length < 256) {
          this.sequence += ch;
        } else {
          this.sequence = "";
          this.mode = "normal";
        }
        continue;
      }

      if (ch === "\u001b") {
        this.mode = "esc";
      } else if (ch === "\u000e") {
        this.activeCharset = 1;
      } else if (ch === "\u000f") {
        this.activeCharset = 0;
      } else if (ch === "\r") {
        this.col = 0; this.wrapPending = false;
      } else if (ch === "\n" || ch === "\v" || ch === "\f") {
        this.newline();
      } else if (ch === "\b") {
        this.backspace();
      } else if (ch === "\t") {
        this.nextTab(1);
      } else if (ch >= " ") {
        this.writeChar(ch);
      }
    }
    return this.responses.slice();
  }

  parseParams(raw) {
    const clean = raw.replace(/^[?>!]/, "").replace(/[ $'"*+]/g, "");
    if (clean === "") return [0];
    return clean.split(";").map((value) => Number(value || 0));
  }

  modeStatus(mode) {
    const on = {
      1: this.applicationCursorKeys, 6: this.originMode, 7: this.autowrap,
      25: this.cursorVisible, 1000: this.mouseTracking === 1000,
      1002: this.mouseTracking === 1002, 1003: this.mouseTracking === 1003,
      1004: this.focusReporting, 1006: this.sgrMouse, 1049: this.alternateScreen,
      2004: this.bracketedPaste
    };
    if (Object.prototype.hasOwnProperty.call(on, mode)) return on[mode] ? 1 : 2;
    return 0;
  }

  setPrivateMode(mode, enabled) {
    if (mode === 1) this.applicationCursorKeys = enabled;
    else if (mode === 6) { this.originMode = enabled; this.homeCursor(); }
    else if (mode === 7) { this.autowrap = enabled; if (!enabled) this.wrapPending = false; }
    else if (mode === 25) this.cursorVisible = enabled;
    else if (mode === 47 || mode === 1047) { if (enabled) this.enterAlternateScreen(false); else this.exitAlternateScreen(false); }
    else if (mode === 1048) { if (enabled) this.saveCursor(); else this.restoreCursor(); }
    else if (mode === 1049) { if (enabled) this.enterAlternateScreen(true); else this.exitAlternateScreen(true); }
    else if (mode === 1000 || mode === 1002 || mode === 1003) this.mouseTracking = enabled ? mode : (this.mouseTracking === mode ? 0 : this.mouseTracking);
    else if (mode === 1004) this.focusReporting = enabled;
    else if (mode === 1006) this.sgrMouse = enabled;
    else if (mode === 2004) this.bracketedPaste = enabled;
  }

  handleSGR(params) {
    if (!params.length) params = [0];
    for (let i = 0; i < params.length; i++) {
      const p = params[i] || 0;
      if (p === 0) this.currentStyle = terminalDefaultStyle();
      else if (p === 1) this.currentStyle.bold = true;
      else if (p === 2) this.currentStyle.dim = true;
      else if (p === 3) this.currentStyle.italic = true;
      else if (p === 4) this.currentStyle.underline = true;
      else if (p === 7) this.currentStyle.inverse = true;
      else if (p === 8) this.currentStyle.hidden = true;
      else if (p === 9) this.currentStyle.strike = true;
      else if (p === 22) { this.currentStyle.bold = false; this.currentStyle.dim = false; }
      else if (p === 23) this.currentStyle.italic = false;
      else if (p === 24) this.currentStyle.underline = false;
      else if (p === 27) this.currentStyle.inverse = false;
      else if (p === 28) this.currentStyle.hidden = false;
      else if (p === 29) this.currentStyle.strike = false;
      else if (p >= 30 && p <= 37) this.currentStyle.fg = terminalAnsi16Color(p - 30, false);
      else if (p >= 90 && p <= 97) this.currentStyle.fg = terminalAnsi16Color(p - 90, true);
      else if (p >= 40 && p <= 47) this.currentStyle.bg = terminalAnsi16Color(p - 40, false);
      else if (p >= 100 && p <= 107) this.currentStyle.bg = terminalAnsi16Color(p - 100, true);
      else if (p === 39) this.currentStyle.fg = "";
      else if (p === 49) this.currentStyle.bg = "";
      else if (p === 38 || p === 48) {
        const field = p === 38 ? "fg" : "bg";
        if (params[i + 1] === 5 && params.length > i + 2) {
          this.currentStyle[field] = terminalXterm256Color(params[i + 2]);
          i += 2;
        } else if (params[i + 1] === 2 && params.length > i + 4) {
          const r = Math.max(0, Math.min(255, params[i + 2] || 0));
          const g = Math.max(0, Math.min(255, params[i + 3] || 0));
          const b = Math.max(0, Math.min(255, params[i + 4] || 0));
          this.currentStyle[field] = "rgb(" + r + "," + g + "," + b + ")";
          i += 4;
        }
      }
    }
  }

  handleCSI(raw, final) {
    const privateMode = raw.startsWith("?");
    const secondary = raw.startsWith(">");
    const params = this.parseParams(raw);
    const first = params[0] || 0;

    if (privateMode && raw.includes("$") && final === "p") {
      const mode = Number((raw.match(/^\?(\d+)/) || [0,0])[1]);
      this.responses.push("\u001b[?" + mode + ";" + this.modeStatus(mode) + "$y");
      return;
    }

    if (privateMode && (final === "h" || final === "l")) {
      const enabled = final === "h";
      for (const p of params) this.setPrivateMode(p, enabled);
      return;
    }

    if (!privateMode && (final === "h" || final === "l")) {
      if (params.includes(4)) this.insertMode = final === "h";
      return;
    }

    const bounds = this.currentBounds();
    const clampRow = (value) => Math.max(bounds[0], Math.min(bounds[1], value));
    switch (final) {
      case "A": this.row = clampRow(this.row - (first || 1)); this.wrapPending = false; break;
      case "B": this.row = clampRow(this.row + (first || 1)); this.wrapPending = false; break;
      case "C": this.col = Math.min(this.cols - 1, this.col + (first || 1)); this.wrapPending = false; break;
      case "D": this.col = Math.max(0, this.col - (first || 1)); this.wrapPending = false; break;
      case "E": this.row = clampRow(this.row + (first || 1)); this.col = 0; this.wrapPending = false; break;
      case "F": this.row = clampRow(this.row - (first || 1)); this.col = 0; this.wrapPending = false; break;
      case "G":
      case "\`": this.col = Math.max(0, Math.min(this.cols - 1, (first || 1) - 1)); this.wrapPending = false; break;
      case "a": this.col = Math.min(this.cols - 1, this.col + (first || 1)); this.wrapPending = false; break;
      case "H":
      case "f": {
        const base = this.originMode ? this.scrollTop : 0;
        const r = base + (params[0] || 1) - 1;
        const c = (params[1] || 1) - 1;
        this.row = clampRow(r);
        this.col = Math.max(0, Math.min(this.cols - 1, c));
        this.wrapPending = false;
        break;
      }
      case "J":
        if (first === 0) {
          this.clearRange(this.row, this.col, this.cols - 1);
          for (let r = this.row + 1; r < this.rows; r++) this.clearRange(r, 0, this.cols - 1);
        } else if (first === 1) {
          for (let r = 0; r < this.row; r++) this.clearRange(r, 0, this.cols - 1);
          this.clearRange(this.row, 0, this.col);
        } else if (first === 2 || first === 3) {
          for (let r = 0; r < this.rows; r++) this.clearRange(r, 0, this.cols - 1);
          if (first === 3) { this.scrollback = []; this.scrollbackStyles = []; }
        }
        break;
      case "K":
        if (first === 1) this.clearRange(this.row, 0, this.col);
        else if (first === 2) this.clearRange(this.row, 0, this.cols - 1);
        else this.clearRange(this.row, this.col, this.cols - 1);
        break;
      case "P": this.deleteCells(first || 1); break;
      case "@": this.insertCells(first || 1); break;
      case "X": this.clearRange(this.row, this.col, Math.min(this.cols - 1, this.col + (first || 1) - 1)); break;
      case "L": {
        const count = Math.min(first || 1, this.scrollBottom - this.row + 1);
        if (this.row >= this.scrollTop && this.row <= this.scrollBottom) {
          for (let i = 0; i < count; i++) {
            this.screen.splice(this.row, 0, this.blankRow());
            this.styleScreen.splice(this.row, 0, this.blankStyleRow());
            this.screen.splice(this.scrollBottom + 1, 1);
            this.styleScreen.splice(this.scrollBottom + 1, 1);
          }
        }
        break;
      }
      case "M": {
        const count = Math.min(first || 1, this.scrollBottom - this.row + 1);
        if (this.row >= this.scrollTop && this.row <= this.scrollBottom) {
          for (let i = 0; i < count; i++) {
            this.screen.splice(this.row, 1);
            this.styleScreen.splice(this.row, 1);
            this.screen.splice(this.scrollBottom, 0, this.blankRow());
            this.styleScreen.splice(this.scrollBottom, 0, this.blankStyleRow());
          }
        }
        break;
      }
      case "S": this.scrollRegionUp(first || 1); break;
      case "T": this.scrollRegionDown(first || 1); break;
      case "d": {
        const base = this.originMode ? this.scrollTop : 0;
        this.row = clampRow(base + (first || 1) - 1); this.wrapPending = false; break;
      }
      case "e": this.row = clampRow(this.row + (first || 1)); this.wrapPending = false; break;
      case "r": {
        const top = Math.max(0, Math.min(this.rows - 1, (params[0] || 1) - 1));
        const bottom = Math.max(top, Math.min(this.rows - 1, (params[1] || this.rows) - 1));
        this.scrollTop = top; this.scrollBottom = bottom; this.homeCursor(); break;
      }
      case "s": this.saveCursor(); break;
      case "u": this.restoreCursor(); break;
      case "m": this.handleSGR(params); break;
      case "b": {
        const count = Math.max(1, Math.min(4096, first || 1));
        if (this.lastPrintable) for (let i = 0; i < count; i++) this.writeChar(this.lastPrintable);
        break;
      }
      case "I": this.nextTab(first || 1); break;
      case "Z": this.previousTab(first || 1); break;
      case "g":
        if (first === 3) this.tabStops.clear();
        else this.tabStops.delete(this.col);
        break;
      case "n":
        if (privateMode && first === 6) this.responses.push("\u001b[?" + (this.row + 1) + ";" + (this.col + 1) + "R");
        else if (first === 5) this.responses.push("\u001b[0n");
        else if (first === 6) this.responses.push("\u001b[" + (this.row + 1) + ";" + (this.col + 1) + "R");
        break;
      case "c":
        if (secondary) this.responses.push("\u001b[>0;0;0c");
        else this.responses.push("\u001b[?1;2c");
        break;
      case "q":
        if (raw.includes(" ")) {
          const value = Number((raw.match(/(\d+)/) || [0,0])[1]);
          if (value === 3 || value === 4) this.cursorStyle = "underline";
          else if (value === 5 || value === 6) this.cursorStyle = "bar";
          else this.cursorStyle = "block";
        }
        break;
      default: break;
    }
  }

  indexDown() {
    this.wrapPending = false;
    if (this.row === this.scrollBottom) { this.scrollRegionUp(1); return; }
    this.row = Math.min(this.rows - 1, this.row + 1);
  }

  reverseIndex() {
    this.wrapPending = false;
    if (this.row === this.scrollTop) { this.scrollRegionDown(1); return; }
    this.row = Math.max(0, this.row - 1);
  }

  newline() {
    this.wrapPending = false;
    this.indexDown();
  }

  trimScrollback() {
    if (this.scrollback.length > this.scrollbackLimit) {
      const drop = this.scrollback.length - this.scrollbackLimit;
      this.scrollback.splice(0, drop);
      this.scrollbackStyles.splice(0, drop);
    }
  }

  loadText(text) {
    const lines = String(text || "").replace(/\r/g, "").split("\n");
    this.screen = Array.from({ length: this.rows }, () => this.blankRow());
    this.styleScreen = Array.from({ length: this.rows }, () => this.blankStyleRow());
    this.scrollback = [];
    this.scrollbackStyles = [];
    const visible = lines.slice(-this.rows);
    const earlier = lines.slice(0, Math.max(0, lines.length - this.rows));
    for (const line of earlier.slice(-this.scrollbackLimit)) {
      const row = this.blankRow();
      let c = 0;
      for (const ch of line) {
        const w = terminalCellWidth(ch);
        if (w === 0 && c > 0) { row[Math.max(0, c - 1)] += ch; continue; }
        if (c >= this.cols) break;
        row[c] = ch;
        if (w === 2 && c + 1 < this.cols) row[c + 1] = "";
        c += Math.max(1, w);
      }
      this.scrollback.push(row);
      this.scrollbackStyles.push(this.blankStyleRow());
    }
    for (let r = 0; r < visible.length; r++) {
      let c = 0;
      for (const ch of visible[r]) {
        const w = terminalCellWidth(ch);
        if (w === 0 && c > 0) { this.screen[r][Math.max(0, c - 1)] += ch; continue; }
        if (c >= this.cols) break;
        this.screen[r][c] = ch;
        if (w === 2 && c + 1 < this.cols) this.screen[r][c + 1] = "";
        c += Math.max(1, w);
      }
    }
    this.row = Math.max(0, Math.min(visible.length - 1, this.rows - 1));
    let width = 0;
    for (const ch of visible[this.row] || "") width += terminalCellWidth(ch);
    this.col = Math.max(0, Math.min(this.cols - 1, width));
    this.wrapPending = false;
  }

  renderRows() {
    const rows = [];
    for (let i = 0; i < this.scrollback.length; i++) rows.push({ cells: this.scrollback[i], styles: this.scrollbackStyles[i] || this.blankStyleRow() });
    for (let i = 0; i < this.screen.length; i++) rows.push({ cells: this.screen[i], styles: this.styleScreen[i] || this.blankStyleRow() });
    return rows;
  }

  render() {
    return this.renderRows().map((entry) => this.rowText(entry.cells)).join("\n");
  }

  cursorModel(visible) {
    return {
      visible: !!visible && this.cursorVisible,
      row: this.scrollback.length + this.row,
      col: this.col,
      style: this.cursorStyle || this.defaultCursorStyle || "bar"
    };
  }
}

const DEFAULT_TERMINAL_CONFIG = {
  term: "xterm-256color",
  encoding: "UTF-8",
  font_family: "Cascadia Code",
  font_size: 13,
  color_scheme: "midnight",
  scrollback_lines: 10000,
  cursor_style: "bar"
};

function terminalConfigForSession(sessionOrId) {
  const session = typeof sessionOrId === "string"
    ? state.sessions.find((item) => item.id === sessionOrId)
    : sessionOrId;
  const profile = session && session.profile_id
    ? (state.settings.profiles || []).find((item) => item.id === session.profile_id)
    : null;
  return { ...DEFAULT_TERMINAL_CONFIG, ...((profile && profile.terminal) || {}) };
}

function terminalFor(sessionId) {
  const config = terminalConfigForSession(sessionId);
  let terminal = state.terminals.get(sessionId);
  if (!terminal) {
    terminal = new TerminalBuffer(100, 30, config.scrollback_lines, {
      cursorStyle: config.cursor_style,
      colorScheme: config.color_scheme
    });
    state.terminals.set(sessionId, terminal);
  } else {
    terminal.setConfig(config);
  }
  return terminal;
}

function showToast(message) {
  const toast = $("#toast");
  toast.textContent = message;
  toast.classList.add("is-visible");
  window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => toast.classList.remove("is-visible"), 2200);
}

function loadSessionHistory() {
  try {
    const raw = window.localStorage.getItem(SESSION_HISTORY_KEY);
    const parsed = raw ? JSON.parse(raw) : [];
    state.history = Array.isArray(parsed)
      ? parsed.slice(0, SESSION_HISTORY_LIMIT).map((item) => ({
          history_id: String(item.history_id || ""),
          original_session_id: String(item.original_session_id || ""),
          profile_id: String(item.profile_id || ""),
          name: String(item.name || "SSH 会话"),
          target: String(item.target || ""),
          closed_at: Number(item.closed_at || Date.now()),
          terminal_text: ""
        })).filter((item) => item.history_id)
      : [];
  } catch (_) {
    state.history = [];
  }
}

function persistSessionHistory() {
  const payload = state.history.slice(0, SESSION_HISTORY_LIMIT).map((item) => ({
    history_id: item.history_id,
    original_session_id: item.original_session_id,
    profile_id: item.profile_id || "",
    name: item.name,
    target: item.target,
    closed_at: item.closed_at
  }));
  try {
    window.localStorage.setItem(SESSION_HISTORY_KEY, JSON.stringify(payload));
  } catch (_) {}
}

function formatHistoryTime(value) {
  const date = new Date(Number(value || 0));
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}

function rememberClosedSession(session, terminalText) {
  const record = {
    history_id: String(session.id) + "-" + Date.now(),
    original_session_id: String(session.id),
    profile_id: session.profile_id || "",
    name: session.name || "SSH 会话",
    target: session.target || "",
    closed_at: Date.now(),
    terminal_text: String(terminalText || "").slice(-SESSION_HISTORY_TEXT_LIMIT)
  };
  state.history.unshift(record);
  state.history = state.history.slice(0, SESSION_HISTORY_LIMIT);
  persistSessionHistory();
  if (state.nav === "history") renderSidebar();
  return record;
}

function clearSessionHistory() {
  state.history = [];
  persistSessionHistory();
  renderSidebar();
}

function restoreHistorySession(record) {
  const restoredId = "history:" + record.history_id;
  const existing = state.sessions.find((item) => item.id === restoredId);
  if (existing) {
    selectSession(restoredId);
    return;
  }

  state.sessions.push({
    id: restoredId,
    profile_id: record.profile_id || "",
    name: record.name,
    target: record.target,
    state: "disconnected",
    stage: "历史记录",
    message: "这是已关闭会话的历史记录，不会恢复远端 SSH 连接。",
    history_only: true,
    history_id: record.history_id
  });

  const terminal = terminalFor(restoredId);
  if (record.terminal_text) {
    terminal.loadText(record.terminal_text);
  } else {
    terminal.loadText(
      "该历史记录来自之前的应用运行。为避免在本机长期保存终端内容，只保留了会话信息。"
    );
  }
  state.activeSessionId = restoredId;
  renderAll();
  window.setTimeout(() => focusTerminalInput(), 0);
}

const SIDEBAR_DEFAULT_WIDTH = 310;
const SIDEBAR_MIN_WIDTH = 220;
const SIDEBAR_MAX_WIDTH = 600;
let sidebarResizeFrame = 0;

function sidebarWidthBounds() {
  const shell = $(".app-shell");
  const rail = $(".rail");
  const railWidth = rail.getBoundingClientRect().width;
  const available = Math.max(SIDEBAR_MIN_WIDTH, shell.clientWidth - railWidth - 420);
  return {
    min: SIDEBAR_MIN_WIDTH,
    max: Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, available))
  };
}

function scheduleSidebarTerminalResize() {
  if (sidebarResizeFrame) return;
  sidebarResizeFrame = window.requestAnimationFrame(() => {
    sidebarResizeFrame = 0;
    resizeActiveTerminal();
  });
}

function applySidebarWidth(width, persist = true) {
  const bounds = sidebarWidthBounds();
  const next = Math.round(Math.max(bounds.min, Math.min(bounds.max, Number(width) || SIDEBAR_DEFAULT_WIDTH)));
  const shell = $(".app-shell");
  const handle = $("#sidebarResizeHandle");
  shell.style.setProperty("--ssh-sidebar-width", next + "px");
  shell.classList.toggle("is-sidebar-narrow", next < 320);
  shell.classList.toggle("is-sidebar-compact", next < 270);
  handle.setAttribute("aria-valuemin", String(bounds.min));
  handle.setAttribute("aria-valuemax", String(bounds.max));
  handle.setAttribute("aria-valuenow", String(next));
  handle.title = "侧边栏宽度 " + next + "px；拖动调整，双击恢复默认";
  if (persist) window.localStorage.setItem("ssh-client.sidebar-width", String(next));
  scheduleSidebarTerminalResize();
  return next;
}

function bindSidebarResize() {
  const shell = $(".app-shell");
  const handle = $("#sidebarResizeHandle");
  const rail = $(".rail");
  let dragging = false;

  const widthFromPointer = (clientX) => clientX - rail.getBoundingClientRect().right;

  const finish = (event) => {
    if (!dragging) return;
    dragging = false;
    shell.classList.remove("is-sidebar-resizing");
    if (event && handle.hasPointerCapture(event.pointerId)) {
      handle.releasePointerCapture(event.pointerId);
    }
    scheduleSidebarTerminalResize();
  };

  handle.addEventListener("pointerdown", (event) => {
    if (event.button !== 0 || state.sidebarCollapsed) return;
    event.preventDefault();
    dragging = true;
    shell.classList.add("is-sidebar-resizing");
    handle.setPointerCapture(event.pointerId);
    applySidebarWidth(widthFromPointer(event.clientX));
  });

  handle.addEventListener("pointermove", (event) => {
    if (!dragging) return;
    event.preventDefault();
    applySidebarWidth(widthFromPointer(event.clientX));
  });

  handle.addEventListener("pointerup", finish);
  handle.addEventListener("pointercancel", finish);

  handle.addEventListener("dblclick", () => {
    applySidebarWidth(SIDEBAR_DEFAULT_WIDTH);
  });

  handle.addEventListener("keydown", (event) => {
    if (!["ArrowLeft", "ArrowRight", "Home"].includes(event.key)) return;
    event.preventDefault();
    if (event.key === "Home") {
      applySidebarWidth(SIDEBAR_DEFAULT_WIDTH);
      return;
    }
    const current = Number.parseInt(getComputedStyle(shell).getPropertyValue("--ssh-sidebar-width"), 10) || SIDEBAR_DEFAULT_WIDTH;
    applySidebarWidth(current + (event.key === "ArrowLeft" ? -10 : 10));
  });

  window.addEventListener("resize", () => {
    const current = Number.parseInt(getComputedStyle(shell).getPropertyValue("--ssh-sidebar-width"), 10) || SIDEBAR_DEFAULT_WIDTH;
    applySidebarWidth(current, false);
  });
}

function applySidebarCollapsed(collapsed, persist = true) {
  state.sidebarCollapsed = !!collapsed;
  const shell = $(".app-shell");
  const button = $("#sidebarToggleButton");
  shell.classList.toggle("is-sidebar-collapsed", state.sidebarCollapsed);
  button.setAttribute("aria-pressed", String(state.sidebarCollapsed));
  button.setAttribute("aria-label", state.sidebarCollapsed ? "展开连接侧边栏" : "折叠连接侧边栏");
  button.title = state.sidebarCollapsed ? "展开连接侧边栏" : "折叠连接侧边栏";
  button.textContent = state.sidebarCollapsed ? "☰" : "◧";
  if (persist) {
    window.localStorage.setItem("ssh-client.sidebar-collapsed", state.sidebarCollapsed ? "1" : "0");
  }
  scheduleSidebarTerminalResize();
}

function terminalSelectionText() {
  const selection = window.getSelection();
  const viewport = $("#terminalViewport");
  if (!selection || selection.isCollapsed || !selection.anchorNode || !selection.focusNode) return "";
  if (!viewport.contains(selection.anchorNode) || !viewport.contains(selection.focusNode)) return "";
  return selection.toString();
}

async function writeClipboardText(text) {
  if (!text) return false;
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch (_) {
    try {
      return !!(await ClipboardSetText(text));
    } catch (_) {
      return false;
    }
  }
}

async function readClipboardText() {
  try {
    return await navigator.clipboard.readText();
  } catch (_) {
    try {
      return await ClipboardGetText();
    } catch (_) {
      return "";
    }
  }
}

async function copyTerminalSelection(text = terminalSelectionText()) {
  if (!text) {
    showToast("请先选择终端中的文本");
    return;
  }
  if (await writeClipboardText(text)) {
    showToast("已复制终端文本");
  } else {
    showToast("复制失败");
  }
}

function normalizePasteText(text) {
  return String(text || "").replace(/\r\n/g, "\n").replace(/\r/g, "\n");
}

async function sendTerminalPaste(text) {
  const session = activeSession();
  if (!session || session.state !== "connected") return;
  const normalized = normalizePasteText(text);
  if (!normalized) return;

  const terminal = terminalFor(session.id);
  const payload = terminal.bracketedPaste
    ? "\u001b[200~" + normalized + "\u001b[201~"
    : normalized;
  await sendTerminalData(payload);
}

async function pasteTerminalClipboard() {
  const text = await readClipboardText();
  if (!text) return;
  await sendTerminalPaste(text);
  focusTerminalInput();
}

function selectAllTerminalText() {
  const text = $("#terminalText");
  if (!text.textContent) return;
  const range = document.createRange();
  range.selectNodeContents(text);
  const selection = window.getSelection();
  selection.removeAllRanges();
  selection.addRange(range);
}

function hideTerminalContextMenu() {
  $("#terminalContextMenu").classList.add("is-hidden");
}

function showTerminalContextMenu(event) {
  const menu = $("#terminalContextMenu");
  state.terminalContextSelection = terminalSelectionText();
  const copyButton = menu.querySelector('[data-terminal-action="copy"]');
  copyButton.disabled = !state.terminalContextSelection;
  menu.classList.remove("is-hidden");

  const width = menu.offsetWidth;
  const height = menu.offsetHeight;
  const left = Math.min(event.clientX, Math.max(8, window.innerWidth - width - 8));
  const top = Math.min(event.clientY, Math.max(8, window.innerHeight - height - 8));
  menu.style.left = left + "px";
  menu.style.top = top + "px";
}

function buildCommandItems() {
  const items = [];

  for (const profile of state.settings.profiles || []) {
    items.push({
      kind: "连接",
      icon: ">_",
      label: profile.name,
      detail: profile.username + "@" + profile.host + ":" + profile.port,
      meta: "连接",
      keywords: [profile.name, profile.host, profile.username, ...(profile.tags || [])].join(" "),
      action: () => beginProfileConnection(profile)
    });
  }

  for (const session of state.sessions) {
    items.push({
      kind: "会话",
      icon: "▤",
      label: session.name,
      detail: stateLabel(session.state) + " · " + session.target,
      meta: "切换",
      keywords: [session.name, session.target, session.state].join(" "),
      action: () => selectSession(session.id)
    });
  }

  items.push(
    {
      kind: "操作",
      icon: "＋",
      label: "新建 SSH 连接",
      detail: "创建新的 Connection Profile",
      meta: "Ctrl N",
      keywords: "new create 新建 连接 profile",
      action: () => openProfileDialog()
    },
    {
      kind: "操作",
      icon: "⚡",
      label: "快速连接",
      detail: "使用 user@host:port 建立一次性会话",
      meta: "Ctrl T",
      keywords: "quick connect 快速 临时",
      action: () => openQuickDialog()
    },
    {
      kind: "操作",
      icon: "⇩",
      label: "从 SSH Config 导入",
      detail: "预览并选择 ~/.ssh/config 中的 Host",
      meta: "",
      keywords: "import config 导入 openssh",
      action: () => openImportDialog()
    },
    {
      kind: "操作",
      icon: "⚙",
      label: "打开设置",
      detail: "主题、开机启动等桌面设置",
      meta: "",
      keywords: "settings theme 设置 主题",
      action: () => openSettings()
    }
  );

  return items;
}

function openCommandPalette() {
  const dialog = $("#commandDialog");
  if (dialog.open) {
    $("#commandSearch").focus();
    return;
  }
  $("#commandSearch").value = "";
  commandSelection = 0;
  renderCommandPalette();
  dialog.showModal();
  window.setTimeout(() => $("#commandSearch").focus(), 0);
}

function renderCommandPalette() {
  const query = $("#commandSearch").value.trim().toLowerCase();
  visibleCommandItems = buildCommandItems().filter((item) => {
    if (!query) return true;
    return [item.label, item.detail, item.keywords, item.kind]
      .filter(Boolean)
      .join(" ")
      .toLowerCase()
      .includes(query);
  });

  if (commandSelection >= visibleCommandItems.length) commandSelection = Math.max(0, visibleCommandItems.length - 1);

  const results = $("#commandResults");
  results.replaceChildren();

  if (!visibleCommandItems.length) {
    const empty = document.createElement("div");
    empty.className = "command-empty";
    empty.textContent = "没有匹配的连接、会话或操作";
    results.append(empty);
    return;
  }

  let lastKind = "";
  visibleCommandItems.forEach((item, index) => {
    if (item.kind !== lastKind) {
      const title = document.createElement("div");
      title.className = "command-section-title";
      title.textContent = item.kind;
      results.append(title);
      lastKind = item.kind;
    }

    const button = document.createElement("button");
    button.type = "button";
    button.className = "command-item" + (index === commandSelection ? " is-active" : "");

    const icon = document.createElement("span");
    icon.className = "command-item-icon";
    icon.textContent = item.icon;

    const copy = document.createElement("span");
    copy.className = "command-item-copy";
    const label = document.createElement("strong");
    label.textContent = item.label;
    const detail = document.createElement("span");
    detail.textContent = item.detail;
    copy.append(label, detail);

    const meta = document.createElement("span");
    meta.className = "command-item-meta";
    meta.textContent = item.meta || "";

    button.append(icon, copy, meta);
    button.addEventListener("mouseenter", () => {
      commandSelection = index;
      results.querySelectorAll(".command-item").forEach((item, itemIndex) => {
        item.classList.toggle("is-active", itemIndex === index);
      });
    });
    button.addEventListener("click", () => executeCommand(index));
    results.append(button);
  });

  const active = results.querySelector(".command-item.is-active");
  if (active) active.scrollIntoView({ block: "nearest" });
}

function executeCommand(index = commandSelection) {
  const item = visibleCommandItems[index];
  if (!item) return;
  $("#commandDialog").close();
  item.action();
}

function escapeInitials(name) {
  const parts = String(name || "SSH").split(/[\s-_]+/).filter(Boolean);
  return parts.slice(0, 2).map((part) => part[0]).join("").toUpperCase() || "S";
}

function profileStatus(profileId) {
  const sessions = state.sessions.filter((session) => session.profile_id === profileId);
  if (!sessions.length) return "offline";
  return sessions[sessions.length - 1].state || "offline";
}

async function loadThemeCatalog(refresh = false) {
  try {
    const catalog = refresh
      ? await window.desktopKitTheme.refreshCatalog()
      : await window.desktopKitTheme.loadCatalog();

    if (catalog && Array.isArray(catalog.packs) && catalog.packs.length) {
      state.themePacks = catalog.packs;
      state.themeCatalog = {
        source: catalog.source || "runtime",
        stale: !!catalog.stale,
        last_error: catalog.last_error || ""
      };
      return catalog;
    }
    throw new Error("主题目录没有可用 Theme Pack");
  } catch (error) {
    state.themePacks = [...BUILTIN_THEME_PACKS];
    state.themeCatalog = {
      source: "builtin",
      stale: true,
      last_error: String(error)
    };
    return null;
  }
}

async function applyTheme() {
  const theme = state.settings.theme || { mode: "dark", variant: "aurora" };
  const preferredPack = theme.variant || "aurora";
  window.desktopKitTheme.apply(theme.mode || "dark");

  try {
    await window.desktopKitTheme.applyPack(preferredPack);
    return preferredPack;
  } catch (error) {
    console.warn("Theme Pack 加载失败，回退到 aurora:", error);
    if (preferredPack !== "aurora") {
      try {
        await window.desktopKitTheme.applyPack("aurora");
        return "aurora";
      } catch (fallbackError) {
        console.warn("aurora fallback 加载失败:", fallbackError);
      }
    }
    window.desktopKitTheme.clearAppliedPack();
    return "";
  }
}

async function loadState() {
  const next = await GetState();
  state.settings = next.settings;
  state.sessions = next.sessions || [];
  state.commandHistory = next.command_history || [];
  state.launchAtLogin = !!next.launch_at_login;
  state.launchAtLoginSupported = !!next.launch_at_login_supported;
  state.dataDir = next.data_dir || "";
  $("#runtimeSummary").title = state.dataDir || "~/.config/ssh-client";
  for (const session of state.sessions) terminalFor(session.id);
  if (!state.activeSessionId && state.sessions.length) state.activeSessionId = state.sessions[state.sessions.length - 1].id;
  await loadThemeCatalog(false);
  await applyTheme();
  renderAll();
  if (next.command_history_error) {
    showToast(next.command_history_error);
  }
}

function renderAll() {
  renderSidebar();
  renderSessions();
  renderActiveSession();
}

function renderSidebar() {
  const title = $("#sidebarTitle");
  const meta = $("#sidebarMeta");
  const list = $("#connectionList");
  const search = $("#connectionSearch");
  list.replaceChildren();

  search.placeholder = state.nav === "history"
    ? "搜索命令或目标主机"
    : "搜索名称、地址、用户或标签";

  if (state.nav === "sessions") {
    title.textContent = "会话";
    meta.textContent = String(state.sessions.length) + " 个标签";
    renderSessionSidebar(list);
    return;
  }

  if (state.nav === "history") {
    const query = state.search.trim().toLowerCase();
    const filtered = state.commandHistory.filter((record) => {
      if (!query) return true;
      return [record.command, record.target].filter(Boolean).join(" ").toLowerCase().includes(query);
    });

    title.textContent = "命令历史";
    meta.textContent = query
      ? String(filtered.length) + " / " + String(state.commandHistory.length) + " 条 · 已加密"
      : String(state.commandHistory.length) + " 条 · 最多 1000 · 已加密";
    renderHistorySidebar(list, filtered, query);
    return;
  }

  if (state.nav === "snippets") {
    title.textContent = "命令片段";
    meta.textContent = "第一阶段只保留插入语义";
    const empty = document.createElement("div");
    empty.className = "dk-empty-state";
    empty.innerHTML = "<div><strong>命令片段稍后接入</strong><p>不会设计成无确认的一键远程执行。</p></div>";
    list.append(empty);
    return;
  }

  title.textContent = "连接";
  const profiles = state.settings.profiles || [];
  meta.textContent = String(profiles.length) + " 个主机 · " + String(state.sessions.filter((s) => s.state === "connected").length) + " 个已连接";

  const query = state.search.trim().toLowerCase();
  const filtered = profiles.filter((profile) => {
    if (!query) return true;
    const group = (state.settings.groups || []).find((item) => item.id === profile.group_id);
    const text = [profile.name, profile.host, profile.username, group && group.name, ...(profile.tags || [])].filter(Boolean).join(" ").toLowerCase();
    return text.includes(query);
  });

  const groups = [];
  const favorites = filtered.filter((profile) => profile.favorite);
  if (favorites.length) groups.push({ id: "__favorites", name: "收藏", profiles: favorites });

  for (const group of state.settings.groups || []) {
    const items = filtered.filter((profile) => profile.group_id === group.id && !profile.favorite);
    if (items.length) groups.push({ id: group.id, name: group.name, profiles: items });
  }
  const ungrouped = filtered.filter((profile) => !profile.group_id && !profile.favorite);
  if (ungrouped.length) groups.push({ id: "__ungrouped", name: "未分组", profiles: ungrouped });

  if (!groups.length) {
    const empty = document.createElement("div");
    empty.className = "dk-empty-state";
    empty.innerHTML = profiles.length
      ? "<div><strong>没有匹配的连接</strong><p>尝试搜索名称、Host、用户、分组或标签。</p></div>"
      : "<div><strong>还没有连接</strong><p>点击右上角 ＋ 创建第一个 SSH Connection Profile。</p></div>";
    list.append(empty);
    return;
  }

  for (const group of groups) {
    const section = document.createElement("section");
    section.className = "connection-group";
    const header = document.createElement("div");
    header.className = "connection-group-title";
    header.innerHTML = "<span>" + group.name + "</span><span>" + group.profiles.length + "</span>";
    section.append(header);
    for (const profile of group.profiles) section.append(createConnectionRow(profile));
    list.append(section);
  }
}

function createConnectionRow(profile) {
  const row = document.createElement("button");
  row.type = "button";
  row.className = "connection-row" + (state.selectedProfileId === profile.id ? " is-selected" : "");
  row.dataset.profileId = profile.id;
  row.title = profile.name + "\n" + profile.username + "@" + profile.host + ":" + profile.port;

  const avatar = document.createElement("span");
  avatar.className = "connection-avatar";
  avatar.textContent = escapeInitials(profile.name);

  const copy = document.createElement("span");
  copy.className = "connection-content";
  const name = document.createElement("span");
  name.className = "connection-name";
  name.textContent = profile.name;
  const meta = document.createElement("span");
  meta.className = "connection-meta";

  const user = document.createElement("span");
  user.className = "connection-user";
  user.textContent = profile.username ? profile.username + "@" : "";

  const host = document.createElement("span");
  host.className = "connection-host";
  host.textContent = profile.host;

  const port = document.createElement("span");
  port.className = "connection-port";
  port.textContent = profile.port ? ":" + profile.port : "";

  meta.append(user, host, port);
  copy.append(name, meta);

  const dot = document.createElement("span");
  dot.className = "connection-status " + profileStatus(profile.id);

  row.append(avatar, copy, dot);
  row.addEventListener("click", () => {
    state.selectedProfileId = profile.id;
    renderSidebar();
  });
  row.addEventListener("dblclick", () => beginProfileConnection(profile));
  row.addEventListener("contextmenu", (event) => {
    event.preventDefault();
    openProfileDialog(profile);
  });
  return row;
}

function renderSessionSidebar(list) {
  if (!state.sessions.length) {
    const empty = document.createElement("div");
    empty.className = "dk-empty-state";
    empty.innerHTML = "<div><strong>没有会话</strong><p>连接主机后，会话会保留在这里。</p></div>";
    list.append(empty);
    return;
  }
  const section = document.createElement("section");
  section.className = "connection-group";
  const header = document.createElement("div");
  header.className = "connection-group-title";
  header.innerHTML = "<span>当前与最近会话</span><span>" + state.sessions.length + "</span>";
  section.append(header);
  for (const session of state.sessions) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "connection-row" + (session.id === state.activeSessionId ? " is-selected" : "");
    row.title = session.name + "\n" + session.target + "\n" + stateLabel(session.state);
    const avatar = document.createElement("span");
    avatar.className = "connection-avatar";
    avatar.textContent = ">_";
    const copy = document.createElement("span");
    copy.className = "connection-content";
    const name = document.createElement("span");
    name.className = "connection-name";
    name.textContent = session.name;
    const meta = document.createElement("span");
    meta.className = "connection-meta session-meta";

    const target = document.createElement("span");
    target.className = "session-target-text";
    target.textContent = session.target;

    meta.append(target);
    copy.append(name, meta);
    const dot = document.createElement("span");
    dot.className = "connection-status " + session.state;
    row.append(avatar, copy, dot);
    row.addEventListener("click", () => selectSession(session.id));
    section.append(row);
  }
  list.append(section);
}

function rememberCommand(command, session = activeSession()) {
  const value = String(command || "").trim();
  if (!session || !value || session.history_only) return;

  const entry = {
    command: value,
    session_id: session.id,
    target: session.target || session.name || "",
    created_at: Date.now()
  };

  state.commandHistory.unshift(entry);
  state.commandHistory = state.commandHistory.slice(0, 1000);
  if (state.nav === "history") renderSidebar();

  RecordCommandHistory(entry).catch((error) => {
    showToast("命令历史加密保存失败：" + error);
  });
}

function terminalRowText(terminal, rowIndex = terminal.row) {
  const row = terminal.screen[rowIndex] || [];
  return row.join("").replace(/\s+$/, "");
}

function observeRemotePrompt(sessionId, terminal) {
  if (!terminal || terminal.alternateScreen) return;

  const raw = (terminal.screen[terminal.row] || [])
    .slice(0, Math.max(0, terminal.col))
    .join("");
  const trimmed = raw.trimEnd();
  if (!trimmed || !/(?:[$#>%]|❯|➜|λ)$/.test(trimmed)) return;

  const existing = state.promptPrefixes.get(sessionId) || "";
  if (existing && raw.startsWith(existing) && raw.length > existing.length) {
    return;
  }
  state.promptPrefixes.set(sessionId, raw);
}

function captureRenderedCommand(session, terminal) {
  if (!session || !terminal || session.history_only || terminal.alternateScreen) return;

  const prefix = state.promptPrefixes.get(session.id);
  if (!prefix) return;

  const line = terminalRowText(terminal);
  if (!line.startsWith(prefix)) return;

  const command = line.slice(prefix.length).trim();
  if (!command) return;
  rememberCommand(command, session);
}

async function insertCommandFromHistory(command) {
  const session = activeSession();
  if (!session || session.state !== "connected") {
    showToast("请先打开一个已连接的会话");
    return;
  }

  // "插入"只作为一次终端粘贴。当前光标位置、已有内容以及最终如何编辑，
  // 全部交给远端 shell/readline 处理，不在客户端模拟清行或移动光标。
  await sendTerminalPaste(command);
  focusTerminalInput();
}

function renderHistorySidebar(list, records = state.commandHistory, query = "") {
  if (!state.commandHistory.length) {
    const empty = document.createElement("div");
    empty.className = "dk-empty-state history-empty-state";
    empty.innerHTML = "<div><strong>还没有命令历史</strong><p>在终端中输入并执行命令后，会加密保存在这里。</p></div>";
    list.append(empty);
    return;
  }

  if (!records.length) {
    const empty = document.createElement("div");
    empty.className = "dk-empty-state history-empty-state";
    empty.innerHTML = "<div><strong>没有匹配的命令</strong><p>换一个命令关键字或目标主机试试。</p></div>";
    list.append(empty);
    return;
  }

  const section = document.createElement("section");
  section.className = "connection-group history-group";

  const header = document.createElement("div");
  header.className = "connection-group-title history-group-title";
  const label = document.createElement("span");
  label.textContent = query ? "搜索结果" : "最近命令";
  const clear = document.createElement("button");
  clear.type = "button";
  clear.className = "history-clear-button";
  clear.textContent = "清空";
  clear.addEventListener("click", async () => {
    try {
      await ClearCommandHistory();
      state.commandHistory = [];
      renderSidebar();
      showToast("命令历史已清空");
    } catch (error) {
      showToast("清空命令历史失败：" + error);
    }
  });
  header.append(label, clear);
  section.append(header);

  for (const record of records) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "connection-row history-row";
    row.title = "点击插入到当前终端";

    const avatar = document.createElement("span");
    avatar.className = "connection-avatar history-avatar";
    avatar.textContent = "›";

    const content = document.createElement("span");
    content.className = "history-content";

    const command = document.createElement("span");
    command.className = "history-command";
    command.textContent = record.command;

    const meta = document.createElement("span");
    meta.className = "history-meta";

    const time = document.createElement("span");
    time.textContent = formatHistoryTime(record.created_at);

    const target = document.createElement("span");
    target.className = "history-target";
    target.textContent = record.target || "当前会话";

    meta.append(time, target);
    content.append(command, meta);

    const action = document.createElement("span");
    action.className = "history-restore-label";
    action.textContent = "插入";

    row.append(avatar, content, action);
    row.addEventListener("click", () => insertCommandFromHistory(record.command));
    section.append(row);
  }

  list.append(section);
}

function renderSessions() {
  const tabs = $("#sessionTabs");
  tabs.replaceChildren();
  const totals = new Map();
  const seen = new Map();
  for (const session of state.sessions) totals.set(session.name, (totals.get(session.name) || 0) + 1);

  for (const session of state.sessions) {
    const nth = (seen.get(session.name) || 0) + 1;
    seen.set(session.name, nth);
    const duplicate = (totals.get(session.name) || 0) > 1;
    const displayName = session.name + (duplicate ? " · " + nth : "") +
      (session.state === "reconnecting" ? " · 重连中" : "");

    const tab = document.createElement("button");
    tab.type = "button";
    tab.className = "session-tab" + (session.id === state.activeSessionId ? " is-active" : "");
    tab.setAttribute("role", "tab");
    tab.setAttribute("aria-selected", String(session.id === state.activeSessionId));
    tab.setAttribute("aria-label", "切换到会话 " + displayName);
    tab.title = session.name + "\n" + (session.target || "") + "\n" + stateLabel(session.state);

    const dot = document.createElement("span");
    dot.className = "connection-status " + session.state;
    dot.setAttribute("aria-hidden", "true");

    const title = document.createElement("span");
    title.className = "session-tab-title";
    title.textContent = displayName;

    const close = document.createElement("button");
    close.type = "button";
    close.className = "session-tab-close";
    close.textContent = "×";
    close.title = "关闭 " + displayName;
    close.setAttribute("aria-label", "关闭会话 " + displayName);
    close.addEventListener("click", async (event) => {
      event.stopPropagation();
      await closeSession(session.id);
    });

    tab.append(dot, title, close);
    tab.addEventListener("click", () => selectSession(session.id));
    tabs.append(tab);
  }
}

function selectSession(id) {
  state.activeSessionId = id;
  renderSessions();
  renderActiveSession();
  if (state.nav === "sessions") renderSidebar();
  scheduleActiveTerminalResize();
  window.setTimeout(() => focusTerminalInput(), 0);
}

function activeSession() {
  return state.sessions.find((session) => session.id === state.activeSessionId) || null;
}

function renderActiveSession() {
  const session = activeSession();
  $("#emptyState").classList.toggle("is-hidden", !!session);
  $("#sessionView").classList.toggle("is-hidden", !session);
  if (!session) return;

  $("#activeSessionName").textContent = session.name;
  $("#activeSessionTarget").textContent = session.target;
  $("#terminalStateText").textContent = session.stage || session.state;

  const pill = $("#activeSessionStatus");
  pill.textContent = session.history_only ? "历史" : stateLabel(session.state);
  pill.className = "dk-status-pill";
  if (session.state === "connected") pill.classList.add("is-success");
  if (session.state === "connecting" || session.state === "reconnecting" || session.state === "host_key_pending" || session.state === "authenticating") pill.classList.add("is-warning");
  if (session.state === "failed" || session.state === "security_blocked") pill.classList.add("is-danger");

  const banner = $("#sessionBanner");
  const showBanner = !!session.history_only || ["reconnecting", "failed", "security_blocked", "host_key_pending"].includes(session.state);
  banner.classList.toggle("is-hidden", !showBanner);
  banner.classList.toggle("is-danger", !session.history_only && (session.state === "failed" || session.state === "security_blocked"));
  if (showBanner) {
    let text = session.message || stateLabel(session.state);
    if (session.reason_code) text += " · " + session.reason_code;
    if (session.state === "reconnecting" && session.next_retry_seconds) text += " · " + session.next_retry_seconds + " 秒后重试";
    banner.textContent = text;
  }

  $("#retrySessionButton").classList.toggle("is-hidden", session.state !== "reconnecting");
  $("#disconnectSessionButton").classList.toggle("is-hidden", !!session.history_only);
  $("#disconnectSessionButton").disabled = !!session.history_only || ["disconnected", "failed", "security_blocked"].includes(session.state);
  renderTerminal();
}

function stateLabel(value) {
  const labels = {
    connecting: "连接中",
    host_key_pending: "等待确认",
    authenticating: "认证中",
    connected: "已连接",
    reconnecting: "重连中",
    disconnected: "已断开",
    failed: "连接失败",
    security_blocked: "安全阻断"
  };
  return labels[value] || value || "未知";
}

function applyTerminalRunStyle(span, style) {
  if (!style) return;
  let fg = style.fg || "";
  let bg = style.bg || "";
  if (style.inverse) {
    const effectiveFG = fg || "var(--ssh-terminal-text)";
    const effectiveBG = bg || "var(--ssh-terminal-bg)";
    fg = effectiveBG;
    bg = effectiveFG;
  }
  if (style.hidden) span.style.color = "transparent";
  else if (fg) span.style.color = fg;
  if (bg) span.style.backgroundColor = bg;
  if (style.bold) span.style.fontWeight = "700";
  if (style.dim) span.style.opacity = "0.65";
  if (style.italic) span.style.fontStyle = "italic";
  const decorations = [];
  if (style.underline) decorations.push("underline");
  if (style.strike) decorations.push("line-through");
  if (decorations.length) span.style.textDecorationLine = decorations.join(" ");
}

function renderTerminalContent(element, terminal) {
  const fragment = document.createDocumentFragment();
  const rows = terminal.renderRows();
  rows.forEach((entry, rowIndex) => {
    const cells = entry.cells || [];
    const styles = entry.styles || [];
    let last = -1;
    for (let c = cells.length - 1; c >= 0; c--) {
      if ((cells[c] !== " " && cells[c] !== "") || (styles[c] || 0) !== 0) {
        last = c;
        break;
      }
    }

    let c = 0;
    while (c <= last) {
      const styleId = styles[c] || 0;
      let end = c + 1;
      while (end <= last && (styles[end] || 0) === styleId) end++;
      const text = cells.slice(c, end).join("");
      if (styleId === 0) {
        fragment.append(document.createTextNode(text));
      } else {
        const span = document.createElement("span");
        span.className = "terminal-style-run";
        span.textContent = text;
        applyTerminalRunStyle(span, terminal.styleFor(styleId));
        fragment.append(span);
      }
      c = end;
    }
    if (rowIndex < rows.length - 1) fragment.append(document.createTextNode("\n"));
  });
  element.replaceChildren(fragment);
}

function applyTerminalProfile(session, terminal) {
  const config = terminalConfigForSession(session);
  terminal.setConfig(config);
  const viewport = $("#terminalViewport");
  const text = $("#terminalText");
  // Keep the emulator palette fixed so ANSI/OSC behaviour stays predictable.
  // Application themes intentionally do not recolor the terminal surface.
  viewport.dataset.terminalScheme = "midnight";
  $("#sessionView").dataset.terminalScheme = "midnight";
  text.style.fontFamily = '"' + String(config.font_family || "Cascadia Code").replace(/"/g, "") + '", "JetBrains Mono", Consolas, monospace';
  text.style.fontSize = String(config.font_size || 13) + "px";
  const profileStatus = $("#terminalProfileText");
  if (profileStatus) profileStatus.textContent = "兼容配色 · " + (config.font_size || 13) + "px";
  const encodingStatus = $("#terminalEncodingText");
  if (encodingStatus) encodingStatus.textContent = config.encoding || "UTF-8";
}

function positionTerminalCursor(terminal, visible) {
  const cursor = $("#terminalCursor");
  const model = terminal.cursorModel(visible);
  const metrics = terminalCellMetrics();
  const left = metrics.paddingLeft + model.col * metrics.charWidth;
  const top = metrics.paddingTop + model.row * metrics.lineHeight;
  const imeInput = $("#terminalImeInput");
  if (imeInput) {
    imeInput.style.left = left + "px";
    imeInput.style.top = top + "px";
    imeInput.style.width = Math.max(2, metrics.charWidth) + "px";
    imeInput.style.height = metrics.lineHeight + "px";
    imeInput.style.fontFamily = getComputedStyle($("#terminalText")).fontFamily;
    imeInput.style.fontSize = getComputedStyle($("#terminalText")).fontSize;
  }
  if (!model.visible) {
    cursor.classList.remove("is-visible");
    return;
  }
  cursor.dataset.cursorStyle = model.style;
  cursor.style.left = left + "px";
  cursor.style.top = top + "px";
  cursor.style.width = metrics.charWidth + "px";
  cursor.style.height = metrics.lineHeight + "px";
  cursor.classList.add("is-visible");
}

function renderTerminal() {
  const session = activeSession();
  if (!session) return;
  const viewport = $("#terminalViewport");
  const terminal = terminalFor(session.id);
  applyTerminalProfile(session, terminal);
  const stick = viewport.scrollTop + viewport.clientHeight >= viewport.scrollHeight - 48;
  renderTerminalContent($("#terminalText"), terminal);
  positionTerminalCursor(terminal, session.state === "connected");
  if (stick) window.requestAnimationFrame(() => { viewport.scrollTop = viewport.scrollHeight; });
}

function updateSession(snapshot) {
  const index = state.sessions.findIndex((session) => session.id === snapshot.id);
  if (index >= 0) state.sessions[index] = snapshot;
  else state.sessions.push(snapshot);
  terminalFor(snapshot.id);
  if (!state.activeSessionId) state.activeSessionId = snapshot.id;

  const pendingSave = state.pendingQuickSaves.get(snapshot.id);
  if (pendingSave && snapshot.state === "connected") {
    state.pendingQuickSaves.delete(snapshot.id);
    CreateProfile(pendingSave)
      .then((next) => {
        state.settings = next.settings;
        showToast("快速连接已保存到连接库");
        renderSidebar();
      })
      .catch((error) => showToast("会话已连接，但保存 Profile 失败：" + error));
  }

  const authFailureCodes = new Set([
    "AUTH_PASSWORD_REQUIRED",
    "AUTH_KEY_PASSPHRASE_REQUIRED",
    "AUTH_METHOD_REJECTED",
    "AUTH_KEY_REJECTED"
  ]);
  if (
    snapshot.state === "failed" &&
    authFailureCodes.has(snapshot.reason_code) &&
    snapshot.profile_id &&
    state.lastAuthPromptSessionId !== snapshot.id
  ) {
    const profile = (state.settings.profiles || []).find((item) => item.id === snapshot.profile_id);
    if (profile) {
      state.lastAuthPromptSessionId = snapshot.id;
      const message = snapshot.reason_code === "AUTH_KEY_PASSPHRASE_REQUIRED"
        ? "私钥需要口令，请重新输入。"
        : "身份验证失败，请检查密码或认证配置后重试。";
      window.setTimeout(() => openCredentialsDialog(profile, message), 0);
    }
  }
  if (snapshot.state === "connected") {
    state.lastAuthPromptSessionId = "";
  }

  renderSessions();
  renderActiveSession();
  renderSidebar();
}

function decoderForSession(sessionId) {
  const config = terminalConfigForSession(sessionId);
  const encoding = String(config.encoding || "UTF-8").trim() || "UTF-8";
  const cached = state.decoders.get(sessionId);
  if (cached && cached.encoding.toLowerCase() === encoding.toLowerCase()) return cached.decoder;

  let sessionDecoder;
  try {
    sessionDecoder = new TextDecoder(encoding);
  } catch (_) {
    sessionDecoder = new TextDecoder("utf-8");
  }
  state.decoders.set(sessionId, { encoding, decoder: sessionDecoder });
  return sessionDecoder;
}

function decodeOutput(encoded, sessionId) {
  const binary = atob(encoded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return decoderForSession(sessionId).decode(bytes, { stream: true });
}

async function beginProfileConnection(profile) {
  if (profile.auth.mode === "password") {
    if (profile.auth.credential_ref) {
      const started = await connectProfileNow(profile, { password: "", passphrase: "" }, false);
      if (!started && !$("#credentialsDialog").open) {
        openCredentialsDialog(profile, "读取已保存密码失败，请重新输入。");
      }
      return;
    }
    openCredentialsDialog(profile);
    return;
  }
  if (profile.auth.mode === "private_key") {
    openCredentialsDialog(profile);
    return;
  }
  await connectProfileNow(profile, { password: "", passphrase: "" }, false);
}

function openCredentialsDialog(profile, errorMessage = "") {
  state.pendingProfile = profile;
  $("#credentialsTitle").textContent = "连接 " + profile.name;
  $("#credentialsHint").textContent = "本次认证信息只保存在内存中。按 Enter 可直接连接。";
  const passwordMode = profile.auth.mode === "password";
  $("#passwordField").classList.toggle("is-hidden", !passwordMode);
  $("#rememberPasswordRow").classList.toggle("is-hidden", !passwordMode);
  $("#passphraseField").classList.toggle("is-hidden", profile.auth.mode !== "private_key");
  $("#connectPassword").value = "";
  $("#connectPassphrase").value = "";
  $("#rememberPassword").checked = passwordMode && !!profile.auth.credential_ref;
  $("#credentialsConnectButton").disabled = false;
  $("#credentialsConnectButton").textContent = "连接";

  const errorBox = $("#credentialsError");
  errorBox.textContent = errorMessage;
  errorBox.classList.toggle("is-hidden", !errorMessage);

  if (!$("#credentialsDialog").open) $("#credentialsDialog").showModal();
  window.setTimeout(() => {
    const target = profile.auth.mode === "password" ? $("#connectPassword") : $("#connectPassphrase");
    target.focus();
  }, 0);
}

async function connectProfileNow(profile, credentials, rememberPassword = false) {
  const dialog = $("#credentialsDialog");
  const button = $("#credentialsConnectButton");
  const hint = $("#credentialsHint");
  const errorBox = $("#credentialsError");

  button.disabled = true;
  button.textContent = "连接中…";
  hint.textContent = "正在创建 SSH 会话，随后会继续进行 Host Key 与身份验证。";
  errorBox.classList.add("is-hidden");

  try {
    const session = await ConnectProfile(profile.id, credentials, rememberPassword);
    updateSession(session);
    state.activeSessionId = session.id;
    renderAll();
    if (dialog.open) dialog.close();
    return true;
  } catch (error) {
    errorBox.textContent = String(error);
    errorBox.classList.remove("is-hidden");
    hint.textContent = "连接尚未开始，请检查配置后重试。";
    return false;
  } finally {
    button.disabled = false;
    button.textContent = "连接";
  }
}

function openProfileDialog(profile = null) {
  const editing = !!profile;
  $("#profileDialogTitle").textContent = editing ? "编辑 SSH 连接" : "新建 SSH 连接";
  $("#profileId").value = editing ? profile.id : "";
  $("#profileName").value = editing ? profile.name : "";
  $("#profileHost").value = editing ? profile.host : "";
  $("#profilePort").value = editing ? profile.port : 22;
  $("#profileUsername").value = editing ? profile.username : "";
  $("#profileAuthMode").value = editing ? profile.auth.mode : "auto";
  $("#profilePrivateKey").value = editing ? profile.auth.private_key_path || "" : "";
  $("#profileTags").value = editing ? (profile.tags || []).join(", ") : "";
  const terminal = { ...DEFAULT_TERMINAL_CONFIG, ...((editing && profile.terminal) || {}) };
  $("#profileTerminalTerm").value = terminal.term;
  $("#profileTerminalEncoding").value = terminal.encoding;
  $("#profileTerminalFontFamily").value = terminal.font_family;
  $("#profileTerminalFontSize").value = terminal.font_size;
  $("#profileTerminalScrollback").value = terminal.scrollback_lines;
  $("#profileTerminalCursorStyle").value = terminal.cursor_style;
  $("#profileFavorite").checked = editing ? !!profile.favorite : false;
  $("#profileError").classList.add("is-hidden");
  $("#deleteProfileButton").classList.toggle("is-hidden", !editing);

  refreshProfileGroupOptions(editing ? profile.group_id || "" : "");

  updatePrivateKeyVisibility();
  $("#profileDialog").showModal();
}

function refreshProfileGroupOptions(selectedId = "") {
  const group = $("#profileGroup");
  group.replaceChildren(new Option("未分组", ""));
  for (const item of state.settings.groups || []) {
    group.add(new Option(item.name, item.id));
  }
  group.value = (state.settings.groups || []).some((item) => item.id === selectedId) ? selectedId : "";
}

function openGroupDialog() {
  $("#groupError").classList.add("is-hidden");
  $("#newGroupName").value = "";
  renderGroupManager();
  $("#groupDialog").showModal();
  window.setTimeout(() => $("#newGroupName").focus(), 0);
}

function renderGroupManager() {
  const list = $("#groupList");
  list.replaceChildren();
  const groups = state.settings.groups || [];

  if (!groups.length) {
    const empty = document.createElement("div");
    empty.className = "dk-empty-state";
    empty.innerHTML = "<div><strong>还没有分组</strong><p>创建分组后，可以在连接编辑页直接选择。</p></div>";
    list.append(empty);
    return;
  }

  for (const group of groups) {
    const row = document.createElement("div");
    row.className = "group-manage-row";

    const input = document.createElement("input");
    input.value = group.name;
    input.autocomplete = "off";
    input.setAttribute("aria-label", "分组名称 " + group.name);

    const count = document.createElement("span");
    count.className = "group-manage-count";
    const profileCount = (state.settings.profiles || []).filter((profile) => profile.group_id === group.id).length;
    count.textContent = profileCount + " 个连接";

    const save = document.createElement("button");
    save.type = "button";
    save.className = "dk-button dk-button-secondary compact-button";
    save.textContent = "保存";
    save.addEventListener("click", async () => {
      const name = input.value.trim();
      if (!name || name === group.name) return;
      try {
        const next = await RenameGroup(group.id, name);
        state.settings = next.settings;
        refreshProfileGroupOptions($("#profileGroup").value);
        renderGroupManager();
        renderSidebar();
        showToast("分组已重命名");
      } catch (error) {
        showGroupError(error);
      }
    });

    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "dk-button dk-button-danger compact-button";
    remove.textContent = "删除";
    remove.addEventListener("click", async () => {
      const profileCount = (state.settings.profiles || []).filter((profile) => profile.group_id === group.id).length;
      const message = profileCount
        ? "删除分组“" + group.name + "”？其中 " + profileCount + " 个连接会移动到“未分组”，连接本身不会删除。"
        : "删除分组“" + group.name + "”？";
      if (!window.confirm(message)) return;
      try {
        const selected = $("#profileGroup").value === group.id ? "" : $("#profileGroup").value;
        const next = await DeleteGroup(group.id);
        state.settings = next.settings;
        refreshProfileGroupOptions(selected);
        renderGroupManager();
        renderSidebar();
        showToast("分组已删除");
      } catch (error) {
        showGroupError(error);
      }
    });

    input.addEventListener("keydown", (event) => {
      if (event.key === "Enter") {
        event.preventDefault();
        save.click();
      }
    });

    row.append(input, count, save, remove);
    list.append(row);
  }
}

function showGroupError(error) {
  const box = $("#groupError");
  box.textContent = String(error);
  box.classList.remove("is-hidden");
}

async function createGroupFromDialog() {
  const input = $("#newGroupName");
  const name = input.value.trim();
  if (!name) {
    showGroupError("请输入分组名称");
    input.focus();
    return;
  }
  try {
    const next = await CreateGroup(name);
    state.settings = next.settings;
    const created = [...(state.settings.groups || [])].reverse().find((group) => group.name === name);
    refreshProfileGroupOptions(created ? created.id : $("#profileGroup").value);
    input.value = "";
    $("#groupError").classList.add("is-hidden");
    renderGroupManager();
    renderSidebar();
    showToast("分组已创建");
    input.focus();
  } catch (error) {
    showGroupError(error);
  }
}

function updatePrivateKeyVisibility() {
  const mode = $("#profileAuthMode").value;
  $("#privateKeyField").classList.toggle("is-hidden", mode !== "private_key" && mode !== "auto");

  const profileId = $("#profileId").value;
  const existing = (state.settings.profiles || []).find((item) => item.id === profileId);
  const hasSavedPassword = mode === "password" && !!(existing && existing.auth && existing.auth.credential_ref);
  $("#savedPasswordRow").classList.toggle("is-hidden", !hasSavedPassword);
}

function profileFromForm() {
  const id = $("#profileId").value;
  const existing = (state.settings.profiles || []).find((item) => item.id === id);
  const tags = $("#profileTags").value.split(",").map((item) => item.trim()).filter(Boolean);
  return {
    id,
    name: $("#profileName").value.trim(),
    host: $("#profileHost").value.trim(),
    port: Number($("#profilePort").value || 22),
    username: $("#profileUsername").value.trim(),
    group_id: $("#profileGroup").value,
    tags,
    favorite: $("#profileFavorite").checked,
    auth: {
      mode: $("#profileAuthMode").value,
      private_key_path: $("#profilePrivateKey").value.trim(),
      credential_ref: existing && existing.auth ? existing.auth.credential_ref || "" : ""
    },
    network: existing && existing.network ? existing.network : { mode: "direct", timeout_sec: 10, keepalive_sec: 30 },
    terminal: {
      term: $("#profileTerminalTerm").value.trim() || DEFAULT_TERMINAL_CONFIG.term,
      encoding: $("#profileTerminalEncoding").value.trim() || DEFAULT_TERMINAL_CONFIG.encoding,
      font_family: $("#profileTerminalFontFamily").value.trim() || DEFAULT_TERMINAL_CONFIG.font_family,
      font_size: Number($("#profileTerminalFontSize").value || DEFAULT_TERMINAL_CONFIG.font_size),
      color_scheme: existing && existing.terminal ? existing.terminal.color_scheme || DEFAULT_TERMINAL_CONFIG.color_scheme : DEFAULT_TERMINAL_CONFIG.color_scheme,
      scrollback_lines: Number($("#profileTerminalScrollback").value || DEFAULT_TERMINAL_CONFIG.scrollback_lines),
      cursor_style: $("#profileTerminalCursorStyle").value || DEFAULT_TERMINAL_CONFIG.cursor_style
    },
    reconnect: existing && existing.reconnect ? existing.reconnect : { enabled: true, keep_tab_on_disconnect: true },
    source: existing && existing.source ? existing.source : { kind: "manual" }
  };
}

async function saveProfile() {
  const profile = profileFromForm();
  const errorBox = $("#profileError");
  try {
    const next = profile.id ? await UpdateProfile(profile) : await CreateProfile(profile);
    state.settings = next.settings;
    applyTheme();
    $("#profileDialog").close();
    renderSidebar();
    showToast(profile.id ? "连接已更新" : "连接已创建");
  } catch (error) {
    errorBox.textContent = String(error);
    errorBox.classList.remove("is-hidden");
  }
}

function openQuickDialog() {
  $("#quickAddress").value = "";
  $("#quickAuthMode").value = "auto";
  $("#quickPassword").value = "";
  $("#quickPrivateKey").value = "";
  $("#quickPassphrase").value = "";
  $("#quickSave").checked = false;
  $("#quickError").classList.add("is-hidden");
  updateQuickAuthFields();
  $("#quickDialog").showModal();
  window.setTimeout(() => $("#quickAddress").focus(), 0);
}

function updateQuickAuthFields() {
  const mode = $("#quickAuthMode").value;
  $("#quickPasswordField").classList.toggle("is-hidden", mode !== "password");
  $("#quickKeyField").classList.toggle("is-hidden", mode !== "private_key");
  $("#quickPassphraseField").classList.toggle("is-hidden", mode !== "private_key");
}

function parseSSHAddress(value) {
  const text = value.trim();
  const at = text.lastIndexOf("@");
  if (at <= 0 || at === text.length - 1) throw new Error("请输入 user@host 或 user@host:port");
  const username = text.slice(0, at);
  let target = text.slice(at + 1);
  let host = target;
  let port = 22;

  if (target.startsWith("[")) {
    const end = target.indexOf("]");
    if (end < 0) throw new Error("IPv6 地址缺少 ]");
    host = target.slice(1, end);
    if (target.slice(end + 1).startsWith(":")) port = Number(target.slice(end + 2));
  } else {
    const firstColon = target.indexOf(":");
    const lastColon = target.lastIndexOf(":");
    if (firstColon > 0 && firstColon === lastColon) {
      host = target.slice(0, lastColon);
      port = Number(target.slice(lastColon + 1));
    }
  }

  if (!host || !Number.isInteger(port) || port < 1 || port > 65535) throw new Error("SSH 地址或端口无效");
  return { username, host, port };
}

async function startQuickConnect() {
  const errorBox = $("#quickError");
  try {
    const parsed = parseSSHAddress($("#quickAddress").value);
    const mode = $("#quickAuthMode").value;
    const request = {
      name: parsed.host,
      host: parsed.host,
      port: parsed.port,
      username: parsed.username,
      auth_mode: mode,
      private_key_path: $("#quickPrivateKey").value.trim(),
      password: $("#quickPassword").value,
      passphrase: $("#quickPassphrase").value,
      save_profile: false
    };
    const session = await QuickConnect(request);
    updateSession(session);
    state.activeSessionId = session.id;

    if ($("#quickSave").checked) {
      state.pendingQuickSaves.set(session.id, {
        id: "",
        name: parsed.host,
        host: parsed.host,
        port: parsed.port,
        username: parsed.username,
        group_id: "",
        tags: [],
        favorite: false,
        auth: { mode, private_key_path: request.private_key_path, credential_ref: "" },
        network: { mode: "direct", timeout_sec: 10, keepalive_sec: 30 },
        terminal: {
          term: "xterm-256color", encoding: "UTF-8", font_family: "Cascadia Code",
          font_size: 13, color_scheme: "midnight", scrollback_lines: 10000, cursor_style: "bar"
        },
        reconnect: { enabled: true, keep_tab_on_disconnect: true },
        source: { kind: "quick_saved" }
      });
    }

    $("#quickDialog").close();
    renderAll();
  } catch (error) {
    errorBox.textContent = String(error);
    errorBox.classList.remove("is-hidden");
  }
}

function openImportDialog() {
  state.importPreview = null;
  $("#importConfigPath").value = "";
  $("#importWarnings").classList.add("is-hidden");
  $("#importError").classList.add("is-hidden");
  $("#importEntries").innerHTML = '<div class="dk-empty-state"><div><strong>尚未读取 SSH Config</strong><p>点击“预览”读取 Host 条目。</p></div></div>';
  updateImportSelectionSummary();
  $("#importDialog").showModal();
}

async function previewSSHConfig() {
  const errorBox = $("#importError");
  errorBox.classList.add("is-hidden");
  try {
    const preview = await PreviewSSHConfig($("#importConfigPath").value.trim());
    state.importPreview = preview;
    $("#importConfigPath").value = preview.path || $("#importConfigPath").value;
    renderImportPreview();
  } catch (error) {
    state.importPreview = null;
    errorBox.textContent = String(error);
    errorBox.classList.remove("is-hidden");
    renderImportPreview();
  }
}

function renderImportPreview() {
  const container = $("#importEntries");
  const warnings = $("#importWarnings");
  container.replaceChildren();

  const preview = state.importPreview;
  if (!preview) {
    const empty = document.createElement("div");
    empty.className = "dk-empty-state";
    empty.innerHTML = "<div><strong>无法显示预览</strong><p>请选择或检查 SSH Config 文件。</p></div>";
    container.append(empty);
    warnings.classList.add("is-hidden");
    updateImportSelectionSummary();
    return;
  }

  if ((preview.warnings || []).length) {
    warnings.textContent = preview.warnings.join(" · ");
    warnings.classList.remove("is-hidden");
  } else {
    warnings.classList.add("is-hidden");
  }

  if (!(preview.entries || []).length) {
    const empty = document.createElement("div");
    empty.className = "dk-empty-state";
    empty.innerHTML = "<div><strong>没有可预览的具体 Host</strong><p>通配符 Host 不会直接导入。</p></div>";
    container.append(empty);
    updateImportSelectionSummary();
    return;
  }

  preview.entries.forEach((entry, index) => {
    const row = document.createElement("label");
    row.className = "import-entry" + (entry.supported ? "" : " is-disabled");

    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.dataset.importIndex = String(index);
    checkbox.checked = !!entry.supported;
    checkbox.disabled = !entry.supported;
    checkbox.addEventListener("change", updateImportSelectionSummary);

    const main = document.createElement("span");
    main.className = "import-entry-main";
    const title = document.createElement("strong");
    title.textContent = entry.alias;
    const detail = document.createElement("span");
    const user = entry.user || "(当前系统用户)";
    let text = user + "@" + entry.host_name + ":" + entry.port;
    if (entry.identity_file) text += " · key " + entry.identity_file;
    if (entry.proxy_jump) text += " · via " + entry.proxy_jump;
    detail.textContent = text;
    main.append(title, detail);

    const note = document.createElement("span");
    note.className = "import-entry-note";
    note.textContent = entry.supported ? (entry.proxy_jump ? "Jump Host 引用" : "可导入") : (entry.warning || "不支持");

    row.append(checkbox, main, note);
    container.append(row);
  });
  updateImportSelectionSummary();
}

function selectedImportEntries() {
  if (!state.importPreview) return [];
  const selected = [];
  document.querySelectorAll("[data-import-index]:checked").forEach((checkbox) => {
    const index = Number(checkbox.dataset.importIndex);
    const entry = state.importPreview.entries[index];
    if (entry && entry.supported) selected.push(entry);
  });
  return selected;
}

function updateImportSelectionSummary() {
  const count = selectedImportEntries().length;
  $("#importSelectionSummary").textContent = count + " 个已选";
  $("#confirmImportButton").disabled = count === 0;
}

async function importSelectedSSHConfig() {
  const entries = selectedImportEntries();
  if (!entries.length) return;
  const errorBox = $("#importError");
  errorBox.classList.add("is-hidden");
  try {
    const next = await ImportSSHConfig(entries);
    state.settings = next.settings;
    $("#importDialog").close();
    renderSidebar();
    showToast("已导入 " + entries.length + " 个 SSH 连接");
  } catch (error) {
    errorBox.textContent = String(error);
    errorBox.classList.remove("is-hidden");
  }
}

function showHostKeyChallenge(challenge) {
  state.pendingHostKey = challenge;
  const changed = challenge.kind === "changed";
  $("#hostKeyTitle").textContent = changed ? "主机身份已变化" : "确认主机身份";
  $("#hostKeyDescription").textContent = changed
    ? challenge.address + " 返回了与历史记录不同的主机密钥。连接已阻止。"
    : "这是第一次连接到 " + challenge.address + "，请核对主机指纹。";
  $("#hostKeyFingerprint").textContent = challenge.algorithm + " · " + challenge.fingerprint;
  $("#previousFingerprintBox").classList.toggle("is-hidden", !changed);
  $("#hostKeyPreviousFingerprint").textContent = challenge.previous_fingerprint || "";
  $("#hostKeyWarning").textContent = changed
    ? "请通过可信渠道确认服务器确实更换了 Host Key。不要把“继续”当成默认动作。"
    : "确认指纹与服务器管理员提供的信息一致后，再决定是否保存信任记录。";
  $("#hostKeyWarning").className = "dk-message" + (changed ? " is-error" : "");

  const actions = $("#hostKeyActions");
  actions.replaceChildren();
  const reject = actionButton(changed ? "保持阻止" : "取消连接", "dk-button dk-button-secondary", () => resolveHostKey("reject"));
  actions.append(reject);
  if (changed) {
    actions.append(actionButton("我已核对，更新记录", "dk-button dk-button-danger", () => resolveHostKey("replace")));
  } else {
    actions.append(actionButton("仅本次信任", "dk-button dk-button-secondary", () => resolveHostKey("trust_once")));
    actions.append(actionButton("信任并保存", "dk-button dk-button-primary", () => resolveHostKey("trust_save")));
  }
  $("#hostKeyDialog").showModal();
}

function actionButton(label, className, handler) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = className;
  button.textContent = label;
  button.addEventListener("click", handler);
  return button;
}

async function resolveHostKey(action) {
  const challenge = state.pendingHostKey;
  if (!challenge) return;
  try {
    await ResolveHostKey(challenge.id, action);
  } catch (error) {
    showToast(String(error));
  } finally {
    state.pendingHostKey = null;
    $("#hostKeyDialog").close();
  }
}

async function closeSession(id) {
  const session = state.sessions.find((item) => item.id === id);
  if (!session) return;

  if (session.history_only) {
    state.sessions = state.sessions.filter((item) => item.id !== id);
    state.terminals.delete(id);
    if (state.activeSessionId === id) {
      state.activeSessionId = state.sessions.length ? state.sessions[state.sessions.length - 1].id : "";
    }
    renderAll();
    return;
  }

  if (["connected", "connecting", "reconnecting", "authenticating", "host_key_pending"].includes(session.state)) {
    const confirmed = window.confirm("当前会话仍在活动。断开并关闭这个标签？");
    if (!confirmed) return;
  }

  const terminalText = terminalFor(id).render(false);
  try {
    await CloseSession(id);
    rememberClosedSession(session, terminalText);
    state.sessions = state.sessions.filter((item) => item.id !== id);
    state.terminals.delete(id);
    if (state.activeSessionId === id) {
      state.activeSessionId = state.sessions.length ? state.sessions[state.sessions.length - 1].id : "";
    }
    renderAll();
  } catch (error) {
    showToast(String(error));
  }
}

function themeCatalogSourceLabel() {
  const source = state.themeCatalog.source || "builtin";
  if (source === "remote") return "远程最新";
  if (source === "cache") return "本地缓存";
  if (source === "builtin") return "Kit 内置 fallback";
  return source;
}

function renderThemePackOptions(preferred = "") {
  const select = $("#themeVariant");
  const wanted = preferred || (state.settings.theme && state.settings.theme.variant) || "aurora";
  select.replaceChildren();

  for (const pack of state.themePacks) {
    select.add(new Option(pack.display_name + " · " + pack.name, pack.name));
  }

  if (!state.themePacks.some((pack) => pack.name === wanted)) {
    const unavailable = new Option(wanted + " · 当前不可用", wanted);
    unavailable.disabled = true;
    select.add(unavailable);
  }

  select.value = wanted;
  updateThemePackDescription();
}

function updateThemePackDescription() {
  const selected = state.themePacks.find((pack) => pack.name === $("#themeVariant").value);
  const description = selected ? selected.description : "当前 Theme Pack 暂不可用。";
  const status = themeCatalogSourceLabel() + (state.themeCatalog.stale ? " · 目录可能已过期" : "");
  $("#themePackDescription").textContent = description + " · " + status;
  $("#themePackDescription").title = state.themeCatalog.last_error || "";
}

function openSettings() {
  const theme = state.settings.theme || { mode: "dark", variant: "aurora" };
  $("#themeMode").value = theme.mode || "dark";
  renderThemePackOptions(theme.variant || "aurora");

  $("#configDirPath").textContent = state.dataDir || "~/.config/ssh-client";
  $("#launchAtLogin").checked = state.launchAtLogin;
  $("#launchAtLogin").disabled = !state.launchAtLoginSupported;
  $("#settingsDialog").showModal();
}

async function refreshThemeCatalogFromSettings() {
  const button = $("#refreshThemeCatalogButton");
  const previous = button.textContent;
  button.disabled = true;
  button.textContent = "刷新中…";
  try {
    const catalog = await loadThemeCatalog(true);
    renderThemePackOptions($("#themeVariant").value || (state.settings.theme && state.settings.theme.variant) || "aurora");
    if (catalog) {
      await applyTheme();
      showToast("主题目录已刷新，共 " + state.themePacks.length + " 个主题");
    } else {
      showToast("远程主题刷新失败，继续使用 Kit 内置 fallback");
    }
  } finally {
    button.disabled = false;
    button.textContent = previous;
  }
}

async function saveSettings() {
  const theme = { mode: $("#themeMode").value, variant: $("#themeVariant").value };
  try {
    let next = await SetTheme(theme);
    state.settings = next.settings;
    next = await SetLaunchAtLogin($("#launchAtLogin").checked);
    state.settings = next.settings;
    state.launchAtLogin = !!next.launch_at_login;
    state.launchAtLoginSupported = !!next.launch_at_login_supported;
    await applyTheme();
    $("#settingsDialog").close();
    showToast("设置已保存");
  } catch (error) {
    showToast(String(error));
  }
}

let terminalResizeFrame = 0;
let terminalMeasureCanvas = null;

function terminalCellMetrics() {
  const viewport = $("#terminalViewport");
  const text = $("#terminalText");
  const style = getComputedStyle(text);
  const viewportStyle = getComputedStyle(viewport);
  const fontSize = Number.parseFloat(style.fontSize) || 13;
  let lineHeight = Number.parseFloat(style.lineHeight);
  if (!Number.isFinite(lineHeight)) lineHeight = fontSize * 1.55;

  if (!terminalMeasureCanvas) terminalMeasureCanvas = document.createElement("canvas");
  const context = terminalMeasureCanvas.getContext("2d");
  if (context) context.font = style.font || (fontSize + "px monospace");
  const measured = context ? context.measureText("M").width : fontSize * 0.61;
  const charWidth = Math.max(1, measured || fontSize * 0.61);

  return {
    charWidth,
    lineHeight,
    paddingLeft: Number.parseFloat(viewportStyle.paddingLeft) || 0,
    paddingRight: Number.parseFloat(viewportStyle.paddingRight) || 0,
    paddingTop: Number.parseFloat(viewportStyle.paddingTop) || 0,
    paddingBottom: Number.parseFloat(viewportStyle.paddingBottom) || 0
  };
}

function calculateTerminalSize() {
  const viewport = $("#terminalViewport");
  const metrics = terminalCellMetrics();
  const width = Math.max(1, viewport.clientWidth - metrics.paddingLeft - metrics.paddingRight);
  const height = Math.max(1, viewport.clientHeight - metrics.paddingTop - metrics.paddingBottom);
  const cols = Math.max(20, Math.floor(width / metrics.charWidth));
  const rows = Math.max(5, Math.floor(height / metrics.lineHeight));
  return { cols, rows, ...metrics };
}

function scheduleActiveTerminalResize() {
  if (terminalResizeFrame) return;
  terminalResizeFrame = window.requestAnimationFrame(() => {
    terminalResizeFrame = 0;
    resizeActiveTerminal();
  });
}

async function resizeActiveTerminal() {
  const session = activeSession();
  if (!session) return;
  const size = calculateTerminalSize();
  const terminal = terminalFor(session.id);
  terminal.resize(size.cols, size.rows);
  $("#terminalSizeText").textContent = size.cols + " × " + size.rows;
  renderTerminal();
  if (session.state === "connected") {
    try { await ResizeSession(session.id, size.cols, size.rows); } catch (_) {}
  }
}

async function writeSessionData(sessionId, data, quiet = false) {
  if (!sessionId || !data) return;
  try {
    await WriteSession(sessionId, data);
  } catch (error) {
    if (!quiet) showToast(String(error));
  }
}

async function sendTerminalData(data) {
  const session = activeSession();
  if (!session || session.state !== "connected" || !data) return;
  await writeSessionData(session.id, data);
}

function focusTerminalInput() {
  const input = $("#terminalImeInput");
  if (!input || $("#sessionView").classList.contains("is-hidden")) return;
  if (document.activeElement === input) return;
  input.value = "";
  try {
    input.focus({ preventScroll: true });
  } catch (_) {
    input.focus();
  }
}

function xtermModifier(event) {
  let modifier = 1;
  if (event.shiftKey) modifier += 1;
  if (event.altKey) modifier += 2;
  if (event.ctrlKey) modifier += 4;
  if (event.metaKey) modifier += 8;
  return modifier;
}

function keySequence(event, terminal = null) {
  if (event.getModifierState && event.getModifierState("AltGraph") && event.key.length === 1) return null;
  if (event.ctrlKey && event.key === "Backspace") return "\u0017";
  if (event.key === "Tab" && event.shiftKey) return "\u001b[Z";

  const modifier = xtermModifier(event);
  const modified = modifier !== 1;
  const applicationCursor = !!(terminal && terminal.applicationCursorKeys);
  const applicationKeypad = !!(terminal && terminal.applicationKeypad);

  const cursorFinal = {
    ArrowUp: "A", ArrowDown: "B", ArrowRight: "C", ArrowLeft: "D",
    Home: "H", End: "F"
  };
  if (cursorFinal[event.key]) {
    if (modified) return "\u001b[1;" + modifier + cursorFinal[event.key];
    if (applicationCursor) return "\u001bO" + cursorFinal[event.key];
    return "\u001b[" + cursorFinal[event.key];
  }

  const functionCodes = {
    F5: 15, F6: 17, F7: 18, F8: 19,
    F9: 20, F10: 21, F11: 23, F12: 24
  };
  const ss3Function = { F1: "P", F2: "Q", F3: "R", F4: "S" };
  if (ss3Function[event.key]) {
    return modified
      ? "\u001b[1;" + modifier + ss3Function[event.key]
      : "\u001bO" + ss3Function[event.key];
  }
  if (functionCodes[event.key]) {
    return modified
      ? "\u001b[" + functionCodes[event.key] + ";" + modifier + "~"
      : "\u001b[" + functionCodes[event.key] + "~";
  }

  const tildeCodes = { Insert: 2, Delete: 3, PageUp: 5, PageDown: 6 };
  if (tildeCodes[event.key]) {
    return modified
      ? "\u001b[" + tildeCodes[event.key] + ";" + modifier + "~"
      : "\u001b[" + tildeCodes[event.key] + "~";
  }

  if (applicationKeypad && event.code && event.code.startsWith("Numpad")) {
    const keypad = {
      Numpad0: "p", Numpad1: "q", Numpad2: "r", Numpad3: "s", Numpad4: "t",
      Numpad5: "u", Numpad6: "v", Numpad7: "w", Numpad8: "x", Numpad9: "y",
      NumpadDecimal: "n", NumpadDivide: "o", NumpadMultiply: "j",
      NumpadSubtract: "m", NumpadAdd: "k", NumpadEnter: "M"
    };
    if (keypad[event.code]) return "\u001bO" + keypad[event.code];
  }

  const direct = {
    Enter: "\r", Backspace: "\u007f", Tab: "\t", Escape: "\u001b"
  };
  if (direct[event.key]) return direct[event.key];

  if (event.ctrlKey && event.key.length === 1) {
    const code = event.key.toUpperCase().charCodeAt(0);
    if (code >= 64 && code <= 95) {
      const control = String.fromCharCode(code - 64);
      return event.altKey ? "\u001b" + control : control;
    }
  }

  if (!event.ctrlKey && !event.metaKey && event.key.length === 1) {
    return event.altKey ? "\u001b" + event.key : event.key;
  }
  return null;
}

function terminalMouseCell(event) {
  const viewport = $("#terminalViewport");
  const metrics = terminalCellMetrics();
  const rect = viewport.getBoundingClientRect();
  const x = Math.floor((event.clientX - rect.left - metrics.paddingLeft) / metrics.charWidth) + 1;
  const y = Math.floor((event.clientY - rect.top - metrics.paddingTop) / metrics.lineHeight) + 1;
  const session = activeSession();
  const terminal = session ? terminalFor(session.id) : null;
  return {
    x: Math.max(1, Math.min(terminal ? terminal.cols : 999, x)),
    y: Math.max(1, Math.min(terminal ? terminal.rows : 999, y))
  };
}

function terminalMouseSequence(event, kind) {
  const session = activeSession();
  if (!session || session.state !== "connected" || event.shiftKey) return null;
  const terminal = terminalFor(session.id);
  if (!terminal.mouseTracking) return null;

  if (kind === "move") {
    if (terminal.mouseTracking === 1000) return null;
    if (terminal.mouseTracking === 1002 && !event.buttons) return null;
  }

  let button = 3;
  if (kind === "wheel") {
    button = event.deltaY < 0 ? 64 : 65;
  } else if (kind === "down") {
    button = event.button === 0 ? 0 : (event.button === 1 ? 1 : 2);
  } else if (kind === "move") {
    if (event.buttons & 1) button = 0;
    else if (event.buttons & 4) button = 1;
    else if (event.buttons & 2) button = 2;
    button += 32;
  }

  if (event.altKey) button += 8;
  if (event.ctrlKey) button += 16;
  const cell = terminalMouseCell(event);

  if (terminal.sgrMouse) {
    const suffix = kind === "up" ? "m" : "M";
    return "\u001b[<" + button + ";" + cell.x + ";" + cell.y + suffix;
  }
  const legacyButton = Math.max(0, Math.min(223, button + 32));
  const legacyX = Math.max(32, Math.min(255, cell.x + 32));
  const legacyY = Math.max(32, Math.min(255, cell.y + 32));
  return "\u001b[M" + String.fromCharCode(legacyButton, legacyX, legacyY);
}

function bindEvents() {
  EventsOn("ssh:session-state", (snapshot) => updateSession(snapshot));
  EventsOn("app:settings-changed", (settings) => {
    state.settings = settings;
    renderSidebar();
    if (activeSession()) renderActiveSession();
    if ($("#profileDialog").open) updatePrivateKeyVisibility();
  });
  EventsOn("app:credential-save-error", (payload) => {
    showToast(payload && payload.message ? payload.message : "密码未能保存到安全存储");
  });
  EventsOn("ssh:output", (payload) => {
    const terminal = terminalFor(payload.session_id);
    const responses = terminal.feed(decodeOutput(payload.data_base64, payload.session_id));
    for (const response of responses) writeSessionData(payload.session_id, response, true);
    observeRemotePrompt(payload.session_id, terminal);
    if (payload.session_id === state.activeSessionId) renderTerminal();
  });
  EventsOn("ssh:host-key", (challenge) => showHostKeyChallenge(challenge));
  EventsOn("ssh:session-closed", (payload) => {
    state.sessions = state.sessions.filter((session) => session.id !== payload.session_id);
    state.terminals.delete(payload.session_id);
    state.decoders.delete(payload.session_id);
    state.promptPrefixes.delete(payload.session_id);
    if (state.activeSessionId === payload.session_id) {
      state.activeSessionId = state.sessions.length ? state.sessions[state.sessions.length - 1].id : "";
    }
    renderAll();
  });

  $("#newProfileButton").addEventListener("click", () => openProfileDialog());
  $("#importConfigButton").addEventListener("click", openImportDialog);
  $("#welcomeNewButton").addEventListener("click", () => openProfileDialog());
  $("#quickConnectButton").addEventListener("click", openQuickDialog);
  $("#welcomeQuickButton").addEventListener("click", openQuickDialog);
  $("#settingsButton").addEventListener("click", openSettings);
  $("#refreshButton").addEventListener("click", () => loadState().catch((error) => showToast(String(error))));
  $("#commandButton").addEventListener("click", openCommandPalette);
  $("#commandSearch").addEventListener("input", () => {
    commandSelection = 0;
    renderCommandPalette();
  });
  $("#commandSearch").addEventListener("keydown", (event) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      if (visibleCommandItems.length) {
        commandSelection = (commandSelection + 1) % visibleCommandItems.length;
        renderCommandPalette();
      }
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      if (visibleCommandItems.length) {
        commandSelection = (commandSelection - 1 + visibleCommandItems.length) % visibleCommandItems.length;
        renderCommandPalette();
      }
    } else if (event.key === "Enter") {
      event.preventDefault();
      executeCommand();
    }
  });

  document.querySelectorAll("[data-nav]").forEach((button) => {
    button.addEventListener("click", () => {
      document.querySelectorAll("[data-nav]").forEach((item) => item.classList.remove("is-active"));
      button.classList.add("is-active");
      state.nav = button.dataset.nav;
      renderSidebar();
    });
  });

  $("#connectionSearch").addEventListener("input", (event) => {
    state.search = event.target.value;
    renderSidebar();
  });

  $("#profileAuthMode").addEventListener("change", updatePrivateKeyVisibility);
  $("#profileForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    await saveProfile();
  });
  const closeProfileDialog = () => {
    if ($("#profileDialog").open) $("#profileDialog").close();
  };
  $("#profileCloseButton").addEventListener("click", closeProfileDialog);
  $("#profileCancelButton").addEventListener("click", closeProfileDialog);

  $("#manageGroupsButton").addEventListener("click", openGroupDialog);
  $("#createGroupButton").addEventListener("click", createGroupFromDialog);
  $("#newGroupName").addEventListener("keydown", (event) => {
    if (event.key === "Enter") {
      event.preventDefault();
      createGroupFromDialog();
    }
  });
  const closeGroupDialog = () => {
    if ($("#groupDialog").open) $("#groupDialog").close();
  };
  $("#groupCloseButton").addEventListener("click", closeGroupDialog);
  $("#groupDoneButton").addEventListener("click", closeGroupDialog);

  $("#clearSavedPasswordButton").addEventListener("click", async () => {
    const id = $("#profileId").value;
    if (!id) return;
    try {
      const next = await ClearSavedCredential(id);
      state.settings = next.settings;
      updatePrivateKeyVisibility();
      renderSidebar();
      showToast("已清除保存的密码");
    } catch (error) {
      showToast(String(error));
    }
  });
  $("#deleteProfileButton").addEventListener("click", async () => {
    const id = $("#profileId").value;
    if (!id || !window.confirm("删除这个 Connection Profile？活动 Session 不会被强制关闭。")) return;
    try {
      const next = await DeleteProfile(id);
      state.settings = next.settings;
      if (state.selectedProfileId === id) state.selectedProfileId = "";
      $("#profileDialog").close();
      renderSidebar();
      showToast("连接已删除");
    } catch (error) {
      $("#profileError").textContent = String(error);
      $("#profileError").classList.remove("is-hidden");
    }
  });
  $("#choosePrivateKeyButton").addEventListener("click", async () => {
    try {
      const path = await ChoosePrivateKey();
      if (path) $("#profilePrivateKey").value = path;
    } catch (error) {
      showToast(String(error));
    }
  });

  $("#credentialsForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!state.pendingProfile) return;
    const profile = state.pendingProfile;
    const credentials = { password: $("#connectPassword").value, passphrase: $("#connectPassphrase").value };
    const rememberPassword = profile.auth.mode === "password" && $("#rememberPassword").checked;
    const started = await connectProfileNow(profile, credentials, rememberPassword);
    if (started) state.pendingProfile = null;
  });
  const closeCredentials = () => {
    state.pendingProfile = null;
    if ($("#credentialsDialog").open) $("#credentialsDialog").close();
  };
  $("#credentialsCloseButton").addEventListener("click", closeCredentials);
  $("#credentialsCancelButton").addEventListener("click", closeCredentials);

  $("#quickAuthMode").addEventListener("change", updateQuickAuthFields);
  $("#quickStartButton").addEventListener("click", startQuickConnect);

  $("#chooseConfigButton").addEventListener("click", async () => {
    try {
      const path = await ChooseSSHConfig();
      if (path) $("#importConfigPath").value = path;
    } catch (error) {
      $("#importError").textContent = String(error);
      $("#importError").classList.remove("is-hidden");
    }
  });
  $("#previewConfigButton").addEventListener("click", previewSSHConfig);
  $("#confirmImportButton").addEventListener("click", importSelectedSSHConfig);
  $("#cancelImportButton").addEventListener("click", () => $("#importDialog").close());
  $("#closeImportButton").addEventListener("click", () => $("#importDialog").close());

  $("#disconnectSessionButton").addEventListener("click", async () => {
    const session = activeSession();
    if (!session) return;
    try { await DisconnectSession(session.id); } catch (error) { showToast(String(error)); }
  });

  $("#retrySessionButton").addEventListener("click", async () => {
    const session = activeSession();
    if (!session) return;
    try { await RetrySession(session.id); } catch (error) { showToast(String(error)); }
  });

  $("#closeSessionButton").addEventListener("click", () => {
    const session = activeSession();
    if (session) closeSession(session.id);
  });

  $("#themeVariant").addEventListener("change", updateThemePackDescription);
  $("#refreshThemeCatalogButton").addEventListener("click", refreshThemeCatalogFromSettings);
  $("#saveSettingsButton").addEventListener("click", saveSettings);

  $("#hostKeyDialog").addEventListener("cancel", (event) => {
    event.preventDefault();
    resolveHostKey("reject");
  });

  $("#sidebarToggleButton").addEventListener("click", () => {
    applySidebarCollapsed(!state.sidebarCollapsed);
  });
  bindSidebarResize();
  applySidebarWidth(Number.parseInt(window.localStorage.getItem("ssh-client.sidebar-width"), 10) || SIDEBAR_DEFAULT_WIDTH, false);
  applySidebarCollapsed(window.localStorage.getItem("ssh-client.sidebar-collapsed") === "1", false);

  const viewport = $("#terminalViewport");
  const imeInput = $("#terminalImeInput");

  imeInput.addEventListener("compositionstart", () => {
    terminalImeComposing = true;
    terminalImeSuppressInput = false;
  });
  imeInput.addEventListener("compositionend", async (event) => {
    terminalImeComposing = false;
    const text = imeInput.value || event.data || "";
    imeInput.value = "";
    if (!text) return;
    terminalImeSuppressInput = true;
    await sendTerminalData(text);
    window.setTimeout(() => { terminalImeSuppressInput = false; }, 0);
  });
  imeInput.addEventListener("input", async (event) => {
    if (terminalImeComposing || event.isComposing) return;
    if (terminalImeSuppressInput) {
      imeInput.value = "";
      return;
    }
    const text = imeInput.value;
    imeInput.value = "";
    if (text) await sendTerminalData(text);
  });
  viewport.addEventListener("click", () => {
    const selection = window.getSelection();
    if (selection && selection.toString()) return;
    focusTerminalInput();
  });

  viewport.addEventListener("keydown", async (event) => {
    if (event.isComposing || event.key === "Process" || event.keyCode === 229) return;
    const key = event.key.toLowerCase();
    const session = activeSession();
    const terminal = session ? terminalFor(session.id) : null;
    const applicationMode = !!(terminal && terminal.alternateScreen);

    // In full-screen terminal applications (vim/tmux/top/less...), the remote
    // application owns all ordinary control keys and cursor semantics.
    if (applicationMode) {
      if (event.ctrlKey && event.shiftKey && key === "c") {
        event.preventDefault();
        await copyTerminalSelection();
        return;
      }
      if ((event.ctrlKey && event.shiftKey && key === "v") || (event.shiftKey && event.key === "Insert")) {
        event.preventDefault();
        await pasteTerminalClipboard();
        return;
      }

      const sequence = keySequence(event, terminal);
      if (sequence !== null) {
        event.preventDefault();
        await sendTerminalData(sequence);
      }
      return;
    }

    // Convenience clipboard behavior is only applied in an ordinary shell.
    // With no selection Ctrl+C remains the shell interrupt character.
    if (event.ctrlKey && !event.altKey && key === "c") {
      const selectedText = terminalSelectionText();
      event.preventDefault();
      if (selectedText) {
        await copyTerminalSelection(selectedText);
      } else {
        await sendTerminalData("\u0003");
      }
      return;
    }

    if ((event.ctrlKey && !event.altKey && key === "v") || (event.shiftKey && event.key === "Insert")) {
      event.preventDefault();
      await pasteTerminalClipboard();
      return;
    }

    if (event.ctrlKey && event.key === "Insert") {
      event.preventDefault();
      await copyTerminalSelection();
      return;
    }

    if (event.ctrlKey && event.shiftKey && key === "a") {
      event.preventDefault();
      selectAllTerminalText();
      return;
    }

    // Command history is observed from the rendered remote prompt just before
    // Enter is forwarded. No local readline/history state is simulated.
    if (event.key === "Enter") {
      captureRenderedCommand(session, terminal);
    }

    const sequence = keySequence(event, terminal);
    if (sequence !== null) {
      event.preventDefault();
      await sendTerminalData(sequence);
    }
  });

  viewport.addEventListener("focusin", () => {
    if (terminalFocusInside) return;
    terminalFocusInside = true;
    const session = activeSession();
    if (!session) return;
    const terminal = terminalFor(session.id);
    renderTerminal();
    if (session.state === "connected" && terminal.focusReporting) {
      writeSessionData(session.id, "\u001b[I", true);
    }
  });

  viewport.addEventListener("focusout", (event) => {
    if (event.relatedTarget && viewport.contains(event.relatedTarget)) return;
    terminalFocusInside = false;
    const session = activeSession();
    if (!session) return;
    const terminal = terminalFor(session.id);
    renderTerminal();
    if (session.state === "connected" && terminal.focusReporting) {
      writeSessionData(session.id, "\u001b[O", true);
    }
  });

  viewport.addEventListener("pointerdown", (event) => {
    const sequence = terminalMouseSequence(event, "down");
    if (!sequence) return;
    event.preventDefault();
    focusTerminalInput();
    writeSessionData(activeSession().id, sequence, true);
  });

  viewport.addEventListener("pointerup", (event) => {
    const sequence = terminalMouseSequence(event, "up");
    if (!sequence) return;
    event.preventDefault();
    writeSessionData(activeSession().id, sequence, true);
  });

  viewport.addEventListener("pointermove", (event) => {
    const sequence = terminalMouseSequence(event, "move");
    if (!sequence) return;
    event.preventDefault();
    writeSessionData(activeSession().id, sequence, true);
  });

  viewport.addEventListener("wheel", (event) => {
    const sequence = terminalMouseSequence(event, "wheel");
    if (!sequence) return;
    event.preventDefault();
    writeSessionData(activeSession().id, sequence, true);
  }, { passive: false });

  viewport.addEventListener("contextmenu", (event) => {
    if (event.shiftKey) return;
    event.preventDefault();
    showTerminalContextMenu(event);
  });

  $("#terminalContextMenu").querySelectorAll("[data-terminal-action]").forEach((button) => {
    button.addEventListener("click", async () => {
      const action = button.dataset.terminalAction;
      hideTerminalContextMenu();
      if (action === "copy") await copyTerminalSelection(state.terminalContextSelection);
      if (action === "paste") await pasteTerminalClipboard();
      if (action === "select-all") selectAllTerminalText();
    });
  });

  document.addEventListener("pointerdown", (event) => {
    if (!$("#terminalContextMenu").contains(event.target)) hideTerminalContextMenu();
  });
  window.addEventListener("blur", hideTerminalContextMenu);
  window.addEventListener("resize", hideTerminalContextMenu);

  const resizeObserver = new ResizeObserver(() => scheduleActiveTerminalResize());
  resizeObserver.observe(viewport);

  window.addEventListener("keydown", (event) => {
    const terminalFocused = viewport.contains(document.activeElement);
    if (terminalFocused) return;

    if (event.ctrlKey && !event.shiftKey && event.key.toLowerCase() === "k") {
      event.preventDefault();
      openCommandPalette();
    }
    if (event.ctrlKey && !event.shiftKey && event.key.toLowerCase() === "n") {
      event.preventDefault();
      openProfileDialog();
    }
    if (event.ctrlKey && !event.shiftKey && event.key.toLowerCase() === "t") {
      event.preventDefault();
      openQuickDialog();
    }
  });
}

loadSessionHistory();
bindEvents();
loadState().catch((error) => {
  showToast("初始化失败：" + error);
  console.error(error);
});
