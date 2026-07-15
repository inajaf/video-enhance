use crate::state::UsageMetric;
use std::path::Path;
use sysinfo::Disks;

/// Reports the disk mounted at `/` (macOS/Linux) when present, otherwise the
/// largest known volume — a reasonable stand-in for "the disk that matters"
/// without asking the user to pick one in v1.
pub fn sample() -> UsageMetric {
    let disks = Disks::new_with_refreshed_list();
    let list = disks.list();

    let primary = list
        .iter()
        .find(|d| d.mount_point() == Path::new("/"))
        .or_else(|| list.iter().max_by_key(|d| d.total_space()));

    match primary {
        Some(disk) => {
            let total = disk.total_space();
            let available = disk.available_space();
            let used = total.saturating_sub(available);
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
        None => UsageMetric::default(),
    }
}
