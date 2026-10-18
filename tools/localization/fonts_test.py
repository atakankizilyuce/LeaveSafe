import hashlib
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from fontTools.fontBuilder import FontBuilder
from fontTools.pens.ttGlyphPen import TTGlyphPen
from fontTools.ttLib import TTFont

import fonts


class FontTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'web/public/fonts').mkdir(parents=True)

    def source(self, name):
        builder = FontBuilder(1000, isTTF=True)
        builder.setupGlyphOrder(['.notdef', 'A', 'B'])
        builder.setupCharacterMap({65: 'A', 66: 'B'})
        glyphs = {}
        for name_ in ('.notdef', 'A', 'B'):
            pen = TTGlyphPen(None)
            pen.moveTo((0, 0))
            pen.lineTo((100, 0))
            pen.lineTo((100, 100))
            pen.closePath()
            glyphs[name_] = pen.glyph()
        builder.setupGlyf(glyphs)
        builder.setupHorizontalMetrics({name_: (500, 0) for name_ in glyphs})
        builder.setupHorizontalHeader(ascent=800, descent=-200)
        builder.setupNameTable({'familyName': 'Test', 'styleName': 'Regular'})
        builder.setupOS2()
        builder.setupPost()
        builder.setupFvar([('wght', 100, 400, 900, 'Weight')], [])
        path = self.root / (name + '.ttf')
        builder.save(path)
        return {'name': name, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}

    def test_cjk_subsets_but_other_scripts_keep_user_text_glyphs(self):
        sources = [self.source('NotoSansCJK'), self.source('NotoSans')]
        with patch.object(fonts, 'ROOT', self.root), patch.object(fonts, 'SOURCES', sources), patch.object(fonts, 'characters', return_value={65}):
            fonts.build(self.root)
            fonts.verify()
        with TTFont(self.root / 'web/public/fonts/NotoSansCJK.ttf') as font:
            self.assertEqual(set(font.getBestCmap()), {65})
            self.assertNotIn('fvar', font)
        with TTFont(self.root / 'web/public/fonts/NotoSans.ttf') as font:
            self.assertEqual(set(font.getBestCmap()), {65, 66})

    def test_wrong_upstream_hash_is_rejected_before_font_parsing(self):
        (self.root / 'Bad.ttf').write_bytes(b'untrusted')
        with patch.object(fonts, 'SOURCES', [{'name': 'Bad', 'sha256': 'wrong'}]), patch.object(fonts, 'characters', return_value=set()):
            with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                fonts.build(self.root)

    def test_missing_glyph_is_reported_with_character_and_codepoint(self):
        with patch.object(fonts, 'ROOT', self.root), patch.object(fonts, 'characters', return_value={0x4E2D}):
            with self.assertRaisesRegex(ValueError, 'U\\+4E2D'):
                fonts.verify()

    def test_source_inventory_includes_catalogs_and_language_names(self):
        locales = self.root / 'web/src/locales'
        locales.mkdir(parents=True)
        (locales / 'en.json').write_text('A\n')
        library = self.root / 'web/src/lib'
        library.mkdir()
        (library / 'i18n.ts').write_text('中')
        with patch.object(fonts, 'ROOT', self.root):
            self.assertEqual(fonts.characters(), {65, 0x4E2D})


if __name__ == '__main__':
    unittest.main()
