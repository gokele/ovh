package purchase

import (
	"context"
	"strings"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/types"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// 预售任务的全部判断集中在这里,PurchaseServer 只按阶段调用。
//
// 阶段(与 docs/SNIPING-REDESIGN.md §3 对应,只认官方接口):
//
//	Watching  账户车 GET /order/cart/{id}/eco 还没有该型号,或所选周期没有价格
//	Listed    基础商品与全部所选选项都有同币种、同周期报价(saleSnapshot.ready)
//	Built     购物车已建好:基础商品、dedicated_datacenter/dedicated_os/region、选项
//	Armed     购物车逐项核对周期与配置(orderterm.VerifyCart)后生成一次性结账请求
//	Submitted 结账登记成功后 POST /order/cart/{id}/checkout 只发一次
//
// 触发方式由任务决定:
//   - 开售即下(默认):Armed 且报价开放即结账,OVH 按官方排期交付。
//   - 有货再下(PreorderWaitStock):Armed 且报价开放后,还要等库存接口在目标机房
//     出现可下单状态;等待期间任务订阅公共库存探测,有货即唤醒。
type preorderStage string

const (
	preorderWatching  preorderStage = "waitingQuote"
	preorderBuilding  preorderStage = "waitingBase"
	preorderOptions   preorderStage = "waitingOptions"
	preorderWaitStock preorderStage = "waitingStock"
)

// preorderSaleBackoff 销售接口限流(Retry-After)期间不建车、不写购物车。
func preorderSaleBackoff(ctx context.Context, session *cartSession) (Outcome, bool) {
	signal, _ := ctx.Value(saleSignalKey{}).(saleSnapshot)
	until := signal.retryAt
	if session.sale.retryAt.After(until) {
		until = session.sale.retryAt
	}
	if until.After(time.Now()) {
		return Outcome{WaitingForCatalog: true, Reason: "销售接口等待重试", RetryAfter: time.Until(until)}, true
	}
	return Outcome{}, false
}

// preorderHardware 返回库存匹配用的 FQN 硬件段与下单用的选项码。
// 已核验的购物车直接复用它保存的映射,目录刷新不在结账路径上。
func preorderHardware(state *app.State, session *cartSession, item *types.QueueItem) (stock, order []string, out Outcome, wait bool) {
	if session.cart != nil {
		return session.cart.stockOptions, session.cart.orderOptions, Outcome{}, false
	}
	stock, order, err := catalog.ResolvePreorderOptions(state, item.AccountID, item.PlanCode, item.Options)
	if err != nil {
		state.Logger.Info("预订等待配置目录: "+err.Error(), "purchase")
		return nil, nil, Outcome{WaitingForCatalog: true, Reason: err.Error()}, true
	}
	return stock, order, Outcome{}, false
}

// preorderPinnedFQN 开售即下时,已核验完整配置的车可以跳过一次库存 RTT,直接以报价信号为检测时间。
// 有货再下必须读库存,所以永远返回空。
func preorderPinnedFQN(ctx context.Context, session *cartSession, item *types.QueueItem, client *ovhsdk.Client, intent string, stock []string) (string, time.Time) {
	if item.PreorderWaitStock {
		return "", time.Time{}
	}
	sale, ok := ctx.Value(saleSignalKey{}).(saleSnapshot)
	if !ok || !sale.fresh(session.account, item.PlanCode) {
		return "", time.Time{}
	}
	fqn := ""
	if session.cart != nil {
		fqn = session.cart.fqn
	} else if p := session.partial; p != nil && p.intent == intent && p.client == client && time.Now().Before(p.expires) {
		fqn = p.fqn
	}
	// 只有每个硬件段都明确且已核验的配置才能跳过库存读取。
	if len(stock) == 0 || len(strings.Split(fqn, "."))-1 != len(stock) || !preorderFQNCoversOptions(fqn, stock) {
		return "", time.Time{}
	}
	return fqn, sale.at
}

// preorderCandidates 只保留本型号自己的 FQN,不让同族其它型号的库存冒充。
func preorderCandidates(availabilities []map[string]interface{}, plan string) []map[string]interface{} {
	var out []map[string]interface{}
	for _, av := range availabilities {
		fqn, _ := av["fqn"].(string)
		if strings.EqualFold(strings.SplitN(fqn, ".", 2)[0], plan) {
			out = append(out, av)
		}
	}
	return out
}

// preorderConfirmSale 已备好的车在结账前确认账户报价仍然开放。
// 返回 (检测时间, 可以结账, 结果)。stockReady 是本轮库存接口在目标机房是否可下单。
func preorderConfirmSale(ctx context.Context, state *app.State, session *cartSession, cart *preparedCart, item *types.QueueItem, stockReady bool) (time.Time, bool, Outcome) {
	sale, out := preorderSaleQuote(ctx, session, cart, item)
	if out.WaitingForCatalog || out.Cancelled {
		if out.RetryImmediately {
			cart.cleanup()
			session.cart = nil
			session.nextPrepare = time.Time{}
			invalidateEcoOptions(state, item, session.account)
		}
		return time.Time{}, false, out
	}
	if item.PreorderWaitStock && !stockReady {
		// 报价已开放、车已备好,等库存出现。保留购物车,库存信号会直接唤醒任务。
		return time.Time{}, false, Outcome{WaitingForStock: true, Reason: "已开售，等待目标机房出现库存"}
	}
	return sale.at, true, Outcome{}
}
