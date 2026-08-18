import re

path = 'cmd/server/main.go'
with open(path, 'rb') as f:
    content = f.read()

# Replace literal \r\n\t sequences with actual CRLF+tab
content = content.replace(b'bool\\r\\n\\tt raeCNLogin', b'bool\r\n\ttraeCNLogin')
content = content.replace(b'var qoderCNLogin       bool\\r\\n\\tt raeCNLogin', b'var qoderCNLogin bool\r\n\tvar traeCNLogin bool')
content = content.replace(b'qoderCNLogin       bool\\r\\n\\tt raeCNLogin        bool', b'qoderCNLogin       bool\r\n\ttraeCNLogin        bool')

with open(path, 'wb') as f:
    f.write(content)

print('Fixed!')
