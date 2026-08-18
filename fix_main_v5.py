#!/usr/bin/env python3
"""Completely fix main.go by rewriting the problematic section."""

path = 'cmd/server/main.go'
with open(path, 'rb') as f:
    content = f.read()

# The issue: literal backtick sequences `r`n and `t are in the file
# We need to replace: bool`r`n`ttraeCNLogin -> bool\r\n\ttraeCNLogin

# Find and replace the struct field
content = content.replace(
    b'qoderCNLogin       bool\x60r\x60n\x60traeCNLogin        bool',
    b'qoderCNLogin       bool\r\n\ttraeCNLogin        bool'
)

# Find and replace the var declaration
content = content.replace(
    b'var qoderCNLogin       bool\x60r\x60n\x60traeCNLogin        bool',
    b'var qoderCNLogin bool\r\n\tvar traeCNLogin bool'
)

with open(path, 'wb') as f:
    f.write(content)
print('Fixed!')

# Verify
with open(path, 'r', encoding='utf-8') as f:
    lines = f.readlines()
print(f'Line 109: {repr(lines[108])}')
print(f'Line 174: {repr(lines[173])}')
