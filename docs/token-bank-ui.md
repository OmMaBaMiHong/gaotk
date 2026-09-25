# Token 银行界面规范

C 端保留 Token 银行菜单，用当前用户的原账号列表组合页面；原 CreateAccountModal、ImportDataModal、KimiOAuthModal、EditAccountModal、ReAuthAccountModal、AccountStatsModal、AccountUsageCell 和 UsageTable 共用，不维护第二套授权和账号创建逻辑。

后台在原账号页展示所属用户并可筛选；原渠道表单增加 Token 储蓄开关、用户分成百分比、收款管理员及接收分组。删除独立后台银行页面，旧地址跳转原账号页。

C 端可添加、导入、重新授权、编辑自己的授权资料、暂停/恢复、删除，查看原账号用量/成本统计、使用记录及储蓄收益。分组、渠道、定价、代理、自定义中转端点与调度参数由后台管理。Antigravity 原 OAuth 保留，自定义中转类型仅在后台使用。

沿用原组件与视觉样式、币种和响应式布局。接口由显式账号工作区选择 admin/user，默认后台行为不变；使用记录隐藏调用方身份和密钥信息。
