package com.movingwoo.wfeature;

import android.app.Activity;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;
import android.util.Base64;
import android.util.Log;
import android.view.InputDevice;
import android.view.InputEvent;
import android.view.KeyEvent;
import android.view.MotionEvent;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowManager;
import android.webkit.JavascriptInterface;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.TextView;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;

/**
 * The app is the server with a WebView in front of it.
 *
 * <p>The emulator is not ported here. The same Go binary a desktop runs is
 * bundled as a native library, started on loopback, and drawn by the same page
 * a desktop browser loads — so the phone runs the emulator itself rather than
 * receiving frames from a machine in the house.
 *
 * <p>Loopback is what makes this worth doing. {@code http://127.0.0.1} is a
 * secure context, so the page is installable and its service worker registers,
 * and there is no address to find, no key to carry, and no router to
 * configure. All of that was the price of reaching a server over a network.
 */
public class MainActivity extends Activity {
    private static final String TAG = "wfeature";

    /**
     * The server binary is shipped as a native library because that is the one
     * place an app may execute a file from: since Android 10 a binary written
     * into the app's own data directory cannot be run, while the extracted
     * library directory is executable by design. The name has to look like a
     * library for the installer to extract it at all.
     */
    private static final String SERVER_LIBRARY = "libwfeature.so";

    /**
     * Web storage is scoped to an origin, including its port. Keeping the
     * loopback port stable lets settings survive a cold app restart.
     */
    private static final int SERVER_PORT = 11541;

    private WebView webView;
    private TextView waiting;

    /**
     * The server outlives an activity that is only being recreated. A change
     * the manifest does not take as its own — a locale, a font scale — makes
     * Android destroy this activity and create another in the same process,
     * and the game running in the server has to be there when the new one
     * shows the page. So the process belongs to the class, and every read and
     * write of it holds the class lock: the thread that starts it and the main
     * thread that ends it are different threads.
     */
    private static Process server;

    /**
     * A server an activity that finished has asked to end, while it may still
     * be draining: closing its games so their exit-time saves are written. The
     * app process outlives the activity, so the player can open the app again
     * within that time, and the next server must not start until this one has
     * ended. Started any sooner, it takes the port the moment the old one
     * stops listening, while the old one may still be closing its games and
     * holding their save directories. Guarded by the class lock, as
     * {@link #server} is.
     */
    private static Process ending;

    /**
     * Set when this activity is gone for good rather than recreated, so a
     * start still in flight does not leave a server nobody will end.
     */
    private boolean finished;
    private ValueCallback<Uri[]> pendingFiles;
    private byte[] pendingExport;
    private static final int PICK_FILE = 1;
    private static final int NAME_EXPORT = 2;

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        // A game is watched as much as it is touched, and a screen that dims
        // in the middle of one is the first thing anybody would report.
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);

        waiting = new TextView(this);
        waiting.setText("에뮬레이터를 시작하는 중...");
        waiting.setTextColor(0xFFEEEEEE);
        waiting.setBackgroundColor(0xFF1A1A1A);
        waiting.setPadding(48, 48, 48, 48);
        setContentView(waiting);

        new Thread(this::startServerAndShow).start();
    }

    /** What the screen says while the page is not up yet. */
    private void showWaiting(String message) {
        runOnUiThread(() -> {
            if (!isDestroyed() && webView == null) {
                waiting.setText(message);
            }
        });
    }

    /**
     * Starts the server, or finds the one an earlier instance of this activity
     * started, waits for it to answer, and then shows the page. A server a
     * finished activity ended is waited out first.
     */
    private void startServerAndShow() {
        try {
            int port = SERVER_PORT;
            File root = dataRoot();
            // Launching and the check that nothing is still draining happen
            // under one lock, so an activity that finishes in between cannot
            // leave a server draining beside the one launched here.
            while (true) {
                Process previous;
                synchronized (MainActivity.class) {
                    if (finished) {
                        return;
                    }
                    if (ending != null && ending.isAlive()) {
                        previous = ending;
                    } else {
                        ending = null;
                        if (server == null || !server.isAlive()) {
                            server = launch(port, root);
                            readLog(server);
                        }
                        break;
                    }
                }
                // No time limit: the drain is what writes the games' exit-time
                // saves, and it is bounded by the server itself.
                Log.i(TAG, "waiting for the server a finished activity ended");
                showWaiting("이전 실행을 마치는 중...");
                int exit = previous.waitFor();
                Log.i(TAG, "the earlier server ended (exit " + exit + ")");
            }
            if (!waitUntilServing(port)) {
                showFailure("서버가 시작되지 않았습니다.");
                return;
            }
            String url = "http://127.0.0.1:" + port;
            runOnUiThread(() -> {
                if (!isDestroyed()) {
                    showPage(url);
                }
            });
        } catch (Exception failure) {
            Log.e(TAG, "the server did not start", failure);
            showFailure("서버를 시작하지 못했습니다: " + failure.getMessage());
        }
    }

    /**
     * The data root is the app's external files directory rather than its
     * private one, so that a phone plugged into a computer shows the games and
     * saves. It is not how a game is expected to arrive — that is the page's
     * ＋ 게임 추가 button, since Android 11 stopped file managers from opening
     * this directory — but a folder that can be copied off a device is worth
     * having when a save needs rescuing.
     */
    private File dataRoot() {
        File external = getExternalFilesDir(null);
        return external != null ? external : getFilesDir();
    }

    private Process launch(int port, File root) throws Exception {
        String binary = getApplicationInfo().nativeLibraryDir + "/" + SERVER_LIBRARY;
        File games = new File(root, "games");
        // Where a game added from the page lands, and therefore the only place
        // the page may delete from. On a phone every game arrives that way, so
        // this is the directory with the games in it and `games/` is the empty
        // one beside it — worth knowing when the folder is read over USB.
        //
        // What an older build left in `games/` moves across on the first
        // start, and the server does that for every host rather than this
        // launcher doing it for one; see internal/webhost/adopt.go.
        File added = new File(root, "ext");
        File saves = new File(root, "savedata/ktf");
        File logs = new File(root, "logs");
        games.mkdirs();
        added.mkdirs();
        saves.mkdirs();
        logs.mkdirs();

        ProcessBuilder builder = new ProcessBuilder(
                binary,
                // Loopback only: the phone is the whole audience, and a server
                // on the Wi-Fi would be one with no key in front of it.
                "-addr", "127.0.0.1:" + port,
                "-games", games.getAbsolutePath(),
                "-ext", added.getAbsolutePath(),
                "-saves", saves.getAbsolutePath(),
                "-logs", logs.getAbsolutePath(),
                // The client is inside the binary; there is no directory of
                // page files on a phone to prefer over it.
                "-web", new File(root, "web-unused").getAbsolutePath());
        builder.directory(root);
        builder.redirectErrorStream(true);
        Log.i(TAG, "starting " + binary + " on port " + port);
        return builder.start();
    }

    /** The server's log is the only thing that says why a start failed. */
    private void readLog(Process process) {
        new Thread(() -> {
            try (BufferedReader reader =
                         new BufferedReader(new InputStreamReader(process.getInputStream()))) {
                String line;
                while ((line = reader.readLine()) != null) {
                    Log.i(TAG, line);
                }
            } catch (Exception ignored) {
                // The process ended; its exit is reported by the wait below.
            }
        }).start();
    }

    /**
     * Waits for the port to answer as this server rather than for a fixed
     * time. A cold start reads the game root and the fonts, and the honest
     * signal that it is ready is /api/status answering.
     *
     * <p>Any program on the phone may listen on a loopback port, so an answer
     * counts only when it names a process this app started: /api/status
     * carries the server's pid, and the kernel says whose child that is. With
     * nothing left draining (see {@link #ending}), the one child that answers
     * is the server just launched.
     */
    private boolean waitUntilServing(int port) {
        long deadline = System.currentTimeMillis() + 20_000;
        while (System.currentTimeMillis() < deadline) {
            Process launched;
            synchronized (MainActivity.class) {
                launched = server;
            }
            if (launched == null || !launched.isAlive()) {
                return false;
            }
            try {
                HttpURLConnection connection =
                        (HttpURLConnection) new URL("http://127.0.0.1:" + port + "/api/status")
                                .openConnection();
                connection.setConnectTimeout(500);
                connection.setReadTimeout(500);
                int status = connection.getResponseCode();
                String body = status == 200 ? readSmall(connection.getInputStream()) : "";
                connection.disconnect();
                if (status == 200 && launched.isAlive() && startedHere(body)) {
                    return true;
                }
            } catch (Exception retry) {
                // Not up yet.
            }
            try {
                Thread.sleep(100);
            } catch (InterruptedException interrupted) {
                return false;
            }
        }
        return false;
    }

    /** A status answer is a few dozen bytes; anything past 4 KiB is not one. */
    private static String readSmall(InputStream in) throws IOException {
        try (InputStream body = in) {
            ByteArrayOutputStream read = new ByteArrayOutputStream();
            byte[] chunk = new byte[512];
            int n;
            while ((n = body.read(chunk)) > 0 && read.size() <= 4096) {
                read.write(chunk, 0, n);
            }
            return read.toString("UTF-8");
        }
    }

    /**
     * Whether a status answer is from a wfeature server that is a child of
     * this app's process. {@code /proc/<pid>/stat} gives the parent after the
     * parenthesised command name; a process of another app is not visible
     * there at all, which also answers no.
     */
    private static boolean startedHere(String status) {
        try {
            JSONObject answer = new JSONObject(status);
            int pid = answer.optInt("pid", 0);
            if (!"wfeature".equals(answer.optString("server")) || pid <= 0) {
                return false;
            }
            String stat;
            try (BufferedReader reader = new BufferedReader(new InputStreamReader(
                    new FileInputStream("/proc/" + pid + "/stat")))) {
                stat = reader.readLine();
            }
            int name = stat == null ? -1 : stat.lastIndexOf(')');
            if (name < 0) {
                return false;
            }
            // ") <state> <ppid> ..."
            String[] fields = stat.substring(name + 1).trim().split(" ");
            return fields.length > 1
                    && Integer.parseInt(fields[1]) == android.os.Process.myPid();
        } catch (Exception unreadable) {
            return false;
        }
    }

    private void showFailure(String message) {
        runOnUiThread(() -> {
            TextView failure = new TextView(this);
            failure.setText(message);
            failure.setTextColor(0xFFEEEEEE);
            failure.setBackgroundColor(0xFF1A1A1A);
            failure.setPadding(48, 48, 48, 48);
            setContentView(failure);
        });
    }

    private void showPage(String url) {
        webView = new WebView(this);
        WebSettings settings = webView.getSettings();
        settings.setJavaScriptEnabled(true);
        // The page keeps the last game, the key bindings and the resume token
        // in localStorage and sessionStorage.
        settings.setDomStorageEnabled(true);
        settings.setMediaPlaybackRequiresUserGesture(false);
        WebView.setWebContentsDebuggingEnabled(true);

        webView.setWebViewClient(new WebViewClient());
        // The other half of the file story. An input is answered by
        // onShowFileChooser below; a *download* is not answered at all — a
        // WebView drops `<a download>` and a blob: URL on the floor with no
        // DownloadListener, silently, so 세이브 내보내기 did nothing here while
        // working in every desktop browser. Rather than teach DownloadManager
        // to fetch a blob it cannot fetch, the page hands the bytes over and
        // this asks where they go. See web/save-backup.js.
        webView.addJavascriptInterface(new ExportBridge(), "wfeatureExport");
        webView.setWebChromeClient(new WebChromeClient() {
            /**
             * The ＋ 게임 추가 button is a file input, and this is the ten
             * lines that make it open the system picker. It is the whole of
             * the native code the import path needs — the same input is
             * answered by iOS and by a desktop browser with no help at all.
             */
            @Override
            public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> callback,
                                             FileChooserParams params) {
                pendingFiles = callback;
                // ACTION_OPEN_DOCUMENT rather than ACTION_GET_CONTENT, and no
                // chooser around it: GET_CONTENT asks which app should supply
                // the file and then hands over whatever that app decides to
                // give, which is why pressing the button used to open a
                // sheet of apps before anything resembling a file list.
                // OPEN_DOCUMENT goes straight to the system's own picker.
                Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT);
                intent.addCategory(Intent.CATEGORY_OPENABLE);
                // A game is a zip or a jar, and phones disagree about which
                // type they call those — some report a zip as
                // application/octet-stream — so the type is left open and the
                // list below is what the picker highlights.
                intent.setType("*/*");
                intent.putExtra(Intent.EXTRA_MIME_TYPES, new String[]{
                        "application/zip",
                        "application/java-archive",
                        "application/x-zip-compressed",
                        "application/octet-stream",
                });
                intent.putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true);
                try {
                    startActivityForResult(intent, PICK_FILE);
                    return true;
                } catch (Exception noPicker) {
                    pendingFiles = null;
                    return false;
                }
            }
        });

        setContentView(webView);
        webView.loadUrl(url);
        takeControllers();
    }

    /**
     * A controller's input goes to the focused view, and nothing gives the web
     * view focus on its own: a phone opened by a touch is in touch mode, where
     * no view is focused until one is touched, and a controller's buttons do
     * not take the window out of it. Its presses went nowhere — the page did
     * not see a controller at all — until the screen was touched or the
     * direction pad moved. The web view is the only thing here that takes
     * input, so it takes focus when the page is shown and again whenever a
     * controller is used without it.
     */
    private void takeControllers() {
        if (webView != null && !webView.hasFocus()) {
            webView.requestFocus();
        }
    }

    private static boolean fromController(InputEvent event) {
        return event.isFromSource(InputDevice.SOURCE_GAMEPAD)
                || event.isFromSource(InputDevice.SOURCE_JOYSTICK);
    }

    /**
     * What the page calls to put a save on the phone. The name and the bytes
     * arrive together because the picker wants a name to suggest.
     */
    private class ExportBridge {
        @JavascriptInterface
        public void save(String name, String base64) {
            final byte[] bytes;
            try {
                bytes = Base64.decode(base64, Base64.DEFAULT);
            } catch (IllegalArgumentException notBase64) {
                Log.e(TAG, "a save export arrived unreadable", notBase64);
                return;
            }
            // A JavascriptInterface method runs on the WebView's own thread,
            // and an Activity is started from the main one.
            runOnUiThread(new Runnable() {
                @Override
                public void run() {
                    pendingExport = bytes;
                    Intent intent = new Intent(Intent.ACTION_CREATE_DOCUMENT);
                    intent.addCategory(Intent.CATEGORY_OPENABLE);
                    // Nothing on a phone knows what a .wfs is, and a type it
                    // guesses wrong is a picker that hides the folder the
                    // player keeps their backups in.
                    intent.setType("application/octet-stream");
                    intent.putExtra(Intent.EXTRA_TITLE, name);
                    try {
                        startActivityForResult(intent, NAME_EXPORT);
                    } catch (Exception noPicker) {
                        pendingExport = null;
                        Log.e(TAG, "no document picker for a save export", noPicker);
                    }
                }
            });
        }
    }

    @Override
    protected void onActivityResult(int request, int result, Intent data) {
        if (request == NAME_EXPORT) {
            writeExport(result, data);
            return;
        }
        if (request != PICK_FILE) {
            super.onActivityResult(request, result, data);
            return;
        }
        if (pendingFiles == null) {
            return;
        }
        pendingFiles.onReceiveValue(chosenFiles(result, data));
        pendingFiles = null;
    }

    /**
     * Writes the export to the place the picker named. The bytes are dropped
     * either way afterwards: a cancelled export must not be written by the
     * next one, and holding a save in memory past the moment it is wanted is
     * holding it for the life of the app.
     */
    private void writeExport(int result, Intent data) {
        byte[] bytes = pendingExport;
        pendingExport = null;
        if (bytes == null || result != RESULT_OK || data == null || data.getData() == null) {
            return;
        }
        OutputStream out = null;
        try {
            out = getContentResolver().openOutputStream(data.getData());
            if (out == null) {
                Log.e(TAG, "the document picker named a place that cannot be written");
                return;
            }
            out.write(bytes);
            out.flush();
        } catch (Exception failed) {
            Log.e(TAG, "a save export could not be written", failed);
        } finally {
            if (out != null) {
                try {
                    out.close();
                } catch (Exception ignored) {
                    // A close that fails after a successful write is nothing
                    // the player can act on, and the file is already there.
                }
            }
        }
    }

    /** What the picker returned, as the array the file input expects. */
    private Uri[] chosenFiles(int result, Intent data) {
        if (result != RESULT_OK || data == null) {
            // Cancelling has to be answered too: an input left waiting cannot
            // be opened a second time.
            return null;
        }
        if (data.getClipData() != null) {
            int count = data.getClipData().getItemCount();
            Uri[] files = new Uri[count];
            for (int at = 0; at < count; at++) {
                files[at] = data.getClipData().getItemAt(at).getUri();
            }
            return files;
        }
        if (data.getData() != null) {
            return new Uri[]{data.getData()};
        }
        return null;
    }

    /**
     * A game controller's buttons arrive here as key events, and the web view
     * reads them into the page's Gamepad API. A button the activity does not
     * consume is given a fallback by the system — B becomes BACK, A the centre
     * of the direction pad — and BACK walks the page's history or leaves the
     * app, which is not what a player pressing 취소 on a controller means. So a
     * controller button goes to the web view first and is then reported as
     * handled whatever the web view said, and no fallback is made from it.
     */
    @Override
    public boolean dispatchKeyEvent(KeyEvent event) {
        if (fromController(event)) {
            takeControllers();
        }
        boolean handled = super.dispatchKeyEvent(event);
        return KeyEvent.isGamepadButton(event.getKeyCode()) || handled;
    }

    /**
     * A controller's sticks, triggers and — on most — direction pad arrive as
     * motion events rather than keys, and go to the focused view the same way.
     */
    @Override
    public boolean dispatchGenericMotionEvent(MotionEvent event) {
        if (fromController(event)) {
            takeControllers();
        }
        return super.dispatchGenericMotionEvent(event);
    }

    /**
     * Back walks the page's own history before it leaves the app, so that
     * closing a panel does not close the emulator.
     */
    @Override
    public void onBackPressed() {
        if (webView != null && webView.canGoBack()) {
            webView.goBack();
            return;
        }
        super.onBackPressed();
    }

    @Override
    protected void onDestroy() {
        closePage();
        super.onDestroy();
        // A recreation is not an ending: the next instance finds the server
        // and the game in it still running. Ending it here closed the game the
        // player was in whenever a split screen or an attached keyboard
        // rebuilt the activity, and the new instance raced the old process for
        // the port.
        if (isChangingConfigurations()) {
            return;
        }
        synchronized (MainActivity.class) {
            finished = true;
            if (server != null) {
                // The session drains on its own when the process goes; what
                // must not happen is the process outliving the app that
                // started it. The next activity waits for the drain.
                server.destroy();
                ending = server;
                server = null;
            }
        }
    }

    /**
     * The web view goes with the activity that made it. A recreated activity
     * shows the page in a web view of its own, and the old one left alive kept
     * its page loaded and hidden — harmless while hidden, since a hidden page
     * parks its game, but one made visible again (a DevTools client did it)
     * took the game back from the new page. Destroying it closes its session
     * socket, and the server parks the game, if the page had not already, for
     * the new page to resume. The server is not touched here.
     */
    private void closePage() {
        if (webView == null) {
            return;
        }
        ViewGroup parent = (ViewGroup) webView.getParent();
        if (parent != null) {
            parent.removeView(webView);
        }
        webView.destroy();
        webView = null;
    }
}
