#!/usr/bin/env python3
"""Fix line endings in main.go that got corrupted during editing."""

path = 'cmd/server/main.go'
with open(path, 'rb') as f:
    content = f.read()

# Find and replace the problematic pattern
# Look for "qoderCNLogin       bool\r\n\ttraeCNLogin" where \r\n is literal
problem1 = b'qoderCNLogin       bool\\r\\n\\tt raeCNLogin        bool'
if problem1 in content:
    content = content.replace(problem1, b'qoderCNLogin       bool\r\n\ttraeCNLogin        bool')
    print('Fixed pattern 1')

problem2 = b'var qoderCNLogin       bool\\r\\n\\tt raeCNLogin        bool'
if problem2 in content:
    content = content.replace(problem2, b'var qoderCNLogin bool\r\n\tvar traeCNLogin bool')
    print('Fixed pattern 2')

# Also check for any other literal backslash-r-backslash-n patterns
import re
pattern = rb'bool[^\x00-\x7f]*traeCNLogin'
matches = list(re.finditer(pattern, content))
if matches:
    print(f'Found {len(matches)} matches with pattern')
    for m in matches:
        print(f'  At {m.start()}: {repr(content[m.start():m.end()])}')

with open(path, 'wb') as f:
    f.write(content)

print('Done!')
