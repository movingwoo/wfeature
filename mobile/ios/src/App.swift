// The app is the server with a web view in front of it.
//
// The same arrangement as the Android app, with one difference that decides
// the shape of both: **iOS does not let an app start a process**, so the Go
// code is linked in rather than executed. WfeatureStart is that library's
// entry point; everything below it is the same page a desktop browser loads.
//
// What this buys is the same thing it buys on Android. `http://127.0.0.1` is a
// secure context, so the page is installable and its service worker registers,
// and there is no address to find, no key to carry and no router to configure.

import UIKit
import WebKit

@main
final class AppDelegate: UIResponder, UIApplicationDelegate {
    var window: UIWindow?

    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]?)
        -> Bool {
        let window = UIWindow(frame: UIScreen.main.bounds)
        window.rootViewController = PlayViewController()
        window.makeKeyAndVisible()
        self.window = window
        // A game is watched as much as it is touched, and a screen that dims
        // in the middle of one is the first thing anybody would report.
        application.isIdleTimerDisabled = true
        return true
    }

    func applicationWillTerminate(_ application: UIApplication) {
        WfeatureStop()
    }
}

final class PlayViewController: UIViewController {
    private var webView: WKWebView!
    private let message = UILabel()

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = UIColor(red: 0.09, green: 0.10, blue: 0.11, alpha: 1)

        message.text = "에뮬레이터를 시작하는 중..."
        message.textColor = .white
        message.textAlignment = .center
        message.numberOfLines = 0
        message.frame = view.bounds
        message.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(message)

        // Starting the server touches the filesystem, so it is not done on the
        // thread that is drawing.
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let port = WfeatureStart(strdup(Self.documentsDirectory().path))
            DispatchQueue.main.async {
                guard let self else { return }
                if port == 0 {
                    self.message.text = "서버를 시작하지 못했습니다.\n" + Self.lastError()
                    return
                }
                self.show(url: URL(string: "http://127.0.0.1:\(port)")!)
            }
        }
    }

    /// The app's Documents directory, which is where the games and the saves
    /// live. It is the one place the app may write, and with
    /// `UIFileSharingEnabled` in the plist it is also a folder the Files app
    /// can open — so a game can arrive from the page's own ＋ 게임 추가 button
    /// or by being dropped in from Files, and neither needs the other.
    private static func documentsDirectory() -> URL {
        FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
    }

    private static func lastError() -> String {
        guard let raw = WfeatureLastError() else { return "" }
        defer { free(raw) }
        return String(cString: raw)
    }

    private func show(url: URL) {
        let configuration = WKWebViewConfiguration()
        // The page unlocks its own audio on the first touch, so the web view
        // must not require a gesture of its own on top of that.
        configuration.mediaTypesRequiringUserActionForPlayback = []
        configuration.allowsInlineMediaPlayback = true

        let webView = WKWebView(frame: view.bounds, configuration: configuration)
        webView.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        // The page draws its own screen and keypad to fill the window; a web
        // view that bounced would show the background behind them.
        webView.scrollView.bounces = false
        webView.scrollView.isScrollEnabled = false
        webView.backgroundColor = view.backgroundColor
        webView.isOpaque = false
        if #available(iOS 16.4, *) {
            webView.isInspectable = true
        }
        view.addSubview(webView)
        self.webView = webView
        message.removeFromSuperview()

        webView.load(URLRequest(url: url))
    }

    // The page paints to the edges and keeps its own safe-area padding, which
    // is what the keypad's bottom row is measured against.
    override var prefersStatusBarHidden: Bool { true }
    override var prefersHomeIndicatorAutoHidden: Bool { true }
}
