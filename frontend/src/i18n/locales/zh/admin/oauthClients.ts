export default {
  oauthClients: {
    title: 'OAuth 授权应用',
    description: '管理允许通过中转站 OAuth 登录的第三方应用',
    pageHint: '管理允许通过中转站 OAuth 授权登录的第三方应用。停用后该应用立即无法发起新授权。',
    create: '新增应用',
    createFirst: '还没有任何授权应用',
    edit: '编辑应用',
    delete: '删除应用',
    deleteConfirm: '删除后该应用立即无法发起授权登录，确定删除？',
    enabled: '已启用',
    disabled: '已停用',
    enable: '启用',
    disable: '停用',
    allowLocalhost: '本地回调',
    secretIssued: '请立即保存密钥（仅此一次展示，关闭后无法再查看）：',
    flags: '状态',
    actions: '操作',
    fields: {
      name: '应用名称',
      clientId: 'Client ID',
      clientIdHint: '创建后不可修改',
      secret: '密钥',
      redirectUris: '回调地址白名单',
      redirectUrisHint: '每行一个，精确匹配；换行或逗号分隔',
      allowLocalhost: '放行 localhost 任意端口回调',
      allowLocalhostHint: '仅本地开发场景使用，生产接入请登记正式回调地址',
      regenerateSecret: '重新生成密钥',
      regenerateSecretHint: '保存后旧密钥立即失效，新密钥仅展示一次',
      remark: '备注',
      updatedAt: '更新时间'
    }
  }
}
