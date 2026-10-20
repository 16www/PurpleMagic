import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import os
import socket
import socketserver
import threading

spec = importlib.util.spec_from_file_location('helper', Path(__file__).with_name('activity_restart_helper.py'))
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)


class BrokerTests(unittest.TestCase):
    @unittest.skipUnless(hasattr(socketserver, 'ThreadingUnixStreamServer') and hasattr(socket, 'SO_PEERCRED'), 'Linux Unix peer credentials required')
    def test_real_socket_accepts_only_configured_uid(self):
        for allowed in [os.getuid(),os.getuid()+1]:
            with self.subTest(allowed=allowed),tempfile.TemporaryDirectory() as root:
                calls=[]
                name=str(Path(root)/'restart.sock')
                server=socketserver.ThreadingUnixStreamServer(name,helper.Handler)
                server.allowed_uid=allowed
                server.broker=helper.Broker(Path(root)/'state',lambda:{'revision':9,'restartRequired':True},calls.append)
                thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
                try:
                    with socket.socket(socket.AF_UNIX,socket.SOCK_STREAM) as client:
                        client.settimeout(3);client.connect(name);client.sendall(b'{"revision":9}\n')
                        reply=json.loads(client.recv(1024))
                    self.assertEqual(reply['accepted'],allowed==os.getuid())
                    self.assertEqual(len(calls),int(allowed==os.getuid()))
                finally:
                    server.shutdown();server.server_close();thread.join()
    def test_exact_revision_pending_and_durable_replay(self):
        calls = []
        with tempfile.TemporaryDirectory() as root:
            snapshot = lambda: {'revision':4, 'restartRequired':True}
            broker = helper.Broker(root, snapshot, calls.append)
            self.assertFalse(broker.accept(3))
            self.assertFalse(broker.accept(True))
            self.assertTrue(broker.accept(4))
            self.assertTrue(helper.Broker(root, snapshot, calls.append).accept(4))
            self.assertEqual(calls, ['purplemagic-activity-r4'])
            self.assertFalse(helper.Broker(root, lambda:{'revision':5,'restartRequired':False}, calls.append).accept(5))

    def test_schedule_uncertainty_is_preserved_and_never_repeated(self):
        calls = []
        def fail(unit):
            calls.append(unit)
            raise TimeoutError('outcome unknown')
        with tempfile.TemporaryDirectory() as root:
            snapshot = lambda:{'revision':6,'restartRequired':True}
            self.assertFalse(helper.Broker(root, snapshot, fail).accept(6))
            self.assertFalse(helper.Broker(root, snapshot, fail).accept(6))
            self.assertEqual(len(calls),1)
            self.assertEqual(json.loads((Path(root)/'revision-6.json').read_text())['state'],'prepared')

    def test_request_is_bounded_and_cannot_name_a_command(self):
        self.assertEqual(helper.parse_request(b'{"revision":4}\n'),4)
        for raw in [b'{"revision":true}\n',b'{"revision":-1}\n',b'{"revision":4,"command":"restart"}\n', b'[]\n', b'x'*257,b'{"revision":4}']:
            with self.subTest(raw=raw),self.assertRaises((ValueError,TypeError)):
                helper.parse_request(raw)


if __name__ == '__main__':
    unittest.main()
