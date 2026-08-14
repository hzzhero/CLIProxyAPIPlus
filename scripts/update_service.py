#!/usr/bin/env python3
# Update service.go to add Trae support

import re

with open('sdk/cliproxy/service.go', 'r', encoding='utf-8') as f:
    content = f.read()

original = content

# 1. Add trae executor case after kimi in registerExecutorForAuth
pattern1 = r'(case "kimi":\n\t\ts\.coreManager\.RegisterExecutor\(executor\.NewKimiExecutor\(s\.cfg\)\))\n\t(case "xai")'
replacement1 = r'\1\n\tcase "trae":\n\t\ts.coreManager.RegisterExecutor(executor.NewTraeExecutor(s.cfg))\n\t\2'
content = re.sub(pattern1, replacement1, content)

# 2. Add trae models case after kimi models
pattern2 = r'(case "kimi":\n\t\tmodels = registry\.GetKimiModels\(\)\n\t\tmodels = applyExcludedModels\(models, excluded\))\n\t(case "xai")'
replacement2 = r'\1\n\tcase "trae":\n\t\tmodels = executor.FetchTraeModels(context.Background(), a, s.cfg)\n\t\tmodels = applyExcludedModels(models, excluded)\n\t\2'
content = re.sub(pattern2, replacement2, content)

if content != original:
    with open('sdk/cliproxy/service.go', 'w', encoding='utf-8') as f:
        f.write(content)
    print('Updated service.go with trae support')
else:
    print('No changes made - pattern not found')
    
# Verify
with open('sdk/cliproxy/service.go', 'r', encoding='utf-8') as f:
    verify = f.read()
    if 'case "trae":' in verify and 'NewTraeExecutor' in verify:
        print('Verification: trae executor added successfully')
    if 'FetchTraeModels' in verify:
        print('Verification: trae models fetch added successfully')
