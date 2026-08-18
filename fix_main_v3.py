#!/usr/bin/env python3
"""Fix corrupted line endings in main.go by working with raw bytes."""

path = 'cmd/server/main.go'
with open(path, 'rb') as f:
    content = f.read()

# Check what the actual bytes are around the problematic area
idx = content.find(b'qoderCNLogin')
if idx >= 0:
    print(f'Found qoderCNLogin at byte {idx}')
    segment = content[idx-10:idx+60]
    print(f'Segment: {repr(segment)}')
    
    # The literal backslash-r-backslash-n is b'\\r\\n' (4 bytes)
    # We need to replace it with actual CRLF b'\r\n' (2 bytes)
    # And literal backslash-t is b'\\t' (2 bytes) -> actual tab b'\t' (1 byte)
    
    # Replace literal \r\n\t with actual CRLF + tab
    content = content.replace(b'bool\\r\\n\\tt raeCNLogin', b'bool\r\n\ttraeCNLogin')
    content = content.replace(b'bool\\r\\n\\ttraeCNLogin', b'bool\r\n\ttraeCNLogin')
    
    # Also fix var declaration
    content = content.replace(b'var qoderCNLogin       bool\\r\\n\\tt raeCNLogin', b'var qoderCNLogin bool\r\n\tvar traeCNLogin bool')
    content = content.replace(b'var qoderCNLogin       bool\\r\\n\\ttraeCNLogin', b'var qoderCNLogin bool\r\n\tvar traeCNLogin bool')
    
    with open(path, 'wb') as f:
        f.write(content)
    print('Fixed!')
else:
    print('qoderCNLogin not found')
