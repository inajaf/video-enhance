use crate::state::{AppState, MetricsSnapshot};
use std::time::Instant;
use tauri::{AppHandle, Manager};
use tauri_plugin_notification::NotificationExt;

/// Evaluates every metric's `AlertGuard` against the latest snapshot and
/// fires a native notification for whichever ones just crossed into
/// "sustained breach". Called once per poll tick from the background loop.
pub fn check_and_notify(app: &AppHandle, snapshot: &MetricsSnapshot) {
    let state = app.state::<AppState>();
    let mut alerts = match state.alerts.lock() {
        Ok(guard) => guard,
        Err(poisoned) => poisoned.into_inner(),
    };
    let now = Instant::now();

    if let Some(percent) = snapshot.cpu.percent {
        if alerts.cpu_usage.evaluate(percent, now) {
            fire(app, "CPU usage is critical", &format!("CPU is at {percent:.0}%."));
        }
    }
    if let Some(percent) = snapshot.gpu.percent {
        if alerts.gpu_usage.evaluate(percent, now) {
            fire(app, "GPU usage is critical", &format!("GPU is at {percent:.0}%."));
        }
    }
    if let Some(temp) = snapshot.cpu.temp_celsius {
        if alerts.cpu_temp.evaluate(temp, now) {
            fire(app, "CPU is running hot", &format!("CPU temperature is {temp:.0}°C."));
        }
    }
    if let Some(temp) = snapshot.gpu.temp_celsius {
        if alerts.gpu_temp.evaluate(temp, now) {
            fire(app, "GPU is running hot", &format!("GPU temperature is {temp:.0}°C."));
        }
    }
}

fn fire(app: &AppHandle, title: &str, body: &str) {
    let result = app
        .notification()
        .builder()
        .title(title)
        .body(body)
        .show();
    if let Err(err) = result {
        eprintln!("failed to show notification: {err}");
    }
}
