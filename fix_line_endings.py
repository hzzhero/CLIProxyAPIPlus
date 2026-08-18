import sys

path = 'cmd/server/main.go'
with open(path, 'rb') as f:
    content = f.read()

# Fix the literal \r\n\t sequences
content = content.replace(b'qoderCNLogin       bool\\r\\n\\tt raeCNLogin        bool', b'qoderCNLogin       bool\r\n\ttraeCNLogin        bool')
content = content.replace(b'var qoderCNLogin       bool\\r\\n\\tt raeCNLogin        bool', b'var qoderCNLogin bool\r\n\tvar traeCNLogin bool')

with open(path, 'wb') as f:
    f.write(content)

print('Fixed line endings in main.go')
