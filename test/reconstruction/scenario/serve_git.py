#!/usr/bin/env python3
"""Serve the fixture bare repository to guests as a read-only dumb-HTTP Git remote over TLS."""

import argparse
import functools
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import ssl


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True)
    parser.add_argument("--cert", required=True)
    parser.add_argument("--key", required=True)
    parser.add_argument("--port", type=int, default=9443)
    args = parser.parse_args()
    # QEMU user networking exposes host loopback to guests as 10.0.2.2.
    handler = functools.partial(SimpleHTTPRequestHandler, directory=args.root)
    server = ThreadingHTTPServer(("127.0.0.1", args.port), handler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(args.cert, args.key)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
