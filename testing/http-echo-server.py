#!/usr/bin/env python3
import http.server
import socketserver
import logging
import argparse
from datetime import datetime

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    handlers=[
        logging.FileHandler('echo-server.log'),
        logging.StreamHandler()
    ]
)
logger = logging.getLogger(__name__)

class EchoHandler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        # Read content length
        content_length = int(self.headers.get('Content-Length', 0))
        
        # Read payload
        payload = self.rfile.read(content_length)
        
        # Log request
        client_ip = self.client_address[0]
        logger.info(f"Received request - Client: {client_ip}, PayloadSize: {content_length} bytes")
        
        # Send response (echo back)
        self.send_response(200)
        self.send_header('Content-Type', 'application/octet-stream')
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, format, *args):
        # Suppress default logging
        pass

def main():
    parser = argparse.ArgumentParser(description='HTTP Echo Server')
    parser.add_argument('--port', type=int, default=8080, help='Listening port')
    args = parser.parse_args()
    
    host = '0.0.0.0'
    port = args.port
    
    logger.info(f"Starting HTTP echo server on {host}:{port}")
    
    try:
        with socketserver.TCPServer((host, port), EchoHandler) as httpd:
            httpd.serve_forever()
    except KeyboardInterrupt:
        logger.info("Server stopped")

if __name__ == '__main__':
    main()
