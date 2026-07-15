use crate::state::{AppState, MetricsSnapshot};
use tauri::State;

/// Lets a freshly-opened window paint immediately instead of waiting for the
/// next background poll tick to emit an event.
#[tauri::command]
pub fn get_current_metrics(state: State<AppState>) -> MetricsSnapshot {
    let guard = match state.snapshot.lock() {
        Ok(guard) => guard,
        Err(poisoned) => poisoned.into_inner(),
    };
    guard.clone()
}
