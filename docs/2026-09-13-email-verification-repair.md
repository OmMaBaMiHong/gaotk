# 注册设置保存错误排查

## 已修复

2026-09-13 生产管理设置保存返回 `DEFAULT_SUBSCRIPTION_GROUP_INVALID`，metadata 的 `group_id=4`。实际问题是 `auth_source_default_wechat_subscriptions` 引用已软删除的分组 4；并非要求创建一个名为 default 的分组。

经用户授权，使用已有服务端 Admin API Key 调用管理设置接口，清空这条无效引用，返回 200。配置快照保存在服务器 root-only 目录 `/opt/sub2api/backups/20260913-email-verification`。比较前后设置，最终仅该微信订阅字段改变；邮箱赠送 group 5 / 1 天、现有订阅、SMTP 和其他原有设置保留。

本次接口保存会顺带把未设置项物化为默认值，并将两个支付展示开关从空串规范化成 false。已从快照恢复本次附带规范化，逐键确认只保留目标修复，没有改变支付配置。

## 邮箱验证码尚未启用

用户明确要求启用邮箱验证码。当前 `registration_enabled=true`，`email_verify_enabled=false`。现有 smtp.gmail.com:587 可以连接，但测试接口认证返回 Gmail 535 / Username and Password not accepted。现有密码含格式空格，去除空格后用临时测试请求再认证仍失败，未改写密码。

等待用户在后台更新有效 SMTP 凭据；未开启一个无法发送验证码的注册流程。后续先调用管理设置 `/test-smtp` 确认连接与认证成功，再保存 `email_verify_enabled=true` 并核对所有非目标字段没有附带变化。没有发送测试邮件，也没有声称已完成邮件投递验证。

## 后台配置位置

系统设置 → 用户默认设置 → 按注册/绑定来源配置 → 邮箱：现有默认订阅为 codex-日卡体验（group 5），有效期 1 天。赠送订阅只能选择未删除的 subscription 类型分组；不需要为解决本次报错新建分组。
