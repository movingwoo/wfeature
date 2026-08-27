// Where on the game's screen a finger landed.
//
// A touch on the canvas is several transforms away from a coordinate the guest
// would recognise, and every one of them is the page's own doing:
//
//   1. the canvas element is laid out by the stylesheet, at whatever size the
//      viewport left for it;
//   2. the frame inside it is *fitted* rather than stretched (`object-fit:
//      contain`), because the screen setting offers handsets that are 3:5 and
//      2:3 as well as the hole's 3:4 — so a frame of another shape keeps its
//      own and leaves the axis that does not fill as bezel;
//   3. the frame arrives magnified, because the filter runs on the server, so
//      the canvas is some whole multiple of the screen the game thinks it has.
//
// The server knows none of that. It would have to be told the canvas geometry
// on every frame to work it out, and the geometry is the page's — so the page
// undoes its own transforms and sends the guest's own pixels.
//
// It is a pure function of numbers so a test can drive every shape without a
// browser: a wide window, a tall one, a frame that letterboxes on the other
// axis, and a finger in the bezel.

// guestPoint answers where a client point lands on the game's screen, or null
// when it lands on the bezel beside the picture rather than on the picture.
//
// rect is the canvas element's bounding box. frameWidth and frameHeight are
// the canvas's backing store — the magnified picture. screenWidth and
// screenHeight are the screen the game believes it has, which is what
// `started` reported.
export const guestPoint = ({
  rect,
  frameWidth,
  frameHeight,
  screenWidth,
  screenHeight,
  clientX,
  clientY,
}) => {
  if (!rect || rect.width <= 0 || rect.height <= 0) return null;
  if (!(frameWidth > 0 && frameHeight > 0)) return null;
  if (!(screenWidth > 0 && screenHeight > 0)) return null;

  // `contain` picks the axis that runs out first and scales both by it.
  const shown = Math.min(rect.width / frameWidth, rect.height / frameHeight);
  const shownWidth = frameWidth * shown;
  const shownHeight = frameHeight * shown;
  // What is left over is split evenly: the picture is centred in the element.
  const left = rect.left + (rect.width - shownWidth) / 2;
  const top = rect.top + (rect.height - shownHeight) / 2;

  const acrossPicture = (clientX - left) / shown;
  const downPicture = (clientY - top) / shown;
  if (acrossPicture < 0 || downPicture < 0) return null;
  if (acrossPicture >= frameWidth || downPicture >= frameHeight) return null;

  // From the magnified picture back to the screen the game has. The two are a
  // whole multiple apart today; the division does not assume it, because the
  // magnification is a setting and the frame is whatever the server sent.
  const x = Math.floor((acrossPicture * screenWidth) / frameWidth);
  const y = Math.floor((downPicture * screenHeight) / frameHeight);
  // Flooring a point on the last row or column can only land inside, but the
  // arithmetic above is floating point and the guest indexes an array with
  // what it is given.
  return {
    x: Math.min(x, screenWidth - 1),
    y: Math.min(y, screenHeight - 1),
  };
};

// createTouchStream turns a browser's pointer events into the press/drag/
// release run a guest expects, and holds the two rules that are easy to get
// wrong.
//
// **A finger that leaves the picture is still holding it.** A handset's touch
// panel *is* the screen, so there is no "beside the screen" to drag onto; a
// finger that wanders into the bezel would, taken literally, produce nothing
// at all until it came back. Instead the last point that was on the picture is
// held, and a release out there releases at that point — which is what the
// guest would have seen on a handset whose panel ended at the glass.
//
// **A drag that has not moved is not a drag.** A thumb resting on a screen
// emits a stream of pointermove events at the same guest pixel, and forwarding
// them is a message per event for a finger that is sitting still.
export const createTouchStream = ({ press, drag, release }) => {
  // The pointer that owns the touch, and where it last was on the picture.
  // Only one is followed: a handset had one touch panel and a title that reads
  // a second finger is a title this could not have run anyway.
  let active = null;
  let last = null;

  return {
    down: (pointerId, point) => {
      if (active !== null || !point) return false;
      active = pointerId;
      last = point;
      press(point.x, point.y);
      return true;
    },
    move: (pointerId, point) => {
      if (active !== pointerId) return false;
      // Off the picture: the finger keeps the point it left from.
      if (!point) return false;
      if (last && point.x === last.x && point.y === last.y) return false;
      last = point;
      drag(point.x, point.y);
      return true;
    },
    up: (pointerId, point) => {
      if (active !== pointerId) return false;
      const at = point ?? last;
      active = null;
      last = null;
      if (!at) return false;
      release(at.x, at.y);
      return true;
    },
    // holding reports whether a touch is in progress, which is what tells the
    // page whether a pointer event on the window belongs to the canvas.
    holding: () => active !== null,
  };
};
