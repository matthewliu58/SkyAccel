#!/usr/bin/env python3
import socket
import time
import logging
import argparse
from datetime import datetime

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    handlers=[
        logging.FileHandler('tcp-echo-client.log'),
        logging.StreamHandler()
    ]
)
logger = logging.getLogger(__name__)

def generate_payload(size_bytes):
    """Generate payload of specified size"""
    return b'X' * size_bytes

def send_request(host, port, payload_size):
    """Send request to TCP server and measure latency"""
    payload = generate_payload(payload_size)
    
    start_time = time.time()
    try:
        # Create TCP socket
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.settimeout(10)
        
        # Connect to server
        sock.connect((host, port))
        
        # Send payload with newline delimiter
        sock.sendall(payload + b'\n')
        
        # Receive response
        buffer = b""
        while True:
            chunk = sock.recv(1024)
            if not chunk:
                break
            buffer += chunk
            if b'\n' in buffer:
                break
        
        end_time = time.time()
        sock.close()
        
        # Clean response (remove newline)
        response = buffer.strip()
        
        # Verify echo
        if response == payload:
            gap_ms = (end_time - start_time) * 1000
            return gap_ms, None
        else:
            return None, f"Response mismatch: expected {len(payload)} bytes, got {len(response)} bytes"
    
    except Exception as e:
        end_time = time.time()
        return None, str(e)

def main():
    parser = argparse.ArgumentParser(description='TCP Echo Client')
    parser.add_argument('--host', type=str, default='localhost', 
                        help='Server hostname or IP')
    parser.add_argument('--port', type=int, default=8082, 
                        help='Server port')
    parser.add_argument('--interval', type=int, default=30, 
                        help='Interval between request batches in seconds')
    args = parser.parse_args()
    
    host = args.host
    port = args.port
    interval = args.interval
    
    logger.info(f"Starting TCP echo client - Server: {host}:{port}, Interval: {interval}s")
    
    # Payload sizes to test
    payload_sizes = [128, 512, 1024]
    
    try:
        while True:
            # Send all three payload sizes in sequence
            for payload_size in payload_sizes:
                # Send request
                gap_ms, error = send_request(host, port, payload_size)
                
                # Log result
                current_time = datetime.now().strftime('%Y-%m-%d %H:%M:%S')
                if error:
                    logger.error(f"PayloadSize: {payload_size}B, Time: {current_time}, TargetIP: {host}, Error: {error}")
                else:
                    logger.info(f"PayloadSize: {payload_size}B, Time: {current_time}, TargetIP: {host}, GapTime: {gap_ms:.2f}ms")
            
            # Wait for next interval
            time.sleep(interval)
    except KeyboardInterrupt:
        logger.info("Client stopped")

if __name__ == '__main__':
    main()
