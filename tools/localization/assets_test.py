import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import assets


class ManifestTest(unittest.TestCase):
    def test_localized_manifest_keeps_scope_icons_and_start_url(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            public = root / 'web/public'
            locales = root / 'web/src/locales'
            public.mkdir(parents=True)
            locales.mkdir(parents=True)
            base = {'name': 'LeaveSafe', 'start_url': '/', 'scope': '/',
                    'icons': [{'src': '/icons/icon.png', 'sizes': '192x192'}]}
            (public / 'manifest.webmanifest').write_text(json.dumps(base))
            for code in ('en', 'ar', 'ur', 'pnb-Arab'):
                (locales / f'{code}.json').write_text(json.dumps({"Leave it. We'll watch it.": code}))
            with patch.object(assets, 'ROOT', root), patch('sys.argv', ['assets.py']):
                assets.main()
            for code in ('en', 'ar', 'ur', 'pnb-Arab'):
                value = json.loads((public / f'manifests/{code}.webmanifest').read_text())
                self.assertEqual(value, {**base, 'description': code, 'lang': code,
                                        'dir': 'ltr' if code == 'en' else 'rtl'})
            with patch.object(assets, 'ROOT', root), patch('sys.argv', ['assets.py', '--check']):
                assets.main()
                (locales / 'ar.json').write_text(json.dumps({"Leave it. We'll watch it.": 'changed'}))
                with self.assertRaisesRegex(SystemExit, 'regenerate'):
                    assets.main()


if __name__ == '__main__':
    unittest.main()
