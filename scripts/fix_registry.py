#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Fix model definitions to have proper Trae support"""
import re

with open('internal/registry/model_definitions.go', 'r', encoding='utf-8') as f:
    content = f.read()

# Fix the broken struct - close the XAI line and add Trae properly with closing brace
content = content.replace(
    'XAI         []*ModelInfo `json:"xai"`\n\tTrae        []*ModelInfo `json:"trae"`\n\n// GetClaudeModels',
    'XAI         []*ModelInfo `json:"xai"`\n\tTrae        []*ModelInfo `json:"trae"`\n}\n\n// GetClaudeModels'
)

# Remove duplicate GetTraeModels function if exists
content = re.sub(
    r'// GetTraeModels returns the standard Trae model definitions\.\nfunc GetTraeModels\(\) \[\]\*ModelInfo \{\n\treturn cloneModelInfos\(getModels\(\)\.Trae\)\n\}\n\n// GetTraeModels returns the standard Trae model definitions\.\nfunc GetTraeModels\(\) \[\]\*ModelInfo \{\n\treturn cloneModelInfos\(getModels\(\)\.Trae\)\n\}\n',
    '// GetTraeModels returns the standard Trae model definitions.\nfunc GetTraeModels() []*ModelInfo {\n\treturn cloneModelInfos(getModels().Trae)\n}\n',
    content
)

# Add data.Trae to LookupStaticModelInfo
content = content.replace(
    'data.Qoder,\n\t}\n\tfor _, models := range allModels',
    'data.Qoder,\n\t\tdata.Trae,\n\t}\n\tfor _, models := range allModels'
)

with open('internal/registry/model_definitions.go', 'w', encoding='utf-8') as f:
    f.write(content)

print("Fixed model_definitions.go")
