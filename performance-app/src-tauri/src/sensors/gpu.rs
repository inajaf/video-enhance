//! GPU usage/temperature. Windows/NVIDIA is the only path with real
//! confidence behind it (NVML, official NVIDIA driver API). AMD/Intel GPUs on
//! Windows and all GPUs on macOS have no maintained public API a Rust crate
//! wraps today (macOS GPU stats even in Activity Monitor go through Apple's
//! private IOReport framework) — those report `Unavailable` rather than a
//! guessed value. This is a known v1 gap, not an oversight.

use crate::state::UsageMetric;

#[cfg(windows)]
mod imp {
    use crate::state::UsageMetric;
    use nvml_wrapper::enum_wrappers::device::TemperatureSensor;
    use nvml_wrapper::Nvml;
    use std::sync::OnceLock;

    static NVML: OnceLock<Option<Nvml>> = OnceLock::new();

    pub fn sample() -> UsageMetric {
        let nvml = NVML.get_or_init(|| Nvml::init().ok());
        let Some(nvml) = nvml.as_ref() else {
            return UsageMetric::default();
        };
        let Ok(device) = nvml.device_by_index(0) else {
            return UsageMetric::default();
        };
        let percent = device.utilization_rates().ok().map(|u| u.gpu as f32);
        let temp_celsius = device
            .temperature(TemperatureSensor::Gpu)
            .ok()
            .map(|t| t as f32);
        let (total_bytes, used_bytes) = device
            .memory_info()
            .ok()
            .map(|m| (Some(m.total), Some(m.used)))
            .unwrap_or((None, None));

        UsageMetric {
            percent,
            used_bytes,
            total_bytes,
            temp_celsius,
        }
    }
}

#[cfg(not(windows))]
mod imp {
    use crate::state::UsageMetric;

    pub fn sample() -> UsageMetric {
        UsageMetric::default()
    }
}

pub fn sample() -> UsageMetric {
    imp::sample()
}
