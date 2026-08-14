#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Script to add Trae support to model_definitions.go"""

with open('internal/registry/model_definitions.go', 'r') as f:
    content = f.read()

# Add Trae to struct (after XAI)
if 'Trae' not in content:
    content = content.replace(
        'XAI         []*ModelInfo `json:"xai"`\n}',
        'XAI         []*ModelInfo `json:"xai"`\n\tTrae        []*ModelInfo `json:"trae"`\n}'
    )
    print("Added Trae to staticModelsJSON struct")
else:
    print("Trae already present in struct")

# Add GetTraeModels function after GetKimiModels
if 'GetTraeModels' not in content:
    content = content.replace(
        'func GetKimiModels() []*ModelInfo {\n\treturn cloneModelInfos(getModels().Kimi)\n}\n\n// GetAntigravityModels',
        'func GetKimiModels() []*ModelInfo {\n\treturn cloneModelInfos(getModels().Kimi)\n}\n\n// GetTraeModels returns the standard Trae model definitions.\nfunc GetTraeModels() []*ModelInfo {\n\treturn cloneModelInfos(getModels().Trae)\n}\n\n// GetAntigravityModels'
    )
    print("Added GetTraeModels function")
else:
    print("GetTraeModels already present")

# Add trae case to GetStaticModelDefinitionsByChannel (before default)
if 'case "trae":' not in content:
    content = content.replace(
        '\tcase "qoder-cn":\n\t\treturn GetQoderCNModels()\n\tdefault:',
        '\tcase "qoder-cn":\n\t\treturn GetQoderCNModels()\n\tcase "trae":\n\t\treturn GetTraeModels()\n\tdefault:'
    )
    print("Added trae case to GetStaticModelDefinitionsByChannel")
else:
    print("trae case already present")

# Add data.Trae to LookupStaticModelInfo
if 'data.Trae' not in content:
    content = content.replace(
        '\t\tdata.Qoder,\n\t}\n\tfor _, models := range allModels',
        '\t\tdata.Qoder,\n\t\tdata.Trae,\n\t}\n\tfor _, models := range allModels'
    )
    print("Added data.Trae to LookupStaticModelInfo")
else:
    print("data.Trae already present")

with open('internal/registry/model_definitions.go', 'w') as f:
    f.write(content)

print("\nAll updates completed!")
