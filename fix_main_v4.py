#!/usr/bin/env python3
"""Completely fix main.go by rewriting the problematic section."""

path = 'cmd/server/main.go'
with open(path, 'rb') as f:
    content = f.read()

# Find the exact bytes we need to replace
# The pattern is: qoderCNLogin       bool`r`n`ttraeCNLogin        bool
# Where `r`n and `t are literal characters (backtick-r-backtick-n, backtick-t)

old_pattern = b'qoderCNLogin       bool\x60r\x60n\x60traeCNLogin        bool'
new_pattern = b'qoderCNLogin       bool\r\n\ttraeCNLogin        bool'

if old_pattern in content:
    content = content.replace(old_pattern, new_pattern)
    print('Fixed struct field')
else:
    print('Pattern not found, trying alternative...')
    # Try without the leading spaces
    old_pattern2 = b'qoderCNLogin       bool\x60r\x60n\x60traeCNLogin'
    if old_pattern2 in content:
        content = content.replace(old_pattern2, new_pattern[:len(new_pattern)-len(b'        bool')])
        print('Fixed with alternative pattern')
    else:
        print('Still not found')
        # Let's just find what's actually there
        idx = content.find(b'qoderCNLogin')
        if idx >= 0:
            print(f'Found at {idx}: {repr(content[idx:idx+80])}')

# Also check for var declaration
old_var_pattern = b'var qoderCNLogin       bool\x60r\x60n\x60traeCNLogin'
new_var_pattern = b'var qoderCNLogin bool\r\n\tvar traeCNLogin bool'
if old_var_pattern in content:
    content = content.replace(old_var_pattern, new_var_pattern)
    print('Fixed var declaration')
else:
    # Search around line 174
    lines = content.split(b'\n')
    for i, line in enumerate(lines[170:180], 171):
        if b'qoderCNLogin' in line:
            print(f'Line {i}: {repr(line)}')

with open(path, 'wb') as f:
    f.write(content)
print('Done!')
