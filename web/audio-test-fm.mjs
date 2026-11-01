// A stand-in for the page's FM synthesizer host in the audio tests: it
// records every command and lane instead of rendering, and lets a test say
// when the synthesizer finished a voice.

export const fakeFM = () => {
  const fm = {
    commands: [],
    lanes: [],
    released: [],
    onEnded: () => {},
    capacity: 32,
    lane(destination, ratio = 1, depth = 0) {
      let lane = this.lanes.findIndex(entry => entry === null);
      if (lane < 0) lane = this.lanes.length;
      if (lane >= this.capacity) return null;
      this.lanes[lane] = { destination, ratio, depth };
      this.commands.push({ op: "lane", lane, ratio, depth });
      return lane;
    },
    release(lane) {
      this.lanes[lane] = null;
      this.released.push(lane);
      this.commands.push({ op: "release", lane });
    },
    post(command) { this.commands.push(command); },
    // notes answers the note-on commands in the order they were posted.
    notes() { return this.commands.filter(command => command.op === "on"); },
    // of answers every timed command posted for one voice.
    of(id) { return this.commands.filter(command => command.id === id && command.op !== "on"); },
    end(...ids) { this.onEnded(ids); },
  };
  const createFM = (context, options) => {
    fm.context = context;
    fm.onEnded = options.onEnded;
    return fm;
  };
  return { fm, createFM };
};
