pub mod cpu;
pub mod disk;
pub mod gpu;
pub mod mem;
pub mod temp;

use crate::state::MetricsSnapshot;
use std::time::{SystemTime, UNIX_EPOCH};
use sysinfo::System;

/// One full sample across all metrics. `sys` is refreshed in place (see
/// `sensors::cpu` for why it must persist across calls).
pub fn sample(sys: &mut System) -> MetricsSnapshot {
    sys.refresh_cpu_usage();
    sys.refresh_memory();

    let cpu_temp = temp::cpu_temp_celsius();

    MetricsSnapshot {
        cpu: cpu::sample(sys, cpu_temp),
        ram: mem::sample(sys),
        disk: disk::sample(),
        gpu: gpu::sample(),
        timestamp_ms: SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_millis() as u64)
            .unwrap_or(0),
    }
}
