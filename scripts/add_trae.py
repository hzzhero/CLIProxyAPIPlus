#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Script to add Trae provider support to CLIProxyAPIPlus"""
import os
import re

def update_service_go():
    """Add trae cases to service.go"""
    path = 'sdk/cliproxy/service.go'
    if not os.path.exists(path):
        print(f"File not found: {path}")
        return False
    
    with open(path, 'r', encoding='utf-8') as f:
        content = f.read()
    
    original = content
    
    # Add trae executor after kimi executor case
    executor_pattern = r'(case "kimi":\n\t\ts\.coreManager\.RegisterExecutor\(executor\.NewKimiExecutor\(s\.cfg\)\))\n\t(case "xai")'
    executor_replacement = r'\1\n\tcase "trae":\n\t\ts.coreManager.RegisterExecutor(executor.NewTraeExecutor(s.cfg))\n\t\2'
    content = re.sub(executor_pattern, executor_replacement, content)
    
    # Add trae models after kimi models
    models_pattern = r'(case "kimi":\n\t\tmodels = registry\.GetKimiModels\(\)\n\t\tmodels = applyExcludedModels\(models, excluded\))\n\t(case "xai")'
    models_replacement = r'\1\n\tcase "trae":\n\t\tmodels = executor.FetchTraeModels(context.Background(), a, s.cfg)\n\t\tmodels = applyExcludedModels(models, excluded)\n\t\2'
    content = re.sub(models_pattern, models_replacement, content)
    
    if content != original:
        with open(path, 'w', encoding='utf-8') as f:
            f.write(content)
        print("[OK] Updated sdk/cliproxy/service.go with trae support")
        return True
    else:
        print("[WARN] No changes made to service.go - patterns not found")
        return False

def update_model_definitions():
    """Add Trae to model definitions"""
    path = 'internal/registry/model_definitions.go'
    if not os.path.exists(path):
        print(f"File not found: {path}")
        return False
    
    with open(path, 'r', encoding='utf-8') as f:
        content = f.read()
    
    original = content
    
    # Add Trae to staticModelsJSON struct
    struct_pattern = r'(XAI\s+\[\]\*ModelInfo `json:"xai"`)\n}'
    struct_replacement = r'\1\n\tTrae        []*ModelInfo `json:"trae"`\n}'
    content = re.sub(struct_pattern, struct_replacement, content)
    
    # Add GetTraeModels function after GetKimiModels
    get_models_pattern = r'(func GetKimiModels\(\) \[\]\*ModelInfo \{\n\treturn cloneModelInfos\(getModels\(\)\.Kimi\)\n\})\n\n//'
    get_models_replacement = r'''\1

// GetTraeModels returns the standard Trae model definitions.
func GetTraeModels() []*ModelInfo {
	return cloneModelInfos(getModels().Trae)
}

//'''
    content = re.sub(get_models_pattern, get_models_replacement, content)
    
    # Add trae case to GetStaticModelDefinitionsByChannel
    channel_pattern = r'(case "qoder-cn":\n\t\treturn GetQoderCNModels\(\)\n\tdefault:\n\t\treturn nil\n\t\})'
    channel_replacement = r'case "qoder-cn":\n\t\treturn GetQoderCNModels()\n\tcase "trae":\n\t\treturn GetTraeModels()\n\tdefault:\n\t\treturn nil\n\t}'
    content = re.sub(channel_pattern, channel_replacement, content)
    
    # Add data.Trae to LookupStaticModelInfo
    lookup_pattern = r'(data\.Qoder,\n\t\})\n\tfor _, models := range allModels'
    lookup_replacement = r'data.Qoder,\n\t\tdata.Trae,\n\t}\n\tfor _, models := range allModels'
    content = re.sub(lookup_pattern, lookup_replacement, content)
    
    if content != original:
        with open(path, 'w', encoding='utf-8') as f:
            f.write(content)
        print("[OK] Updated internal/registry/model_definitions.go with trae support")
        return True
    else:
        print("[WARN] No changes made to model_definitions.go")
        return False

def main():
    print("Adding Trae OAuth2 support to CLIProxyAPIPlus...")
    print("=" * 50)
    
    success = True
    success = update_service_go() and success
    success = update_model_definitions() and success
    
    print("=" * 50)
    if success:
        print("\n[OK] All updates completed successfully!")
        print("\nNext steps:")
        print("1. Build the project: go build ./...")
        print("2. Test login: ./server --trae-login")
    else:
        print("\n[WARN] Some updates failed. Check the messages above.")
        print("You may need to manually verify the patterns match.")

if __name__ == '__main__':
    main()
