#!/usr/bin/env python3
"""Fix corrupted line endings in main.go."""

path = 'cmd/server/main.go'
with open(path, 'r', encoding='utf-8') as f:
    content = f.read()

# The issue is literal backslash sequences in the file
# Replace the problematic pattern
content = content.replace(
    'bool\\r\\n\\tt raeCNLogin        bool',
    'bool\r\n\ttraeCNLogin        bool'
)

content = content.replace(
    'var qoderCNLogin       bool\\r\\n\\tt raeCNLogin        bool',
    'var qoderCNLogin bool\r\n\tvar traeCNLogin bool'
)

content = content.replace(
    'qoderCNLogin       bool\\r\\n\\tt raeCNLogin        bool',
    'qoderCNLogin       bool\r\n\ttraeCNLogin        bool'
)

with open(path, 'w', encoding='utf-8') as f:
    f.write(content)

print('Fixed!')
print(f'Checking for remaining issues...')
with open(path, 'r', encoding='utf-8') as f:
    lines = f.readlines()
for i, line in enumerate(lines[105:115], 106):
    print(f'{i}: {repr(line)}')
