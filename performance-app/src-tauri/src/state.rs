use serde::Serialize;
use std::time::{Duration, Instant};

/// A single gauge's worth of data. `percent`/`temp_celsius` are `None` when the
/// underlying sensor path isn't available on this OS/hardware (e.g. GPU on
/// macOS, or a laptop that doesn't expose ACPI thermal zones) — the frontend
/// renders that as an explicit "Unavailable" state rather than a false `0%`.
#[derive(Clone, Serialize, Default)]
pub struct UsageMetric {
    pub percent: Option<f32>,
    pub used_bytes: Option<u64>,
    pub total_bytes: Option<u64>,
    pub temp_celsius: Option<f32>,
}

#[derive(Clone, Serialize)]
pub struct MetricsSnapshot {
    pub cpu: UsageMetric,
    pub ram: UsageMetric,
    pub disk: UsageMetric,
    pub gpu: UsageMetric,
    pub timestamp_ms: u64,
}

impl Default for MetricsSnapshot {
    fn default() -> Self {
        Self {
            cpu: UsageMetric::default(),
            ram: UsageMetric::default(),
            disk: UsageMetric::default(),
            gpu: UsageMetric::default(),
            timestamp_ms: 0,
        }
    }
}

/// Debounced threshold alert: ignores brief spikes (`sustain_for`), fires once
/// per breach, then only re-fires every `renotify_interval` while still hot,
/// and fully re-arms only after dropping below `hysteresis_low`. Without this,
/// a value sitting at 99% would trigger a native notification on every poll tick.
pub struct AlertGuard {
    state: AlertState,
    threshold_high: f32,
    hysteresis_low: f32,
    sustain_for: Duration,
    renotify_interval: Duration,
}

enum AlertState {
    Idle,
    Sustaining { since: Instant },
    Fired { last_fired: Instant },
}

impl AlertGuard {
    pub fn new(
        threshold_high: f32,
        hysteresis_low: f32,
        sustain_for: Duration,
        renotify_interval: Duration,
    ) -> Self {
        Self {
            state: AlertState::Idle,
            threshold_high,
            hysteresis_low,
            sustain_for,
            renotify_interval,
        }
    }

    /// Feed the latest value; returns `true` exactly when a notification should fire now.
    pub fn evaluate(&mut self, value: f32, now: Instant) -> bool {
        match self.state {
            AlertState::Idle => {
                if value >= self.threshold_high {
                    self.state = AlertState::Sustaining { since: now };
                }
                false
            }
            AlertState::Sustaining { since } => {
                if value < self.threshold_high {
                    self.state = AlertState::Idle;
                    false
                } else if now.duration_since(since) >= self.sustain_for {
                    self.state = AlertState::Fired { last_fired: now };
                    true
                } else {
                    false
                }
            }
            AlertState::Fired { last_fired } => {
                if value < self.hysteresis_low {
                    self.state = AlertState::Idle;
                    false
                } else if now.duration_since(last_fired) >= self.renotify_interval {
                    self.state = AlertState::Fired { last_fired: now };
                    true
                } else {
                    false
                }
            }
        }
    }
}

pub struct Alerts {
    pub cpu_usage: AlertGuard,
    pub gpu_usage: AlertGuard,
    pub cpu_temp: AlertGuard,
    pub gpu_temp: AlertGuard,
}

impl Default for Alerts {
    fn default() -> Self {
        let usage_guard = || {
            AlertGuard::new(
                99.0,
                90.0,
                Duration::from_secs(5),
                Duration::from_secs(600),
            )
        };
        // Thermal thresholds are hardware-dependent (this is a conservative
        // default, not a verified per-chip max) — tune once real sensor data
        // is available on target hardware.
        let temp_guard = || {
            AlertGuard::new(
                95.0,
                85.0,
                Duration::from_secs(5),
                Duration::from_secs(600),
            )
        };
        Self {
            cpu_usage: usage_guard(),
            gpu_usage: usage_guard(),
            cpu_temp: temp_guard(),
            gpu_temp: temp_guard(),
        }
    }
}

pub struct AppState {
    pub snapshot: std::sync::Mutex<MetricsSnapshot>,
    pub alerts: std::sync::Mutex<Alerts>,
}

impl Default for AppState {
    fn default() -> Self {
        Self {
            snapshot: std::sync::Mutex::new(MetricsSnapshot::default()),
            alerts: std::sync::Mutex::new(Alerts::default()),
        }
    }
}
