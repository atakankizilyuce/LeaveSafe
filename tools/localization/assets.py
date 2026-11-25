#!/usr/bin/env python3
"""Generate localized install manifests from the reviewed product tagline."""
import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    base = json.loads((ROOT / 'web/public/manifest.webmanifest').read_text())
    output = ROOT / 'web/public/manifests'
    if not args.check:
        output.mkdir(exist_ok=True)
    for source in sorted((ROOT / 'web/src/locales').glob('*.json')):
        locale = source.stem
        copy = json.loads(source.read_text())
        manifest = {**base, 'description': copy["Leave it. We'll watch it."], 'lang': locale,
                    'dir': 'rtl' if locale in ('ar', 'arz', 'ur', 'pnb-Arab') else 'ltr'}
        target = output / f'{locale}.webmanifest'
        text = json.dumps(manifest, ensure_ascii=False, indent=2) + '\n'
        if args.check:
            if not target.exists() or target.read_text() != text:
                raise SystemExit(f'{target}: regenerate localized manifests')
        else:
            target.write_text(text)


if __name__ == '__main__':
    main()
