# Linux VM sandbox

Boots a real Ubuntu VM under QEMU/KVM and creates hardware the kernel really
reports, so the unmodified `leavesafe` binary reads a real `/sys` and `/dev`.
The scenarios then arm the app over a real WebSocket, change the hardware, and
require the alert to arrive on the client.

## Why a VM and not a container

A container shares the host kernel: it cannot load modules or own device nodes.
Measured inside one, `/proc/acpi/button/lid`, `/sys/bus/usb/devices` and
`/dev/input` are all absent, and `/sys/class/power_supply` reports the host VM's
battery rather than the laptop's. That is why this project has no Docker support
and why the sandbox is a VM.

## What it actually proves

Measured on `ubuntu-latest`, not assumed:

| Sensor | Real trigger | Result |
| --- | --- | --- |
| power | `test_power` module; writing `ac_online=off` unplugs the charger for real | **proven** |
| network | `ip addr add` on the loopback interface | **proven** |
| input | `uinput` device driven by `evemu-event` | not proven — see below |
| usb | `dummy_hcd` host controller with a gadget bound to it | not proven — `dummy_hcd` is not in the runner kernel's module set |
| screen | `Xvfb` plus `xset dpms force off` | not proven — this Xvfb build has no DPMS extension even with `+extension DPMS` |
| lid | — | not possible; QEMU x86 emulates no ACPI lid button |

Every "not proven" row is a skip carrying that exact reason, printed in the
coverage matrix and forwarded to the GitHub job summary. Nothing here reports
success for hardware it did not actually change.

Each helper verifies its own effect before the scenario asserts anything. The
power helper writes the module parameter and then **reads the value back through
`/sys/class/power_supply`**, because `test_power` maps the written string through
a lookup table and silently keeps the old value for anything it does not
recognise — `1` and `0` are ignored, `on` and `off` are not. Without the
read-back this scenario failed as a mysterious timeout.

### The input result is a finding, not just a gap

The input helper measured that injected key events leave the mtime of
`/dev/input/event*` unchanged. That timestamp is the *only* signal
`internal/monitor/input_linux.go` reads. Device nodes on devtmpfs get their mtime
at creation and do not update as events flow through them, which suggests the
Linux input sensor would not detect a real keyboard either. That is a product
question rather than a test-environment one, so the scenario skips and says so
instead of quietly passing or quietly failing.

## Safety

The scenarios load kernel modules, so they refuse to run unless
`/etc/leavesafe-sandbox` exists — a marker cloud-init writes only inside the
disposable VM. Running the suite by accident on a workstation skips every
scenario instead of modifying that machine's kernel.

## Running it

Needs a Linux host with `/dev/kvm`:

```bash
sudo apt-get install -y qemu-system-x86 qemu-utils cloud-image-utils
make test-sandbox
```

The first run downloads a ~600 MB cloud image into `.work/` and takes a few
minutes; later runs reuse it. `.work/vm-console.log` holds the guest's serial
console if a boot goes wrong.
