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
    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]?)
        -> Bool {
        // A game is watched as much as it is touched, and a screen that dims
        // in the middle of one is the first thing anybody would report.
        application.isIdleTimerDisabled = true
        return true
    }

    // The window belongs to a scene. UIKit refuses to launch an app built with
    // the iOS 27 SDK that makes its window here instead — it stops with "UIScene
    // life cycle is required for apps built with this SDK" before anything is
    // drawn — and an app built with an older SDK only works because UIKit still
    // excuses it. Scenes exist since iOS 13, below this app's minimum.
    //
    // Only the app's own window plays. A display the phone is connected to — a
    // cable, AirPlay — opens a scene of its own role, and one given the play
    // delegate made a window there with a second page, which took the game from
    // the phone's page as a second tab would, where the display used to mirror
    // the phone. Left without a delegate, that scene shows nothing of its own,
    // and the display mirrors the screen as it did before the app had scenes.
    func application(_ application: UIApplication,
                     configurationForConnecting session: UISceneSession,
                     options: UIScene.ConnectionOptions) -> UISceneConfiguration {
        let configuration = UISceneConfiguration(name: nil, sessionRole: session.role)
        if session.role == .windowApplication {
            configuration.delegateClass = PlaySceneDelegate.self
        }
        return configuration
    }

    func applicationWillTerminate(_ application: UIApplication) {
        WfeatureStop()
    }
}

final class PlaySceneDelegate: UIResponder, UIWindowSceneDelegate {
    var window: UIWindow?

    // A scene connected again after the system let it go makes a new play view,
    // which finds the server already running (WfeatureStart answers with the
    // one it started) and so the same page and library.
    func scene(_ scene: UIScene, willConnectTo session: UISceneSession,
               options connectionOptions: UIScene.ConnectionOptions) {
        guard let scene = scene as? UIWindowScene else { return }
        let window = UIWindow(windowScene: scene)
        window.rootViewController = PlayViewController()
        window.makeKeyAndVisible()
        self.window = window
    }
}

final class PlayViewController: UIViewController {
    private var webView: WKWebView!
    // The file a document picker is exporting, kept only for as long as that
    // picker is up; see the WKScriptMessageHandler extension below.
    private var exportStaged: URL?
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
        // A file input is answered by WKWebView itself, which is why nothing
        // here does that. A *download* is not: `<a download>` and a blob: URL
        // are dropped without a download delegate, silently, so 세이브 내보내기
        // did nothing on this platform while working in every desktop browser.
        // The page hands the bytes over instead and this puts up the document
        // picker. See web/save-backup.js.
        configuration.userContentController.add(self, name: "wfeatureExport")

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
        takeControllers()
    }

    // A game controller reaches the page only through the responder chain:
    // WebKit hands the controllers to a web view that is the first responder.
    // Nothing else here wants that role, so the web view takes it when it
    // first loads and again whenever a picker has been over it.
    private func takeControllers() {
        _ = webView?.becomeFirstResponder()
    }

    fileprivate func clearStagedExport() {
        guard let staged = exportStaged else { return }
        exportStaged = nil
        try? FileManager.default.removeItem(at: staged)
    }

    // The page paints to the edges and keeps its own safe-area padding, which
    // is what the keypad's bottom row is measured against.
    override var prefersStatusBarHidden: Bool { true }
    override var prefersHomeIndicatorAutoHidden: Bool { true }
}

// Where a save goes when the page hands one over.
extension PlayViewController: WKScriptMessageHandler {
    func userContentController(_ controller: WKUserContentController,
                               didReceive message: WKScriptMessage) {
        guard message.name == "wfeatureExport",
              let body = message.body as? [String: Any],
              let name = body["name"] as? String,
              let encoded = body["data"] as? String,
              let bytes = Data(base64Encoded: encoded) else {
            NSLog("a save export arrived unreadable")
            return
        }
        // The picker exports a file that exists, so the bytes are written
        // before it opens. It goes in the caches directory rather than the
        // documents one: `UIFileSharingEnabled` puts documents in the Files
        // app, and a half-exported save sitting there beside the games would
        // be a second copy nobody asked for.
        let staged = FileManager.default.temporaryDirectory
            .appendingPathComponent(name.isEmpty ? "save.wfs" : name)
        do {
            try bytes.write(to: staged, options: .atomic)
        } catch {
            NSLog("a save export could not be staged: \(error)")
            return
        }
        // asCopy, so the picker moves its own copy where the player says and
        // the staged file stays ours to clean up.
        let picker = UIDocumentPickerViewController(forExporting: [staged], asCopy: true)
        picker.shouldShowFileExtensions = true
        exportStaged = staged
        picker.delegate = self
        present(picker, animated: true)
    }
}

// The staged file outlives the picker either way — a place chosen or a cancel
// — so both endings clear it. Leaving it costs the player nothing today and is
// a save of theirs lying in a temporary directory.
extension PlayViewController: UIDocumentPickerDelegate {
    func documentPicker(_ controller: UIDocumentPickerViewController,
                        didPickDocumentsAt urls: [URL]) {
        clearStagedExport()
        takeControllers()
    }

    func documentPickerWasCancelled(_ controller: UIDocumentPickerViewController) {
        clearStagedExport()
        takeControllers()
    }
}
