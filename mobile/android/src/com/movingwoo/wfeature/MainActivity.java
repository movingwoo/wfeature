package com.movingwoo.wfeature;

import android.app.Activity;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;
import android.util.Log;
import android.view.View;
import android.view.WindowManager;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.TextView;

import java.io.BufferedReader;
import java.io.File;
import java.io.InputStreamReader;
import java.net.HttpURLConnection;
import java.net.ServerSocket;
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

    private WebView webView;
    private Process server;
    private ValueCallback<Uri[]> pendingFiles;
    private static final int PICK_FILE = 1;

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        // A game is watched as much as it is touched, and a screen that dims
        // in the middle of one is the first thing anybody would report.
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);

        TextView waiting = new TextView(this);
        waiting.setText("에뮬레이터를 시작하는 중...");
        waiting.setTextColor(0xFFEEEEEE);
        waiting.setBackgroundColor(0xFF1A1A1A);
        waiting.setPadding(48, 48, 48, 48);
        setContentView(waiting);

        new Thread(this::startServerAndShow).start();
    }

    /** Starts the server, waits for it to answer, and then shows the page. */
    private void startServerAndShow() {
        try {
            int port = freePort();
            File root = dataRoot();
            server = launch(port, root);
            readLog(server);
            if (!waitUntilServing(port)) {
                showFailure("서버가 시작되지 않았습니다.");
                return;
            }
            String url = "http://127.0.0.1:" + port;
            runOnUiThread(() -> showPage(url));
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

    /**
     * A port nothing else holds. The socket is closed before the server is
     * told to take it, which is a race in principle and never one in practice:
     * the alternative is parsing the server's log for the port it chose, and
     * that costs a stream reader on the startup path.
     */
    private int freePort() throws Exception {
        try (ServerSocket probe = new ServerSocket(0)) {
            return probe.getLocalPort();
        }
    }

    private Process launch(int port, File root) throws Exception {
        String binary = getApplicationInfo().nativeLibraryDir + "/" + SERVER_LIBRARY;
        File games = new File(root, "games");
        File saves = new File(root, "savedata/ktf");
        File logs = new File(root, "logs");
        games.mkdirs();
        saves.mkdirs();
        logs.mkdirs();

        ProcessBuilder builder = new ProcessBuilder(
                binary,
                // Loopback only: the phone is the whole audience, and a server
                // on the Wi-Fi would be one with no key in front of it.
                "-addr", "127.0.0.1:" + port,
                "-games", games.getAbsolutePath(),
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
     */
    private boolean waitUntilServing(int port) {
        long deadline = System.currentTimeMillis() + 20_000;
        while (System.currentTimeMillis() < deadline) {
            try {
                HttpURLConnection connection =
                        (HttpURLConnection) new URL("http://127.0.0.1:" + port + "/api/status")
                                .openConnection();
                connection.setConnectTimeout(500);
                connection.setReadTimeout(500);
                int status = connection.getResponseCode();
                connection.disconnect();
                if (status == 200) {
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
    }

    @Override
    protected void onActivityResult(int request, int result, Intent data) {
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
        super.onDestroy();
        if (server != null) {
            // The session drains on its own when the process goes; what must
            // not happen is the process outliving the app that started it.
            server.destroy();
            server = null;
        }
    }
}
