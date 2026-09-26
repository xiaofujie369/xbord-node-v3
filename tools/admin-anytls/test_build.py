import unittest
from pathlib import Path
import sys
import build


class BuildTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = Path(sys.argv[-1]).read_bytes()
        cls.extension = Path(__file__).with_name('extension.mjs').read_text(encoding='utf-8')

    def test_unknown_upstream_version_is_rejected(self):
        with self.assertRaises(ValueError):
            build.build(self.source + b' ', self.extension)

    def test_other_protocol_forms_and_crypto_are_unchanged(self):
        before = self.source.decode('utf-8')
        after = build.build(self.source, self.extension).decode('utf-8')
        for start, end in [('I4t=(e,t)=>', ',M4t=(e,t)=>'), ('j4t=(e,t)=>', ',V4t=(e,t)=>'), ('D4t=()=>', ',I4t=(e,t)=>')]:
            self.assertEqual(build.between(before, start, end)[2], build.between(after, start, end)[2])
        self.assertEqual(before.count('D4t()'), after.count('D4t()'))

    def test_certificate_controls_are_conditional_and_build_is_reproducible(self):
        result = build.build(self.source, self.extension)
        self.assertEqual(result, build.build(self.source, self.extension))
        self.assertIn(b'Number(x.watch("protocol_settings.tls"))!==1?null:', result)
        self.assertIn(b'prefix:"tls_settings.ech"', result)


if __name__ == '__main__':
    suite = unittest.TestLoader().loadTestsFromTestCase(BuildTest)
    outcome = unittest.TextTestRunner(verbosity=2).run(suite)
    raise SystemExit(not outcome.wasSuccessful())
