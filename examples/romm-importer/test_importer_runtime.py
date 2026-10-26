import io
import json
import sys
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
from urllib.error import URLError

from test_importer import imp


class NativeVersionTests(unittest.TestCase):
    def response(self, value):
        opener = Mock()
        opener.open.return_value = io.BytesIO(json.dumps(value).encode())
        return opener

    def test_matching_server_is_checked_without_catalog_token(self):
        opener = self.response({'SYSTEM': {'VERSION': imp.VERSION}})
        with patch.object(imp, 'build_opener', return_value=opener):
            imp.check_native_version('http://romm.test:8080')
        request = opener.open.call_args.args[0]
        self.assertEqual(request.full_url, 'http://romm.test:8080/api/heartbeat')
        self.assertIsNone(request.get_header('Authorization'))
        self.assertEqual(opener.open.call_args.kwargs['timeout'], 10)

    def test_server_mismatch_stops_before_native_database_imports(self):
        opener = self.response({'SYSTEM': {'VERSION': '999.0.0'}})
        with patch.dict(sys.modules, {'utils': SimpleNamespace(get_version=lambda: imp.VERSION)}), patch.object(imp, 'build_opener', return_value=opener):
            with self.assertRaisesRegex(ValueError, 'server.*999.0.0.*requires'):
                imp.NativeRuntime()

    def test_missing_or_invalid_server_version_is_rejected(self):
        for value in [{}, {'SYSTEM': []}, {'SYSTEM': {'VERSION': None}}, ['unexpected']]:
            with self.subTest(value=value), patch.object(imp, 'build_opener', return_value=self.response(value)):
                with self.assertRaisesRegex(ValueError, 'heartbeat'):
                    imp.check_native_version('http://romm.test')

    def test_invalid_origin_is_rejected_before_network_access(self):
        for origin in ['ftp://romm.test', 'http://user:password@romm.test', 'http://romm.test/path', 'http://romm.test/?token=private', 'http://romm.test/#fragment']:
            with self.subTest(origin=origin), patch.object(imp, 'build_opener') as opener:
                with self.assertRaisesRegex(ValueError, 'ROMM_SERVER_URL'):
                    imp.check_native_version(origin)
                opener.assert_not_called()

    def test_unreachable_server_has_actionable_error(self):
        opener = Mock()
        opener.open.side_effect = URLError('connection refused')
        with patch.object(imp, 'build_opener', return_value=opener):
            with self.assertRaisesRegex(ValueError, 'ROMM_SERVER_URL'):
                imp.check_native_version('http://romm.test')

    def test_oversized_heartbeat_is_rejected(self):
        opener = Mock()
        opener.open.return_value = io.BytesIO(b"x" * ((1 << 20) + 1))
        with patch.object(imp, 'build_opener', return_value=opener):
            with self.assertRaisesRegex(ValueError, 'exceeds'):
                imp.check_native_version('http://romm.test')

    def test_invalid_json_has_actionable_error(self):
        opener = Mock()
        opener.open.return_value = io.BytesIO(b'not JSON')
        with patch.object(imp, 'build_opener', return_value=opener):
            with self.assertRaisesRegex(ValueError, 'ROMM_SERVER_URL'):
                imp.check_native_version('http://romm.test')
