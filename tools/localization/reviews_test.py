import json
from pathlib import Path
import tempfile
import unittest

import reviews


class ReviewsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for directory in reviews.CATALOGS:
            for locale in ('en', 'tr'):
                self.write(f'{directory}/{locale}.json', {'message': locale})

    def write(self, path, value):
        target = self.root / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(json.dumps(value))

    def record(self, locale, reviewer):
        data = {'locale': locale, 'reviewer': reviewer,
                'sources': {f'{d}/en.json': reviews.digest(self.root / d / 'en.json') for d in reviews.CATALOGS},
                'files': {f'{d}/{locale}.json': reviews.digest(self.root / d / f'{locale}.json') for d in reviews.CATALOGS}}
        self.write(f'docs/localization-reviews/{locale}.json', data)
        (self.root / f'docs/localization-reviews/{locale}.md').write_text('Full semantic review.')

    def check(self, require=True):
        return reviews.validate(self.root, ['en', 'tr'], require)

    def test_partial_work_is_allowed_but_cannot_release_without_every_review(self):
        self.assertEqual(self.check(False), [])
        self.assertEqual(len(self.check()), 2)

    def test_independent_current_reviews_pass(self):
        self.record('en', '/root/en')
        self.record('tr', '/root/tr')
        self.assertEqual(self.check(), [])

    def test_same_reviewer_cannot_validate_multiple_languages(self):
        self.record('en', '/root/reviewer')
        self.record('tr', '/root/reviewer')
        self.assertTrue(any('distinct' in e for e in self.check()))

    def test_target_or_source_edits_invalidate_review(self):
        self.record('en', '/root/en')
        self.record('tr', '/root/tr')
        self.write('internal/localization/service/tr.json', {'changed': 'service meaning'})
        self.assertTrue(any('tr: stale' in e for e in self.check()))
        self.write('web/src/locales/en.json', {'changed': 'source meaning'})
        self.assertTrue(any('en: stale' in e for e in self.check()))

    def test_report_and_valid_json_are_required(self):
        self.record('en', '/root/en')
        self.record('tr', '/root/tr')
        (self.root / 'docs/localization-reviews/tr.md').unlink()
        self.assertTrue(any('report missing' in e for e in self.check()))
        self.write('docs/localization-reviews/en.json', [])
        self.assertTrue(any('invalid review' in e for e in self.check()))


if __name__ == '__main__':
    unittest.main()
