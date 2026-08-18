import re

path = 'cmd/server/main.go'
with open(path, 'r', encoding='utf-8') as f:
    content = f.read()

# Fix the broken lines with literal \r\n
content = content.replace('qoderCNLogin       bool\\r\\n\\ttraeCNLogin        bool', 
                          'qoderCNLogin       bool\n\ttraeCNLogin        bool')
content = content.replace('var qoderCNLogin       bool\\r\\n\\ttraeCNLogin        bool',
                          'var qoderCNLogin bool\n\tvar traeCNLogin bool')

# Verify the changes
print("Checking main.go...")
if 'qoderCNLogin       bool\n\ttraeCNLogin        bool' in content:
    print("  ✓ Struct field added")
else:
    print("  ✗ Struct field NOT found")

if 'var qoderCNLogin bool\n\tvar traeCNLogin bool' in content:
    print("  ✓ Var declaration added")
else:
    print("  ✗ Var declaration NOT found")

if 'opts.traeCNLogin' in content:
    print("  ✓ One-shot mode check added")
else:
    print("  ✗ One-shot mode check NOT found")

if 'flag.BoolVar(&traeCNLogin' in content:
    print("  ✓ Flag registration added")
else:
    print("  ✗ Flag registration NOT found")

if 'traeCNLogin:        traeCNLogin,' in content:
    print("  ✓Opts literal added")
else:
    print("  ✗Opts literal NOT found")

if 'cmd.DoTraeCNLogin(cfg, options)' in content:
    print("  ✓ Dispatch branch added")
else:
    print("  ✗ Dispatch branch NOT found")

with open(path, 'w', encoding='utf-8') as f:
    f.write(content)

print("\nDone!")
