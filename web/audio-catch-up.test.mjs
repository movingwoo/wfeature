import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { fakeFM } from "./audio-test-fm.mjs";
import { playAudioEvents } from "./session.js";

const LEAD = 0.1;
const near = (actual, expected, message) => assert.ok(Math.abs(actual - expected) < 1e-9, `${message}: ${actual} != ${expected}`);

function setup(t) {
  t.mock.method(performance, "now", () => 0);
  const reports = [];
  const { fm, createFM } = fakeFM();
  const audio = new PageAudio({ createFM, diagnostics: () => true, report: message => reports.push(message) });
  const node = () => ({ connect() {}, disconnect() {} });
  const parameter = () => ({ value: 1, setValueAtTime() {}, linearRampToValueAtTime() {},
    exponentialRampToValueAtTime() {}, cancelScheduledValues() {} });
  audio.context = {
    state: "running", currentTime: 1, baseLatency: 0.005,
    createGain: () => ({ ...node(), gain: parameter() }),
    createStereoPanner: () => ({ ...node(), pan: parameter() }),
  };
  audio.midiGain = node();
  audio.waveGain = node();
  t.after(() => audio.stopAll());
  // deliver plays one note at server time p, its batch reaching the page at
  // render time arrival. It answers how far ahead of the render clock the
  // note was placed, or null when the batch was refused. The synthesizer
  // finishes every earlier note, so one is always sounding.
  let note = 0;
  const deliver = (p, arrival) => {
    audio.context.currentTime = arrival;
    const before = fm.notes().length;
    const previous = note;
    note = (note + 1) % 100;
    const accepted = playAudioEvents(audio, [
      { kind: "noteOff", sound: 1, channel: 0, note: previous, velocity: 0, at: p },
      { kind: "noteOn", sound: 1, channel: 0, note, velocity: 90, at: p },
      { kind: "clock", at: p },
    ]);
    if (!accepted || fm.notes().length === before) return null;
    fm.end(...fm.notes().slice(0, -1).slice(-2).map(command => command.id));
    return { p, at: fm.notes().at(-1).at, lead: fm.notes().at(-1).at - arrival };
  };
  // silent delivers a batch holding only its frontier, with nothing sounding,
  // and answers how far ahead that frontier lands.
  const silent = (p, arrival) => {
    fm.end(...fm.notes().map(command => command.id));
    audio.context.currentTime = arrival;
    if (!playAudioEvents(audio, [{ kind: "clock", at: p }])) return null;
    const anchor = audio.presentationAnchor;
    return { p, lead: anchor.audio + (p - anchor.presentation) - arrival };
  };
  const report = () => {
    audio.reportTiming();
    const reported = reports.filter(line => line.startsWith("audio playout timing: "))
      .map(line => JSON.parse(line.slice("audio playout timing: ".length)));
    const sum = key => reported.reduce((total, entry) => total + entry.continuing[key], 0);
    return { earlier: sum("anchor_earlier_ms"), later: sum("anchor_later_ms"), reanchored: sum("reanchored") };
  };
  return { audio, deliver, silent, report };
}

const steps = (from, to, step = 0.05) => Array.from({ length: Math.round((to - from) / step) }, (_, index) => from + index * step);
const at = (played, p) => played.find(entry => Math.abs(entry.p - p) < 1e-9);

// Notes keep their order and, while something sounds, their spacing changes
// by at most the steering rate: 10% quicker or slower while the anchor moves.
const assertSpacing = played => {
  for (let index = 1; index < played.length; index++) {
    const spacing = played[index].at - played[index - 1].at;
    const authored = played[index].p - played[index - 1].p;
    assert.ok(spacing >= authored * 0.9 - 1e-9 && spacing <= authored * 1.1 + 1e-9,
      `notes at ${played[index - 1].p.toFixed(2)} and ${played[index].p.toFixed(2)}: ${spacing} for ${authored}`);
  }
};

// **One late batch no longer leaves every later sound behind.** A batch 200
// ms late moves the anchor and every later note lands 0.3 s ahead instead of
// 0.1 s. Two seconds after it, with every batch on time, the anchor comes back
// at a tenth of the render time: the delay is gone about four seconds after
// the late batch.
test("a late batch's extra delay comes back once batches are on time again", t => {
  const { deliver, report } = setup(t);
  const played = [];
  for (const p of steps(0, 12)) {
    const result = deliver(p, 1 + p + (Math.abs(p - 2) < 1e-9 ? 0.2 : 0));
    assert.ok(result, `batch at ${p.toFixed(2)} was refused`);
    played.push(result);
  }
  near(at(played, 1).lead, LEAD, "before");
  near(at(played, 3).lead, LEAD + 0.2, "the late batch moved every later note");
  near(at(played, 4.15).lead, LEAD + 0.2, "nothing moves for two seconds after a late batch");
  assert.ok(at(played, 6.5).lead <= LEAD + 0.02 + 1e-9, `back within 20 ms of the lead by then: ${at(played, 6.5).lead}`);
  assert.ok(played.at(-1).lead >= LEAD - 1e-9, "never closer than the page's own lead");
  assertSpacing(played.filter(entry => entry.p > 2));
  const { earlier, later, reanchored } = report();
  assert.ok(earlier > 175 && earlier < 200 + 1e-6, `moved earlier by ${earlier} ms`);
  assert.equal(later, 0);
  assert.equal(reanchored, 1);
});

test("in silence the delay comes back at once after the hold", t => {
  const { deliver, silent } = setup(t);
  for (const p of steps(0, 2)) assert.ok(deliver(p, 1 + p));
  assert.ok(deliver(2, 3.2), "the late batch");
  const frontiers = steps(2.1, 6, 0.1).map(p => silent(p, 1 + p));
  assert.ok(frontiers.every(Boolean));
  near(at(frontiers, 4.1).lead, LEAD + 0.2, "held for two seconds");
  assert.ok(at(frontiers, 4.3).lead <= LEAD + 0.02 + 1e-9, `one batch later it is back: ${at(frontiers, 4.3).lead}`);
});

test("batches late again and again keep the lead that absorbs them", t => {
  const { deliver, report } = setup(t);
  const played = [];
  for (const p of steps(0, 40)) {
    const late = p >= 2 && Math.abs((p - 2) / 3 - Math.round((p - 2) / 3)) < 1e-6 ? 0.2 : 0;
    const result = deliver(p, 1 + p + late);
    assert.ok(result, `batch at ${p.toFixed(2)} was refused`);
    played.push({ ...result, late });
  }
  // Every later spike lands ahead of the render clock: the anchor never came
  // back so far that one of them was late again.
  for (const entry of played.filter(entry => entry.p > 3)) assert.ok(entry.lead >= 0, `${entry.p.toFixed(2)} lands ${entry.lead}`);
  const onTime = played.filter(entry => entry.p > 30 && entry.late === 0);
  assert.ok(onTime.every(entry => entry.lead >= 0.2 + 0.02 - 1e-9), "on-time batches keep the lead the spikes need");
  assert.ok(onTime.some(entry => entry.lead < 0.3 - 0.01), "and give back what they do not need");
  assertSpacing(played.filter(entry => entry.p > 2));
  assert.equal(report().reanchored, 1, "only the first spike was late");
});

// **A batch late again soon doubles the hold.** Spikes too far apart for the
// lead to absorb each move the anchor; the second within thirty seconds of the
// first waits four seconds before coming back, the third eight.
test("a page late again soon waits longer before coming back", t => {
  const { deliver, report } = setup(t);
  const played = [];
  const spikes = [2, 8, 16];
  for (const p of steps(0, 34)) {
    const late = spikes.some(spike => Math.abs(p - spike) < 1e-9) ? 0.2 : 0;
    const result = deliver(p, 1 + p + late);
    assert.ok(result, `batch at ${p.toFixed(2)} was refused`);
    played.push(result);
  }
  assert.equal(report().reanchored, 3);
  near(at(played, 8 + 3.9).lead, LEAD + 0.2, "four seconds held after the second");
  assert.ok(at(played, 8 + 7).lead <= LEAD + 0.02 + 1e-9, "and back three seconds later");
  near(at(played, 16 + 7.9).lead, LEAD + 0.2, "eight seconds held after the third");
  assert.ok(at(played, 16 + 11).lead <= LEAD + 0.02 + 1e-9, "and back three seconds later");
});

test("a render clock slower than the server's does not build up delay until a rebuild", t => {
  const { deliver } = setup(t);
  const played = [];
  // The speaker's clock runs half a percent slow: 0.6 s behind after two
  // minutes, past the 350 ms window the page accepts.
  for (const p of steps(0, 120, 0.1)) {
    const result = deliver(p, 1 + p * 0.995);
    assert.ok(result, `batch at ${p.toFixed(2)} was refused`);
    played.push(result);
  }
  assert.ok(played.at(-1).lead <= LEAD + 0.02 + 0.006, `lead after two minutes: ${played.at(-1).lead}`);
  for (let index = 1; index < played.length; index++) assert.ok(played[index].at >= played[index - 1].at);
});

test("catching up never moves a note before one already scheduled", t => {
  const { deliver } = setup(t);
  for (const p of steps(0, 14)) assert.ok(deliver(p, 1 + p + (Math.abs(p - 2) < 1e-9 ? 0.2 : 0)));
  // Two batches carry notes at the same instant: the second cannot move
  // earlier than the first, whatever lead is left to give back.
  const first = deliver(14, 15), second = deliver(14, 15.01);
  assert.ok(first && second);
  assert.ok(second.at >= first.at, `${second.at} < ${first.at}`);
  const third = deliver(14.05, 15.05);
  assert.ok(third.at > second.at);
});

test("the lead comes back to where a typical batch lands, not the luckiest one", t => {
  const { deliver } = setup(t);
  const played = [];
  const jitter = [0, 0.02, 0.04];
  steps(0, 30).forEach((p, index) => {
    const late = Math.abs(p - 2) < 1e-9 ? 0.2 : jitter[index % 3];
    const result = deliver(p, 1 + p + late);
    assert.ok(result, `batch at ${p.toFixed(2)} was refused`);
    played.push({ ...result, late });
  });
  const last = played.filter(entry => entry.p >= 28);
  const typical = last.filter(entry => entry.late === 0.02).map(entry => entry.lead);
  assert.ok(typical.every(lead => lead >= LEAD - 1e-9 && lead <= LEAD + 0.02 + 1e-9), `typical batches land ${typical}`);
  assert.ok(last.every(entry => entry.lead >= LEAD - 0.02 - 1e-9), "the most delayed land at most the jitter closer");
  assertSpacing(played.filter(entry => entry.p > 2 && entry.late === 0));
});

// **An anchor set by a batch that came early is moved later.** The batch that
// sets the anchor can be delayed less than those after it — the first after a
// rebuild often is — and then every later one lands closer than the page's
// lead, with less room for the next late batch. The anchor moves later until
// the typical batch lands one lead ahead.
test("batches landing closer than the lead move the anchor later", t => {
  const { deliver, report } = setup(t);
  const played = [];
  for (const p of steps(0, 12)) {
    const result = deliver(p, 1 + p + (p === 0 ? 0 : 0.06));
    assert.ok(result, `batch at ${p.toFixed(2)} was refused`);
    played.push(result);
  }
  near(played[1].lead, LEAD - 0.06, "the batches after the first land 60 ms closer");
  assert.ok(played.at(-1).lead >= LEAD - 0.02 - 1e-9 && played.at(-1).lead <= LEAD + 1e-9, `then ${played.at(-1).lead}`);
  assertSpacing(played.filter(entry => entry.p > 0));
  const { earlier, later } = report();
  assert.equal(earlier, 0);
  assert.ok(later > 35 && later <= 60 + 1e-6, `moved later by ${later} ms`);
});

test("a render clock faster than the server's moves the anchor later instead of falling late", t => {
  const { deliver } = setup(t);
  const played = [];
  for (const p of steps(0, 120, 0.1)) {
    const result = deliver(p, 1 + p * 1.005);
    assert.ok(result, `batch at ${p.toFixed(2)} was refused`);
    played.push(result);
  }
  assert.ok(played.every(entry => entry.lead > 0.05), `closest ${Math.min(...played.map(entry => entry.lead))}`);
  assert.ok(played.at(-1).lead >= LEAD - 0.02 - 0.006, `lead after two minutes: ${played.at(-1).lead}`);
});
