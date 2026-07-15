use crate::state::UsageMetric;
use sysinfo::System;

pub fn sample(sys: &System) -> UsageMetric {
    let total = sys.total_memory();
    let used = sys.used_memory();
    let percent = if total > 0 {
        (used as f32 / total as f32) * 100.0
    } else {
        0.0
    };
    UsageMetric {
        percent: Some(percent),
        used_bytes: Some(used),
        total_bytes: Some(total),
        temp_celsius: None,
    }
}
