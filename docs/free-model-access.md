# 焚诀免费模型入口

免费分组当前只提供 **Step 3.7 Flash 免费版**，模型 ID 为 `stepfun/step-3.7-flash:free`。免费组用户模型调用实际扣费为零；免费模型的供给和限流由上游决定，不承诺永久可用或无限使用。

模型菜单来自用户 Key 所在分组的授权目录。原有随机模型及其他免费模型已从免费组移除；已有用户刷新模型列表并重新选择该模型，不自动改写用户的默认模型。

中转站主机通过 `sub2api-free-model.timer` 每十分钟运行 `scripts/free_model_health.py`，核对 Kilo 官方模型目录的零价 / 工具支持并进行极短真实调用。模型下架、转收费、网络失败、限流或没有可交付正文时暂停免费组；检测恢复后重新开放。检测存在最多十分钟的周期延迟，过程中不会自动切到付费模型或其他模型。

此策略只管理 `openskoob-free`（group 16），付费月卡、用户自配模型及共享上游账号映射保持原配置。手动停用时先执行 `systemctl disable --now sub2api-free-model.timer`，再在管理台停用分组。

运维查看：`systemctl list-timers sub2api-free-model.timer` 和 `journalctl -u sub2api-free-model.service`。日志只记录模型 ID、健康状态与错误分类，不记录密钥或用户内容。
