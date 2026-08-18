import re

# 1. Fix sdk/auth/filestore.go
print("Fixing filestore.go...")
with open('sdk/auth/filestore.go', 'r', encoding='utf-8') as f:
    content = f.read()

# Add import
if 'traecn' not in content:
    content = content.replace(
        'qoderauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/qoder"',
        'qoderauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/qoder"\n\ttraecn "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/traecn"'
    )

# Add trae-cn case after qoder-cn case
old_block = '''\tif provider == "qoder" || provider == "qoder-cn" {
\t\tvar storage qoderauth.QoderTokenStorage
\t\tif raw, errMarshal := json.Marshal(metadata); errMarshal == nil {
\t\t\tif errUnmarshal := json.Unmarshal(raw, &storage); errUnmarshal == nil {
\t\t\t\tif strings.TrimSpace(storage.Type) == "" {
\t\t\t\t\tstorage.Type = provider
\t\t\t\t}
\t\t\t\tauth.Storage = &storage
\t\t\t}
\t\t}
\t}'''

new_block = '''\tif provider == "qoder" || provider == "qoder-cn" {
\t\tvar storage qoderauth.QoderTokenStorage
\t\tif raw, errMarshal := json.Marshal(metadata); errMarshal == nil {
\t\t\tif errUnmarshal := json.Unmarshal(raw, &storage); errUnmarshal == nil {
\t\t\t\tif strings.TrimSpace(storage.Type) == "" {
\t\t\t\t\tstorage.Type = provider
\t\t\t\t}
\t\t\t\tauth.Storage = &storage
\t\t\t}
\t\t}
\t} else if provider == "trae-cn" {
\t\tvar storage traecn.TraeCNTokenStorage
\t\tif raw, errMarshal := json.Marshal(metadata); errMarshal == nil {
\t\t\tif errUnmarshal := json.Unmarshal(raw, &storage); errUnmarshal == nil {
\t\t\t\tif strings.TrimSpace(storage.Type) == "" {
\t\t\t\t\tstorage.Type = provider
\t\t\t\t}
\t\t\t\tauth.Storage = &storage
\t\t\t}
\t\t}
\t}'''

if old_block in content and new_block not in content:
    content = content.replace(old_block, new_block)
    print("  - Added trae-cn case")
else:
    print("  - Case already exists or pattern not found")

with open('sdk/auth/filestore.go', 'w', encoding='utf-8') as f:
    f.write(content)

# 2. Fix internal/watcher/synthesizer/file.go
print("Fixing synthesizer/file.go...")
with open('internal/watcher/synthesizer/file.go', 'r', encoding='utf-8') as f:
    content = f.read()

if 'traecn' not in content:
    content = content.replace(
        'qoderauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/qoder"',
        'qoderauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/qoder"\n\ttraecn "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/traecn"'
    )

if old_block in content and new_block not in content:
    content = content.replace(old_block, new_block)
    print("  - Added trae-cn case")
else:
    print("  - Case already exists or pattern not found")

with open('internal/watcher/synthesizer/file.go', 'w', encoding='utf-8') as f:
    f.write(content)

# 3. Fix internal/registry/model_definitions.go
print("Fixing model_definitions.go...")
with open('internal/registry/model_definitions.go', 'r', encoding='utf-8') as f:
    content = f.read()

if 'GetTraeCNModels' not in content:
    # Add case in switch
    content = content.replace(
        '\tcase "qoder-cn":\n\t\treturn GetQoderCNModels()',
        '\tcase "qoder-cn":\n\t\treturn GetQoderCNModels()\n\tcase "trae-cn":\n\t\treturn GetTraeCNModels()'
    )
    # Add function at end
    content += '\n\n// GetTraeCNModels returns the static fallback model list for trae-cn.\n'
    content += '// The authoritative list is fetched dynamically via executor.FetchTraeCNModels;\n'
    content += '// this fallback only applies when the upstream model_list call fails.\n'
    content += 'func GetTraeCNModels() []*ModelInfo {\n'
    content += '\treturn []*ModelInfo{}\n'
    content += '}\n'
    print("  - Added GetTraeCNModels")
else:
    print("  - Already has GetTraeCNModels")

with open('internal/registry/model_definitions.go', 'w', encoding='utf-8') as f:
    f.write(content)

# 4. Fix sdk/cliproxy/service.go
print("Fixing service.go...")
with open('sdk/cliproxy/service.go', 'r', encoding='utf-8') as f:
    content = f.read()

# Add executor registration
if 'NewTraeCNExecutor' not in content:
    content = content.replace(
        '\t\tcase "qoder-cn":\n\t\t\ts.coreManager.RegisterExecutor(executor.NewQoderCNExecutor(s.cfg))',
        '\t\tcase "qoder-cn":\n\t\t\ts.coreManager.RegisterExecutor(executor.NewQoderCNExecutor(s.cfg))\n\t\tcase "trae-cn":\n\t\t\ts.coreManager.RegisterExecutor(executor.NewTraeCNExecutor(s.cfg))'
    )
    print("  - Added executor registration")
else:
    print("  - Executor registration already exists")

# Add model parsing
if 'FetchTraeCNModels' not in content:
    content = content.replace(
        '\t\tcase "qoder-cn":\n\t\t\tmodels = executor.FetchQoderCNModels(ctx, auth, s.cfg)\n\t\t\tmodels = applyExcludedModels(models, excluded)',
        '\t\tcase "qoder-cn":\n\t\t\tmodels = executor.FetchQoderCNModels(ctx, auth, s.cfg)\n\t\t\tmodels = applyExcludedModels(models, excluded)\n\t\tcase "trae-cn":\n\t\t\tmodels = executor.FetchTraeCNModels(ctx, auth, s.cfg)\n\t\t\tmodels = applyExcludedModels(models, excluded)'
    )
    print("  - Added model parsing")
else:
    print("  - Model parsing already exists")

with open('sdk/cliproxy/service.go', 'w', encoding='utf-8') as f:
    f.write(content)

print("\nAll fixes applied!")
