#!/usr/bin/env python3
import requests
import time
import logging
import argparse
from datetime import datetime
import random

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    handlers=[
        logging.FileHandler('echo-client.log'),
        logging.StreamHandler()
    ]
)
logger = logging.getLogger(__name__)

def generate_payload(size_bytes):
    """Generate payload of specified size"""
    return b'X' * size_bytes

def send_request(server_url, payload_size):
    """Send request to server and measure latency"""
    payload = generate_payload(payload_size)
    
    start_time = time.time()
    try:
        response = requests.post(server_url, data=payload, timeout=10)
        end_time = time.time()
        
        if response.status_code == 200:
            # Verify echo
            if response.content == payload:
                gap_ms = (end_time - start_time) * 1000
                return gap_ms, None
            else:
                return None, "Response mismatch"
        else:
            return None, f"HTTP error {response.status_code}"
    except Exception as e:
        end_time = time.time()
        return None, str(e)

def main():
    parser = argparse.ArgumentParser(description='HTTP Echo Client')
    parser.add_argument('--server', type=str, default='http://localhost:8080', 
                        help='Server URL')
    parser.add_argument('--interval', type=int, default=30, 
                        help='Interval between requests in seconds')
    args = parser.parse_args()
    
    server_url = args.server
    interval = args.interval
    
    # Extract target IP from URL
    target_ip = server_url.replace('http://', '').replace('https://', '').split(':')[0]
    
    logger.info(f"Starting HTTP echo client - Server: {server_url}, Interval: {interval}s")
    
    # Payload sizes to test
    payload_sizes = [128, 512, 1024]
    
    try:
        while True:
            # Send all three payload sizes in sequence
            for payload_size in payload_sizes:
                # Send request
                gap_ms, error = send_request(server_url, payload_size)
                
                # Log result
                current_time = datetime.now().strftime('%Y-%m-%d %H:%M:%S')
                if error:
                    logger.error(f"PayloadSize: {payload_size}B, Time: {current_time}, TargetIP: {target_ip}, Error: {error}")
                else:
                    logger.info(f"PayloadSize: {payload_size}B, Time: {current_time}, TargetIP: {target_ip}, GapTime: {gap_ms:.2f}ms")
            
            # Wait for next interval (1 minute)
            time.sleep(interval)
    except KeyboardInterrupt:
        logger.info("Client stopped")

if __name__ == '__main__':
    main()
