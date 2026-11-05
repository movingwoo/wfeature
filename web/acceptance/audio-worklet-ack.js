// Loaded into an offline context's AudioWorkletGlobalScope before the page's
// synthesizer module (see audio-offline.js). The synthesizer's processor then
// answers every batch of commands it has handled with an empty list, which its
// host reads as "no notes ended". A harness that steps an OfflineAudioContext
// counts those answers and resumes rendering only once every batch it posted
// has reached the audio thread: a batch that arrived a few quanta late would
// shift its notes, and offline rendering runs far ahead of any message.
// Nothing else about the processor changes.
const register = globalThis.registerProcessor;
globalThis.registerProcessor = (name, processor) => register.call(globalThis, name, name !== "wfeature-fm" ? processor
  : class extends processor {
    constructor(...args) {
      super(...args);
      const handle = this.port.onmessage;
      this.port.onmessage = event => {
        handle.call(this.port, event);
        this.port.postMessage([]);
      };
    }
  });
