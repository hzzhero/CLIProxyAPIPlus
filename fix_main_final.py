#!/usr/bin/env python3
"""Fix corrupted line endings in main.go."""

path = 'cmd/server/main.go'

# Read as binary to preserve exact bytes
with open(path, 'rb') as f:
    content = f.read()

# The problematic pattern contains literal backtick characters
# \x60 is backtick character
old1 = b'qoderCNLogin       bool\x60r\x60n\x60traeCNLogin        bool'
new1 = b'qoderCNLogin       bool\r\n\ttraeCNLogin        bool'

old2 = b'var qoderCNLogin       bool\x60r\x60n\x60traeCNLogin        bool'
new2 = b'var qoderCNLogin bool\r\n\tvar traeCNLogin bool'

if old1 in content:
    content = content.replace(old1, new1)
    print('Fixed struct field (line ~109)')
else:
    print('Struct field pattern not found')

if old2 in content:
    content = content.replace(old2, new2)
    print('Fixed var declaration (line ~175)')
else:
    print('Var declaration pattern not found')

with open(path, 'wb') as f:
    f.write(content)

print('Done!')
