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

## What it creates

| Sensor | Real trigger |
| --- | --- |
| power | `test_power` module; writing `ac_online=0` unplugs the charger for real |
| input | `uinput` virtual keyboard driven by `evemu-event` |
| usb | `dummy_hcd` host controller with the `g_zero` gadget bound to it |
| screen | `Xvfb +extension DPMS` plus `xset dpms force off` |
| network | `ip addr add` on the loopback interface |
| lid | not possible — QEMU x86 emulates no ACPI lid button |

Anything that cannot be created is skipped with the reason attached and appears
in the coverage matrix the run prints. The matrix is forwarded to the GitHub job
summary, so a reader of the pull request sees the gaps rather than inferring
coverage from a green check.

The `+extension DPMS` flag is not decoration: a plain `Xvfb` reports "Server
does not have the DPMS Extension", which makes `xset dpms force off` a no-op and
the screen scenario meaningless. The captured runner fixture in
`internal/monitor/testdata/linux/xset_q_on.txt` shows exactly that.

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
