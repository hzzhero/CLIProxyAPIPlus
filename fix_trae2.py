# -*- coding: utf-8 -*-
import io

p = 'sdk/auth/trae_cn.go'
with io.open(p, 'r', encoding='utf-8') as f:
    data = f.read()

T = chr(9)
old = (
    T + T + 'return &coreauth.Auth{\n'
    + T + T + T + 'ID:       fileName,\n'
    + T + T + T + 'Provider: a.Provider(),\n'
    + T + T + T + 'FileName: fileName,\n'
    + T + T + T + 'Storage:  storage,\n'
    + T + T + T + 'Metadata: metadata,\n'
    + T + T + '// The trae-cn provider is routed through the generic\n'
    + T + T + '// OpenAICompatExecutor, which resolves its upstream base URL and\n'
    + T + T + '// bearer token from the auth Attributes (base_url / api_key).\n'
    + T + T + '// If base_url is missing the executor aborts every request with\n'
    + T + T + '// "missing provider baseURL", so we must populate it here. The\n'
    + T + T + '// OpenAI-compatible gateway lives under the exchange origin at\n'
    + T + T + '// /v1 (e.g. https://api.trae.com.cn/v1). The bearer token is the\n'
    + T + T + '// OAuth access token; we also mirror it into the x-cloudide-token\n'
    + T + T + '// header for endpoints that accept it.\n'
    + T + T + 'compatBaseURL := strings.TrimRight(ex.UsedOrigin, "/") + "/v1"\n'
    + T + T + 'accessToken := strings.TrimSpace(ex.Resp.AccessToken)\n'
    + '\n'
    + T + T + 'Attributes: map[string]string{\n'
    + T + T + T + '"email":        info.Email,\n'
    + T + T + T + '"nickname":     info.Nickname,\n'
    + T + T + T + '"login_region": callback.LoginRegion,\n'
    + T + T + T + '"region_id":    regionID(endpoints, callback.LoginRegion, ex.UsedOrigin),\n'
    + T + T + T + '"base_url":     compatBaseURL,\n'
    + T + T + T + '"api_key":      accessToken,\n'
    + T + T + T + '"header:x-cloudide-token": accessToken,\n'
    + T + T + '},\n'
    + T + '}, nil\n'
    + '}\n'
)

assert old in data, 'OLD not found'

new = (
    T + T + '// The trae-cn provider is routed through the generic\n'
    + T + T + '// OpenAICompatExecutor, which resolves its upstream base URL and\n'
    + T + T + '// bearer token from the auth Attributes (base_url / api_key).\n'
    + T + T + '// If base_url is missing the executor aborts every request with\n'
    + T + T + '// "missing provider baseURL", so we must populate it here. The\n'
    + T + T + '// OpenAI-compatible gateway lives under the exchange origin at\n'
    + T + T + '// /v1 (e.g. https://api.trae.com.cn/v1). The bearer token is the\n'
    + T + T + '// OAuth access token; we also mirror it into the x-cloudide-token\n'
    + T + T + '// header for endpoints that accept it.\n'
    + T + T + 'compatBaseURL := strings.TrimRight(ex.UsedOrigin, "/") + "/v1"\n'
    + T + T + 'accessToken := strings.TrimSpace(ex.Resp.AccessToken)\n'
    + '\n'
    + T + T + 'return &coreauth.Auth{\n'
    + T + T + T + 'ID:       fileName,\n'
    + T + T + T + 'Provider: a.Provider(),\n'
    + T + T + T + 'FileName: fileName,\n'
    + T + T + T + 'Storage:  storage,\n'
    + T + T + T + 'Metadata: metadata,\n'
    + T + T + T + 'Attributes: map[string]string{\n'
    + T + T + T + T + '"email":        info.Email,\n'
    + T + T + T + T + '"nickname":     info.Nickname,\n'
    + T + T + T + T + '"login_region": callback.LoginRegion,\n'
    + T + T + T + T + '"region_id":    regionID(endpoints, callback.LoginRegion, ex.UsedOrigin),\n'
    + T + T + T + T + '"base_url":     compatBaseURL,\n'
    + T + T + T + T + '"api_key":      accessToken,\n'
    + T + T + T + T + '"header:x-cloudide-token": accessToken,\n'
    + T + T + T + '},\n'
    + T + T + '}, nil\n'
    + '}\n'
)

data = data.replace(old, new)
with io.open(p, 'w', encoding='utf-8') as f:
    f.write(data)
print('OK')
