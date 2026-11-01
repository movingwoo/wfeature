// Where the page's FM synthesizer runs.
//
// An AudioWorklet runs it on the audio thread, but only a secure context has
// one: a page opened by the machine's address over plain HTTP does not. There
// the same synthesizer runs in a ScriptProcessor on the page's thread, with a
// buffer long enough to ride out a busy frame. Either way it renders into
// FM_LANES mono outputs, split so the page can route each lane through one
// channel's volume and pan.

import { FMSynth, FM_LANES } from "./fm-synth.js";

// Two buffers of this many frames sit between the page's thread and the
// speaker in the ScriptProcessor fallback: about 43 ms each at 48 kHz.
const SCRIPT_BUFFER_FRAMES = 2048;

export class FMHost {
  constructor(context, { onEnded = () => {}, report = () => {} } = {}) {
    this.context = context;
    this.onEnded = onEnded;
    this.report = report;
    this.splitter = context.createChannelSplitter(FM_LANES);
    // Nodes that render nothing audible still have to be pulled. A silent
    // path to the destination keeps the synthesizer's clock running even
    // before any lane has a channel.
    this.keepAlive = context.createGain();
    this.keepAlive.gain.value = 0;
    this.keepAlive.connect(context.destination);
    this.lanes = new Array(FM_LANES).fill(null);
    // Notes the synthesizer has not reported ended. If it stops, the page
    // still learns they are over, so their places in its budget come back.
    this.live = new Set();
    this.queued = [];
    this.flushing = false;
    this.node = null;
    this.synth = null;
    this.mode = "pending";
    const worklet = context.audioWorklet;
    if (worklet && typeof globalThis.AudioWorkletNode === "function") {
      worklet.addModule(new URL("./fm-synth.js", import.meta.url).href)
        .then(() => this.attachWorklet())
        .catch(error => {
          this.report(`audio worklet unavailable, synthesizing on the page: ${error.message}`);
          this.attachScript();
        });
    } else {
      this.attachScript();
    }
  }

  attachWorklet() {
    if (this.closed) return;
    try {
      const node = new globalThis.AudioWorkletNode(this.context, "wfeature-fm", {
        numberOfInputs: 0, numberOfOutputs: 1, outputChannelCount: [FM_LANES],
      });
      node.port.onmessage = event => this.ended(event.data);
      // A processor that throws stops rendering for good. Its notes end, and
      // the page's own thread takes over.
      node.onprocessorerror = () => {
        this.report("audio worklet stopped, synthesizing on the page");
        node.disconnect?.();
        this.node = null;
        this.ended([...this.live]);
        this.attachScript();
        for (const [lane, destination] of this.lanes.entries()) {
          if (destination !== null) this.post({ op: "lane", lane, ratio: 1, depth: 0 });
        }
      };
      this.connect(node);
      this.mode = "worklet";
    } catch (error) {
      this.report(`audio worklet failed, synthesizing on the page: ${error.message}`);
      this.attachScript();
    }
  }

  attachScript() {
    if (this.closed || this.node) return;
    let node;
    try {
      node = this.context.createScriptProcessor(SCRIPT_BUFFER_FRAMES, 0, FM_LANES);
    } catch {
      // Some engines refuse a processor without inputs.
      node = this.context.createScriptProcessor(SCRIPT_BUFFER_FRAMES, 1, FM_LANES);
    }
    const synth = new FMSynth(this.context.sampleRate);
    const ended = [];
    synth.onEnded = id => ended.push(id);
    const lanes = [];
    node.onaudioprocess = event => {
      const buffer = event.outputBuffer;
      lanes.length = buffer.numberOfChannels;
      for (let index = 0; index < buffer.numberOfChannels; index++) lanes[index] = buffer.getChannelData(index);
      const start = Number.isFinite(event.playbackTime) ? event.playbackTime : this.context.currentTime;
      try {
        synth.render(lanes, buffer.length, start);
      } catch (error) {
        // Never leave a half-rendered state to throw again every buffer.
        this.report(`audio synthesizer failed and was reset: ${error.message}`);
        for (const lane of lanes) lane.fill(0);
        synth.handle({ op: "reset" });
      }
      this.deliver(ended);
    };
    this.synth = synth;
    this.endedQueue = ended;
    this.connect(node);
    this.mode = "script";
  }

  deliver(ended) {
    if (ended.length) this.ended(ended.splice(0));
  }

  ended(ids) {
    for (const id of ids) this.live.delete(id);
    if (ids.length) this.onEnded(ids);
  }

  connect(node) {
    this.node = node;
    node.connect(this.splitter);
    node.connect(this.keepAlive);
    this.flush();
    if (this.endedQueue?.length) queueMicrotask(() => this.deliver(this.endedQueue));
  }

  // post queues a command. Commands reach the synthesizer in the order they
  // were posted: directly on the page's thread, or in one message per task
  // to the worklet.
  post(command) {
    if (command.op === "on") this.live.add(command.id);
    if (this.synth) {
      this.synth.handle(command);
      // A command can end voices at once (a released lane). Report them
      // after the caller's own bookkeeping, as the worklet would.
      if (this.endedQueue.length && !this.delivering) {
        this.delivering = true;
        queueMicrotask(() => {
          this.delivering = false;
          this.deliver(this.endedQueue);
        });
      }
      return;
    }
    this.queued.push(command);
    if (this.node && !this.flushing) {
      this.flushing = true;
      queueMicrotask(() => this.flush());
    }
  }

  flush() {
    this.flushing = false;
    if (!this.node || !this.queued.length) return;
    const commands = this.queued.splice(0);
    if (this.synth) {
      for (const command of commands) this.synth.handle(command);
    } else {
      this.node.port.postMessage(commands);
    }
  }

  // lane claims an output for a channel's gain node, or answers null when
  // every lane is in use. The synthesizer starts it unbent.
  lane(destination, ratio = 1, depth = 0) {
    const lane = this.lanes.indexOf(null);
    if (lane < 0) return null;
    this.lanes[lane] = destination;
    this.splitter.connect(destination, lane);
    this.post({ op: "lane", lane, ratio, depth });
    return lane;
  }

  // release returns a lane, silencing whatever still sounds in it.
  release(lane) {
    const destination = this.lanes[lane];
    if (destination === null || destination === undefined) return;
    this.lanes[lane] = null;
    try {
      this.splitter.disconnect(destination, lane);
    } catch {
      // Already disconnected with its channel.
    }
    this.post({ op: "release", lane });
  }

  close() {
    this.closed = true;
    this.node?.disconnect?.();
    this.splitter.disconnect?.();
    this.keepAlive.disconnect?.();
  }
}
