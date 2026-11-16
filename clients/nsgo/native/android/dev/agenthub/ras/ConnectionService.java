package dev.agenthub.ras;

import android.app.Activity;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.os.Build;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import android.os.PowerManager;
import android.net.wifi.WifiManager;
import android.net.ConnectivityManager;
import android.net.Network;
import dev.oneo.nsgo.NsgoView;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.Map;
import java.util.function.Consumer;
import org.json.JSONObject;
import org.json.JSONArray;

/** User-visible lifetime for an active remote-device connection, not a daemon. */
public final class ConnectionService extends Service {
    static final String SESSION = "dev.agenthub.ras.session";
    private static final int CONNECTION = 26330;
    private static final String CHANNEL = "reignhub.connections", TASKS = "reignhub.tasks";
    private static ConnectionService current;
    private static final ArrayList<NsgoView.HostReply> pending = new ArrayList<>();
    static Consumer<Boolean> listener;
    static Runnable networkListener;
    private final HashMap<String,String> taskStates = new HashMap<>();
    private final Handler handler = new Handler(Looper.getMainLooper());
    private PowerManager.WakeLock wake;
    private WifiManager.WifiLock wifi;
    private boolean maintaining;
    private ConnectivityManager connectivity;
    private ConnectivityManager.NetworkCallback networks;
    private final Runnable networkChanged = () -> { if (networkListener != null) networkListener.run(); };

    static void request(Activity activity, boolean foreground, Object data, NsgoView.HostReply reply) {
        try {
            JSONObject status = new JSONObject((Map<?,?>)data);
            if (!status.optBoolean("enabled")) {
                activity.stopService(new Intent(activity, ConnectionService.class));
                for (NsgoView.HostReply waiting : new ArrayList<>(pending)) waiting.resolve(result(false));
                pending.clear();
                reply.resolve(result(false));
                return;
            }
            if (current != null) {
                current.update(status);
                reply.resolve(result(true));
            } else if (foreground) {
                // Android 12+ requires starting an FGS while the user is in the
                // app. Connection starts arrange this before activity onPause.
                pending.add(reply);
                activity.startForegroundService(new Intent(activity, ConnectionService.class)
                        .putExtra("status", status.toString()));
            } else {
                reply.resolve(result(false));
            }
        } catch (RuntimeException error) {
            pending.remove(reply);
            reply.reject("无法启用后台连接服务：" + error.getMessage());
        }
    }

    private static Map<String,Object> result(boolean running) {
        HashMap<String,Object> result = new HashMap<>();
        result.put("running", running);
        return result;
    }

    @Override public void onCreate() {
        super.onCreate();
        NotificationManager manager = getSystemService(NotificationManager.class);
        manager.createNotificationChannel(new NotificationChannel(CHANNEL, "设备连接", NotificationManager.IMPORTANCE_LOW));
        manager.createNotificationChannel(new NotificationChannel(TASKS, "Agent 会话", NotificationManager.IMPORTANCE_DEFAULT));
        connectivity = getSystemService(ConnectivityManager.class);
        networks = new ConnectivityManager.NetworkCallback() {
            private void changed() {
                handler.removeCallbacks(networkChanged);
                handler.postDelayed(networkChanged, 300);
            }
            @Override public void onAvailable(Network network) { changed(); }
            @Override public void onLost(Network network) { changed(); }
        };
        try { connectivity.registerDefaultNetworkCallback(networks, handler); }
        catch (RuntimeException error) { networks = null; }
        wake = getSystemService(PowerManager.class).newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "ReignHub:Connection");
        wake.setReferenceCounted(false);
        WifiManager managerWifi = (WifiManager)getApplicationContext().getSystemService(Context.WIFI_SERVICE);
        if (managerWifi != null) {
            wifi = managerWifi.createWifiLock(WifiManager.WIFI_MODE_FULL_HIGH_PERF, "ReignHub:Connection");
            wifi.setReferenceCounted(false);
        }
    }

    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        // No START_STICKY: never silently recreate an empty Go runtime or
        // pretend that a killed activity still has an authenticated socket.
        try {
            if (intent == null) { stopSelf(); return START_NOT_STICKY; }
            update(new JSONObject(intent.getStringExtra("status")));
            current = this;
            for (NsgoView.HostReply reply : new ArrayList<>(pending)) reply.resolve(result(true));
            pending.clear();
            if (listener != null) listener.accept(true);
        } catch (Exception error) {
            for (NsgoView.HostReply reply : new ArrayList<>(pending)) reply.reject("后台连接服务启动失败：" + error.getMessage());
            pending.clear();
            stopSelf();
        }
        return START_NOT_STICKY;
    }

    private Notification.Builder notification(String channel, String title, String body, JSONObject task) {
        Intent open = new Intent(this, RasActivity.class)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        if (task != null) open.putExtra(SESSION, task.toString());
        int request = task == null ? CONNECTION : task.optString("key").hashCode();
        PendingIntent press = PendingIntent.getActivity(this, request, open, PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
        return new Notification.Builder(this, channel).setSmallIcon(android.R.drawable.stat_notify_sync)
                .setContentTitle(title).setContentText(body).setContentIntent(press)
                .setVisibility(Notification.VISIBILITY_PRIVATE).setShowWhen(false);
    }

    private void update(JSONObject status) {
        startForeground(CONNECTION, notification(CHANNEL, status.optString("title", "ReignHub"),
                status.optString("body", "后台连接保持中"), null).setOngoing(true).setOnlyAlertOnce(true).build());
        JSONArray tasks = status.optJSONArray("tasks");
        if (tasks != null) {
            for (int i = 0; i < tasks.length(); i++) {
                JSONObject task = tasks.optJSONObject(i);
                if (task == null) continue;
                updateTask(task);
            }
        }
        // Protect the connection for the whole user-visible service lifetime,
        // including idle chats and recovery. Releasing on a temporary socket
        // loss would prevent Android from running the reconnect itself.
        if (!maintaining) {
            maintaining = true;
            renewWake();
            if (wifi != null && !wifi.isHeld()) wifi.acquire();
        }
    }

    private void updateTask(JSONObject task) {
        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission("android.permission.POST_NOTIFICATIONS") != PackageManager.PERMISSION_GRANTED)
            return;
        String key = task.optString("key"), state = task.optString("status");
        String version = state + ":" + task.optBoolean("connected");
        String previousVersion = taskStates.put(key, version);
        String previous = previousVersion == null ? "" : previousVersion.split(":", 2)[0];
        if (!version.equals(previousVersion)) {
            boolean running = "inProgress".equals(state);
            // Completion is meaningful only for a run this process observed.
            if (running || "inProgress".equals(previous)) {
                String label = running && !task.optBoolean("connected") ? "连接中断，等待同步" : task.optString("label");
                Notification.Builder b = notification(running ? CHANNEL : TASKS, task.optString("title", "Agent 会话"), label, task)
                        .setOngoing(running).setAutoCancel(!running).setShowWhen(true);
                if (running) b.setProgress(0, 0, task.optBoolean("connected")).setOnlyAlertOnce(true);
                else if (state.equals(previous)) b.setOnlyAlertOnce(true);
                getSystemService(NotificationManager.class).notify("task:" + key, 0, b.build());
            }
        }
    }

    private void renewWake() {
        if (!maintaining) return;
        wake.acquire(10 * 60 * 1000L);
        handler.postDelayed(this::renewWake, 5 * 60 * 1000L);
    }

    @Override public void onDestroy() {
        if (current == this) current = null;
        for (NsgoView.HostReply waiting : new ArrayList<>(pending)) waiting.resolve(result(false));
        pending.clear();
        maintaining = false;
        if (networks != null) {
            try { connectivity.unregisterNetworkCallback(networks); } catch (RuntimeException ignored) {}
            networks = null;
        }
        handler.removeCallbacksAndMessages(null);
        if (wake != null && wake.isHeld()) wake.release();
        if (wifi != null && wifi.isHeld()) wifi.release();
        // Do not leave a running notification after the user disconnects or
        // removes the app's task; retain completed notifications for reading.
        NotificationManager manager = getSystemService(NotificationManager.class);
        for (Map.Entry<String,String> task : taskStates.entrySet())
            if (task.getValue().startsWith("inProgress:")) manager.cancel("task:" + task.getKey(), 0);
        stopForeground(STOP_FOREGROUND_REMOVE);
        if (listener != null) listener.accept(false);
        super.onDestroy();
    }
    @Override public IBinder onBind(Intent intent) { return null; }
}
