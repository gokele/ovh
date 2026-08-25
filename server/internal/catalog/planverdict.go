package catalog

import (
	"errors"
	"fmt"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
)

// PlanVerdict 机型归属判定。
//
// 这份判定同时被三个地方用:入队闸门(挡住建不出来的任务)、抢购重试闸门(挡住
// 重试一万次也不会变的任务)、以及给用户看的诊断文案。以前它在 handlers/queue.go
// 和 purchase/purchase.go 各有一份**逐字复制**的实现,已经开始分裂(日志前缀不同)——
// 两处口径一旦真的分开,就会出现"能入队但每轮都被判死"的幽灵任务。
//
// 判据说明:eco 目录只回答"本工具能不能用 /order/cart/{id}/eco 下单它",
// **不回答"它属于哪个大区"**。实测公开接口(2026-08):EU 站点 availabilities 有
// 244 个 planCode、eco 目录只有 99 个,145 个差集里整条 Scale / HCI / SDS /
// High-Grade 产品线都在;US 站点是 423 对 143。所以归属必须靠可用性接口探。
type PlanVerdict int

const (
	PlanVerdictOK          PlanVerdict = iota // 属于本区,而且本工具下得了单
	PlanVerdictUnknown                        // 探测/目录失败 —— 不下结论,保持"无货"语义继续重试
	PlanVerdictCrossRegion                    // ① 真跨区:别的大区才有它的库存记录
	PlanVerdictNotEco                         // ② 本区有它,但不属于 Eco 系列,本工具下不了
	PlanVerdictNoSuchPlan                     // ③ 三区都查不到:planCode 拼错 / 已下架
)

// ClassifyPlan 判定 planCode 对这个账户属于哪种情况,并给出面向用户的说明。
// 说明为空 = 无需提示(OK / Unknown),调用方必须保持原来的"无货,下轮再来"语义。
//
// 顺序有讲究:先查本子公司的 eco 目录(2 小时内存缓存,不耗账户配额、命中时不发网络请求),
// 有就直接放行 —— 抢购主链路上正常任务一次探测都不会多发。只有"目录里没有"才去探大区归属。
//
// logSource 只影响日志归类(queue / purchase),不影响判定。
func ClassifyPlan(state *app.State, accountID, planCode, logSource string) (PlanVerdict, string) {
	acc, _ := state.FindAccount(accountID)
	accRegion := ovh.EndpointRegion(acc.Endpoint)
	subsidiary := SubsidiaryOfAccount(acc)

	_, catErr := AddonFamiliesForPlan(state, accountID, planCode)
	if catErr == nil {
		// 本子公司的 Eco 目录里就有它,能下单,不用再探
		return PlanVerdictOK, ""
	}
	if !errors.Is(catErr, ErrPlanNotInCatalog) {
		// 目录拉不动(网络 / 429):不下结论。这里下结论 = 一次瞬断把正常任务判死。
		state.Logger.Warn(fmt.Sprintf("判定 %s 归属时目录拉取失败(子公司 %s)，本次不下结论: %s",
			planCode, subsidiary, catErr.Error()), logSource)
		return PlanVerdictUnknown, ""
	}

	// 本区排第一:命中就立刻返回,只花一次公开请求
	region, probeErr := RegionOfPlan(state, planCode, []string{accRegion, "EU", "US", "CA"})
	if probeErr != nil {
		state.Logger.Warn(fmt.Sprintf("探测 %s 的大区归属失败，本次不下结论: %s",
			planCode, probeErr.Error()), logSource)
		return PlanVerdictUnknown, ""
	}

	switch {
	case region == "":
		// ③ 三区的可用性接口都没有这个 planCode 的任何记录(哪怕缺货也会有记录)
		return PlanVerdictNoSuchPlan, fmt.Sprintf(
			"机型 %s 在 OVH 的 EU / US / CA 三个站点都查不到任何库存记录（缺货的机型也会有记录）："+
				"planCode 可能拼错了，或者这个机型已经下架。",
			planCode)
	case region != accRegion:
		// ① 真跨区:别的大区有它的库存记录
		return PlanVerdictCrossRegion, fmt.Sprintf(
			"机型 %s 属于 OVH 的 %s 区，而账户 %s 在 %s 区（%s）。三个站点的目录 / 库存 / 购物车互不相通"+
				"（美区机型带 -us / -eu / -ca 后缀，欧区和加区不带），请改用本区的 planCode，或换一个 %s 区的账户下单。",
			planCode, region, acc.Name, accRegion, acc.Endpoint, region)
	}

	// ② 本区有货但不走 Eco 下单链路
	return PlanVerdictNotEco, fmt.Sprintf(
		"机型 %s 在本区（%s 区，子公司 %s）确实有库存记录，但它不在该子公司的 Eco 目录里 —— "+
			"Scale / HCI / SDS / High-Grade 这些产品线都不走 Eco（实测欧区 244 个有库存的机型里 145 个如此）。"+
			"本工具的下单链路只有 /order/cart/eco 一条，买不了这台，请到 OVH 官网下单。",
		planCode, accRegion, subsidiary)
}
