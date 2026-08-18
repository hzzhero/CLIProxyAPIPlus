#!/usr/bin/env python3
"""Fix corrupted line endings in main.go."""

path = 'cmd/server/main.go'

# Read as binary to preserve exact bytes
with open(path, 'rb') as f:
    lines = f.readlines()

print(f"Total lines: {len(lines)}")
for i, line in enumerate(lines[107:112], 108):
    print(f"Line {i}: {repr(line)}")

# Fix line 109 (index 108)
if b'bool\x60r\x60n\x60traeCNLogin' in lines[108]:
    lines[108] = b'\tqoderCNLogin       bool\r\n\ttraeCNLogin        bool\r\n'
    print("Fixed line 109")

# Fix line 175 (index 174)
if len(lines) > 174 and b'var qoderCNLogin' in lines[174]:
    lines[174] = b'\tvar qoderCNLogin bool\r\n\tvar traeCNLogin bool\r\n'
    print("Fixed line 175")

with open(path, 'wb') as f:
    f.writelines(lines)

print("Done!")
