#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Add Trae OAuth2 support to CLIProxyAPIPlus"""
import re

def update_service_go():
    """Add trae cases to service.go"""
    path = 'sdk/cliproxy/service.go'
    with open(path, 'r', encoding='utf-8') as f:
        content = f.read()
    
    original = content
    
    # Add trae executor case after kimi
    pattern1 = r'(case "kimi":\n\t\ts\.coreManager\.RegisterExecutor\(executor\.NewKimiExecutor\(s\.cfg\)\))\n\t(case "xai")'
    replacement1 = r'\1\n\tcase "trae":\n\t\ts.coreManager.RegisterExecutor(executor.NewTraeExecutor(s.cfg))\n\t\2'
    content = re.sub(pattern1, replacement1, content)
    
    # Add trae models case after kimi models
    pattern2 = r'(case "kimi":\n\t\tmodels = registry\.GetKimiModels\(\)\n\t\tmodels = applyExcludedModels\(models, excluded\))\n\t(case "xai")'
    replacement2 = r'\1\n\tcase "trae":\n\t\tmodels = executor.FetchTraeModels(context.Background(), a, s.cfg)\n\t\tmodels = applyExcludedModels(models, excluded)\n\t\2'
    content = re.sub(pattern2, replacement2, content)
    
    if content != original:
        with open(path, 'w', encoding='utf-8') as f:
            f.write(content)
        print("[OK] Updated sdk/cliproxy/service.go")
        return True
    else:
        print("[WARN] No changes to service.go")
        return False

def update_model_definitions():
    """Add Trae to model definitions"""
    path = 'internal/registry/model_definitions.go'
    with open(path, 'r', encoding='utf-8') as f:
        content = f.read()
    
    original = content
    
    # Add Trae to staticModelsJSON struct
    content = content.replace(
        'XAI         []*ModelInfo `json:"xai"`\n}',
        'XAI         []*ModelInfo `json:"xai"`\n\tTrae        []*ModelInfo `json:"trae"`\n}'
    )
    
    # Add GetTraeModels function
    content = content.replace(
        'func GetKimiModels() []*ModelInfo {\n\treturn cloneModelInfos(getModels().Kimi)\n}\n\n// GetAntigravityModels',
        'func GetKimiModels() []*ModelInfo {\n\treturn cloneModelInfos(getModels().Kimi)\n}\n\n// GetTraeModels returns the standard Trae model definitions.\nfunc GetTraeModels() []*ModelInfo {\n\treturn cloneModelInfos(getModels().Trae)\n}\n\n// GetAntigravityModels'
    )
    
    # Add trae case to GetStaticModelDefinitionsByChannel
    content = content.replace(
        '\tcase "qoder-cn":\n\t\treturn GetQoderCNModels()\n\tdefault:',
        '\tcase "qoder-cn":\n\t\treturn GetQoderCNModels()\n\tcase "trae":\n\t\treturn GetTraeModels()\n\tdefault:'
    )
    
    # Add data.Trae to LookupStaticModelInfo
    content = content.replace(
        '\t\tdata.Qoder,\n\t}\n\tfor _, models := range allModels',
        '\t\tdata.Qoder,\n\t\tdata.Trae,\n\t}\n\tfor _, models := range allModels'
    )
    
    if content != original:
        with open(path, 'w', encoding='utf-8') as f:
            f.write(content)
        print("[OK] Updated internal/registry/model_definitions.go")
        return True
    else:
        print("[WARN] No changes to model_definitions.go")
        return False

def main():
    print("Adding Trae OAuth2 support...")
    update_service_go()
    update_model_definitions()
    print("\nDone!")

if __name__ == '__main__':
    main()
