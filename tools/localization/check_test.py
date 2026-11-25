import unittest
import json
import tempfile
from pathlib import Path
from unittest.mock import patch

import check
from check import validate


class CatalogContractTest(unittest.TestCase):
    def test_full_sentences_can_reorder_variables(self):
        self.assertEqual([], validate({'Hello {name}: {count}': 'Hello {name}: {count}'}, {'Hello {name}: {count}': '{count}: merhaba {name}'}))

    def test_plural_branches_must_keep_all_variables(self):
        source = {'count': {'one': '{count} sensor · {since}', 'other': '{count} sensors · {since}'}}
        self.assertTrue(validate(source, {'count': {'other': '{count} sensör'}}))
        self.assertFalse(validate(source, {'count': {'other': '{count} sensör · {since}'}}))

    def test_missing_or_empty_translation_cannot_pass_release(self):
        self.assertTrue(validate({'Settings': 'Settings'}, {}))
        self.assertTrue(validate({'Settings': 'Settings'}, {'Settings': ' '}))
        self.assertFalse(validate({'Settings': 'Settings'}, {}, True))

    def test_unknown_plural_categories_and_extra_keys_fail(self):
        self.assertTrue(validate({'x': 'x'}, {'x': {'other': 'x', 'singular': 'x'}}))
        self.assertTrue(validate({'x': 'x'}, {'x': 'x', 'y': 'y'}))

    def test_bad_plural_types_are_rejected(self):
        self.assertTrue(validate({'x': 'x'}, {'x': {'one': 'one'}}))
        self.assertTrue(validate({'x': 'x'}, {'x': {'other': 4}}))
        self.assertTrue(validate({'x': 'x'}, {'x': {'other': 'many', 'one': 1}}))

    def test_cli_gate_checks_both_terminal_surfaces_and_argument_order(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for directory in ('web/src/locales', 'internal/localization/catalogs', 'internal/localization/service'):
                target = root / directory
                target.mkdir(parents=True)
                for locale in ('en', 'tr'):
                    (target / f'{locale}.json').write_text(json.dumps({'Status %s %d': 'Status %s %d'}))
            with patch.object(check, 'ROOT', root), patch.object(check, 'LOCALES', ['en', 'tr']), patch('sys.argv', ['check.py']):
                check.main()
                service = root / 'internal/localization/service/tr.json'
                service.write_text(json.dumps({'Status %s %d': 'Status %d %s'}))
                with self.assertRaisesRegex(SystemExit, 'changed Go formatting'):
                    check.main()
                service.unlink()
                with self.assertRaisesRegex(SystemExit, 'service/tr: missing catalog'):
                    check.main()
                with patch('sys.argv', ['check.py', '--allow-incomplete']):
                    check.main()
                (root / 'web/src/locales/tr.json').unlink()
                with self.assertRaisesRegex(SystemExit, 'tr: missing catalog'):
                    check.main()


if __name__ == '__main__':
    unittest.main()
