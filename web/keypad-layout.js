import { RAPID_FIRE } from "./rapid-fire.js";
import { QUICK_SAVE, QUICK_LOAD } from "./checkpoint.js";
import { local } from "./storage.js";
import { keyLabel, keyOrder } from "./keybindings.js";

export const COLUMNS = 14;
export const ROWS = 9;
export const EMPTY = "";
export const SETTINGS = "SETTINGS";
export const GRID_KEY = "wfeature:keypadGridV2";
export const GRID_V1_KEY = "wfeature:keypadGrid";
const MAX_RECORD = 65536;
const OLD_COLUMNS = 7;
export const shapes = ["type1", "type2", "type3", "type4"];
export const isShape = (name) => shapes.includes(name);
export const assignable = [
  SETTINGS,
  ...keyOrder,
  RAPID_FIRE,
  QUICK_SAVE,
  QUICK_LOAD,
];
const knownKey = new Set([EMPTY, ...assignable]);
const localKeys = new Set([RAPID_FIRE, QUICK_SAVE, QUICK_LOAD, SETTINGS]);
const directKeys = new Set([...localKeys, "MENU", "CALL", "SOFT2", "CLR"]);
export const defaultActivation = (key) =>
  directKeys.has(key) ? "press" : "slide";
const localNames = {
  [RAPID_FIRE]: "연사",
  [QUICK_SAVE]: "퀵세이브",
  [QUICK_LOAD]: "퀵로드",
  [SETTINGS]: "설정",
};
export const keyName = (key) => localNames[key] ?? keyLabel(key);
export const keyFace = (key) => (key === "OK" ? "OK" : keyName(key));

export const cells = Array.from({ length: ROWS * COLUMNS }, (_, index) => ({
  id: `r${Math.floor(index / COLUMNS) + 1}c${(index % COLUMNS) + 1}`,
  row: Math.floor(index / COLUMNS) + 1,
  column: (index % COLUMNS) + 1,
}));
export const cellIds = cells.map((cell) => cell.id);
const cellById = new Map(cells.map((cell) => [cell.id, cell]));
const order = new Map(cellIds.map((id, index) => [id, index]));
const sortCells = (values) =>
  [...values].sort((a, b) => order.get(a) - order.get(b));
const copy = (grid) => ({
  columns: COLUMNS,
  rows: ROWS,
  groups: grid.groups.map((group) => ({ ...group, cells: [...group.cells] })),
});
const equal = (a, b) => JSON.stringify(a) === JSON.stringify(b);
const singleton = (cell, key = EMPTY, activation = defaultActivation(key)) => ({
  id: cell,
  cells: [cell],
  key,
  activation,
});
const sortedGrid = (groups, columns = COLUMNS) => ({
  columns,
  rows: ROWS,
  groups: groups.sort((a, b) => order.get(a.id) - order.get(b.id)),
});

// Version 1 used one wide cell where version 2 has two half-width cells.
// Preserve buttons, including cleared custom shapes, while exposing both halves
// of an ordinary empty cell for immediate selection.
const refineGrid = (grid) => {
  const groups = [];
  for (const group of grid.groups) {
    const members = sortCells(
      group.cells.flatMap((id) => {
        const { row, column } = cellById.get(id);
        return [`r${row}c${column * 2 - 1}`, `r${row}c${column * 2}`];
      }),
    );
    if (!group.key && group.cells.length === 1)
      groups.push(...members.map((id) => singleton(id)));
    else groups.push({ ...group, id: members[0], cells: members });
  }
  return sortedGrid(groups);
};

export const connected = (members) => {
  if (
    !members.length ||
    members.length > cells.length ||
    members.some((id) => !cellById.has(id))
  )
    return false;
  const remaining = new Set(members),
    pending = [members[0]];
  remaining.delete(members[0]);
  for (let at = 0; at < pending.length; at++) {
    const { row, column } = cellById.get(pending[at]);
    for (const neighbor of [
      `r${row - 1}c${column}`,
      `r${row + 1}c${column}`,
      `r${row}c${column - 1}`,
      `r${row}c${column + 1}`,
    ]) {
      if (remaining.delete(neighbor)) pending.push(neighbor);
    }
  }
  return remaining.size === 0;
};

// These positions are the old storage contract. Keep their mapping independent
// of viewport dimensions so rotating a phone never rewrites an arrangement.
export const legacyCells = [
  ...Array.from({ length: 7 }, (_, c) => ({
    id: `band-c${c + 1}`,
    region: "band",
    row: 0,
    column: c + 1,
  })),
  ...Array.from({ length: 28 }, (_, index) => ({
    id: `pad-r${Math.floor(index / 7) + 1}c${(index % 7) + 1}`,
    region: "pad",
    row: Math.floor(index / 7) + 1,
    column: (index % 7) + 1,
  })),
];
const fillLegacy = (assignments) => ({
  ...Object.fromEntries(legacyCells.map((cell) => [cell.id, EMPTY])),
  ...assignments,
});
const band = {
  "band-c1": SETTINGS,
  "band-c3": "MENU",
  "band-c4": RAPID_FIRE,
  "band-c5": "CALL",
  "band-c6": "SOFT2",
  "band-c7": "CLR",
};
const footer = { "pad-r4c3": "*", "pad-r4c4": "0", "pad-r4c5": "#" };
export const legacyPresets = {
  type1: fillLegacy({
    ...band,
    ...footer,
    "pad-r1c2": "UP",
    "pad-r2c1": "LEFT",
    "pad-r2c2": "OK",
    "pad-r2c3": "RIGHT",
    "pad-r3c2": "DOWN",
    "pad-r1c5": "1",
    "pad-r1c6": "2",
    "pad-r1c7": "3",
    "pad-r2c5": "4",
    "pad-r2c6": "5",
    "pad-r2c7": "6",
    "pad-r3c5": "7",
    "pad-r3c6": "8",
    "pad-r3c7": "9",
  }),
  type2: fillLegacy({
    ...band,
    ...footer,
    "pad-r1c2": "2",
    "pad-r2c1": "4",
    "pad-r2c3": "6",
    "pad-r3c2": "8",
    "pad-r1c5": "1",
    "pad-r1c7": "3",
    "pad-r2c6": "5",
    "pad-r3c5": "7",
    "pad-r3c7": "9",
  }),
  type3: fillLegacy({
    ...band,
    ...footer,
    "pad-r1c1": "1",
    "pad-r1c2": "2",
    "pad-r1c3": "3",
    "pad-r2c1": "4",
    "pad-r2c3": "6",
    "pad-r3c2": "8",
    "pad-r2c6": "5",
    "pad-r3c5": "7",
    "pad-r3c7": "9",
  }),
  type4: fillLegacy({ "band-c1": SETTINGS }),
};

export const migrateLegacy = (source) => {
  const table = Object.fromEntries(
    legacyCells.map(({ id }) => [
      id,
      knownKey.has(source?.[id]) ? source[id] : EMPTY,
    ]),
  );
  const settings = legacyCells.filter((cell) => table[cell.id] === SETTINGS);
  if (!settings.length)
    table[legacyCells.find((cell) => !table[cell.id])?.id ?? "band-c1"] =
      SETTINGS;
  for (const cell of settings.slice(1)) table[cell.id] = EMPTY;
  const groups = [];
  for (const cell of legacyCells) {
    const row = cell.region === "band" ? 1 : cell.row * 2;
    const first = `r${row}c${cell.column}`,
      key = table[cell.id];
    const activation =
      localKeys.has(key) || cell.region === "band" ? "press" : "slide";
    const group = singleton(first, key, activation);
    if (cell.region === "pad") {
      const second = `r${row + 1}c${cell.column}`;
      if (key) group.cells.push(second);
      else groups.push(singleton(second));
    }
    groups.push(group);
  }
  return refineGrid(sortedGrid(groups, OLD_COLUMNS));
};
export const defaultGrid = (shape) => {
  const name = isShape(shape) ? shape : shapes[0];
  const grid = migrateLegacy(legacyPresets[name]);
  if (name === "type4") return grid;
  // Presets may change without rewriting the legacy storage contract.
  const topKeys = [
    EMPTY,
    EMPTY,
    QUICK_SAVE,
    QUICK_LOAD,
    RAPID_FIRE,
    "MENU",
    "CALL",
    "SOFT2",
    EMPTY,
    EMPTY,
  ];
  return sortedGrid([
    ...grid.groups.filter(
      (group) =>
        !group.id.startsWith("r1c") ||
        group.id === "r1c1" ||
        group.id === "r1c13",
    ),
    ...topKeys.map((key, index) => singleton(`r1c${index + 3}`, key)),
  ]);
};

// A record is a complete partition of a bounded board. Refuse an invalid type
// as a whole rather than salvaging half a button or silently losing settings.
const validate = (value, columns) => {
  const limit = columns * ROWS;
  if (
    !value ||
    value.columns !== columns ||
    value.rows !== ROWS ||
    !Array.isArray(value.groups) ||
    value.groups.length < 1 ||
    value.groups.length > limit
  )
    return null;
  const occupied = new Set(),
    groups = [];
  let settings = 0;
  for (const group of value.groups) {
    if (
      !group ||
      !Array.isArray(group.cells) ||
      !group.cells.length ||
      group.cells.length > limit ||
      !knownKey.has(group.key) ||
      !["press", "slide"].includes(group.activation) ||
      (localKeys.has(group.key) && group.activation !== "press")
    )
      return null;
    const members = sortCells(group.cells);
    if (group.id !== members[0] || !connected(members)) return null;
    for (const id of members) {
      if (cellById.get(id).column > columns) return null;
      if (occupied.has(id)) return null;
      occupied.add(id);
    }
    if (group.key === SETTINGS) settings++;
    groups.push({
      id: members[0],
      cells: members,
      key: group.key,
      activation: group.activation,
    });
  }
  return occupied.size === limit && settings === 1
    ? sortedGrid(groups, columns)
    : null;
};
export const validateGrid = (value) => validate(value, COLUMNS);

const record = (value) =>
  value !== null && typeof value === "object" && !Array.isArray(value);
const fail = (error) => ({ ok: false, error });

export const createKeypadLayout = (
  storage = local,
  layoutKey = "wfeature:keypadLayout",
) => {
  let status = { saved: true, reason: "" },
    backup = null,
    protectedRecord = false;
  const read = (key) => {
    try {
      return storage?.getItem(key) ?? null;
    } catch {
      status = { saved: false, reason: "storage" };
      return null;
    }
  };
  const write = (key, value) => {
    try {
      return storage?.setItem(key, value) !== false && storage != null;
    } catch {
      return false;
    }
  };
  const parse = (text) => {
    try {
      return text && text.length <= MAX_RECORD ? JSON.parse(text) : null;
    } catch {
      return null;
    }
  };
  let shape = read(layoutKey);
  if (!isShape(shape)) shape = shapes[0];
  const original = read(GRID_KEY);
  // Older tabs can still write the coarse grid. Keep its storage slot separate
  // so their edits cannot overwrite a refined arrangement after migration.
  const coarseSource = original === null ? read(GRID_V1_KEY) : null;
  const source = original ?? coarseSource;
  const stored = parse(source);
  const layouts = new Map(shapes.map((name) => [name, defaultGrid(name)]));
  let migrated = false;
  if (source !== null) {
    if (
      record(stored) &&
      stored.version !== 2 &&
      !(coarseSource !== null && stored.version === 1)
    ) {
      protectedRecord = true;
      status = { saved: false, reason: "future" };
    } else if (source.length > MAX_RECORD) {
      protectedRecord = true;
      status = { saved: false, reason: "oversized" };
    } else {
      let corrupt =
        !record(stored) ||
        ![1, 2].includes(stored.version) ||
        !record(stored.layouts);
      for (const name of shapes) {
        const entry = stored?.layouts?.[name];
        if (entry === null || entry === undefined) continue;
        const old = stored.version === 1;
        const valid = validate(entry, old ? OLD_COLUMNS : COLUMNS);
        if (valid) layouts.set(name, old ? refineGrid(valid) : valid);
        else corrupt = true;
      }
      if (corrupt) {
        backup = original;
        status = { saved: true, reason: "recovered" };
      }
      if (coarseSource !== null && [1, 2].includes(stored?.version)) {
        migrated = true;
      }
    }
  } else {
    const legacy = parse(read("wfeature:keypadKeys"));
    for (const name of shapes) {
      if (record(legacy?.[name])) {
        layouts.set(name, migrateLegacy(legacy[name]));
        migrated = true;
      }
    }
  }
  const persist = () => {
    if (protectedRecord) return;
    // Keep the source before replacing a damaged record. Failed backups leave
    // it intact and retain the new edit only in this page's model.
    if (backup !== null) {
      if (!write(`${GRID_KEY}Backup`, backup)) {
        status = { saved: false, reason: "storage" };
        return;
      }
      backup = null;
    }
    const layoutsRecord = Object.fromEntries(
      shapes.map((name) => [
        name,
        equal(layouts.get(name), defaultGrid(name)) ? null : layouts.get(name),
      ]),
    );
    const saved = write(
      GRID_KEY,
      JSON.stringify({ version: 2, layouts: layoutsRecord }),
    );
    status = { saved, reason: saved ? "" : "storage" };
  };
  if (migrated) {
    const recovered = status.reason === "recovered";
    persist();
    if (status.saved && recovered) status.reason = "recovered";
  }
  const current = () => layouts.get(shape);
  const groupAt = (id) =>
    current().groups.find((group) => group.cells.includes(id));
  let previous = null;
  const commit = (next) => {
    if (equal(current(), next)) return { ok: true, changed: false };
    previous = copy(current());
    layouts.set(shape, next);
    persist();
    return { ok: true, changed: true };
  };
  return {
    grid: () => copy(current()),
    shape: () => shape,
    edited: () => !equal(current(), defaultGrid(shape)),
    groupAt: (id) => {
      const group = groupAt(id);
      return group ? { ...group, cells: [...group.cells] } : null;
    },
    keyAt: (id) => groupAt(id)?.key ?? EMPTY,
    keys: () =>
      Object.fromEntries(
        current().groups.flatMap((group) =>
          group.cells.map((id) => [id, group.key]),
        ),
      ),
    persistence: () => ({ ...status }),
    beginEdit: () => {
      previous = null;
    },
    canUndo: () => previous !== null,
    useShape: (name) => {
      if (!isShape(name) || name === shape) return;
      shape = name;
      previous = null;
      if (!write(layoutKey, shape) && !protectedRecord)
        status = { saved: false, reason: "storage" };
    },
    set: (id, key) => {
      const group = groupAt(id);
      if (!group || !knownKey.has(key)) return fail("invalid");
      if (group.key === SETTINGS && key !== SETTINGS) return fail("settings");
      const next = copy(current());
      for (const other of next.groups) {
        if (other.id === group.id) {
          if (other.key !== key) other.activation = defaultActivation(key);
          other.key = key;
        } else if (key === SETTINGS && other.key === SETTINGS)
          other.key = EMPTY;
      }
      return commit(next);
    },
    merge: (ids, chosenKey) => {
      if (
        !Array.isArray(ids) ||
        ids.length > cells.length ||
        ids.some((id) => !groupAt(id))
      )
        return fail("invalid");
      const selected = [...new Set(ids.map((id) => groupAt(id).id))].map(
        groupAt,
      );
      if (selected.length < 2) return fail("select-more");
      const members = sortCells(selected.flatMap((group) => group.cells));
      if (!connected(members)) return fail("disconnected");
      const keys = [
        ...new Set(selected.map((group) => group.key).filter(Boolean)),
      ];
      if (chosenKey !== undefined && !knownKey.has(chosenKey))
        return fail("invalid");
      if (
        keys.includes(SETTINGS) &&
        chosenKey !== undefined &&
        chosenKey !== SETTINGS
      )
        return fail("settings");
      if (
        !keys.includes(SETTINGS) &&
        chosenKey === undefined &&
        keys.length > 1
      )
        return fail("choose-key");
      const key = keys.includes(SETTINGS)
        ? SETTINGS
        : (chosenKey ?? keys[0] ?? EMPTY);
      const activation = localKeys.has(key)
        ? "press"
        : (selected.find((group) => group.key === key)?.activation ??
          defaultActivation(key));
      const removed = new Set(selected.map((group) => group.id));
      const kept = copy(current()).groups.filter(
        (group) => !removed.has(group.id),
      );
      if (key === SETTINGS)
        for (const group of kept) if (group.key === SETTINGS) group.key = EMPTY;
      return commit(
        sortedGrid([
          ...kept,
          { id: members[0], cells: members, key, activation },
        ]),
      );
    },
    split: (id) => {
      const group = groupAt(id);
      if (!group || group.cells.length === 1) return fail("select-merged");
      return commit(
        sortedGrid([
          ...copy(current()).groups.filter((other) => other.id !== group.id),
          ...group.cells.map((cell, index) =>
            singleton(
              cell,
              index === 0 ? group.key : EMPTY,
              index === 0 ? group.activation : defaultActivation(EMPTY),
            ),
          ),
        ]),
      );
    },
    reset: () => commit(defaultGrid(shape)),
    undo: () => {
      if (!previous) return false;
      layouts.set(shape, previous);
      previous = null;
      persist();
      return true;
    },
  };
};
