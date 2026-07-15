use crate::state::UsageMetric;
use sysinfo::System;

/// `sys` must be the same `System` instance across calls — usage is computed
/// as a diff against the previous `refresh_cpu_usage()`, so a fresh instance
/// every tick would always read ~0%.
pub fn sample(sys: &System, temp_celsius: Option<f32>) -> UsageMetric {
    UsageMetric {
        percent: Some(sys.global_cpu_usage()),
        used_bytes: None,
        total_bytes: None,
        temp_celsius,
    }
}
