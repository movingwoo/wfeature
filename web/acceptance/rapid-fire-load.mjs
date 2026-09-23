// Fixed-rate server load probe. The auto phase models an already toggled-on OK stream. Requires Node 22+, ps (macOS/Linux), and a server binary.
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { mkdirSync, copyFileSync, rmSync, writeFileSync } from "node:fs";
import { resolve, join } from "node:path";
import net from "node:net";

const [binary, secondsText = "10"] = process.argv.slice(2);
const seconds = Number(secondsText);
if (!binary || !Number.isFinite(seconds) || seconds < 2) throw new Error("Usage: node web/acceptance/rapid-fire-load.mjs SERVER [SECONDS_PER_PHASE>=2]");
const run = `rapid-fire-load-${Date.now()}`;
const output = resolve("var/acceptance", run), games = resolve("var/games", `.${run}`);
const ext = resolve("var/ext", run), saves = resolve("var/savedata", run);
for (const path of [output, games, ext, saves]) mkdirSync(path, { recursive: true });
mkdirSync(join(games, "library"));
copyFileSync("internal/platform/skt/testdata/canvas-skt.zip", join(games, "library", "canvas.zip"));
const listener = net.createServer();
await new Promise(done => listener.listen(0, "127.0.0.1", done));
const port = listener.address().port;
await new Promise(done => listener.close(done));
const server = spawn(resolve(binary), ["-addr", `127.0.0.1:${port}`, "-games", games, "-ext", ext,
  "-saves", saves, "-logs", join(output, "logs"), "-open=false"]);
let logs = "", socket, timer;
server.stdout.on("data", data => { logs += data; });
server.stderr.on("data", data => { logs += data; });
const pause = ms => new Promise(done => setTimeout(done, ms));
const result = { seconds, samples: [], errors: [] };
const cpuSeconds = () => {
  const time = execFileSync("ps", ["-p", String(server.pid), "-o", "time="], { encoding: "utf8" }).trim();
  return time.split(":").reduce((total, field) => total * 60 + Number(field), 0);
};
try {
  for (let i = 0; ; i++) {
    try { if ((await fetch(`http://127.0.0.1:${port}/api/status`)).ok) break; } catch {}
    if (i === 100 || server.exitCode !== null) throw new Error("server did not start");
    await pause(50);
  }
  let ready = false, started = false, frames = 0;
  socket = new WebSocket(`ws://127.0.0.1:${port}/api/session`);
  socket.onmessage = event => {
    if (typeof event.data !== "string") { frames++; return; }
    const message = JSON.parse(event.data);
    if (message.kind === "ready") ready = true;
    if (message.kind === "started") started = true;
    if (message.kind === "error") result.errors.push(message.message);
  };
  const until = async condition => {
    for (let i = 0; !condition(); i++) {
      if (result.errors.length) throw new Error(result.errors.join("; "));
      if (i === 200) throw new Error("session response timed out");
      await pause(50);
    }
  };
  await until(() => ready);
  socket.send(JSON.stringify({ kind: "start", game: "games/library/canvas.zip", value: 1 }));
  await until(() => started && frames > 0);
  await pause(1000);
  for (let round = 1; round <= 2; round++) {
    for (const [mode, codes] of [["off", []], ["auto", [148]], ["manual-both", [53, 148]]]) {
      let messages = 0, down = false, bytes = 0;
      const send = action => { for (const code of codes) {
        const text = JSON.stringify({ kind: "key", action, code });
        socket.send(text); messages++; bytes += Buffer.byteLength(text);
      } };
      const beforeCPU = cpuSeconds(), beforeFrames = frames, beforeTime = performance.now();
      if (codes.length) timer = setInterval(() => { down = !down; send(down ? "press" : "release"); }, 50);
      await pause(seconds * 1000);
      clearInterval(timer); timer = null;
      if (down) send("release");
      const elapsed = (performance.now() - beforeTime) / 1000;
      const cpu = cpuSeconds() - beforeCPU;
      const sample = { round, mode, elapsed, messages, jsonBytes: bytes, frames: frames - beforeFrames, cpuSeconds: cpu, corePercent: cpu / elapsed * 100 };
      result.samples.push(sample);
      console.log(JSON.stringify(sample));
      await pause(500);
    }
  }
  assert.deepEqual(result.errors, []);
} finally {
  clearInterval(timer);
  socket?.close();
  server.kill("SIGTERM");
  await new Promise(done => { if (server.exitCode !== null) done(); else server.once("exit", done); });
  writeFileSync(join(output, "result.json"), JSON.stringify(result, null, 2));
  writeFileSync(join(output, "server.log"), logs);
  rmSync(ext, { recursive: true, force: true });
}
