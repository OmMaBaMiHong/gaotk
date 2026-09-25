import { apiClient } from './client'
import type { AxiosInstance } from 'axios'

export type AccountScope = 'admin' | 'user'

// Scope belongs to the account workspace, never to the global HTTP client or route.
export function createAccountScopeClient(scope: AccountScope): Pick<AxiosInstance, 'get' | 'post' | 'put' | 'patch' | 'delete'> {
  const path = (url: string) => scope === 'user' ? url.replace(/^\/admin\//, '/user/') : url
  return {
    get: (url, config) => apiClient.get(path(url), config),
    post: (url, data, config) => apiClient.post(path(url), data, config),
    put: (url, data, config) => apiClient.put(path(url), data, config),
    patch: (url, data, config) => apiClient.patch(path(url), data, config),
    delete: (url, config) => apiClient.delete(path(url), config)
  }
}
