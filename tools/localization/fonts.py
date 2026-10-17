#!/usr/bin/env python3
"""Build offline font subsets from pinned, locally downloaded upstream sources.

Requires fonttools==4.65.0. Download each font-sources.json URL to SOURCE/NAME.ttf.
Regenerate after adding characters: --source-dir SOURCE.
Use --check in CI to detect missing glyphs without downloading font sources.
"""
import argparse
import hashlib
import json
import unicodedata
from pathlib import Path

from fontTools import subset
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont

ROOT = Path(__file__).resolve().parents[2]
SOURCES = json.loads((Path(__file__).parent / 'font-sources.json').read_text())
WEIGHTS = (400,)


def characters():
    paths = list((ROOT / 'web/src/locales').glob('*.json'))
    paths.append(ROOT / 'web/src/lib/i18n.ts')
    return {ord(c) for path in paths for c in path.read_text() if not unicodedata.category(c).startswith('C')}


def verify():
    coverage = set()
    for font in (ROOT / 'web/public/fonts').glob('*.ttf'):
        with TTFont(font) as data:
            coverage.update(data.getBestCmap())
    missing = characters() - coverage
    if missing:
        raise ValueError('Missing bundled glyphs: ' + ' '.join(f'U+{c:04X} ({chr(c)})' for c in sorted(missing)))


def build(source_dir):
    required = characters()
    for item in SOURCES:
        path = source_dir / (item['name'] + '.ttf')
        if hashlib.sha256(path.read_bytes()).hexdigest() != item['sha256']:
            raise ValueError(f'Upstream checksum mismatch: {path}')
        font = TTFont(path, recalcTimestamp=False)
        options = subset.Options()
        # Retain shaping/conjuncts and locl regional glyph substitutions.
        options.layout_features = ['*']
        options.name_IDs = ['*']
        options.name_legacy = True
        options.name_languages = ['*']
        worker = subset.Subsetter(options=options)
        # Non-CJK families retain their entire writing systems, including text
        # entered by the user. CJK is subset with layout closure to control size.
        worker.populate(unicodes=required if item['name'] == 'NotoSansCJK' else font.getBestCmap())
        worker.subset(font)
        for weight in WEIGHTS:
            axes = {axis.axisTag: axis.defaultValue for axis in font['fvar'].axes}
            axes['wght'] = weight
            instance = instantiateVariableFont(font, axes, inplace=False)
            instance.recalcTimestamp = False
            target = ROOT / f'web/public/fonts/{item["name"]}.ttf'
            instance.save(target)
            print(f'{target.name}: {target.stat().st_size} bytes', flush=True)
    verify()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    parser.add_argument('--source-dir', type=Path)
    args = parser.parse_args()
    if args.check:
        verify()
    elif args.source_dir:
        build(args.source_dir)
    else:
        parser.error('Use --check or provide --source-dir')


if __name__ == '__main__':
    main()
