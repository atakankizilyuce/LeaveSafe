#!/usr/bin/env bash
# Boots a real Ubuntu VM under QEMU/KVM, installs the compiled scenario suite
# and the leavesafe binary, and runs the suite as root inside the guest.
#
# A VM rather than a container: the scenarios load kernel modules and own real
# device nodes. A container shares the host kernel and cannot do either without
# mutating the host — which is also why this project has no Docker support.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../../.." && pwd)"
WORK="${SANDBOX_WORKDIR:-$HERE/.work}"
IMG_URL="${SANDBOX_IMAGE_URL:-https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-amd64.img}"
SSH_PORT="${SANDBOX_SSH_PORT:-2222}"

fail() { echo "::error::$*" >&2; exit 1; }

for tool in qemu-system-x86_64 qemu-img cloud-localds ssh scp ssh-keygen curl; do
  command -v "$tool" >/dev/null 2>&1 || fail "$tool is required but not installed"
done

[ -e /dev/kvm ] || fail "/dev/kvm is missing; this machine cannot boot the sandbox VM"

mkdir -p "$WORK"
cd "$WORK"

if [ ! -f base.img ]; then
  echo "Downloading the Ubuntu cloud image (this happens once)..."
  curl -fsSL -o base.img.part "$IMG_URL"
  mv base.img.part base.img
fi

# A fresh overlay per run keeps the downloaded base image pristine and cacheable.
rm -f disk.qcow2
qemu-img create -f qcow2 -F qcow2 -b base.img disk.qcow2 20G >/dev/null

if [ ! -f id_sandbox ]; then
  ssh-keygen -t ed25519 -N "" -f id_sandbox -q
fi
sed "s|SSH_PUBLIC_KEY_PLACEHOLDER|$(cat id_sandbox.pub)|" \
  "$HERE/cloud-init.yaml" > user-data
: > meta-data
cloud-localds seed.img user-data meta-data

echo "Cross-compiling the scenario suite and the binary for the guest..."
(cd "$REPO" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go test -c -tags sandbox -o "$WORK/sandbox.test" ./test/sandbox/linuxvm)
(cd "$REPO" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -o "$WORK/leavesafe" ./cmd/leavesafe)

echo "Booting the VM..."
qemu-system-x86_64 \
  -enable-kvm -m 2048 -smp 2 -nographic \
  -drive file=disk.qcow2,if=virtio \
  -drive file=seed.img,if=virtio,format=raw \
  -netdev "user,id=net0,hostfwd=tcp::${SSH_PORT}-:22" \
  -device virtio-net-pci,netdev=net0 \
  > vm-console.log 2>&1 &
QEMU_PID=$!
cleanup() { kill "$QEMU_PID" 2>/dev/null || true; }
trap cleanup EXIT

SSH_OPTS=(-i id_sandbox -o StrictHostKeyChecking=no
          -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR
          -o ConnectTimeout=5)

echo "Waiting for SSH..."
for _ in $(seq 1 120); do
  if ssh "${SSH_OPTS[@]}" -p "$SSH_PORT" sandbox@127.0.0.1 true 2>/dev/null; then
    break
  fi
  sleep 5
done
if ! ssh "${SSH_OPTS[@]}" -p "$SSH_PORT" sandbox@127.0.0.1 true 2>/dev/null; then
  echo "--- VM console ---" >&2
  tail -60 vm-console.log >&2
  fail "the VM never became reachable over SSH"
fi

echo "Waiting for cloud-init to finish installing packages and modules..."
ssh "${SSH_OPTS[@]}" -p "$SSH_PORT" sandbox@127.0.0.1 'sudo cloud-init status --wait' || true

echo "Copying artifacts into the guest..."
scp "${SSH_OPTS[@]}" -P "$SSH_PORT" sandbox.test leavesafe sandbox@127.0.0.1:/home/sandbox/

echo "Running the scenario suite as root inside the VM..."
set +e
ssh "${SSH_OPTS[@]}" -p "$SSH_PORT" sandbox@127.0.0.1 \
  'chmod +x /home/sandbox/sandbox.test /home/sandbox/leavesafe && \
   cd /home/sandbox && \
   sudo LEAVESAFE_TEST_BINARY=/home/sandbox/leavesafe \
        ./sandbox.test -test.v -test.timeout=20m' 2>&1 | tee guest-output.log
RESULT=${PIPESTATUS[0]}
set -e

# The suite prints its coverage matrix inside the guest, where the runner's
# summary file is out of reach. Forward it so the gaps stay visible in the PR.
if [ -n "${GITHUB_STEP_SUMMARY:-}" ] && grep -q "Real-trigger coverage" guest-output.log; then
  sed -n '/### Real-trigger coverage/,$p' guest-output.log >> "$GITHUB_STEP_SUMMARY"
fi

if [ "$RESULT" -ne 0 ]; then
  echo "--- VM console (last 60 lines) ---" >&2
  tail -60 vm-console.log >&2
fi

exit "$RESULT"
