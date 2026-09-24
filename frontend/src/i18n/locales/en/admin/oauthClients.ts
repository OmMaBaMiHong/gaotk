export default {
  oauthClients: {
    title: 'OAuth Clients',
    description: 'Manage third-party apps allowed to authenticate via the relay OAuth server',
    pageHint: 'Manage third-party apps allowed to authenticate via the relay OAuth server. Disabled apps cannot start new authorizations.',
    create: 'New App',
    createFirst: 'No OAuth clients yet',
    edit: 'Edit App',
    delete: 'Delete App',
    deleteConfirm: 'The app will be unable to start new authorizations immediately. Delete?',
    enabled: 'Enabled',
    disabled: 'Disabled',
    enable: 'Enable',
    disable: 'Disable',
    allowLocalhost: 'Localhost',
    secretIssued: 'Save this secret now — it is shown only once:',
    flags: 'Status',
    actions: 'Actions',
    fields: {
      name: 'App Name',
      clientId: 'Client ID',
      clientIdHint: 'Immutable after creation',
      secret: 'Secret',
      redirectUris: 'Redirect URI Allowlist',
      redirectUrisHint: 'One per line, exact match; separated by newline or comma',
      allowLocalhost: 'Allow localhost callbacks on any port',
      allowLocalhostHint: 'Local development only; register production callback URIs instead',
      regenerateSecret: 'Regenerate secret',
      regenerateSecretHint: 'Old secret is revoked on save; the new one is shown once',
      remark: 'Remark',
      updatedAt: 'Updated At'
    }
  }
}
