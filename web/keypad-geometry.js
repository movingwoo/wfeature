// Shared geometry for the visible face, clipping, label and pointer checks.
// Half a gap belongs to each adjacent member of the same button. This fills
// internal seams without extending into an empty corner or another group.
export const groupGeometry = (members, cellWidth, cellHeight, gap = 4) => {
  const cells = members.map((id) => {
    const [, row, column] = /^r(\d+)c(\d+)$/.exec(id);
    return { row: +row - 1, column: +column - 1 };
  });
  const minRow = Math.min(...cells.map((cell) => cell.row)),
    maxRow = Math.max(...cells.map((cell) => cell.row));
  const minColumn = Math.min(...cells.map((cell) => cell.column)),
    maxColumn = Math.max(...cells.map((cell) => cell.column));
  const held = new Set(cells.map((cell) => `${cell.row},${cell.column}`));
  const has = (row, column) => held.has(`${row},${column}`);
  const pitchX = cellWidth + gap,
    pitchY = cellHeight + gap;
  const width = (maxColumn - minColumn + 1) * pitchX - gap;
  const height = (maxRow - minRow + 1) * pitchY - gap;
  const rectangles = cells.map(({ row, column }) => {
    const left = has(row, column - 1) ? gap / 2 : 0,
      right = has(row, column + 1) ? gap / 2 : 0;
    const top = has(row - 1, column) ? gap / 2 : 0,
      bottom = has(row + 1, column) ? gap / 2 : 0;
    return {
      x: (column - minColumn) * pitchX - left,
      y: (row - minRow) * pitchY - top,
      width: cellWidth + left + right,
      height: cellHeight + top + bottom,
    };
  });
  // Labels use the largest occupied rectangle. A bounding-box center can be
  // outside an L shape or in a ring's hole.
  let label = { x: 0, y: 0, width: cellWidth, height: cellHeight },
    score = -1,
    distance = Infinity;
  for (let top = minRow; top <= maxRow; top++)
    for (let left = minColumn; left <= maxColumn; left++) {
      for (let bottom = top; bottom <= maxRow; bottom++) {
        for (let right = left; right <= maxColumn; right++) {
          let full = true;
          for (let row = top; row <= bottom && full; row++)
            for (let column = left; column <= right; column++) {
              if (!has(row, column)) {
                full = false;
                break;
              }
            }
          if (!full) break;
          const box = {
            x: (left - minColumn) * pitchX,
            y: (top - minRow) * pitchY,
            width: (right - left + 1) * pitchX - gap,
            height: (bottom - top + 1) * pitchY - gap,
          };
          const area = box.width * box.height;
          const centerDistance =
            (box.x + box.width / 2 - width / 2) ** 2 +
            (box.y + box.height / 2 - height / 2) ** 2;
          if (area > score || (area === score && centerDistance < distance)) {
            label = box;
            score = area;
            distance = centerDistance;
          }
        }
      }
    }
  // Partition at rectangle edges, then keep only exposed edges. The same union
  // feeds the clip path; the outline therefore includes concave edges and holes.
  const xs = [...new Set(rectangles.flatMap((r) => [r.x, r.x + r.width]))].sort(
    (a, b) => a - b,
  );
  const ys = [
    ...new Set(rectangles.flatMap((r) => [r.y, r.y + r.height])),
  ].sort((a, b) => a - b);
  const filled = ys
    .slice(1)
    .map((_, y) =>
      xs
        .slice(1)
        .map((_, x) =>
          rectangles.some(
            (r) =>
              (xs[x] + xs[x + 1]) / 2 >= r.x &&
              (xs[x] + xs[x + 1]) / 2 < r.x + r.width &&
              (ys[y] + ys[y + 1]) / 2 >= r.y &&
              (ys[y] + ys[y + 1]) / 2 < r.y + r.height,
          ),
        ),
    );
  const edges = [];
  for (let y = 0; y < filled.length; y++)
    for (let x = 0; x < filled[y].length; x++) {
      if (!filled[y][x]) continue;
      const x1 = xs[x],
        x2 = xs[x + 1],
        y1 = ys[y],
        y2 = ys[y + 1];
      if (!filled[y - 1]?.[x]) edges.push(`M${x1},${y1}H${x2}`);
      if (!filled[y + 1]?.[x]) edges.push(`M${x1},${y2}H${x2}`);
      if (!filled[y]?.[x - 1]) edges.push(`M${x1},${y1}V${y2}`);
      if (!filled[y]?.[x + 1]) edges.push(`M${x2},${y1}V${y2}`);
    }
  return {
    left: minColumn * pitchX,
    top: minRow * pitchY,
    width,
    height,
    rectangles,
    label,
    outline: edges.join(""),
  };
};

export const containsPoint = (geometry, x, y) =>
  geometry.rectangles.some(
    (rect) =>
      x >= rect.x &&
      x < rect.x + rect.width &&
      y >= rect.y &&
      y < rect.y + rect.height,
  );
