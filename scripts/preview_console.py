"""Serve the PurpleMagic GM preview with sample data on loopback."""
import argparse
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
WEB = ROOT/'gm-web'

class Handler(SimpleHTTPRequestHandler):
    def do_GET(self):
        path=urlsplit(self.path)
        if path.path == '/preview-items.json':
            source=getattr(self.server,'item_details',None)
            if source is None:self.send_error(404);return
            raw=source.read_bytes();self.send_response(200);self.send_header('Content-Type','application/json; charset=utf-8');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw);return
        if path.path in ('/', '/index.html'):
            self.path = '/preview.html'+('?' + path.query if path.query else '')
        super().do_GET()

    def end_headers(self):
        self.send_header('Cache-Control', 'no-store')
        self.send_header('X-Content-Type-Options', 'nosniff')
        super().end_headers()

    def log_message(self, *_):
        pass

def create_server(port=0,item_details=None):
    server=ThreadingHTTPServer(('127.0.0.1', port), partial(Handler, directory=str(WEB)))
    server.item_details=Path(item_details).resolve() if item_details else None
    return server

def main():
    raise SystemExit("First GM UI is archived. Use preview-gm-console.bat or gm-console live mode.")
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port', type=int, default=8765)
    parser.add_argument('--item-details',type=Path,help='local compiled PVF samples for ?pvf=1 preview')
    args=parser.parse_args()
    with create_server(args.port,args.item_details) as server:
        print(f'Visual prototype: http://127.0.0.1:{server.server_port}/', flush=True)
        try:server.serve_forever()
        except KeyboardInterrupt:pass

if __name__=='__main__':main()
