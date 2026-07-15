mod commands;
mod notify;
mod sensors;
mod state;
mod tray;

use state::{AppState, MetricsSnapshot};
use std::time::Duration;
use sysinfo::System;
use tauri::{Emitter, Manager, WindowEvent};

const METRICS_EVENT: &str = "metrics://update";
const VISIBLE_POLL_INTERVAL: Duration = Duration::from_secs(1);
const HIDDEN_POLL_INTERVAL: Duration = Duration::from_secs(3);
const MAIN_WINDOW: &str = "main";

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            // A second launch should surface the existing tray app, not start another.
            if let Some(window) = app.get_webview_window(MAIN_WINDOW) {
                let _ = window.show();
                let _ = window.set_focus();
            }
        }))
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_notification::init())
        .manage(AppState::default())
        .invoke_handler(tauri::generate_handler![commands::get_current_metrics])
        .setup(|app| {
            let handle = app.handle().clone();

            tray::build(&handle)?;

            #[cfg(target_os = "macos")]
            handle.set_activation_policy(tauri::ActivationPolicy::Accessory)?;

            if let Some(window) = handle.get_webview_window(MAIN_WINDOW) {
                #[cfg(not(target_os = "macos"))]
                let _ = window.set_skip_taskbar(true);

                // Keep the tray app (and its background poller) alive when the
                // user clicks the window's close button — hide instead of destroy.
                window.on_window_event(|event| {
                    if let WindowEvent::CloseRequested { api, .. } = event {
                        api.prevent_close();
                    }
                });
            }

            spawn_metrics_loop(handle);

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

fn spawn_metrics_loop(app: tauri::AppHandle) {
    tauri::async_runtime::spawn(async move {
        let mut sys = System::new_all();

        loop {
            let snapshot = sensors::sample(&mut sys);

            if let Some(state) = app.try_state::<AppState>() {
                if let Ok(mut guard) = state.snapshot.lock() {
                    *guard = snapshot.clone();
                }
            }

            let _ = app.emit(METRICS_EVENT, &snapshot);
            tray::update_tooltip(&app, &format_tooltip(&snapshot));
            notify::check_and_notify(&app, &snapshot);

            let visible = app
                .get_webview_window(MAIN_WINDOW)
                .and_then(|w| w.is_visible().ok())
                .unwrap_or(false);
            let interval = if visible {
                VISIBLE_POLL_INTERVAL
            } else {
                HIDDEN_POLL_INTERVAL
            };
            tokio::time::sleep(interval).await;
        }
    });
}

fn format_tooltip(snapshot: &MetricsSnapshot) -> String {
    let fmt = |p: Option<f32>| p.map(|v| format!("{v:.0}%")).unwrap_or_else(|| "--".into());
    format!(
        "Performance Monitor\nCPU {}  ·  RAM {}  ·  GPU {}",
        fmt(snapshot.cpu.percent),
        fmt(snapshot.ram.percent),
        fmt(snapshot.gpu.percent)
    )
}
