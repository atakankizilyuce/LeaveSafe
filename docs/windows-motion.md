# Windows movement sensor

The `motion` sensor uses the documented Windows Runtime
`Windows.Devices.Sensors.Accelerometer` interface. It requires an integrated
accelerometer exposed by the manufacturer's Windows driver. It does not use
GPS, infer location, install a driver, request elevation or record raw samples.

The driver is registered on Windows only. A probe must obtain a finite,
recent reading before the sensor is reported available. Missing hardware,
denied access, absent readings and a stopped driver must not appear as coverage.
An unavailable sensor does not disable the existing power, lid, USB, network,
display or input sensors.

At the start of monitoring, the sensor establishes a fixed acceleration
reference, including gravity. It polls at 20 Hz, respecting the driver's minimum
report interval. Three distinct samples differing by at least 0.18 g, or one
sample differing by at least 0.65 g, trigger the existing critical alarm path.
One event is emitted per uninterrupted monitoring run. Samples older than one
second withdraw coverage. Cancellation releases the sensor and COM apartment.

This detects movement, not distance, and cannot guarantee detection of every
slow translation. It requires the computer and sensor driver to remain awake.
Thresholds need acceptance testing on supported physical Windows hardware.

There is no macOS adapter: the public CMMotionManager interface is unavailable
on native macOS. Private Apple SPU/HID protocols and privileged helpers are not
included. This does not assert that MacBooks lack physical sensors.

Before release, complete the localized sensor description and alarm message,
their independent language reviews, Windows native CI, sensor loss/reconnect
acceptance, and app/daemon package integration. Do not describe a compiled test
binary or simulated input as a successful physical movement test.

References:
- https://learn.microsoft.com/en-us/uwp/api/windows.devices.sensors.accelerometer
- https://learn.microsoft.com/en-us/windows/apps/desktop/modernize/winrt-api-desktop-app-support
- https://developer.apple.com/documentation/coremotion/cmmotionmanager
