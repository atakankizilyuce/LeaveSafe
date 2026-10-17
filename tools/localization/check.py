#!/usr/bin/env python3
"""Validate complete catalogs and named-variable contracts without network access."""
import argparse
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
LOCALES = ('en zh-Hans hi es ar fr bn pt id ur ru de ja pcm arz mr vi te sw ha tr pnb-Arab tl ta yue-Hant').split()
VARIABLE = re.compile(r'\{([A-Za-z][A-Za-z0-9]*)\}')
CATEGORIES = {'zero', 'one', 'two', 'few', 'many', 'other'}


def variants(value):
    if isinstance(value, str):
        return [value]
    if not isinstance(value, dict) or not isinstance(value.get('other'), str):
        raise ValueError('plural messages require an other branch')
    if any(key not in CATEGORIES and not re.fullmatch(r'=\d+', key) for key in value):
        raise ValueError('unknown plural category')
    if not all(isinstance(item, str) for item in value.values()):
        raise ValueError('every plural branch must be text')
    return list(value.values())


def validate(source, target, allow_incomplete=False):
    errors = []
    if set(target) - set(source):
        errors.append('unknown keys: ' + ', '.join(sorted(set(target) - set(source))))
    if not allow_incomplete and set(source) - set(target):
        errors.append('missing keys: ' + ', '.join(sorted(set(source) - set(target))))
    for key in set(source) & set(target):
        expected = set(VARIABLE.findall(variants(source[key])[0]))
        try:
            for text in variants(target[key]):
                if not text.strip():
                    errors.append(f'{key}: empty message')
                if set(VARIABLE.findall(text)) != expected:
                    errors.append(f'{key}: changed variables')
        except ValueError as error:
            errors.append(f'{key}: {error}')
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--allow-incomplete', action='store_true', help='development only; missing translations stay unavailable in the picker')
    args = parser.parse_args()
    directory = ROOT / 'web/src/locales'
    source = json.loads((directory / 'en.json').read_text())
    errors = []
    for locale in LOCALES:
        path = directory / f'{locale}.json'
        if not path.exists():
            errors.append(f'{locale}: missing catalog')
            continue
        target = json.loads(path.read_text())
        errors.extend(f'{locale}: {error}' for error in validate(source, target, args.allow_incomplete))
    formatting = re.compile(r'%[-+ #0-9.]*[a-zA-Z]')
    for section in ('catalogs', 'service'):
        terminal = ROOT / 'internal/localization' / section
        terminal_source = json.loads((terminal / 'en.json').read_text())
        for locale in LOCALES:
            path = terminal / f'{locale}.json'
            if not path.exists():
                if not args.allow_incomplete:
                    errors.append(f'{section}/{locale}: missing catalog')
                continue
            target = json.loads(path.read_text())
            errors.extend(f'{section}/{locale}: {error}' for error in validate(terminal_source, target, args.allow_incomplete))
            for key in set(terminal_source) & set(target):
                if not isinstance(target[key], str) or formatting.findall(key) != formatting.findall(target[key]):
                    errors.append(f'{section}/{locale}: changed Go formatting: {key}')
    if errors:
        raise SystemExit('\n'.join(errors))
    print(f'{len(LOCALES)} catalogs validated against {len(source)} source messages')


if __name__ == '__main__':
    main()
