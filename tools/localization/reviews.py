#!/usr/bin/env python3
"""Require a different independent reviewer for each complete daemon language."""
import argparse
import hashlib
import json
from pathlib import Path

from check import LOCALES, ROOT

CATALOGS = ('web/src/locales', 'internal/localization/catalogs', 'internal/localization/service')


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def validate(root=ROOT, locales=LOCALES, require_all=False):
    errors, reviewers = [], set()
    sources = {f'{directory}/en.json': digest(root / directory / 'en.json') for directory in CATALOGS}
    for locale in locales:
        path = root / f'docs/localization-reviews/{locale}.json'
        if not path.exists():
            if require_all:
                errors.append(f'{locale}: independent review missing')
            continue
        try:
            record = json.loads(path.read_text())
            reviewer = record.get('reviewer')
            if not isinstance(reviewer, str) or not reviewer.strip() or reviewer in reviewers:
                errors.append(f'{locale}: a distinct independent reviewer is required')
            reviewers.add(reviewer)
            files = {f'{directory}/{locale}.json': digest(root / directory / f'{locale}.json')
                     for directory in CATALOGS}
            if record != {'locale': locale, 'reviewer': reviewer, 'sources': sources, 'files': files}:
                errors.append(f'{locale}: stale or incomplete review')
            if not path.with_suffix('.md').is_file():
                errors.append(f'{locale}: semantic review report missing')
        except (OSError, ValueError, TypeError, AttributeError) as error:
            errors.append(f'{locale}: invalid review: {error}')
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--require-all', action='store_true')
    args = parser.parse_args()
    errors = validate(require_all=args.require_all)
    if errors:
        parser.exit(1, '\n'.join(errors) + '\n')


if __name__ == '__main__':
    main()
