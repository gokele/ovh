/**
 * VPS 控制台语言包(中文,文案唯一事实来源):
 * 覆盖 VpsReinstallDialog / VpsTasksDialog / VpsSnapshotPane / VpsMitigationPane。
 *
 * 命名空间 vps.*:reinstall / tasks / snapshot / mitigation。
 * 由主语言包(locales/zh.ts)聚合后生效;英文对照见 vps-dialogs.en.ts。
 *
 * 语义约束:所有"读失败 ≠ 没有"类警示(快照读失败 ≠ 没有快照、缓解信息读失败 ≠ 无 IP)
 * 是这批面板的核心防误导文案,翻译时整句语义必须完整保留,不许简化成普通报错。
 * tasks.state / tasks.type / mitigation.state 的值是 OVH 枚举翻译表,
 * 组件里存的是 key(如 vps.tasks.state.done),渲染处统一走 t()。
 */
export const vpsDialogsZh = {
  vps: {
    reinstall: {
      title: "重装系统",
      currentOs: "当前系统",
      wipeTitle: "数据将被完全清除",
      wipeDesc: "VPS 当前磁盘数据无法恢复(除非有快照)。建议先创建快照。",
      tplLabel: "系统模板",
      loadingTemplates: "加载模板中…",
      tplLoadFailed: "模板列表读取失败:",
      tplEmpty: "暂无可用模板(账户/区域可能无系统模板)",
      tplPlaceholder: "选择 OS 模板",
      partialWhat: "系统模板",
      tplId: "模板 ID: {{id}}",
      tplLocale: "默认语言: {{locale}}",
      tplLangs: "支持 {{n}} 种语言",
      sshLabel: "SSH 公钥(可选)",
      sshPlaceholder: "OVH SSH key 名称,多个用逗号分隔(从 /me/sshKey 拿)",
      sshHint: "填了 SSH key 可勾选下面「不发送密码邮件」,装机后直接用 key 登录",
      noPwdMail: "不发送初始密码邮件(用 SSH key 登录)",
      confirmNameLabel: "输入 VPS 名 <code>{{name}}</code> 确认:",
      submitting: "提交中…",
      submitConfirm: "确认重装",
      toast: {
        selectTemplate: "请选择系统模板",
        nameMismatch: "VPS 名称不匹配,无法确认",
        submitted: "重装任务已提交,通常 5-10 分钟完成",
      },
    },
    tasks: {
      title: "任务历史",
      desc: "最近 10 个任务(重启 / 装系统 / 快照 / 改密 等)+ 实时状态",
      loadFailed: "任务历史读取失败",
      empty: "暂无任务历史",
      /** OVH vps.TaskStateEnum: blocked / cancelled / doing / done / error / paused / todo / waitingAck */
      state: {
        blocked: "已阻塞",
        cancelled: "已取消",
        doing: "进行中",
        done: "完成",
        error: "失败",
        paused: "已暂停",
        todo: "排队中",
        waitingack: "待确认",
      },
      /** OVH vps.TaskTypeEnum 全集 —— 注意 OVH 命名大多带 Vm 后缀(rebootVm 不是 reboot) */
      type: {
        addVeeamBackupJob: "添加 Veeam 备份",
        changeRootPassword: "重置 root 密码",
        createSnapshot: "创建快照",
        deleteSnapshot: "删除快照",
        deliverVm: "交付 VM",
        getConsoleUrl: "生成控制台链接",
        internalTask: "内部任务",
        migrate: "迁移",
        openConsoleAccess: "打开控制台",
        provisioningAdditionalIp: "分配额外 IP",
        reOpenVm: "重新开机",
        rebootVm: "重启",
        reinstallVm: "重装系统",
        removeVeeamBackup: "移除 Veeam 备份",
        rescheduleAutoBackup: "调整自动备份",
        restoreFullVeeamBackup: "Veeam 完整还原",
        restoreVeeamBackup: "Veeam 还原",
        restoreVm: "还原 VM",
        revertSnapshot: "回滚快照",
        setMonitoring: "设置监控",
        setNetboot: "设置网络启动",
        startVm: "启动",
        stopVm: "关机",
        upgradeVm: "升级 VM",
      },
    },
    snapshot: {
      loadFailed: "快照信息读取失败",
      empty: "暂无快照",
      emptyDesc:
        "OVH 免费档每台 VPS 同时只能存 1 个快照。改大动作前先做一个,出问题能 1 分钟回滚",
      createTitle: "创建快照",
      createDesc: "OVH 会暂停 VPS 30 秒-3 分钟做快照,期间网络中断",
      createPlaceholder: "描述(可选),如 装 nginx 前",
      creating: "创建中…",
      createBtn: "创建快照",
      currentTitle: "当前快照",
      creationDate: "创建时间: {{time}}",
      region: "区域: {{region}}",
      editDescBtn: "改描述",
      revertBtn: "回滚到此快照",
      deleteBtn: "删除快照",
      singleWarn: "免费档单 VPS 只能存 1 个快照。要做新快照得先删旧的。",
      editTitle: "修改快照描述",
      descPlaceholder: "给快照一段描述,方便日后辨识",
      saving: "保存中…",
      revertTitle: "回滚到此快照?",
      revertDesc: "这个操作不可逆",
      revertWarnTitle: "快照之后所有改动会丢失:",
      revertItemFs: "文件系统回到 {{time}} 那一刻",
      revertItemReboot: "VPS 会自动重启,期间几分钟无法访问",
      revertItemMeta: "IP / 密码 等元数据不变",
      confirmNameLabel: "请输入 VPS 名称 <code>{{name}}</code> 确认:",
      reverting: "回滚中…",
      revertConfirmBtn: "确认回滚",
      deleteConfirm: "确认删除当前快照?快照本身会删除,VPS 当前状态不受影响。",
      toast: {
        created: "快照创建任务已提交,通常 1-3 分钟完成",
        nameMismatch: "VPS 名称不匹配",
        reverted: "回滚任务已提交,VPS 即将进入维护态",
        deleted: "快照已删除",
        descUpdated: "快照描述已更新",
      },
    },
    mitigation: {
      loadFailed: "DDoS 缓解信息读取失败",
      noIp: "该 VPS 无 IP",
      intro:
        "OVH 自动缓解(auto)默认开启,检测到攻击时自动启用。下面是手动启用「永久缓解」的开关 — 开启后 VPS 所有流量长期过 Anti-DDoS 设备(延迟略增,持续防护)。",
      ipv6Note: "仅支持 IPv4。IPv6 走 OVH 网络层默认免疫,无需手动配置。",
      v6NotApplicable: "IPv6 不适用 anti-DDoS Mitigation(OVH 网络层免疫)",
      noPermanent: "无永久缓解,自动缓解备用中",
      enableBtn: "启用永久缓解",
      /** OVH MitigationStateEnum: creationPending / ok / removalPending(只有 ok 是稳定态) */
      state: {
        ok: "已生效",
        creationPending: "应用中",
        removalPending: "移除中",
      },
      autoTag: "自动",
      permanentTag: "永久",
      creatingTitle: "正在启用中,通常 30 秒-2 分钟,等状态变 ok 再点关闭",
      removingTitle: "正在移除中,稍后会自动从列表消失",
      applying: "应用中…",
      removing: "移除中…",
      disableBtn: "关闭永久",
      toast: {
        enabled: "已启用永久 DDoS 缓解",
        disabled: "已关闭永久 DDoS 缓解",
        stateNotOk:
          "当前 mitigation 状态不允许关闭(可能正在被自动启用或攻击中)。等状态变 ok 再试",
        ipv4Only: "OVH anti-DDoS 只支持 IPv4。IPv6 默认有网络层防护,无需手动配置",
        failed: "操作失败",
      },
    },
  },
};

export type VpsDialogsPack = typeof vpsDialogsZh;
