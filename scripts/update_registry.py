#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Script to update model definitions for Trae support"""
import re

def update_model_definitions():
    """Add Trae to model definitions"""
    path = 'internal/registry/model_definitions.go'
    
    with open(path, 'r', encoding='utf-8') as f:
        content = f.read()
    
    original = content
    
    # Add Trae to staticModelsJSON struct (after XAI)
    struct_pattern = r'(XAI\s+\[\]\*ModelInfo `json:"xai"`)\n}'
    struct_replacement = r'\1\n\tTrae        []*ModelInfo `json:"trae"`'
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
    
    # Add trae case to GetStaticModelDefinitionsByChannel (before default)
    channel_pattern = r'(case "qoder-cn":\n\t\treturn GetQoderCNModels\(\)\n)(\tdefault:)'
    channel_replacement = r'\1\tcase "trae":\n\t\treturn GetTraeModels()\n\2'
    content = re.sub(channel_pattern, channel_replacement, content)
    
    # Add data.Trae to LookupStaticModelInfo (after data.Qoder)
    lookup_pattern = r'(data\.Qoder,\n\t\})'
    lookup_replacement = r'data.Qoder,\n\t\tdata.Trae,\n\t}'
    content = re.sub(lookup_pattern, lookup_replacement, content)
    
    if content != original:
        with open(path, 'w', encoding='utf-8') as f:
            f.write(content)
        print("[OK] Updated internal/registry/model_definitions.go")
        return True
    else:
        print("[WARN] No changes made - patterns not found or already applied")
        return False

if __name__ == '__main__':
    update_model_definitions()
