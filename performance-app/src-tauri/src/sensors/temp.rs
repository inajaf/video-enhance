//! CPU temperature. There is no public, reliable cross-platform sensor API:
//! macOS SMC keys aren't exposed to normal apps, and Windows ACPI thermal
//! zones are frequently unpopulated. Both paths degrade to `None`
//! ("Unavailable" in the UI) rather than guessing.

#[cfg(windows)]
mod imp {
    use serde::Deserialize;
    use wmi::{COMLibrary, WMIConnection};

    #[derive(Deserialize)]
    #[serde(rename = "MSAcpi_ThermalZoneTemperature")]
    #[serde(rename_all = "PascalCase")]
    struct ThermalZone {
        current_temperature: u32,
    }

    /// Re-initializes COM/WMI on every call rather than caching a connection.
    /// This poller may run on different tokio worker threads between ticks,
    /// and COM objects are thread-affine — caching a connection across calls
    /// would risk using it from the wrong thread. Verify this on real Windows
    /// hardware; ACPI thermal zones are known to be absent on many machines.
    pub fn cpu_temp_celsius() -> Option<f32> {
        let com_con = COMLibrary::new().ok()?;
        let wmi_con = WMIConnection::with_namespace_path("root\\WMI", com_con).ok()?;
        let results: Vec<ThermalZone> = wmi_con
            .raw_query("SELECT CurrentTemperature FROM MSAcpi_ThermalZoneTemperature")
            .ok()?;
        // Value is in tenths of a Kelvin.
        let raw = results.first()?.current_temperature;
        Some((raw as f32 / 10.0) - 273.15)
    }
}

#[cfg(not(windows))]
mod imp {
    pub fn cpu_temp_celsius() -> Option<f32> {
        None
    }
}

pub use imp::cpu_temp_celsius;
