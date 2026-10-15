// The game controller driver of web/acceptance/ios-gamepad.mjs, linked into a
// simulator build next to the app's own App.swift, which it does not change.
// load.c calls padcheck_start before main. Once the app is active the driver
// finds the web view, starts the fixture game from inside the page, connects a
// hidden GCVirtualController — a GameController device in this process, which
// is where WebKit reads controllers from — presses it programmatically, and
// prints what the page sent: one `PADCHECK {"step": …, "value": …}` line per
// step, then exits. PADCHECK_MODE=resign also takes first responder away from
// the web view before pressing, and gives it back the way the app does.
import GameController
import UIKit
import WebKit

@_cdecl("padcheck_start")
public func padcheckStart() {
    var started = false
    NotificationCenter.default.addObserver(
        forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main
    ) { _ in
        if started { return }
        started = true
        Task { @MainActor in await PadDriver().run() }
    }
}

private func report(_ step: String, _ value: Any?) {
    let body: [String: Any] = ["step": step, "value": value ?? NSNull()]
    let line = (try? JSONSerialization.data(withJSONObject: body)).flatMap { String(data: $0, encoding: .utf8) }
    print("PADCHECK " + (line ?? "{\"step\":\"unprintable\"}"))
    fflush(stdout)
}

@MainActor
private final class PadDriver {
    private var webView: WKWebView?
    private var pad: GCVirtualController?
    private let mode = ProcessInfo.processInfo.environment["PADCHECK_MODE"] ?? "plain"

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

    // UIKit has no public way to ask; the key window answers the private
    // selector, and this build never ships.
    private func firstResponder() -> String {
        guard let window = windows.first(where: { $0.isKeyWindow }) ?? windows.first,
              let responder = window.value(forKey: "firstResponder") else { return "none" }
        return String(describing: type(of: responder))
    }

    private let hook = """
    (() => {
      if (window.padcheck) return 'already';
      window.padcheck = { keys: [], events: [] };
      const send = WebSocket.prototype.send;
      WebSocket.prototype.send = function(data) {
        try {
          const m = JSON.parse(data);
          if (m.kind === 'key') window.padcheck.keys.push((m.action === 'press' ? '+' : '-') + m.code);
        } catch {}
        return send.call(this, data);
      };
      addEventListener('gamepadconnected', e => window.padcheck.events.push(e.gamepad.mapping));
      return 'hooked';
    })()
    """

    private func fail(_ why: String) -> Never {
        report("failed", why)
        exit(1)
    }

    func run() async {
        report("mode", mode)
        for _ in 0..<120 {
            if let web = windows.compactMap({ $0.rootViewController?.view }).compactMap({ self.find($0) }).first {
                webView = web
                break
            }
            await pause(500)
        }
        if webView == nil { fail("no web view") }
        for _ in 0..<120 {
            let ready = await js("!!document.getElementById('game-start') && !document.getElementById('game-start').disabled")
            if (ready as? Bool) == true { break }
            await pause(500)
        }
        report("first responder at load", firstResponder())
        report("hook", await js(hook))
        report("start", await js("""
        (() => {
          const select = document.getElementById('game-select');
          const option = [...select.options].find(o => o.value.includes('ios-gamepad-check'));
          if (!option) return 'no fixture among ' + [...select.options].map(o => o.value).join(', ');
          select.value = option.value;
          select.dispatchEvent(new Event('change'));
          document.getElementById('game-start').click();
          return option.value;
        })()
        """))
        for _ in 0..<60 {
            let running = await js("!document.getElementById('restart').classList.contains('hidden')")
            if (running as? Bool) == true { break }
            await pause(500)
        }
        await pause(1500)
        report("running", await js("!document.getElementById('restart').classList.contains('hidden')"))
        if mode == "resign" {
            // What a view controller presented over the web view leaves behind.
            _ = webView?.resignFirstResponder()
        }
        report("first responder before the controller", firstResponder())

        let configuration = GCVirtualController.Configuration()
        configuration.elements = [
            GCInputButtonA, GCInputButtonB, GCInputButtonX, GCInputButtonY,
            GCInputDirectionPad, GCInputLeftThumbstick, GCInputRightThumbstick,
            GCInputLeftShoulder, GCInputRightShoulder, GCInputLeftTrigger, GCInputRightTrigger,
        ]
        configuration.isHidden = true
        let pad = GCVirtualController(configuration: configuration)
        self.pad = pad
        do {
            try await pad.connect()
        } catch {
            fail("connect: \(error)")
        }
        report("connected", pad.controller?.vendorName ?? "?")
        await pause(1500)

        await press("A (first)", GCInputButtonA)
        report("page events", await js("window.padcheck.events"))
        await press("A", GCInputButtonA)
        await press("B", GCInputButtonB)
        await press("X", GCInputButtonX)
        await press("Y", GCInputButtonY)
        await press("LB", GCInputLeftShoulder)
        await press("RB", GCInputRightShoulder)
        await press("LT", GCInputLeftTrigger)
        await press("RT", GCInputRightTrigger)
        await move("pad up", GCInputDirectionPad, 0, 1)
        await move("pad right", GCInputDirectionPad, 1, 0)
        await move("pad down", GCInputDirectionPad, 0, -1)
        await move("pad left", GCInputDirectionPad, -1, 0)
        await move("left stick left", GCInputLeftThumbstick, -1, 0)
        await move("right stick up", GCInputRightThumbstick, 0, 1)
        await move("right stick up-left", GCInputRightThumbstick, -0.7, 0.7)
        if mode == "resign" {
            // The app's remedy: App.swift's takeControllers does this.
            report("take back", webView?.becomeFirstResponder() ?? false)
            report("first responder after taking it back", firstResponder())
            await press("A after taking it back", GCInputButtonA)
        }
        report("done", true)
        pad.disconnect()
        await pause(300)
        exit(0)
    }

    private func press(_ name: String, _ element: String) async {
        pad?.setValue(1, forButtonElement: element)
        await pause(300)
        pad?.setValue(0, forButtonElement: element)
        await pause(300)
        report(name, await js("window.padcheck.keys.splice(0)"))
    }

    private func move(_ name: String, _ element: String, _ x: CGFloat, _ y: CGFloat) async {
        pad?.setPosition(CGPoint(x: x, y: y), forDirectionPadElement: element)
        await pause(300)
        pad?.setPosition(.zero, forDirectionPadElement: element)
        await pause(300)
        report(name, await js("window.padcheck.keys.splice(0)"))
    }
}
