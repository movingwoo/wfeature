import {
  createKeypadLayout,
  COLUMNS,
  ROWS,
  SETTINGS,
  assignable,
  keyFace,
  keyName,
  connected,
} from "./keypad-layout.js";
import { groupGeometry } from "./keypad-geometry.js";
import { local } from "./storage.js";

const svgNamespace = "http://www.w3.org/2000/svg";
const errors = {
  disconnected: "인접한 칸끼리만 합칠 수 있습니다.",
  "choose-key": "서로 다른 키가 있습니다. 합친 버튼에 넣을 키를 먼저 고르세요.",
  settings: "설정 키를 다른 칸으로 옮긴 뒤 변경하세요.",
  "select-more": "합칠 버튼이나 빈 칸을 두 개 이상 고르세요.",
  "select-merged": "나눌 병합 버튼 하나를 고르세요.",
  invalid: "선택한 칸을 다시 확인하세요.",
};

// Measure the actual font rather than estimating width from character count.
// Reset first so wider buttons and shorter dynamic labels can grow back.
export const fitKeypadLabel = (label) => {
  label.style.fontSize = "";
  const width = label.getBoundingClientRect().width;
  if (width <= 0) return;
  const document = label.ownerDocument;
  const range = document.createRange();
  range.selectNodeContents(label);
  const textWidth = range.getBoundingClientRect().width;
  if (textWidth <= width) return;
  const fontSize = Number.parseFloat(
    document.defaultView.getComputedStyle(label).fontSize,
  );
  label.style.fontSize = `${(fontSize * width) / textWidth}px`;
};

export const initKeypadEditor = ({
  document,
  releaseInput,
  onEditing,
  changed,
  storage = local,
}) => {
  const container = document.querySelector(".button-container");
  const board = document.getElementById("keypad-grid");
  const panel = document.getElementById("keypad-arrange");
  if (!container || !board || !panel) return;
  board.dataset.columns = COLUMNS;
  board.dataset.rows = ROWS;
  const window = document.defaultView;
  const layout = createKeypadLayout(storage);
  const byId = (id) => document.getElementById(id);
  const hint = byId("keypad-arrange-hint"),
    keyList = byId("keypad-arrange-keys");
  const multiButton = byId("keypad-arrange-multiple"),
    mergeButton = byId("keypad-arrange-merge");
  const splitButton = byId("keypad-arrange-split"),
    clearButton = byId("keypad-arrange-clear");
  const undoButton = byId("keypad-arrange-undo"),
    notice = byId("keypad-arrange-storage");
  const shapeLists = [...document.querySelectorAll(".keypad-shape-list")];
  const shapeNames = new Map();
  for (const list of shapeLists)
    for (const option of list.options)
      shapeNames.set(option, option.textContent);
  let editing = false,
    multiple = false,
    selected = new Set(),
    mergeKey,
    message = "",
    gesture = null;
  const buttons = new Map();
  const svg = (tag, attrs = {}) => {
    const node = document.createElementNS(svgNamespace, tag);
    for (const [name, value] of Object.entries(attrs))
      node.setAttribute(name, String(value));
    return node;
  };
  const selection = () =>
    [...selected].map((id) => layout.groupAt(id)).filter(Boolean);
  const update = () => {
    const groups = selection(),
      members = groups.flatMap((group) => group.cells);
    const keepsSettings = groups.some((group) => group.key === SETTINGS);
    for (const [id, { button }] of buttons) {
      button.classList.toggle("picked", selected.has(id));
      if (editing)
        button.setAttribute("aria-pressed", String(selected.has(id)));
      else button.removeAttribute("aria-pressed");
    }
    container.classList.toggle("arranging", editing);
    panel.classList.toggle("visible", editing);
    multiButton.setAttribute("aria-pressed", String(multiple));
    mergeButton.disabled =
      !multiple || groups.length < 2 || !connected(members);
    splitButton.disabled = groups.length !== 1 || groups[0].cells.length < 2;
    clearButton.disabled =
      groups.length !== 1 || keepsSettings || !groups[0].key;
    undoButton.disabled = !layout.canUndo();
    keyList.hidden = groups.length === 0;
    for (const button of keyList.querySelectorAll("button")) {
      button.disabled = keepsSettings && button.dataset.assign !== SETTINGS;
      button.setAttribute(
        "aria-pressed",
        String(multiple && mergeKey === button.dataset.assign),
      );
    }
    if (message) hint.textContent = message;
    else if (!groups.length)
      hint.textContent = multiple
        ? "여러 칸을 선택 후 합칠 수 있습니다."
        : "버튼이나 빈 칸을 눌러 키를 지정합니다. 합치거나 나눠서 크기와 모양을 변경합니다.";
    else if (multiple)
      hint.textContent = connected(members)
        ? `${members.length}칸 선택.`
        : errors.disconnected;
    else if (keepsSettings)
      hint.textContent = `${members.length}칸 · 설정 키는 지울 수 없습니다. 다른 칸에 설정을 넣으면 옮겨집니다.`;
    else
      hint.textContent = `${members.length}칸 · 넣을 키를 고르세요. 나누면 첫 칸에만 키가 남습니다.`;
    const state = layout.persistence();
    notice.hidden = !state.reason;
    notice.textContent =
      state.reason === "recovered"
        ? "저장된 일부 배치를 읽지 못해 기본 배치로 복구했습니다."
        : state.reason === "future" || state.reason === "oversized"
          ? "저장된 배치를 보존했습니다. 이번 변경은 이 탭에서만 유지됩니다."
          : "배치를 저장하지 못했습니다. 이번 변경은 이 탭에서만 유지됩니다.";
    const edited = layout.edited();
    for (const [option, name] of shapeNames) {
      const mine = option.value === layout.shape();
      option.textContent = mine && edited ? `${name} (수정됨)` : name;
    }
    for (const list of shapeLists) list.value = layout.shape();
  };
  const measure = () => {
    const rect = board.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return;
    const gap = Number.parseFloat(window.getComputedStyle(board).columnGap),
      cellWidth = (rect.width - gap * (COLUMNS - 1)) / COLUMNS;
    const cellHeight = (rect.height - gap * (ROWS - 1)) / ROWS;
    for (const { group, button, clip, outline, label } of buttons.values()) {
      const geometry = groupGeometry(group.cells, cellWidth, cellHeight, gap);
      Object.assign(button.style, {
        left: `${geometry.left}px`,
        top: `${geometry.top}px`,
        width: `${geometry.width}px`,
        height: `${geometry.height}px`,
      });
      clip.replaceChildren(
        ...geometry.rectangles.map((r) =>
          svg("rect", {
            x: r.x / geometry.width,
            y: r.y / geometry.height,
            width: r.width / geometry.width,
            height: r.height / geometry.height,
          }),
        ),
      );
      outline.setAttribute(
        "viewBox",
        `0 0 ${geometry.width} ${geometry.height}`,
      );
      outline.firstChild.setAttribute("d", geometry.outline);
      Object.assign(label.style, {
        left: `${geometry.label.x + 2}px`,
        top: `${geometry.label.y + geometry.label.height / 2}px`,
        width: `${Math.max(0, geometry.label.width - 4)}px`,
      });
    }
    for (const { label } of buttons.values()) fitKeypadLabel(label);
  };
  const draw = () => {
    releaseInput();
    gesture = null;
    const focused =
      document.activeElement?.closest?.("button[data-cell]")?.dataset.cell;
    const clips = svg("svg", {
      width: 0,
      height: 0,
      "aria-hidden": "true",
      focusable: "false",
    });
    clips.classList.add("keypad-clips");
    const defs = svg("defs");
    clips.append(defs);
    const fragment = document.createDocumentFragment();
    fragment.append(clips);
    buttons.clear();
    for (const group of layout.grid().groups) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = `keypad-cell${group.key ? "" : " empty"}`;
      button.dataset.cell = group.id;
      button.dataset.activation = group.activation;
      if (group.key) button.dataset.key = group.key;
      const [, row, column] = /^r(\d+)c(\d+)$/.exec(group.id);
      button.setAttribute(
        "aria-label",
        group.key ? keyName(group.key) : `${row}행 ${column}열 빈 칸`,
      );
      const clipId = `keypad-clip-${group.id}`;
      const clip = svg("clipPath", {
        id: clipId,
        clipPathUnits: "objectBoundingBox",
      });
      defs.append(clip);
      button.style.clipPath = `url(#${clipId})`;
      const outline = svg("svg", {
        "aria-hidden": "true",
        focusable: "false",
        preserveAspectRatio: "none",
      });
      outline.classList.add("keypad-outline");
      outline.append(svg("path"));
      const label = document.createElement("span");
      label.className = "keypad-label";
      label.textContent = group.key ? keyFace(group.key) : "+";
      button.append(outline, label);
      fragment.append(button);
      buttons.set(group.id, { group, button, clip, outline, label });
    }
    board.replaceChildren(fragment);
    measure();
    update();
    changed();
    if (focused) {
      const next = layout.groupAt(focused);
      if (next && (editing || next.key))
        buttons.get(next.id)?.button.focus({ preventScroll: true });
    }
  };
  const clearSelection = () => {
    selected.clear();
    mergeKey = undefined;
    message = "";
    gesture = null;
  };
  const apply = (result) => {
    message = result.ok ? "" : errors[result.error];
    if (result.ok) {
      clearSelection();
      draw();
    } else update();
  };
  for (const key of assignable) {
    const button = document.createElement("button");
    button.type = "button";
    button.dataset.assign = key;
    button.textContent = keyFace(key);
    button.setAttribute("aria-label", keyName(key));
    if (key === SETTINGS) button.classList.add("keypad-arrange-settings");
    button.addEventListener("click", () => {
      if (!selected.size) return;
      if (multiple) {
        mergeKey = key;
        message = "";
        update();
      } else {
        const id = [...selected][0],
          result = layout.set(id, key);
        message = result.ok ? "" : errors[result.error];
        draw();
      }
    });
    keyList.append(button);
  }
  multiButton.addEventListener("click", () => {
    multiple = !multiple;
    clearSelection();
    update();
  });
  byId("keypad-arrange-unselect").addEventListener("click", () => {
    clearSelection();
    update();
  });
  mergeButton.addEventListener("click", () => {
    const first = [...selected][0];
    const result = layout.merge([...selected], mergeKey);
    if (!result.ok) {
      apply(result);
      return;
    }
    multiple = false;
    clearSelection();
    selected.add(layout.groupAt(first).id);
    draw();
  });
  splitButton.addEventListener("click", () =>
    apply(layout.split([...selected][0])),
  );
  clearButton.addEventListener("click", () =>
    apply(layout.set([...selected][0], "")),
  );
  undoButton.addEventListener("click", () => {
    layout.undo();
    clearSelection();
    draw();
  });
  byId("keypad-arrange-reset").addEventListener("click", () => {
    layout.reset();
    clearSelection();
    draw();
  });
  for (const list of shapeLists) {
    list.addEventListener("change", () => {
      layout.useShape(list.value);
      clearSelection();
      draw();
    });
  }

  const visible = (on) => {
    releaseInput();
    clearSelection();
    editing = on;
    multiple = false;
    if (on) layout.beginEdit();
    onEditing(on);
    update();
    changed();
    if (on) byId("keypad-arrange-close").focus({ preventScroll: true });
    else
      board
        .querySelector(`button[data-key="${SETTINGS}"]`)
        ?.focus({ preventScroll: true });
  };
  byId("keypad-arrange-open").addEventListener("click", () => {
    if (!window.matchMedia("(min-width: 900px)").matches)
      byId("settings-panel")?.classList.remove("visible");
    visible(true);
  });
  byId("keypad-arrange-close").addEventListener("click", () => visible(false));
  document.addEventListener("keydown", (event) => {
    if (editing && event.code === "Escape") {
      event.preventDefault();
      visible(false);
    }
  });
  const pick = (id, toggle) => {
    const group = layout.groupAt(id);
    if (!group) return;
    if (!multiple) selected = new Set([group.id]);
    else if (toggle && selected.has(group.id)) selected.delete(group.id);
    else selected.add(group.id);
    mergeKey = undefined;
    message = "";
    update();
  };
  board.addEventListener("pointerdown", (event) => {
    if (!editing || gesture || event.button !== 0) return;
    const button = event.target.closest?.("button[data-cell]");
    if (!button) return;
    event.preventDefault();
    pick(button.dataset.cell, true);
    gesture = {
      id: event.pointerId,
      x: event.clientX,
      y: event.clientY,
      visited: new Set([button.dataset.cell]),
    };
    board.setPointerCapture(event.pointerId);
  });
  board.addEventListener("pointermove", (event) => {
    if (!gesture || gesture.id !== event.pointerId || !multiple) return;
    const from = gesture,
      dx = event.clientX - from.x,
      dy = event.clientY - from.y;
    const steps = Math.min(256, Math.max(1, Math.ceil(Math.hypot(dx, dy) / 4)));
    for (let step = 1; step <= steps; step++) {
      const element = document.elementFromPoint(
        from.x + (dx * step) / steps,
        from.y + (dy * step) / steps,
      );
      const button = element?.closest?.("#keypad-grid button[data-cell]");
      if (button && !gesture.visited.has(button.dataset.cell)) {
        gesture.visited.add(button.dataset.cell);
        pick(button.dataset.cell, false);
      }
    }
    gesture.x = event.clientX;
    gesture.y = event.clientY;
  });
  for (const type of ["pointerup", "pointercancel", "lostpointercapture"])
    window.addEventListener(type, (event) => {
      if (gesture?.id === event.pointerId) gesture = null;
    });
  board.addEventListener("click", (event) => {
    if (!editing || event.detail !== 0) return;
    const button = event.target.closest?.("button[data-cell]");
    if (button) {
      event.preventDefault();
      pick(button.dataset.cell, true);
    }
  });
  new window.ResizeObserver(() => {
    releaseInput();
    gesture = null;
    measure();
  }).observe(board);
  draw();
};
