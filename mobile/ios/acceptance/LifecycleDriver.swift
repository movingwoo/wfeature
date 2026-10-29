// The driver of web/acceptance/ios-lifecycle.mjs, linked into a simulator
// build next to the app's own App.swift, which it does not change.
// lifecycle-load.c calls lifecheck_start before main.
//
// Once the app is active the driver finds the web view and gives the page a
// hook that notes its sockets, what they send and receive, its visibility and
// how many times it has loaded. It adds the archives the script left in the
// app's temporary directory through the page's own add-game request, and runs
// one cycle per archive in LIFECHECK_PLAN: it starts the game from inside the
// page, presses the plan's keys, sets a marker in the page and prints `ready`.
// The script sends the app to the background and brings it back; the driver
// prints what the page shows (`after`), waits for the script's `continue-<n>`
// file, presses the plan's probe key (`alive`) and goes on. Every line is
// `LIFECHECK {"step": …, "value": …}`. It does not exit after `done`: the
// script takes the app down with the device.
import UIKit
import WebKit

@_cdecl("lifecheck_start")
public func lifecheckStart() {
    let center = NotificationCenter.default
    center.addObserver(forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main) { _ in
        MainActor.assumeIsolated { LifecycleDriver.shared.becameActive() }
    }
    center.addObserver(forName: UIApplication.didEnterBackgroundNotification, object: nil, queue: .main) { _ in
        report("background", ProcessInfo.processInfo.processIdentifier)
    }
    center.addObserver(forName: UIApplication.willEnterForegroundNotification, object: nil, queue: .main) { _ in
        report("foreground", ProcessInfo.processInfo.processIdentifier)
    }
}

private func report(_ step: String, _ value: Any?) {
    let body: [String: Any] = ["step": step, "value": value ?? NSNull()]
    let line = (try? JSONSerialization.data(withJSONObject: body)).flatMap { String(data: $0, encoding: .utf8) }
    print("LIFECHECK " + (line ?? "{\"step\":\"unprintable\"}"))
    fflush(stdout)
}

private struct Cycle: Decodable {
    let name: String
    let before: [String]
    let probe: String?
    let kept: String?
}

@MainActor
private final class LifecycleDriver {
    static let shared = LifecycleDriver()

    private var webView: WKWebView?
    private var activations = 0
    private var started = false
    private let pid = ProcessInfo.processInfo.processIdentifier
    private let folder = FileManager.default.temporaryDirectory.appendingPathComponent("lifecheck")

    func becameActive() {
        activations += 1
        if started { return }
        started = true
        Task { await run() }
    }

    private func pause(_ ms: UInt64) async {
        try? await Task.sleep(nanoseconds: ms * 1_000_000)
    }

    private func js(_ source: String) async -> Any? {
        guard let webView else { return nil }
        do {
            return try await webView.evaluateJavaScript(source)
        } catch {
            return "error: \(error.localizedDescription)"
        }
    }

    // What the page's hook says about it, as an object for the report.
    private func state() async -> Any {
        guard let text = await js("window.lifecheck ? window.lifecheck.state() : 'null'") as? String,
              let data = text.data(using: .utf8),
              let value = try? JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed]) else {
            return NSNull()
        }
        return value
    }

    private func until(_ seconds: Double, _ condition: String) async -> Bool {
        let deadline = Date().addingTimeInterval(seconds)
        while Date() < deadline {
            if (await js(condition) as? Bool) == true { return true }
            await pause(250)
        }
        return false
    }

    private func fail(_ why: String) -> Never {
        report("failed", why)
        exit(1)
    }

    private var windows: [UIWindow] {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        return scenes.flatMap { $0.windows }
    }

    private func find(_ view: UIView) -> WKWebView? {
        if let web = view as? WKWebView { return web }
        for child in view.subviews {
            if let web = find(child) { return web }
        }
        return nil
    }

    // Installed at the start of every document from here on, and run once by
    // hand on the page that was already up. Nothing in it changes what the
    // page does: it wraps WebSocket to watch it, and reads the canvas.
    private static let hook = """
    (() => {
      if (window.lifecheck) return 'already';
      let loads = 1;
      try {
        loads = Number(sessionStorage.getItem('lifecheck.loads') || 0) + 1;
        sessionStorage.setItem('lifecheck.loads', String(loads));
      } catch {}
      const record = { marker: null, markedAt: null, origin: performance.timeOrigin, loads, sockets: 0, log: [] };
      window.lifecheck = record;
      const note = what => { if (record.log.length < 600) record.log.push([Math.round(performance.now()), what]); };
      const quiet = new Set(['stats', 'audio', 'vibrate']);
      const Native = window.WebSocket;
      window.WebSocket = class extends Native {
        constructor(...args) {
          super(...args);
          const id = ++record.sockets;
          this.lifecheckId = id;
          note(`socket ${id} opening`);
          this.addEventListener('open', () => note(`socket ${id} open`));
          this.addEventListener('close', event => note(`socket ${id} closed ${event.code}`));
          this.addEventListener('message', event => {
            if (typeof event.data !== 'string') return;
            let message;
            try { message = JSON.parse(event.data); } catch { return; }
            if (quiet.has(message.kind)) return;
            const detail = message.started ? ` ${message.started.game || message.started.name || ''}`
              : message.kind === 'resumed' ? ` resumed=${message.resumed === true} occupied=${message.occupied === true}`
              : message.message ? ` ${message.message}` : '';
            note(`socket ${id} got ${message.kind}${detail}`);
          });
        }
        send(data) {
          try {
            const message = JSON.parse(data);
            if (message.kind !== 'pointer') {
              note(`socket ${this.lifecheckId} sent ${message.kind}${message.kind === 'key' ? ` ${message.action} ${message.code}` : ''}`);
            }
          } catch {}
          return super.send(data);
        }
      };
      document.addEventListener('visibilitychange', () => note(`visibility ${document.visibilityState}`));
      addEventListener('pagehide', () => note('pagehide'));
      addEventListener('pageshow', event => note(`pageshow persisted=${event.persisted}`));
      record.picture = () => {
        try {
          const canvas = document.getElementById('canvas');
          const pixel = canvas.getContext('2d').getImageData(canvas.width >> 1, canvas.height >> 1, 1, 1).data;
          return ((pixel[0] << 16) | (pixel[1] << 8) | pixel[2]).toString(16).padStart(6, '0');
        } catch (error) {
          return `error: ${error.message}`;
        }
      };
      record.state = () => JSON.stringify({
        marker: record.marker, markedAt: record.markedAt, origin: record.origin, loads: record.loads,
        playing: !document.getElementById('restart')?.classList.contains('hidden'),
        visibility: document.visibilityState,
        status: document.getElementById('status-text')?.textContent ?? '',
        picture: record.picture(),
        log: record.log,
      });
      return 'hooked';
    })()
    """

    private static let playing = "!document.getElementById('restart').classList.contains('hidden')"

    // A key as the page's own keyboard handler reads it: by its code, through
    // the page's key table.
    private func key(_ code: String, _ type: String) async {
        _ = await js("""
        (() => {
          const code = \(literal(code));
          document.body.dispatchEvent(new KeyboardEvent('\(type)', {
            code, key: code.replace(/^(Digit|Key)/, '').toLowerCase(), bubbles: true, cancelable: true,
          }));
          return true;
        })()
        """)
    }

    private func literal(_ text: String) -> String {
        let data = (try? JSONSerialization.data(withJSONObject: [text])) ?? Data("[\"\"]".utf8)
        let array = String(data: data, encoding: .utf8) ?? "[\"\"]"
        return String(array.dropFirst().dropLast())
    }

    private func picture() async -> String {
        (await js("window.lifecheck.picture()") as? String) ?? "unread"
    }

    func run() async {
        guard let plan = ProcessInfo.processInfo.environment["LIFECHECK_PLAN"],
              let cycles = try? JSONDecoder().decode([Cycle].self, from: Data(plan.utf8)) else {
            // A launch without the script's plan, such as one that started
            // the app afresh, has nothing to drive.
            report("no plan", pid)
            return
        }
        report("pid", pid)
        // A freshly made device is slow to bring the first app up.
        for _ in 0..<480 {
            if let web = windows.compactMap({ $0.rootViewController?.view }).compactMap({ self.find($0) }).first {
                webView = web
                break
            }
            await pause(250)
        }
        guard let webView else { fail("no web view") }
        webView.configuration.userContentController.addUserScript(
            WKUserScript(source: Self.hook, injectionTime: .atDocumentStart, forMainFrameOnly: true))
        if !(await until(120, "document.readyState === 'complete' && !!document.getElementById('game-select')")) {
            fail("the page did not load")
        }
        report("hook", await js(Self.hook))

        // The page's own add-game request, as its ＋ 게임 추가 button sends it.
        var added: [String: Any] = [:]
        for cycle in cycles {
            guard let bytes = try? Data(contentsOf: folder.appendingPathComponent(cycle.name)) else {
                fail("no archive \(cycle.name) in the app's temporary directory")
            }
            do {
                added[cycle.name] = try await webView.callAsyncJavaScript("""
                    const body = Uint8Array.from(atob(bytes), c => c.charCodeAt(0));
                    const response = await fetch('api/games?name=' + encodeURIComponent(name), { method: 'POST', body });
                    return response.status;
                    """, arguments: ["bytes": bytes.base64EncodedString(), "name": cycle.name],
                    in: nil, contentWorld: .page) ?? NSNull()
            } catch {
                fail("adding \(cycle.name): \(error.localizedDescription)")
            }
        }
        report("added", added)
        webView.reload()
        let every = cycles.map {
            "[...document.getElementById('game-select').options].some(o => o.value.includes(\(literal(stem($0.name)))))"
        }.joined(separator: " && ")
        if !(await until(120, """
            (window.lifecheck?.loads ?? 0) >= 2 && document.readyState === 'complete' &&
            !!document.getElementById('game-select') && \(every) &&
            !document.getElementById('game-start').disabled
            """)) {
            fail("the added games are not listed after a reload")
        }

        for (index, cycle) in cycles.enumerated() {
            await runCycle(index + 1, cycle)
        }
        report("done", true)
    }

    private func stem(_ name: String) -> String {
        name.hasSuffix(".zip") ? String(name.dropLast(4)) : name
    }

    private func runCycle(_ number: Int, _ cycle: Cycle) async {
        // A game left running by the cycle before is stopped the way a player
        // stops one: 다시 시작, then the page's own question.
        if (await js(Self.playing) as? Bool) == true {
            _ = await js("document.getElementById('restart').click(); true")
            if !(await until(10, "document.getElementById('confirm-dialog').classList.contains('visible')")) {
                fail("cycle \(number): the page did not ask before stopping the game")
            }
            _ = await js("document.getElementById('confirm-accept').click(); true")
            if !(await until(60, "!(\(Self.playing)) && !document.getElementById('game-start').disabled")) {
                fail("cycle \(number): the game before did not stop")
            }
        }
        let game = await js("""
        (() => {
          const select = document.getElementById('game-select');
          const option = [...select.options].find(o => o.value.includes(\(literal(stem(cycle.name)))));
          if (!option) return 'no archive among ' + [...select.options].map(o => o.value).join(', ');
          select.value = option.value;
          select.dispatchEvent(new Event('change'));
          document.getElementById('game-start').click();
          return option.value;
        })()
        """)
        if !(await until(120, Self.playing)) {
            fail("cycle \(number): \(cycle.name) did not start (\(String(describing: game)))")
        }
        await pause(1500)
        for code in cycle.before {
            await key(code, "keydown")
            await pause(400)
            await key(code, "keyup")
            await pause(600)
        }
        await pause(500)
        // On a slow machine the keys' picture can still be on its way after
        // the pauses above; the marker waits for it, so the page's state
        // before leaving is the game's picture rather than the one before it.
        // Within the script's own tolerance of 24 per channel.
        if let kept = cycle.kept, let rgb = Int(kept, radix: 16) {
            _ = await until(30, """
            (p => /^[0-9a-f]{6}$/.test(p) && [16, 8, 0].every(s =>
              Math.abs(((parseInt(p, 16) >> s) & 255) - ((\(rgb) >> s) & 255)) <= 24))(window.lifecheck.picture())
            """)
        }
        let marker = UUID().uuidString
        _ = await js("window.lifecheck.marker = \(literal(marker)); window.lifecheck.markedAt = performance.now(); true")
        let seen = activations
        report("ready", ["cycle": number, "name": cycle.name, "game": game ?? NSNull(), "pid": pid, "page": await state()])

        // The script opens Settings and, later, the app again.
        let deadline = Date().addingTimeInterval(300)
        while activations == seen {
            if Date() > deadline { fail("cycle \(number): the app was not brought back") }
            await pause(250)
        }
        // A page the app loads again on the way back does so within this.
        await pause(3000)
        _ = await until(60, "document.readyState === 'complete' && !!window.lifecheck && !!document.getElementById('game-select')")
        await pause(6000)
        report("after", ["cycle": number, "pid": pid, "page": await state()])

        let go = folder.appendingPathComponent("continue-\(number)")
        let wait = Date().addingTimeInterval(120)
        while !FileManager.default.fileExists(atPath: go.path) {
            if Date() > wait { fail("cycle \(number): the script did not let the driver go on") }
            await pause(250)
        }
        var alive: [String: Any] = ["cycle": number, "playing": (await js(Self.playing) as? Bool) ?? false]
        if let probe = cycle.probe, (alive["playing"] as? Bool) == true {
            alive["before"] = await picture()
            await key(probe, "keydown")
            // Read while the key is down and before the handset's repeat,
            // which the server starts 600 ms into a held key.
            var down: [String] = []
            for _ in 0..<5 {
                await pause(100)
                down.append(await picture())
            }
            alive["pressed"] = down
            await key(probe, "keyup")
            await pause(800)
            alive["released"] = await picture()
        }
        report("alive", alive)
    }
}
