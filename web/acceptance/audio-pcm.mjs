// Native PCM rendering acceptance, with no listening server or external assets.
// Syntax checking does not prove rendering; PASS is printed only after a run.
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const engineName = process.argv[2] || "chromium";
if (!process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-pcm.mjs [chromium|webkit]");
}
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName), "unsupported browser engine");
const browser = await engine.launch({ headless: true });
try {
  const page = await browser.newPage();
  await page.route("http://wfeature.test/**", async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/") return route.fulfill({ contentType: "text/html", body: "<!doctype html><title>PCM audio acceptance</title>" });
    if (!/^\/[a-z0-9-]+\.js$/.test(path)) return route.abort();
    return route.fulfill({ contentType: "text/javascript", body: await readFile(new URL(`..${path}`, import.meta.url), "utf8") });
  });
  await page.goto("http://wfeature.test/");
  const results = await page.evaluate(async () => {
    const { PageAudio } = await import("./audio.js");
    const { playAudioEvents } = await import("./session.js");
    const rate = 48000, lead = .1, fade = .005;
    const authored = duration => Float32Array.from({ length: Math.round(rate * duration) }, (_, frame) => {
      const at = frame / rate;
      return .09 * Math.sin(2 * Math.PI * (173 * at + 311 * at * at)) + .023 * Math.cos(2 * Math.PI * 487 * at);
    });
    const cc = (at, sound, pcmChannel, control, value) => ({ kind: "pcmControl", at, sound, pcmChannel, control, value });
    const wave = (at, sound, pcmChannel, samples) => ({ kind: "playWave", at, sound, pcmChannel, samples, channels: 1, rate, cacheable: true });
    const stop = (at, sound) => ({ kind: "stopSound", at, sound });
    const held = authored(.55), shared = authored(.45), restarted = authored(.6), complete = authored(.75);
    const trimmedFrames = rate / 4, suffix = complete.subarray(trimmedFrames);

    // Reference plans are authored independently of event dispatch. Each change
    // gives [time, volume, expression, pan], and each voice states its stop/offset.
    const laws = [
      [.05, 64, 127, 64], [.10, 64, 96, 64], [.15, 64, 96, 0], [.20, 64, 96, 127],
      [.25, 0, 96, 127], [.30, 127, 96, 127], [.35, 127, 0, 127], [.40, 127, 127, 127], [.45, 127, 127, 64],
    ];
    const leftGroup = [[.08, 0, 127, 0], [.13, 127, 127, 0]];
    const restoreChanges = [[.12, 64, 127, 127], [.20, 0, 127, 127], [.30, 127, 127, 127]];
    const scenarios = [
      {
        name: "squared-gain-pan-and-midwave-mute", duration: .8,
        events: [wave(0, 11, 1, held),
          cc(.05, 11, 1, 7, 64), cc(.10, 11, 1, 11, 96), cc(.15, 11, 1, 10, 0), cc(.20, 11, 1, 10, 127),
          cc(.25, 11, 1, 7, 0), cc(.30, 11, 1, 7, 127), cc(.35, 11, 1, 11, 0), cc(.40, 11, 1, 11, 127), cc(.45, 11, 1, 10, 64)],
        voices: [{ at: 0, samples: held, initial: [127, 127, 64], changes: laws }],
        windows: [
          { name: "default-pan-64", from: .005, to: .04, audible: [0, 1] },
          { name: "pan-left", from: .16, to: .19, audible: [0], silent: [1] },
          { name: "pan-right", from: .21, to: .24, audible: [1], silent: [0] },
          { name: "volume-zero", from: .26, to: .29, silent: [0, 1] },
          { name: "expression-zero", from: .36, to: .39, silent: [0, 1] },
          { name: "unmuted", from: .42, to: .44, audible: [1] },
        ],
      },
      {
        name: "overlapping-groups-and-owners", duration: .75,
        events: [cc(0, 11, 1, 10, 0), cc(0, 11, 2, 10, 127), cc(0, 22, 1, 7, 64),
          wave(0, 11, 1, shared), wave(0, 11, 2, shared), wave(0, 22, 1, shared), wave(.04, 11, 1, shared),
          cc(.08, 11, 1, 7, 0), cc(.13, 11, 1, 7, 127), cc(.18, 11, 2, 11, 32), cc(.23, 22, 1, 10, 127),
          { kind: "controlChange", at: .28, sound: 11, channel: 0, control: 7, value: 0 }],
        voices: [
          { at: 0, samples: shared, initial: [127, 127, 0], changes: leftGroup },
          { at: 0, samples: shared, initial: [127, 127, 127], changes: [[.18, 127, 32, 127]] },
          { at: 0, samples: shared, initial: [64, 127, 64], changes: [[.23, 64, 127, 127]] },
          { at: .04, samples: shared, initial: [127, 127, 0], changes: leftGroup },
        ],
      },
      {
        name: "future-stop-and-fresh-owner-generation", duration: .85, retired: 2,
        events: [cc(0, 11, 1, 10, 0), wave(0, 11, 1, restarted), wave(0, 22, 1, restarted),
          wave(.02, 11, 2, restarted), stop(.12, 11), wave(.14, 11, 1, restarted),
          cc(.16, 11, 1, 7, 0), cc(.20, 11, 1, 7, 127), cc(.22, 11, 1, 10, 127), stop(.30, 11)],
        voices: [
          { at: 0, samples: restarted, initial: [127, 127, 0], stop: .12 },
          { at: 0, samples: restarted, initial: [127, 127, 64] },
          { at: .02, samples: restarted, initial: [127, 127, 64], stop: .12 },
          { at: .14, samples: restarted, initial: [127, 127, 64], stop: .30,
            changes: [[.16, 0, 127, 64], [.20, 127, 127, 64], [.22, 127, 127, 127]] },
        ],
      },
      {
        name: "trimmed-restored-pcm", duration: .75, origin: 12.5, reset: true, trimmedFrames,
        events: [cc(0, 11, 65535, 7, 64), cc(0, 11, 65535, 11, 96), cc(0, 11, 65535, 10, 127), wave(0, 11, 65535, suffix),
          cc(.12, 11, 65535, 11, 127), cc(.20, 11, 65535, 7, 0), cc(.30, 11, 65535, 7, 127)],
        // The reference plays the complete buffer at an offset, never the
        // already-trimmed array sent to PageAudio.
        voices: [{ at: 0, samples: complete, offset: trimmedFrames / rate, initial: [64, 96, 127], changes: restoreChanges }],
        windows: [{ name: "restored-unmute", from: .32, to: .4, audible: [1], silent: [0] }],
      },
      {
        name: "fractional-restored-pcm", duration: .75, origin: 12.5, reset: true, trimmedFrames,
        events: [cc(0, 11, 1, 10, 0), cc(0, 22, 1, 10, 127),
          { ...wave(0, 11, 1, suffix), framePhase: 500000000 },
          { ...wave(0, 22, 1, suffix), framePhase: 999999999 }],
        // Compare the same supplied suffix against independently authored
        // native offsets. A prior resampler's hidden history is not portable.
        voices: [
          { at: 0, samples: suffix, offset: .5 / rate, initial: [127, 127, 0] },
          { at: 0, samples: suffix, offset: .999999999 / rate, initial: [127, 127, 127] },
        ],
      },
    ];

    const renderPage = async scenario => {
      const context = new OfflineAudioContext(2, Math.round(rate * scenario.duration), rate);
      const audio = new PageAudio();
      const clock = { state: "running", currentTime: 0, baseLatency: 0, sampleRate: rate };
      const counts = { sources: 0, buffers: 0, ended: 0, starts: [] };
      for (const name of ["createGain", "createBuffer", "createBufferSource", "createChannelMerger"]) {
        clock[name] = (...args) => {
          const node = context[name](...args);
          if (name === "createBuffer") counts.buffers++;
          if (name === "createBufferSource") {
            counts.sources++;
            const start = node.start.bind(node);
            node.start = (...values) => { counts.starts.push(values[0]); return start(...values); };
            node.addEventListener("ended", () => { counts.ended++; });
          }
          return node;
        };
      }
      audio.context = clock;
      audio.midiGain = context.createGain(); audio.midiGain.connect(context.destination);
      audio.waveGain = context.createGain(); audio.waveGain.connect(context.destination);
      const origin = scenario.origin || 0;
      const first = [{ kind: "clock", at: origin }];
      if (scenario.reset) first.unshift({ kind: "allOff" });
      if (playAudioEvents(audio, first) !== true) throw new Error(`${scenario.name}: initial clock refused`);
      const events = scenario.events.map(event => ({ ...event, at: origin + event.at })).sort((a, b) => a.at - b.at);
      let cursor = 0;
      const lastPacket = Math.ceil(Math.max(...scenario.events.map(event => event.at)) * 20);
      for (let packet = 0; packet <= lastPacket; packet++) {
        const frontier = packet / 20;
        clock.currentTime = frontier;
        const batch = [];
        while (cursor < events.length && events[cursor].at <= origin + frontier) batch.push(events[cursor++]);
        batch.push({ kind: "clock", at: origin + frontier });
        if (playAudioEvents(audio, batch) !== true) throw new Error(`${scenario.name}: packet ${packet} refused`);
      }
      if (cursor !== events.length) throw new Error(`${scenario.name}: undelivered events`);
      counts.retiredBefore = audio.retiredOutputs.size;
      const rendered = await context.startRendering();
      await new Promise(resolve => setTimeout(resolve, 0));
      counts.activeAfter = audio.sources.size;
      counts.retiredAfter = audio.retiredOutputs.size;
      return { samples: [rendered.getChannelData(0), rendered.getChannelData(1)], counts };
    };

    // Independent native graph: squared volume/expression and equal-power pan.
    // Neutral initial volume is compatibility policy; expression=127/pan=64
    // are the specified defaults. Five-millisecond smoothing is page policy.
    const coefficients = ([volume, expression, pan]) => {
      const amplitude = ((volume * expression) / (127 * 127)) ** 2;
      const angle = (pan / 127) * (Math.PI / 2);
      return [amplitude * Math.cos(angle), amplitude * Math.sin(angle)];
    };
    const renderReference = async scenario => {
      const context = new OfflineAudioContext(2, Math.round(rate * scenario.duration), rate);
      for (const voice of scenario.voices) {
        const buffer = context.createBuffer(1, voice.samples.length, rate);
        buffer.getChannelData(0).set(voice.samples);
        const source = context.createBufferSource(), amplitude = context.createGain(), merger = context.createChannelMerger(2);
        source.buffer = buffer;
        amplitude.gain.value = .8;
        source.connect(amplitude);
        const sides = [context.createGain(), context.createGain()];
        let previous = coefficients(voice.initial);
        for (const [index, side] of sides.entries()) {
          side.gain.value = previous[index];
          amplitude.connect(side);
          side.connect(merger, 0, index);
        }
        for (const [at, ...state] of voice.changes || []) {
          const next = coefficients(state);
          for (const [index, side] of sides.entries()) {
            side.gain.setValueAtTime(previous[index], lead + at);
            side.gain.linearRampToValueAtTime(next[index], lead + at + fade);
          }
          previous = next;
        }
        merger.connect(context.destination);
        source.start(lead + voice.at, voice.offset || 0);
        if (voice.stop !== undefined) source.stop(lead + voice.stop);
      }
      const rendered = await context.startRendering();
      return [rendered.getChannelData(0), rendered.getChannelData(1)];
    };
    const rms = (samples, from, to) => {
      const first = Math.round((lead + from) * rate), last = Math.round((lead + to) * rate);
      let sum = 0;
      for (let frame = first; frame < last; frame++) sum += samples[frame] ** 2;
      return Math.sqrt(sum / (last - first));
    };
    const results = [];
    for (const scenario of scenarios) {
      const actual = await renderPage(scenario), expected = await renderReference(scenario);
      let maximumError = 0, energy = 0;
      for (let channel = 0; channel < 2; channel++) {
        for (let frame = 0; frame < expected[channel].length; frame++) {
          maximumError = Math.max(maximumError, Math.abs(actual.samples[channel][frame] - expected[channel][frame]));
          energy += actual.samples[channel][frame] ** 2;
        }
      }
      results.push({ name: scenario.name, maximumError, energy, ...actual.counts,
        expectedSources: scenario.voices.length, expectedRetired: scenario.retired || 0,
        expectedStarts: scenario.voices.map(voice => lead + voice.at).sort((a, b) => a - b),
        trimmedFrames: scenario.trimmedFrames || 0,
        windows: (scenario.windows || []).map(window => ({ ...window, rms: actual.samples.map(samples => rms(samples, window.from, window.to)) })),
      });
    }
    return results;
  });
  for (const result of results) {
    assert.ok(result.energy > 1, `${result.name}: rendered silence`);
    assert.ok(result.maximumError < 1e-6, `${result.name}: native reference error ${result.maximumError}`);
    assert.equal(result.sources, result.expectedSources, `${result.name}: controls restarted or lost sources`);
    assert.equal(result.buffers, 1, `${result.name}: immutable raw buffer was not reused across groups/owners`);
    assert.equal(result.ended, result.sources, `${result.name}: a source did not end`);
    assert.equal(result.activeAfter, 0, `${result.name}: ended sources were retained`);
    assert.equal(result.retiredBefore, result.expectedRetired, `${result.name}: scheduled stop lost a graph generation`);
    assert.equal(result.retiredAfter, 0, `${result.name}: retired graphs were retained`);
    const starts = result.starts.sort((a, b) => a - b);
    for (let index = 0; index < starts.length; index++) {
      assert.ok(Math.abs(starts[index] - result.expectedStarts[index]) < 1e-9, `${result.name}: source ${index} moved in time`);
    }
    for (const window of result.windows) {
      for (const side of window.audible || []) assert.ok(window.rms[side] > 1e-4, `${result.name}/${window.name}: side ${side} is silent`);
      for (const side of window.silent || []) assert.ok(window.rms[side] < 1e-8, `${result.name}/${window.name}: side ${side} leaked audio`);
    }
  }
  console.log(`PASS ${engineName}: native PCM laws, live controls, isolated overlap, future stop/restart and restored suffix match; ${JSON.stringify(results)}`);
} finally {
  await browser.close();
}
