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

const decoder = new TextDecoder("utf-8");
let toastTimer = 0;
let commandSelection = 0;
let visibleCommandItems = [];

class TerminalBuffer {
  constructor(cols = 100, rows = 30, scrollbackLimit = 10000) {
    this.cols = cols;
    this.rows = rows;
    this.scrollbackLimit = scrollbackLimit;
    this.screen = Array.from({ length: rows }, () => this.blankRow(cols));
    this.scrollback = [];
    this.row = 0;
    this.col = 0;
    this.savedRow = 0;
    this.savedCol = 0;
    this.mode = "normal";
    this.sequence = "";
    this.bracketedPaste = false;
    this.alternateScreen = false;
    this.applicationCursorKeys = false;
    this.cursorVisible = true;
    this.mainScreenState = null;
    this.scrollTop = 0;
    this.scrollBottom = rows - 1;
  }

  blankRow(cols = this.cols) {
    return Array.from({ length: cols }, () => " ");
  }

  enterAlternateScreen() {
    if (this.alternateScreen) return;
    this.mainScreenState = {
      screen: this.screen.map((row) => row.slice()),
      scrollback: this.scrollback.slice(),
      row: this.row,
      col: this.col,
      savedRow: this.savedRow,
      savedCol: this.savedCol,
      scrollTop: this.scrollTop,
      scrollBottom: this.scrollBottom
    };
    this.alternateScreen = true;
    this.screen = Array.from({ length: this.rows }, () => this.blankRow());
    this.scrollback = [];
    this.row = 0;
    this.col = 0;
    this.savedRow = 0;
    this.savedCol = 0;
    this.scrollTop = 0;
    this.scrollBottom = this.rows - 1;
  }

  exitAlternateScreen() {
    if (!this.alternateScreen) return;
    const saved = this.mainScreenState;
    this.alternateScreen = false;
    this.mainScreenState = null;
    if (!saved) return;

    this.screen = saved.screen.map((row) => {
      if (row.length > this.cols) return row.slice(0, this.cols);
      if (row.length < this.cols) return row.concat(Array.from({ length: this.cols - row.length }, () => " "));
      return row;
    });
    while (this.screen.length < this.rows) this.screen.push(this.blankRow());
    while (this.screen.length > this.rows) this.screen.shift();

    this.scrollback = saved.scrollback.slice(-this.scrollbackLimit);
    this.row = Math.max(0, Math.min(saved.row, this.rows - 1));
    this.col = Math.max(0, Math.min(saved.col, this.cols - 1));
    this.savedRow = Math.max(0, Math.min(saved.savedRow, this.rows - 1));
    this.savedCol = Math.max(0, Math.min(saved.savedCol, this.cols - 1));
    this.scrollTop = 0;
    this.scrollBottom = this.rows - 1;
  }

  scrollRegionUp(count = 1) {
    const top = Math.max(0, Math.min(this.scrollTop, this.rows - 1));
    const bottom = Math.max(top, Math.min(this.scrollBottom, this.rows - 1));
    for (let i = 0; i < count; i++) {
      const removed = this.screen.splice(top, 1)[0];
      this.screen.splice(bottom, 0, this.blankRow());
      if (!this.alternateScreen && top === 0 && bottom === this.rows - 1 && removed) {
        this.scrollback.push(removed.join("").replace(/\s+$/, ""));
        this.trimScrollback();
      }
    }
  }

  scrollRegionDown(count = 1) {
    const top = Math.max(0, Math.min(this.scrollTop, this.rows - 1));
    const bottom = Math.max(top, Math.min(this.scrollBottom, this.rows - 1));
    for (let i = 0; i < count; i++) {
      this.screen.splice(bottom, 1);
      this.screen.splice(top, 0, this.blankRow());
    }
  }

  resize(cols, rows) {
    cols = Math.max(20, Math.floor(cols));
    rows = Math.max(5, Math.floor(rows));
    if (cols === this.cols && rows === this.rows) return;

    this.screen = this.screen.map((row) => {
      if (row.length > cols) return row.slice(0, cols);
      if (row.length < cols) return row.concat(Array.from({ length: cols - row.length }, () => " "));
      return row;
    });

    while (this.screen.length < rows) this.screen.push(this.blankRow(cols));
    while (this.screen.length > rows) {
      this.scrollback.push(this.screen.shift().join("").replace(/\s+$/, ""));
    }

    this.cols = cols;
    this.rows = rows;
    this.row = Math.max(0, Math.min(this.row, rows - 1));
    this.col = Math.max(0, Math.min(this.col, cols - 1));
    this.scrollTop = Math.max(0, Math.min(this.scrollTop, rows - 1));
    this.scrollBottom = Math.max(this.scrollTop, Math.min(this.scrollBottom, rows - 1));
    if (this.scrollBottom < this.scrollTop || this.scrollBottom >= rows) {
      this.scrollTop = 0;
      this.scrollBottom = rows - 1;
    }
    this.trimScrollback();
  }

  feed(text) {
    for (const ch of text) {
      if (this.mode === "osc") {
        if (ch === "\u0007") {
          this.mode = "normal";
          this.sequence = "";
        } else if (ch === "\u001b") {
          this.mode = "osc-esc";
        }
        continue;
      }
      if (this.mode === "osc-esc") {
        this.mode = ch === "\\" ? "normal" : "osc";
        continue;
      }
      if (this.mode === "charset") {
        this.mode = "normal";
        continue;
      }
      if (this.mode === "esc") {
        if (ch === "[") {
          this.mode = "csi";
          this.sequence = "";
        } else if (ch === "]") {
          this.mode = "osc";
          this.sequence = "";
        } else if ("()*+-%#".includes(ch)) {
          this.mode = "charset";
        } else if (ch === "7") {
          this.savedRow = this.row;
          this.savedCol = this.col;
          this.mode = "normal";
        } else if (ch === "8") {
          this.row = this.savedRow;
          this.col = this.savedCol;
          this.mode = "normal";
        } else if (ch === "D") {
          this.indexDown();
          this.mode = "normal";
        } else if (ch === "E") {
          this.col = 0;
          this.indexDown();
          this.mode = "normal";
        } else if (ch === "M") {
          this.reverseIndex();
          this.mode = "normal";
        } else if (ch === "c") {
          this.screen = Array.from({ length: this.rows }, () => this.blankRow());
          this.scrollback = [];
          this.row = 0;
          this.col = 0;
          this.savedRow = 0;
          this.savedCol = 0;
          this.scrollTop = 0;
          this.scrollBottom = this.rows - 1;
          this.applicationCursorKeys = false;
          this.bracketedPaste = false;
          this.mode = "normal";
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
        } else {
          this.sequence += ch;
        }
        continue;
      }

      if (ch === "\u001b") {
        this.mode = "esc";
      } else if (ch === "\r") {
        this.col = 0;
      } else if (ch === "\n") {
        this.newline();
      } else if (ch === "\b") {
        this.col = Math.max(0, this.col - 1);
      } else if (ch === "\t") {
        this.col = Math.min(this.cols - 1, Math.ceil((this.col + 1) / 8) * 8);
      } else if (ch >= " ") {
        this.writeChar(ch);
      }
    }
  }

  handleCSI(raw, final) {
    const privateMode = raw.startsWith("?");
    const clean = raw.replace(/^\?/, "");
    const params = clean === "" ? [] : clean.split(";").map((value) => Number(value || 0));
    const first = params[0] || 0;

    if (privateMode && (final === "h" || final === "l")) {
      const enabled = final === "h";
      if (params.includes(2004)) this.bracketedPaste = enabled;
      if (params.includes(1)) this.applicationCursorKeys = enabled;
      if (params.includes(25)) this.cursorVisible = enabled;
      if (params.includes(47) || params.includes(1047) || params.includes(1049)) {
        if (enabled) this.enterAlternateScreen();
        else this.exitAlternateScreen();
      }
      return;
    }

    switch (final) {
      case "A":
        this.row = Math.max(0, this.row - (first || 1));
        break;
      case "B":
        this.row = Math.min(this.rows - 1, this.row + (first || 1));
        break;
      case "C":
        this.col = Math.min(this.cols - 1, this.col + (first || 1));
        break;
      case "D":
        this.col = Math.max(0, this.col - (first || 1));
        break;
      case "E":
        this.row = Math.min(this.rows - 1, this.row + (first || 1));
        this.col = 0;
        break;
      case "F":
        this.row = Math.max(0, this.row - (first || 1));
        this.col = 0;
        break;
      case "G":
        this.col = Math.max(0, Math.min(this.cols - 1, (first || 1) - 1));
        break;
      case "H":
      case "f": {
        const r = (params[0] || 1) - 1;
        const c = (params[1] || 1) - 1;
        this.row = Math.max(0, Math.min(this.rows - 1, r));
        this.col = Math.max(0, Math.min(this.cols - 1, c));
        break;
      }
      case "J":
        if (first === 2 || first === 3) {
          this.screen = Array.from({ length: this.rows }, () => this.blankRow());
          this.row = 0;
          this.col = 0;
          if (first === 3) this.scrollback = [];
        } else if (first === 0) {
          for (let c = this.col; c < this.cols; c++) this.screen[this.row][c] = " ";
          for (let r = this.row + 1; r < this.rows; r++) this.screen[r] = this.blankRow();
        }
        break;
      case "K":
        if (first === 1) {
          for (let c = 0; c <= this.col; c++) this.screen[this.row][c] = " ";
        } else if (first === 2) {
          this.screen[this.row] = this.blankRow();
        } else {
          for (let c = this.col; c < this.cols; c++) this.screen[this.row][c] = " ";
        }
        break;
      case "P": {
        const count = first || 1;
        const row = this.screen[this.row];
        row.splice(this.col, count);
        while (row.length < this.cols) row.push(" ");
        break;
      }
      case "@": {
        const count = first || 1;
        const row = this.screen[this.row];
        row.splice(this.col, 0, ...Array.from({ length: count }, () => " "));
        row.length = this.cols;
        break;
      }
      case "X": {
        const count = first || 1;
        for (let c = this.col; c < Math.min(this.cols, this.col + count); c++) this.screen[this.row][c] = " ";
        break;
      }
      case "L": {
        const count = Math.min(first || 1, this.scrollBottom - this.row + 1);
        if (this.row >= this.scrollTop && this.row <= this.scrollBottom) {
          for (let i = 0; i < count; i++) {
            this.screen.splice(this.row, 0, this.blankRow());
            this.screen.splice(this.scrollBottom + 1, 1);
          }
        }
        break;
      }
      case "M": {
        const count = Math.min(first || 1, this.scrollBottom - this.row + 1);
        if (this.row >= this.scrollTop && this.row <= this.scrollBottom) {
          for (let i = 0; i < count; i++) {
            this.screen.splice(this.row, 1);
            this.screen.splice(this.scrollBottom, 0, this.blankRow());
          }
        }
        break;
      }
      case "S":
        this.scrollRegionUp(first || 1);
        break;
      case "T":
        this.scrollRegionDown(first || 1);
        break;
      case "d":
        this.row = Math.max(0, Math.min(this.rows - 1, (first || 1) - 1));
        break;
      case "e":
        this.row = Math.max(0, Math.min(this.rows - 1, this.row + (first || 1)));
        break;
      case "r": {
        const top = Math.max(0, Math.min(this.rows - 1, (params[0] || 1) - 1));
        const bottom = Math.max(top, Math.min(this.rows - 1, (params[1] || this.rows) - 1));
        this.scrollTop = top;
        this.scrollBottom = bottom;
        this.row = 0;
        this.col = 0;
        break;
      }
      case "s":
        this.savedRow = this.row;
        this.savedCol = this.col;
        break;
      case "u":
        this.row = this.savedRow;
        this.col = this.savedCol;
        break;
      default:
        break;
    }
  }

  writeChar(ch) {
    if (this.col >= this.cols) this.newline();
    this.screen[this.row][this.col] = ch;
    this.col += 1;
    if (this.col >= this.cols) {
      this.col = 0;
      this.newline();
    }
  }

  indexDown() {
    if (this.row === this.scrollBottom) {
      this.scrollRegionUp(1);
      return;
    }
    this.row = Math.min(this.rows - 1, this.row + 1);
  }

  reverseIndex() {
    if (this.row === this.scrollTop) {
      this.scrollRegionDown(1);
      return;
    }
    this.row = Math.max(0, this.row - 1);
  }

  newline() {
    this.indexDown();
  }

  trimScrollback() {
    if (this.scrollback.length > this.scrollbackLimit) {
      this.scrollback.splice(0, this.scrollback.length - this.scrollbackLimit);
    }
  }

  loadText(text) {
    const lines = String(text || "").replace(/\r/g, "").split("\n");
    this.screen = Array.from({ length: this.rows }, () => this.blankRow());
    this.scrollback = [];
    const visible = lines.slice(-this.rows);
    const earlier = lines.slice(0, Math.max(0, lines.length - this.rows));
    this.scrollback = earlier.slice(-this.scrollbackLimit);
    for (let r = 0; r < visible.length; r++) {
      const chars = Array.from(visible[r]).slice(0, this.cols);
      for (let c = 0; c < chars.length; c++) this.screen[r][c] = chars[c];
    }
    this.row = Math.max(0, Math.min(visible.length - 1, this.rows - 1));
    this.col = Math.min(this.cols - 1, Array.from(visible[this.row] || "").length);
  }

  render(cursorVisible) {
    const screenLines = this.screen.map((row) => row.join("").replace(/\s+$/, ""));
    if (cursorVisible && this.cursorVisible) {
      let line = screenLines[this.row] || "";
      if (line.length < this.col) line += " ".repeat(this.col - line.length);
      if (this.col >= line.length) {
        line += "█";
      } else {
        line = line.slice(0, this.col) + "█" + line.slice(this.col + 1);
      }
      screenLines[this.row] = line;
    }
    return this.scrollback.concat(screenLines).join("\n");
  }
}

function terminalFor(sessionId) {
  let terminal = state.terminals.get(sessionId);
  if (!terminal) {
    terminal = new TerminalBuffer();
    state.terminals.set(sessionId, terminal);
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
  window.setTimeout(() => $("#terminalViewport").focus(), 0);
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
  $("#terminalViewport").focus();
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
    const items = filtered.filter((profile) => profile.group_id === group.id);
    if (items.length) groups.push({ id: group.id, name: group.name, profiles: items });
  }
  const ungrouped = filtered.filter((profile) => !profile.group_id);
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
  $("#terminalViewport").focus();
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
  for (const session of state.sessions) {
    const tab = document.createElement("button");
    tab.type = "button";
    tab.className = "session-tab" + (session.id === state.activeSessionId ? " is-active" : "");

    const dot = document.createElement("span");
    dot.className = "connection-status " + session.state;
    const title = document.createElement("span");
    title.className = "session-tab-title";
    title.textContent = session.name + (session.state === "reconnecting" ? " · 重连中" : "");
    const close = document.createElement("button");
    close.type = "button";
    close.className = "session-tab-close";
    close.textContent = "×";
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
  window.setTimeout(() => $("#terminalViewport").focus(), 0);
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

function renderTerminalContent(element, text) {
  const fragment = document.createDocumentFragment();
  const promptPattern = /^([^\s@\n]+@[^:\s\n]+)(:)([^\s\n]*?)([$#>%])(?=[ \t█]|$)/gm;
  let cursor = 0;
  let match;

  while ((match = promptPattern.exec(text)) !== null) {
    if (match.index > cursor) {
      fragment.append(document.createTextNode(text.slice(cursor, match.index)));
    }

    const prompt = document.createElement("span");
    prompt.className = "terminal-prompt";

    const userHost = document.createElement("span");
    userHost.className = "terminal-prompt-userhost";
    userHost.textContent = match[1];

    const path = document.createElement("span");
    path.className = "terminal-prompt-path";
    path.textContent = match[2] + match[3];

    const sigil = document.createElement("span");
    sigil.className = "terminal-prompt-sigil";
    sigil.textContent = match[4];

    prompt.append(userHost, path, sigil);
    fragment.append(prompt);
    cursor = match.index + match[0].length;
  }

  if (cursor < text.length) {
    fragment.append(document.createTextNode(text.slice(cursor)));
  }

  element.replaceChildren(fragment);
}

function renderTerminal() {
  const session = activeSession();
  if (!session) return;
  const viewport = $("#terminalViewport");
  const terminal = terminalFor(session.id);
  const stick = viewport.scrollTop + viewport.clientHeight >= viewport.scrollHeight - 48;
  renderTerminalContent($("#terminalText"), terminal.render(session.state === "connected"));
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

function decodeOutput(encoded) {
  const binary = atob(encoded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return decoder.decode(bytes, { stream: true });
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
    terminal: existing && existing.terminal ? existing.terminal : {
      term: "xterm-256color",
      encoding: "UTF-8",
      font_family: "Cascadia Code",
      font_size: 13,
      color_scheme: "midnight",
      scrollback_lines: 10000,
      cursor_style: "block"
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
          font_size: 13, color_scheme: "midnight", scrollback_lines: 10000, cursor_style: "block"
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

function calculateTerminalSize() {
  const viewport = $("#terminalViewport");
  const style = getComputedStyle($("#terminalText"));
  const fontSize = Number.parseFloat(style.fontSize) || 12.5;
  const lineHeight = Number.parseFloat(style.lineHeight) || fontSize * 1.55;
  const charWidth = fontSize * 0.61;
  const cols = Math.max(20, Math.floor((viewport.clientWidth - 28) / charWidth));
  const rows = Math.max(5, Math.floor((viewport.clientHeight - 30) / lineHeight));
  return { cols, rows };
}

async function resizeActiveTerminal() {
  const session = activeSession();
  if (!session) return;
  const size = calculateTerminalSize();
  terminalFor(session.id).resize(size.cols, size.rows);
  $("#terminalSizeText").textContent = size.cols + " × " + size.rows;
  renderTerminal();
  if (session.state === "connected") {
    try { await ResizeSession(session.id, size.cols, size.rows); } catch (_) {}
  }
}

async function sendTerminalData(data) {
  const session = activeSession();
  if (!session || session.state !== "connected" || !data) return;
  try {
    await WriteSession(session.id, data);
  } catch (error) {
    showToast(String(error));
  }
}

function keySequence(event, terminal = null) {
  if (event.ctrlKey && event.key === "ArrowLeft") return "\u001b[1;5D";
  if (event.ctrlKey && event.key === "ArrowRight") return "\u001b[1;5C";
  if (event.ctrlKey && event.key === "ArrowUp") return "\u001b[1;5A";
  if (event.ctrlKey && event.key === "ArrowDown") return "\u001b[1;5B";
  if (event.ctrlKey && event.key === "Backspace") return "\u0017";
  if (event.ctrlKey && event.key.length === 1) {
    const code = event.key.toUpperCase().charCodeAt(0);
    if (code >= 64 && code <= 95) return String.fromCharCode(code - 64);
  }

  const applicationCursor = !!(terminal && terminal.applicationCursorKeys);
  const keys = {
    Enter: "\r",
    Backspace: "\u007f",
    Tab: "\t",
    Escape: "\u001b",
    ArrowUp: applicationCursor ? "\u001bOA" : "\u001b[A",
    ArrowDown: applicationCursor ? "\u001bOB" : "\u001b[B",
    ArrowRight: applicationCursor ? "\u001bOC" : "\u001b[C",
    ArrowLeft: applicationCursor ? "\u001bOD" : "\u001b[D",
    Home: applicationCursor ? "\u001bOH" : "\u001b[H",
    End: applicationCursor ? "\u001bOF" : "\u001b[F",
    Delete: "\u001b[3~",
    Insert: "\u001b[2~",
    PageUp: "\u001b[5~",
    PageDown: "\u001b[6~"
  };
  if (keys[event.key]) return keys[event.key];
  if (!event.ctrlKey && !event.metaKey && event.key.length === 1) {
    return event.altKey ? "\u001b" + event.key : event.key;
  }
  return null;
}

function bindEvents() {
  EventsOn("ssh:session-state", (snapshot) => updateSession(snapshot));
  EventsOn("app:settings-changed", (settings) => {
    state.settings = settings;
    renderSidebar();
    if ($("#profileDialog").open) updatePrivateKeyVisibility();
  });
  EventsOn("app:credential-save-error", (payload) => {
    showToast(payload && payload.message ? payload.message : "密码未能保存到安全存储");
  });
  EventsOn("ssh:output", (payload) => {
    const terminal = terminalFor(payload.session_id);
    terminal.feed(decodeOutput(payload.data_base64));
    observeRemotePrompt(payload.session_id, terminal);
    if (payload.session_id === state.activeSessionId) renderTerminal();
  });
  EventsOn("ssh:host-key", (challenge) => showHostKeyChallenge(challenge));
  EventsOn("ssh:session-closed", (payload) => {
    state.sessions = state.sessions.filter((session) => session.id !== payload.session_id);
    state.terminals.delete(payload.session_id);
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
  viewport.addEventListener("keydown", async (event) => {
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

  viewport.addEventListener("contextmenu", (event) => {
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

  const resizeObserver = new ResizeObserver(() => resizeActiveTerminal());
  resizeObserver.observe(viewport);

  window.addEventListener("keydown", (event) => {
    const terminalFocused = document.activeElement === viewport;
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
